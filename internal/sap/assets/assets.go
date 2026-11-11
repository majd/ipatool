package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/majd/ipatool/v2/internal/sap/machimage"
)

const (
	macOSVersion              = "27.0.1"
	macOSBuild                = "26A434"
	macOSRestoreURL           = "https://updates.cdn-apple.com/2026FallFCS/59241290-5d51-4ca8-9df4-31624b9a4eac/UniversalMac_27.0.1_26A434_Restore.ipsw"
	assetCacheDirectory       = "apple-assets-macos-27-26A434-arm64-v5"
	legacyAssetCacheDirectory = "apple-assets-macos-27-26A434-arm64-v4"
)

type Bundle struct {
	AppleMediaServices []byte
	Commerce           []byte
	ADID               []byte
	CoreFP             []byte
	CoreFPICXS         []byte
	FairPlayD          []byte
	FairPlayKext       []byte
}

type fileSpec struct {
	name   string
	size   int
	digest [32]byte
}

var requiredFiles = []fileSpec{
	{name: "AppleMediaServices", size: 14813888, digest: mustDigest("95f4558aa2c0ccd9f96ed479e336b630b46bf4b4e0baabc1c1ac8c7d55e21ab4")},
	{name: "commerce", size: 2735088, digest: mustDigest("c4b2088dd39aeb353f59cb57597902e8317aec66ae7da0414b03c5f932771343")},
	{name: "adid", size: 3047808, digest: mustDigest("43419a206c28bc93ecb8884256088d18306a0a462350f83adbced8e60a94e390")},
	{name: "CoreFP", size: 30764976, digest: mustDigest("afbc123e688a822d980d5816cc2b52f69fde669d1dca92cf85627e1cbee587f6")},
	{name: "CoreFP.icxs", size: 7365344, digest: mustDigest("cb8f55330ec567da3e692dab5f5388bed0312dd9093340477c5759078cc2aa7b")},
	{name: "fairplayd", size: 17389744, digest: mustDigest("1e58d37e9602dd0ca5e24b14762063b6c310248b4d3a94272771d815e4ea1784")},
	{name: "FairPlayIOKit", size: 2132320, digest: mustDigest("47452960ec010d3a80b02cceec73a65f8a9758850fd0fb7b2fdac7a5d1b81025")},
}

var legacyFiles = []fileSpec{
	{
		name:   "AppleMediaServices",
		size:   14813888,
		digest: mustDigest("95f4558aa2c0ccd9f96ed479e336b630b46bf4b4e0baabc1c1ac8c7d55e21ab4"),
	},
	{
		name:   "commerce",
		size:   8092640,
		digest: mustDigest("76de40a4a32f691947cb926ce3b128d2fc446f7cac9755f831c406cf5a316ac9"),
	},
	{
		name:   "adid",
		size:   9797952,
		digest: mustDigest("1fec225b6917206e3d9b00fe636c04878f82bfd06a87a4b6941711de60efab5b"),
	},
	{
		name:   "CoreFP",
		size:   94842464,
		digest: mustDigest("2c30fa2b5f695fd66c0360dce72f54757d03ddb7107549715c12cb359a003b46"),
	},
	{
		name:   "CoreFP.icxs",
		size:   7365344,
		digest: mustDigest("cb8f55330ec567da3e692dab5f5388bed0312dd9093340477c5759078cc2aa7b"),
	},
	{
		name:   "fairplayd",
		size:   37689264,
		digest: mustDigest("28b4c7b2a5a0409c5020da55fc3984713a8e2b0eec8557076c8717f7166d003e"),
	},
	{
		name:   "FairPlayIOKit",
		size:   2132320,
		digest: mustDigest("47452960ec010d3a80b02cceec73a65f8a9758850fd0fb7b2fdac7a5d1b81025"),
	},
}

type bundleLoader struct {
	gate      chan struct{}
	installed *Bundle
	resolve   func(context.Context) (Bundle, error)
}

var defaultLoader = bundleLoader{gate: make(chan struct{}, 1), resolve: loadBundle}

