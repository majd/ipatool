package appstore

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/blacktop/go-macho/pkg/xar"
)

type macPreflightDistributionInfo struct {
	XMLName  xml.Name `xml:"installer-gui-script"`
	Products []struct {
		ID      string `xml:"id,attr"`
		Version string `xml:"version,attr"`
	} `xml:"product"`
	Packages []struct {
		ID      string `xml:"id,attr"`
		Version string `xml:"version,attr"`
		Bundles []struct {
			ID string `xml:"id,attr"`
		} `xml:"relocate>bundle"`
	} `xml:"pkg-ref"`
}

func macPreflightVersionMetadata(archive *xar.Reader, bundleID string) (versionMetadata, error) {
	var distribution *xar.File

	for _, file := range archive.Files {
		if file.Name != "Distribution" {
			continue
		}

		if distribution != nil {
			return versionMetadata{}, errors.New("macOS preflight package contains multiple Distribution files")
		}

		distribution = file
	}

	if distribution == nil {
		return versionMetadata{}, errors.New("macOS preflight package does not contain Distribution")
	}

	if !distribution.VerifyChecksum() {
		return versionMetadata{}, errors.New("macOS preflight Distribution checksum mismatch")
	}

	source, err := distribution.Open()
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to open Mac preflight Distribution: %w", err)
	}

	data, readErr := io.ReadAll(source)
	closeErr := source.Close()

	if err := errors.Join(readErr, closeErr); err != nil {
		return versionMetadata{}, fmt.Errorf("failed to read Mac preflight Distribution: %w", err)
	}

	var info macPreflightDistributionInfo
	if err := xml.Unmarshal(data, &info); err != nil {
		return versionMetadata{}, fmt.Errorf("failed to parse Mac preflight Distribution: %w", err)
	}

	if len(info.Products) != 1 || info.Products[0].ID == "" || (bundleID != "" && info.Products[0].ID != bundleID) {
		return versionMetadata{}, errors.New("macOS preflight package does not contain a unique product for the requested bundle")
	}

	product := info.Products[0]
	version := strings.TrimSpace(product.Version)

	if version == "" {
		return versionMetadata{}, errors.New("macOS preflight package does not contain a display version")
	}

	// References to the same package can be split across several pkg-ref
	// elements. Only use versions belonging to the requested product's bundle.
	packageIDs := make(map[string]bool)

	for _, pkg := range info.Packages {
		for _, bundle := range pkg.Bundles {
			if bundle.ID == product.ID && pkg.ID != "" {
				packageIDs[pkg.ID] = true
			}
		}
	}

	var built time.Time

	for _, pkg := range info.Packages {
		if !packageIDs[pkg.ID] || pkg.Version == "" {
			continue
		}

		timestamp, err := legacyMacPackageBuildTime(version, pkg.Version)
		if err != nil {
			return versionMetadata{}, err
		}

		if !built.IsZero() && !built.Equal(timestamp) {
			return versionMetadata{}, errors.New("macOS preflight package contains conflicting build timestamps")
		}

		built = timestamp
	}

	if built.IsZero() {
		return versionMetadata{}, errors.New("macOS preflight package does not contain a build timestamp for the requested bundle")
	}

	// As with the payload timestamp fallback, this is the build time, not an
	// exact store release date. The API date is app-level, and the preflight
	// archive's creation time can reflect a much later repackaging date.
	return versionMetadata{DisplayVersion: version, ReleaseDate: built}, nil
}

func legacyMacPackageBuildTime(displayVersion, packageVersion string) (time.Time, error) {
	// Legacy Apple installers use major.minor.patch.0.1.<Unix build time>.
	// Do not interpret arbitrary dotted versions as timestamps.
	parts := strings.Split(packageVersion, ".")
	version := strings.Split(displayVersion, ".")

	for len(version) < 3 {
		version = append(version, "0")
	}

	if len(parts) != 6 || len(version) != 3 || strings.Join(parts[:3], ".") != strings.Join(version, ".") ||
		parts[3] != "0" || parts[4] != "1" || len(parts[5]) != 10 {
		return time.Time{}, errors.New("macOS preflight package does not contain a supported build timestamp")
	}

	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return time.Time{}, errors.New("macOS preflight package contains an invalid installer version")
		}
	}

	seconds, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, errors.New("macOS preflight package contains an invalid build timestamp")
	}

	return time.Unix(seconds, 0).UTC(), nil
}
