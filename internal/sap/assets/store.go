package assets

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

const authenticationPluginImage = "/System/Library/Accounts/Authentication/AMSAccountAuthenticationPlugin.bundle/Contents/MacOS/AMSAccountAuthenticationPlugin"

// StoreCache reads the pinned shared-cache data without loading host frameworks.
// It is used both by the profile generator and the portable asset installer.
type StoreCache struct {
	cache          *dyldCache
	ctx            context.Context
	opener         dyldCacheOpener
	cleanup        func() error
	slides         map[uint64]map[uint64]uint64
	images         map[string]uint64
	readSystemFile func(string) ([]byte, error)
}

// OpenLocalStoreCache opens shared-cache files for offline profile generation.
func OpenLocalStoreCache(ctx context.Context, path string) (*StoreCache, error) {
	opener := func(suffix string) (*dyldCacheFile, error) {
		file, err := os.Open(path + suffix)
		if err != nil {
			return nil, fmt.Errorf("open Store cache member: %w", err)
		}

		info, err := file.Stat()
		if err != nil {
			_ = file.Close()

			return nil, fmt.Errorf("inspect Store cache member: %w", err)
		}

		return &dyldCacheFile{reader: file, size: info.Size(), close: file.Close}, nil
	}

	s, err := newStoreCache(ctx, opener, func() error { return nil })
	if err == nil {
		s.readSystemFile = os.ReadFile
	}

	return s, err
}

// OpenRemoteStoreCache obtains data directly from Apple's pinned restore image.
func OpenRemoteStoreCache(ctx context.Context) (*StoreCache, error) {
	if ctx == nil {
		return nil, errors.New("store asset context is nil")
	}

	client := &http.Client{}

	remote, err := openRemoteFile(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("open Store restore image: %w", err)
	}

	archive, err := zip.NewReader(remote, remote.size)
	if err != nil {
		return nil, fmt.Errorf("read Store restore archive: %w", err)
	}

	entry, err := pinnedZipEntry(archive, systemOSImageName, systemOSArchiveSize, systemOSArchiveSize)
	if err != nil {
		return nil, err
	}

	offset, err := entry.DataOffset()
	if err != nil {
		return nil, fmt.Errorf("locate Store cryptex: %w", err)
	}

	image, err := newAEAReader(ctx, client, io.NewSectionReader(remote, offset, int64(entry.CompressedSize64)), int64(entry.CompressedSize64))
	if err != nil {
		return nil, fmt.Errorf("open Store cryptex: %w", err)
	}

	if image.size != systemOSImageSize {
		return nil, errors.New("store cryptex does not match the pinned image")
	}

	extractor, closeExtractor, err := newAPFSExtractor(image, image.size)
	if err != nil {
		return nil, err
	}

	opener := func(suffix string) (*dyldCacheFile, error) {
		file, err := extractor.openFile(ctx, arm64CachePath+suffix)
		if err != nil {
			return nil, fmt.Errorf("open Store cache member: %w", err)
		}

		return &dyldCacheFile{reader: file, size: file.size, close: func() error { return nil }}, nil
	}

	var systemExtractor *apfsExtractor

	var closeSystem func() error

	cleanup := func() error {
		err := closeExtractor()
		if closeSystem != nil {
			err = errors.Join(err, closeSystem())
		}

		return err
	}

	s, err := newStoreCache(ctx, opener, cleanup)
	if err == nil {
		s.readSystemFile = func(path string) ([]byte, error) {
			if systemExtractor == nil {
				entry, e := pinnedZipEntry(archive, fileSystemImageName, fileSystemArchiveSize, fileSystemArchiveSize)
				if e != nil {
					return nil, e
				}

				offset, e := entry.DataOffset()
				if e != nil {
					return nil, fmt.Errorf("locate Store filesystem: %w", e)
				}

				image, e := newAEAReader(ctx, client, io.NewSectionReader(remote, offset, int64(entry.CompressedSize64)), int64(entry.CompressedSize64))
				if e != nil {
					return nil, e
				}

				if image.size != fileSystemImageSize {
					return nil, errors.New("store filesystem does not match pinned image")
				}

				systemExtractor, closeSystem, e = newAPFSExtractor(image, image.size)
				if e != nil {
					return nil, e
				}
			}

			f, e := systemExtractor.openFile(ctx, path)
			if e != nil {
				return nil, e
			}

			if f.size > 4<<20 {
				return nil, errors.New("store system file exceeds size limit")
			}

			return io.ReadAll(io.NewSectionReader(f, 0, f.size))
		}
	}

	return s, err
}

func newStoreCache(ctx context.Context, opener dyldCacheOpener, cleanup func() error) (*StoreCache, error) {
	if ctx == nil {
		_ = cleanup()

		return nil, errors.New("store asset context is nil")
	}

	if err := ctx.Err(); err != nil {
		_ = cleanup()

		return nil, fmt.Errorf("open Store cache: %w", err)
	}

	cache, err := openDyldCache(opener)
	if err != nil {
		_ = cleanup()

		return nil, fmt.Errorf("open Store shared cache: %w", err)
	}

	return &StoreCache{cache: cache, ctx: ctx, opener: opener, cleanup: cleanup, slides: make(map[uint64]map[uint64]uint64)}, nil
}

func (s *StoreCache) Close() error {
	return errors.Join(s.cache.Close(), s.cleanup())
}

// Page returns one unslid 4 KiB data page. Runtime-initialized metadata is applied
// separately by the versioned Store profile; these bytes always come from Apple.
func (s *StoreCache) Page(address uint64) ([]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, fmt.Errorf("read Store page: %w", err)
	}

	if address&4095 != 0 {
		return nil, errors.New("unaligned Store page")
	}

	mapping, err := s.cache.mappingForAddress(address)
	if err != nil {
		return nil, err
	}

	data := make([]byte, 4096)
	reader := &dyldCacheReader{cache: s.cache}

	if _, err = reader.ReadAtAddr(data, address); err != nil {
		return nil, fmt.Errorf("read Store page: %w", err)
	}

	if mapping.slideInfoSize == 0 {
		return data, nil
	}

	pageSize, err := s.cache.slidePageSize(*mapping)
	if err != nil {
		return nil, err
	}

	index := (address - mapping.address) / pageSize
	base := mapping.address + index*pageSize

	rebases, ok := s.slides[base]
	if !ok {
		values, err := s.cache.rebases(*mapping, index, index+1)
		if err != nil {
			return nil, err
		}

		rebases = make(map[uint64]uint64, len(values))
		for _, value := range values {
			rebases[value.address] = value.target
		}

		s.slides[base] = rebases
	}

	for location, target := range rebases {
		if location >= address && location+8 <= address+4096 {
			binary.LittleEndian.PutUint64(data[location-address:], target)
		}
	}

	return data, nil
}

// ExtractAuthenticationPlugin exports the same image on every host platform.
func (s *StoreCache) ExtractAuthenticationPlugin(destination string) error {
	if err := s.ctx.Err(); err != nil {
		return fmt.Errorf("extract authentication plugin: %w", err)
	}

	return extractDyldImageFromFiles(s.opener, authenticationPluginImage, destination)
}

// FeatureFlags reads the pinned operating system defaults used by Store metadata.
func (s *StoreCache) FeatureFlags(domain string) ([]byte, error) {
	switch domain {
	case "AppleMediaServices", "Foundation", "CFNetwork":
	default:
		return nil, errors.New("unsupported Store feature domain")
	}

	return s.readSystemFile("/System/Library/FeatureFlags/Domain/" + domain + ".plist")
}