// Load resolves and verifies the pinned macOS framework profile once per process.
// The returned slices are shared and must not be modified by callers.
func Load(ctx context.Context) (Bundle, error) {
	return defaultLoader.load(ctx)
}

func (l *bundleLoader) load(ctx context.Context) (Bundle, error) {
	if ctx == nil {
		return Bundle{}, errors.New("apple asset context is nil")
	}

	if err := ctx.Err(); err != nil {
		return Bundle{}, fmt.Errorf("load Apple assets: %w", err)
	}

	select {
	case l.gate <- struct{}{}:
	case <-ctx.Done():
		return Bundle{}, fmt.Errorf("load Apple assets: %w", ctx.Err())
	}

	defer func() { <-l.gate }()

	if err := ctx.Err(); err != nil {
		return Bundle{}, fmt.Errorf("load Apple assets: %w", err)
	}

	if l.installed != nil {
		return *l.installed, nil
	}

	bundle, err := l.resolve(ctx)
	if err != nil {
		return Bundle{}, err
	}

	if err := ctx.Err(); err != nil {
		return Bundle{}, fmt.Errorf("load Apple assets: %w", err)
	}

	l.installed = &bundle

	return bundle, nil
}

func loadBundle(ctx context.Context) (Bundle, error) {
	directory, err := cacheDirectory()
	if err != nil {
		return Bundle{}, err
	}

	if bundle, found := loadCachedBundle(directory); found {
		return bundle, nil
	}

	bundle, err := resolveRemoteImage(ctx)
	if err != nil {
		return Bundle{}, err
	}

	if err := writeCache(directory, bundle); err != nil {
		return Bundle{}, fmt.Errorf("cache Apple SAP assets: %w", err)
	}

	return bundle, nil
}

// Keep only the architecture used by the emulator after verifying and caching
// the original files. Cloning releases the unused slices of universal binaries.
func compactBundle(bundle Bundle) (Bundle, error) {
	for _, image := range []struct {
		name string
		data *[]byte
	}{
		{"AppleMediaServices", &bundle.AppleMediaServices},
		{"commerce", &bundle.Commerce},
		{"adid", &bundle.ADID},
		{"CoreFP", &bundle.CoreFP},
		{"fairplayd", &bundle.FairPlayD},
		{"FairPlayIOKit", &bundle.FairPlayKext},
	} {
		data, err := compactImage(image.name, *image.data)
		if err != nil {
			return Bundle{}, err
		}

		*image.data = data
	}

	return bundle, nil
}

func compactImage(name string, input []byte) ([]byte, error) {
	data, err := machimage.ARM64Slice(input)
	if err != nil {
		return nil, fmt.Errorf("select %s architecture: %w", name, err)
	}

	if len(data) != len(input) {
		return bytes.Clone(data), nil
	}

	return data, nil
}

func cacheDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}

	return filepath.Join(root, "ipatool", "sap", assetCacheDirectory), nil
}

func loadCachedBundle(directory string) (Bundle, bool) {
	if bundle, err := readCache(directory); err == nil {
		return bundle, true
	}

	legacyDirectory := filepath.Join(filepath.Dir(directory), legacyAssetCacheDirectory)

	bundle, err := readCacheSpecs(legacyDirectory, legacyFiles)
	if err != nil {
		return Bundle{}, false
	}

	if err := validate(bundle); err != nil {
		return Bundle{}, false
	}

	// A verified legacy cache remains usable on read-only mounts. Persist the
	// smaller representation when possible without requiring a new download.
	_ = writeCache(directory, bundle)

	return bundle, true
}

func readCache(directory string) (Bundle, error) {
	return readCacheSpecs(directory, requiredFiles)
}

func readCacheSpecs(directory string, specs []fileSpec) (Bundle, error) {
	files := make(map[string][]byte, len(specs))

	for _, spec := range specs {
		data, err := readCacheData(filepath.Join(directory, spec.name), spec.size, &spec)
		if err != nil {
			return Bundle{}, fmt.Errorf("read cached Apple SAP asset %s: %w", spec.name, err)
		}

		files[spec.name] = data
	}

	return bundleFrom(files), nil
}

