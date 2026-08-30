package assets

import (
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/blacktop/go-macho/pkg/xar"
	"howett.net/ranger"

	"github.com/majd/ipatool/v2/internal/sap/cpio"
)

const (
	updateURL       = "https://swcdn.apple.com/content/downloads/27/34/041-98128-A_SYPWICN3KH/5dqkl4rqgbsr18yzy61yeie9g3cmjc5hiv/OSXUpd10.9.pkg"
	payloadName     = "Payload"
	payloadBZOffset = int64(0x352F40D5)
	payloadCPIO     = int64(0x3A4)
)

type Bundle struct {
	CommerceKit  []byte
	CommerceCore []byte
	CoreFP       []byte
	CoreFPICXS   []byte
}

type fileSpec struct {
	name   string
	path   string
	size   int
	digest [32]byte
}

var requiredFiles = []fileSpec{
	{
		name:   "CommerceKit",
		path:   "./System/Library/PrivateFrameworks/CommerceKit.framework/Versions/A/CommerceKit",
		size:   3271840,
		digest: mustDigest("b84ff12c21987856c0a17b78f1ad82b73195a6dec5f3b208a17d245555a2c8a2"),
	},
	{
		name:   "CommerceCore",
		path:   "./System/Library/PrivateFrameworks/CommerceKit.framework/Versions/A/Frameworks/CommerceCore.framework/Versions/A/CommerceCore",
		size:   207744,
		digest: mustDigest("c5401e57402230f3c876409d295319ddf1e61287bc882683c5d61277be7bc1f2"),
	},
	{
		name:   "CoreFP",
		path:   "./System/Library/PrivateFrameworks/CoreFP.framework/Versions/A/CoreFP",
		size:   29014912,
		digest: mustDigest("f19141336be4198d0f8991bb00017c915efc7aeaece36c345f7faa1237ea6074"),
	},
	{
		name:   "CoreFP.icxs",
		path:   "./System/Library/PrivateFrameworks/CoreFP.framework/Versions/A/CoreFP.icxs",
		size:   5288352,
		digest: mustDigest("473e78af86979f5bd4f6269561caf770b3d16c098d918846eeac8cdd2fe6566a"),
	},
}

func Load(ctx context.Context) (Bundle, error) {
	directory, err := cacheDirectory()
	if err != nil {
		return Bundle{}, err
	}

	if bundle, err := readCache(directory); err == nil {
		return bundle, nil
	}

	bundle, err := download(ctx)
	if err != nil {
		return Bundle{}, err
	}

	if err := writeCache(directory, bundle); err != nil {
		return Bundle{}, fmt.Errorf("cache Apple SAP assets: %w", err)
	}

	return bundle, nil
}

func download(ctx context.Context) (Bundle, error) {
	found, err := downloadFiles(ctx, requiredFiles, "download Apple SAP assets")
	if err != nil {
		return Bundle{}, err
	}

	bundle := bundleFrom(found)
	if err := validate(bundle); err != nil {
		return Bundle{}, err
	}

	return bundle, nil
}

func downloadFiles(ctx context.Context, specs []fileSpec, operation string) (map[string][]byte, error) {
	parsed, err := url.Parse(updateURL)
	if err != nil {
		return nil, fmt.Errorf("parse Apple software update URL: %w", err)
	}

	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: contextTransport{ctx: ctx, next: http.DefaultTransport},
	}

	remote, err := ranger.NewReader(&ranger.HTTPRanger{Client: client, URL: parsed})
	if err != nil {
		return nil, fmt.Errorf("open Apple software update: %w", err)
	}

	length, err := remote.Length()
	if err != nil {
		return nil, fmt.Errorf("measure Apple software update: %w", err)
	}

	container, err := xar.NewReader(remote, length)
	if err != nil {
		return nil, fmt.Errorf("read Apple software update: %w", err)
	}

	var payload *xar.File

	for _, candidate := range container.Files {
		if candidate.Name == payloadName {
			payload = candidate

			break
		}
	}

	if payload == nil {
		return nil, &fs.PathError{Op: "open", Path: payloadName, Err: fs.ErrNotExist}
	}

	raw := payload.OpenRaw()
	if _, err := raw.Seek(payloadBZOffset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek Apple update payload: %w", err)
	}

	compressed := io.MultiReader(bytes.NewReader([]byte{'B', 'Z', 'h', '9'}), raw)

	archive := bzip2.NewReader(compressed)
	if _, err := io.CopyN(io.Discard, archive, payloadCPIO); err != nil {
		return nil, fmt.Errorf("seek Apple payload archive: %w", err)
	}

	wanted := make(map[string]fileSpec, len(specs))
	for _, spec := range specs {
		wanted[spec.path] = spec
	}

	found := make(map[string][]byte, len(specs))
	reader := cpio.NewReader(archive)

	for len(found) != len(wanted) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", operation, err)
		}

		path, body, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read Apple payload archive: %w", err)
		}

		spec, ok := wanted[path]
		if !ok {
			continue
		}

		data, err := io.ReadAll(io.LimitReader(body, int64(spec.size)+1))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", spec.name, err)
		}

		found[spec.name] = data
	}

	return found, nil
}

func cacheDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}

	return filepath.Join(root, "ipatool", "sap", "apple-assets-v2"), nil
}

func readCache(directory string) (Bundle, error) {
	files := make(map[string][]byte, len(requiredFiles))

	for _, spec := range requiredFiles {
		data, err := readCacheFile(filepath.Join(directory, spec.name), spec.size)
		if err != nil {
			return Bundle{}, fmt.Errorf("read cached Apple SAP asset %s: %w", spec.name, err)
		}

		files[spec.name] = data
	}

	bundle := bundleFrom(files)
	if err := validate(bundle); err != nil {
		return Bundle{}, err
	}

	return bundle, nil
}

func readCacheFile(path string, expectedSize int) ([]byte, error) {
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

	data := make([]byte, expectedSize)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, fmt.Errorf("read cached asset: %w", err)
	}

	var extra [1]byte
	if count, err := io.ReadFull(file, extra[:]); err == nil || count != 0 {
		return nil, fmt.Errorf("grew beyond expected size %d while being read", expectedSize)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("verify cached asset length: %w", err)
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
		data := files[spec.name]
		if len(data) != spec.size {
			return fmt.Errorf("apple SAP asset %s has size %d, expected %d", spec.name, len(data), spec.size)
		}

		if sha256.Sum256(data) != spec.digest {
			return fmt.Errorf("apple SAP asset %s failed integrity verification", spec.name)
		}
	}

	return nil
}

func bundleFrom(files map[string][]byte) Bundle {
	return Bundle{
		CommerceKit:  files["CommerceKit"],
		CommerceCore: files["CommerceCore"],
		CoreFP:       files["CoreFP"],
		CoreFPICXS:   files["CoreFP.icxs"],
	}
}

func bundleFiles(bundle Bundle) map[string][]byte {
	return map[string][]byte{
		"CommerceKit":  bundle.CommerceKit,
		"CommerceCore": bundle.CommerceCore,
		"CoreFP":       bundle.CoreFP,
		"CoreFP.icxs":  bundle.CoreFPICXS,
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

type contextTransport struct {
	ctx  context.Context
	next http.RoundTripper
}

func (t contextTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request.Clone(t.ctx))
	if err != nil {
		return nil, fmt.Errorf("fetch Apple software update: %w", err)
	}

	return response, nil
}
