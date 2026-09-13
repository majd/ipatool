package appstore

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/blacktop/go-macho/pkg/xar"
)

func (t *appstore) readVersionMetadataFromMacPackage(ctx context.Context, item downloadItemResult, hardwareID []byte, bundleID string) (versionMetadata, error) {
	directory, err := os.MkdirTemp("", "ipatool-version-metadata-*")
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to create metadata staging: %w", err)
	}

	defer os.RemoveAll(directory)

	destination := filepath.Join(directory, "app.pkg")
	if _, err := t.downloadMacPackage(ctx, item, destination, hardwareID, nil); err != nil {
		return versionMetadata{}, err
	}

	if bundleID == "" {
		bundleID = downloadMetadataString(item.Metadata, "softwareVersionBundleId")
	}

	archive, err := xar.Open(destination)
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to open Mac package: %w", err)
	}

	defer archive.Close()

	return macPackageVersionMetadata(&archive.Reader, bundleID)
}

func macPackageVersionMetadata(archive *xar.Reader, bundleID string) (versionMetadata, error) {
	var result *versionMetadata

	for _, file := range archive.Files {
		if path.Base(file.Name) != "PackageInfo" {
			continue
		}

		source, err := file.Open()
		if err != nil {
			return versionMetadata{}, fmt.Errorf("failed to open PackageInfo: %w", err)
		}

		data, readErr := io.ReadAll(source)
		closeErr := source.Close()

		if err := errors.Join(readErr, closeErr); err != nil {
			return versionMetadata{}, fmt.Errorf("failed to read PackageInfo: %w", err)
		}

		var info struct {
			Bundles []struct {
				ID      string `xml:"id,attr"`
				Version string `xml:"CFBundleShortVersionString,attr"`
			} `xml:"bundle"`
		}

		if err := xml.Unmarshal(data, &info); err != nil {
			return versionMetadata{}, fmt.Errorf("failed to parse PackageInfo: %w", err)
		}

		for _, bundle := range info.Bundles {
			if bundleID != "" && bundle.ID != bundleID {
				continue
			}

			version := strings.TrimSpace(bundle.Version)
			if version == "" {
				return versionMetadata{}, errors.New("macOS package does not contain a display version")
			}

			if result != nil {
				return versionMetadata{}, errors.New("macOS package contains multiple matching bundles")
			}
			// Like the IPA Info.plist timestamp fallback, use the selected build's
			// archived payload timestamp, not the API's app-level release date.
			var modified int64

			for _, payload := range archive.Files {
				if payload.Name == path.Join(path.Dir(file.Name), "Payload") {
					modified = payload.Info.Mtime

					break
				}
			}

			if modified <= 0 {
				return versionMetadata{}, errors.New("macOS package does not contain a payload timestamp")
			}

			result = &versionMetadata{DisplayVersion: version, ReleaseDate: time.Unix(modified, 0).UTC()}
		}
	}

	if result == nil {
		return versionMetadata{}, errors.New("macOS package does not contain metadata for the requested bundle")
	}

	return *result, nil
}