func readCacheFile(path string, expectedSize int) ([]byte, error) {
	return readCacheData(path, expectedSize, nil)
}

func readCacheData(path string, expectedSize int, spec *fileSpec) ([]byte, error) {
	if expectedSize < 0 {
		return nil, errors.New("invalid expected size")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cached asset: %w", err)
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect cached asset: %w", err)
	}

	if info.Size() != int64(expectedSize) {
		return nil, fmt.Errorf("has size %d, expected %d", info.Size(), expectedSize)
	}

	var reader io.Reader = file

	var digest hash.Hash
	if spec != nil {
		digest = sha256.New()
		reader = io.TeeReader(file, digest)
	}

	var data []byte
	if spec != nil && spec.name != "CoreFP.icxs" {
		data, err = machimage.ReadARM64(reader, expectedSize)
	} else {
		data = make([]byte, expectedSize)
		_, err = io.ReadFull(reader, data)
	}

	if err != nil {
		return nil, fmt.Errorf("read cached asset: %w", err)
	}

	var extra [1]byte
	if count, err := io.ReadFull(file, extra[:]); err == nil || count != 0 {
		return nil, fmt.Errorf("grew beyond expected size %d while being read", expectedSize)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("verify cached asset length: %w", err)
	}

	if spec != nil && !bytes.Equal(digest.Sum(nil), spec.digest[:]) {
		return nil, fmt.Errorf("apple SAP asset %s failed integrity verification", spec.name)
	}

	return data, nil
}

func writeCache(directory string, bundle Bundle) error {
	if err := validate(bundle); err != nil {
		return err
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create SAP asset cache: %w", err)
	}

	files := bundleFiles(bundle)
	for _, spec := range requiredFiles {
		if err := replaceFile(filepath.Join(directory, spec.name), files[spec.name]); err != nil {
			return fmt.Errorf("write cached SAP asset %s: %w", spec.name, err)
		}
	}

	return nil
}

func replaceFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return fmt.Errorf("create temporary cache file: %w", err)
	}

	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("set cache file permissions: %w", err)
	}

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("write temporary cache file: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary cache file: %w", err)
	}

	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove existing cache file: %w", err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install cache file: %w", err)
	}

	return nil
}

func validate(bundle Bundle) error {
	files := bundleFiles(bundle)
	for _, spec := range requiredFiles {
		if err := validateFile(spec, files[spec.name]); err != nil {
			return err
		}
	}

	return nil
}

func validateFile(spec fileSpec, data []byte) error {
	if len(data) != spec.size {
		return fmt.Errorf("apple SAP asset %s has size %d, expected %d", spec.name, len(data), spec.size)
	}

	if sha256.Sum256(data) != spec.digest {
		return fmt.Errorf("apple SAP asset %s failed integrity verification", spec.name)
	}

	return nil
}

func bundleFrom(files map[string][]byte) Bundle {
	return Bundle{
		AppleMediaServices: files["AppleMediaServices"],
		Commerce:           files["commerce"],
		ADID:               files["adid"],
		CoreFP:             files["CoreFP"],
		CoreFPICXS:         files["CoreFP.icxs"],
		FairPlayD:          files["fairplayd"],
		FairPlayKext:       files["FairPlayIOKit"],
	}
}

func bundleFiles(bundle Bundle) map[string][]byte {
	return map[string][]byte{
		"AppleMediaServices": bundle.AppleMediaServices,
		"commerce":           bundle.Commerce,
		"adid":               bundle.ADID,
		"CoreFP":             bundle.CoreFP,
		"CoreFP.icxs":        bundle.CoreFPICXS,
		"fairplayd":          bundle.FairPlayD,
		"FairPlayIOKit":      bundle.FairPlayKext,
	}
}

func mustDigest(value string) [32]byte {
	var digest [32]byte

	decoded, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}

	if len(decoded) != len(digest) {
		panic("invalid SHA-256 length")
	}

	copy(digest[:], decoded)

	return digest
}
