//nolint:wsl // Archive extraction is clearer when resource setup remains adjacent.
package assets

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	appleMediaServicesImage = "/System/Library/PrivateFrameworks/AppleMediaServices.framework/Versions/A/AppleMediaServices"
	fileSystemImageName     = "043-70867-640.dmg.aea"
	systemOSImageName       = "043-70701-651.dmg.aea"
	fileSystemArchiveSize   = 11274289152
	fileSystemImageSize     = 14189330432
	systemOSArchiveSize     = 2415919104
	systemOSImageSize       = 2879389696
	arm64CachePath          = "/System/Library/dyld/dyld_shared_cache_arm64e"
)

var frameworkImagePaths = []string{
	"/System/Library/PrivateFrameworks/CommerceKit.framework/Versions/A/Resources/commerce",
	"/System/Library/PrivateFrameworks/CoreFP.framework/Versions/A/CoreFP",
	"/System/Library/PrivateFrameworks/CoreFP.framework/Versions/A/Resources/CoreFP.icxs",
}

func resolveRemoteImage(ctx context.Context) (Bundle, error) {
	client := &http.Client{}
	remote, err := openRemoteFile(ctx, client, macOSRestoreURL)
	if err != nil {
		return Bundle{}, fmt.Errorf("resolve macOS %s (%s) frameworks: %w", macOSVersion, macOSBuild, err)
	}

	archive, err := zip.NewReader(remote, remote.size)
	if err != nil {
		return Bundle{}, fmt.Errorf("read macOS %s (%s) restore image: %w", macOSVersion, macOSBuild, err)
	}

	fileSystemEntry, err := pinnedZipEntry(archive, fileSystemImageName, fileSystemArchiveSize, fileSystemArchiveSize)
	if err != nil {
		return Bundle{}, err
	}

	systemOSEntry, err := pinnedZipEntry(archive, systemOSImageName, systemOSArchiveSize, systemOSArchiveSize)
	if err != nil {
		return Bundle{}, err
	}

	workspace, err := os.MkdirTemp("", "ipatool-macos-27-*")
	if err != nil {
		return Bundle{}, fmt.Errorf("create macOS image workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	frameworkDirectory := filepath.Join(workspace, "frameworks")
	if err := extractFrameworkFiles(ctx, client, remote, fileSystemEntry, frameworkDirectory); err != nil {
		return Bundle{}, err
	}

	appleMediaServicesPath := filepath.Join(frameworkDirectory, "AppleMediaServices")
	if err := extractAppleMediaServices(ctx, client, remote, systemOSEntry, appleMediaServicesPath); err != nil {
		return Bundle{}, fmt.Errorf("extract AppleMediaServices from macOS %s (%s): %w", macOSVersion, macOSBuild, err)
	}

	files := make(map[string][]byte, len(requiredFiles))

	for _, spec := range requiredFiles {
		data, err := os.ReadFile(filepath.Join(frameworkDirectory, spec.name))
		if err != nil {
			return Bundle{}, fmt.Errorf("read extracted %s: %w", spec.name, err)
		}

		files[spec.name] = data
	}

	bundle := bundleFrom(files)
	if err := validate(bundle); err != nil {
		return Bundle{}, err
	}

	return bundle, nil
}

func extractAppleMediaServices(ctx context.Context, client *http.Client, remote *remoteFile, entry *zip.File, destination string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("extract AppleMediaServices: %w", err)
	}

	offset, err := entry.DataOffset()
	if err != nil {
		return fmt.Errorf("locate encrypted SystemOS cryptex: %w", err)
	}
	encryptedImage := io.NewSectionReader(remote, offset, int64(entry.CompressedSize64))
	image, err := newAEAReader(ctx, client, encryptedImage, int64(entry.CompressedSize64))
	if err != nil {
		return fmt.Errorf("open encrypted SystemOS cryptex: %w", err)
	}

	if image.size != systemOSImageSize {
		return fmt.Errorf("SystemOS cryptex has size %d, expected %d", image.size, systemOSImageSize)
	}

	extractor, closeExtractor, err := newAPFSExtractor(image, image.size)
	if err != nil {
		return err
	}

	defer func() { _ = closeExtractor() }()

	opener := func(suffix string) (*dyldCacheFile, error) {
		file, err := extractor.openFile(ctx, arm64CachePath+suffix)
		if err != nil {
			return nil, fmt.Errorf("open arm64e shared-cache member %q: %w", suffix, err)
		}

		return &dyldCacheFile{reader: file, size: file.size, close: func() error { return nil }}, nil
	}
	if err := extractDyldImageFromFiles(opener, appleMediaServicesImage, destination); err != nil {
		return fmt.Errorf("export AppleMediaServices from arm64e shared cache: %w", err)
	}

	return nil
}

func pinnedZipEntry(archive *zip.Reader, name string, compressedSize, uncompressedSize uint64) (*zip.File, error) {
	for _, entry := range archive.File {
		if entry.Name != name {
			continue
		}

		if entry.Method != zip.Store || entry.CompressedSize64 != compressedSize || entry.UncompressedSize64 != uncompressedSize {
			return nil, fmt.Errorf("macOS %s (%s) restore member %s does not match the pinned image", macOSVersion, macOSBuild, name)
		}

		return entry, nil
	}

	return nil, fmt.Errorf("macOS %s (%s) restore image does not contain %s", macOSVersion, macOSBuild, name)
}

func extractFrameworkFiles(ctx context.Context, client *http.Client, remote *remoteFile, entry *zip.File, destination string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("extract framework files: %w", err)
	}

	offset, err := entry.DataOffset()
	if err != nil {
		return fmt.Errorf("locate encrypted macOS filesystem: %w", err)
	}

	encryptedImage := io.NewSectionReader(remote, offset, int64(entry.CompressedSize64))
	image, err := newAEAReader(ctx, client, encryptedImage, int64(entry.CompressedSize64))
	if err != nil {
		return fmt.Errorf("open encrypted macOS filesystem: %w", err)
	}

	if image.size != fileSystemImageSize {
		return fmt.Errorf("macOS filesystem has size %d, expected %d", image.size, fileSystemImageSize)
	}

	if err := extractAPFSFiles(ctx, image, image.size, frameworkImagePaths, destination); err != nil {
		return fmt.Errorf("extract frameworks from macOS %s (%s): %w", macOSVersion, macOSBuild, err)
	}

	return nil
}
