package appstore

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/schollz/progressbar/v3"
	"howett.net/plist"
)

var (
	ErrLicenseRequired = errors.New("license is required")
)

type DownloadInput struct {
	Context           context.Context
	Account           Account
	App               App
	OutputPath        string
	Progress          *progressbar.ProgressBar
	ExternalVersionID string
	Platform          Platform
}

type DownloadOutput struct {
	DestinationPath string
	Sinfs           []Sinf
}

func (t *appstore) Download(input DownloadInput) (DownloadOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	var machineGUID []byte
	if input.Platform == PlatformMacOS {
		guid, machineGUID, err = machineIdentity(macAddr)
		if err != nil {
			return DownloadOutput{}, fmt.Errorf("failed to resolve machine identity: %w", err)
		}
	}

	externalVersionID := input.ExternalVersionID
	if externalVersionID == "" && (input.Platform == PlatformAppleTV || input.Platform == PlatformVisionOS) {
		externalVersionID, err = t.lookupLatestExternalVersionID(input.Account, input.App, input.Platform)
		if err != nil {
			return DownloadOutput{}, fmt.Errorf("failed to resolve platform version: %w", err)
		}
	}

	res, resolvedPlatform, err := t.sendDownloadProduct(input.Account, input.App, guid, externalVersionID, input.Platform)
	if err != nil {
		return DownloadOutput{}, err
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired ||
		res.Data.FailureType == FailureTypeSignInRequired ||
		res.Data.FailureType == FailureTypeDeviceVerificationFailed ||
		res.Data.FailureType == FailureTypeLicenseAlreadyExists {
		return DownloadOutput{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return DownloadOutput{}, ErrLicenseRequired
	}

	if res.Data.CustomerMessage != "" && (res.Data.FailureType != "" || len(res.Data.Items) == 0) {
		return DownloadOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return DownloadOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.FailureType), res)
	}

	if len(res.Data.Items) == 0 {
		return DownloadOutput{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	}

	item := res.Data.Items[0]

	version := "unknown"

	// Read the version from the item metadata
	if itemVersion, ok := item.Metadata["bundleShortVersionString"]; ok {
		version = fmt.Sprintf("%v", itemVersion)
	}

	packagePlatform, err := downloadPackagePlatform(input.Platform, item)
	if err != nil {
		return DownloadOutput{}, err
	}

	destination, err := t.resolveDestinationPath(input.App, version, input.OutputPath, packagePlatform)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to resolve destination path: %w", err)
	}

	if packagePlatform == PlatformMacOS {
		return t.downloadMacPackage(input.Context, item, destination, machineGUID, input.Progress)
	}

	tmpPath := fmt.Sprintf("%s.tmp", destination)

	if err := t.downloadFile(input.Context, item.URL, tmpPath, input.Progress); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to download file: %w", err)
	}

	if err := t.validatePackagePlatform(tmpPath, resolvedPlatform); err != nil {
		if removeErr := t.os.Remove(tmpPath); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to remove invalid package: %w", removeErr))
		}

		return DownloadOutput{}, fmt.Errorf("failed to validate package platform: %w", err)
	}

	artwork, err := t.downloadArtwork(input.Context, item.ArtworkURL)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to download artwork: %w", err)
	}

	if err := t.applyPatches(item, input.Account, tmpPath, destination, artwork); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to apply patches: %w", err)
	}

	if err := t.os.Remove(tmpPath); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to remove file: %w", err)
	}

	return DownloadOutput{
		DestinationPath: destination,
		Sinfs:           item.Sinfs,
	}, nil
}

type platformPackageInfo struct {
	SupportedPlatforms []string `plist:"CFBundleSupportedPlatforms,omitempty"`
}

func (*appstore) validatePackagePlatform(path string, platform Platform) error {
	var expectedPlatform string

	switch platform {
	case PlatformIPhone, PlatformIPad:
		expectedPlatform = "iPhoneOS"
	case PlatformAppleTV:
		expectedPlatform = "AppleTVOS"
	case PlatformVisionOS:
		expectedPlatform = "XROS"
	default:
		return nil
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("failed to open zip reader: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if !isTopLevelAppInfoPlist(file.Name) {
			continue
		}

		infoFile, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open info plist: %w", err)
		}

		data, readErr := io.ReadAll(infoFile)
		closeErr := infoFile.Close()

		if readErr != nil {
			return fmt.Errorf("failed to read info plist: %w", readErr)
		}

		if closeErr != nil {
			return fmt.Errorf("failed to close info plist: %w", closeErr)
		}

		var info platformPackageInfo

		_, err = plist.Unmarshal(data, &info)
		if err != nil {
			return fmt.Errorf("failed to decode info plist: %w", err)
		}

		for _, supportedPlatform := range info.SupportedPlatforms {
			if supportedPlatform == expectedPlatform {
				return nil
			}
		}
	}

	return fmt.Errorf("downloaded package does not declare %s support", expectedPlatform)
}

func isTopLevelAppInfoPlist(path string) bool {
	parts := strings.Split(path, "/")

	return len(parts) == 3 && parts[0] == "Payload" && strings.HasSuffix(parts[1], ".app") && parts[2] == "Info.plist"
}

type downloadItemResult struct {
	ArtworkURL string                 `plist:"artworkURL,omitempty"`
	HashMD5    string                 `plist:"md5,omitempty"`
	URL        string                 `plist:"URL,omitempty"`
	Sinfs      []Sinf                 `plist:"sinfs,omitempty"`
	Metadata   map[string]interface{} `plist:"metadata,omitempty"`
}

type downloadResult struct {
	FailureType     string               `plist:"failureType,omitempty"`
	CustomerMessage string               `plist:"customerMessage,omitempty"`
	Items           []downloadItemResult `plist:"songList,omitempty"`
}

//nolint:nonamedreturns // Deferred close errors must propagate to callers.
func (t *appstore) downloadFile(ctx context.Context, src, dst string, progress *progressbar.ProgressBar) (err error) {
	req, err := t.httpClient.NewRequest("GET", src, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if req != nil {
		req = req.WithContext(ctx)
	}

	file, err := t.os.OpenFile(dst, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = joinCleanupError(err, "failed to close downloaded file", closeErr)
		}
	}()

	stat, err := t.os.Stat(dst)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	if req != nil && stat != nil {
		req.Header.Add("range", fmt.Sprintf("bytes=%d-", stat.Size()))
	}

	res, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	offset, remaining, total, complete, err := downloadResponseRange(res, stat.Size())
	if err != nil {
		return err
	}

	if complete {
		return nil
	}

	if res.StatusCode == gohttp.StatusOK {
		if err := file.Truncate(0); err != nil {
			return fmt.Errorf("failed to restart download: %w", err)
		}
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("can not seek file: %w", err)
	}

	var writer io.Writer = file

	if progress != nil {
		progress.ChangeMax64(total)

		if err := progress.Set64(offset); err != nil {
			return fmt.Errorf("can not set bar progress: %w", err)
		}

		writer = io.MultiWriter(file, progress)
	}

	var body io.Reader = res.Body
	if remaining >= 0 {
		body = io.LimitReader(body, remaining)
	}

	written, err := io.Copy(writer, body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if remaining >= 0 && written != remaining || total >= 0 && offset+written != total {
		return fmt.Errorf("download is incomplete: %w", io.ErrUnexpectedEOF)
	}

	return nil
}

func fileName(app App, version string) string {
	return packageFileName(app, version, "")
}

func packageFileName(app App, version string, platform Platform) string {
	var parts []string

	if app.BundleID != "" {
		parts = append(parts, app.BundleID)
	}

	if app.ID != 0 {
		parts = append(parts, strconv.FormatInt(app.ID, 10))
	}

	if version != "" {
		parts = append(parts, version)
	}

	extension := "ipa"
	if platform == PlatformMacOS {
		extension = "pkg"
	}

	return fmt.Sprintf("%s.%s", strings.Join(parts, "_"), extension)
}

func (t *appstore) resolveDestinationPath(app App, version string, path string, platform Platform) (string, error) {
	file := packageFileName(app, version, platform)

	if path == "" {
		workdir, err := t.os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}

		return fmt.Sprintf("%s/%s", workdir, file), nil
	}

	isDir, err := t.isDirectory(path)
	if err != nil {
		return "", fmt.Errorf("failed to determine whether path is a directory: %w", err)
	}

	if isDir {
		return filepath.Join(path, file), nil
	}

	return path, nil
}

func (t *appstore) isDirectory(path string) (bool, error) {
	info, err := t.os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to read file metadata: %w", err)
	}

	if info == nil {
		return false, nil
	}

	return info.IsDir(), nil
}

func (t *appstore) applyPatches(item downloadItemResult, acc Account, src, dst string, artwork []byte) error {
	srcZip, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open zip reader: %w", err)
	}
	defer srcZip.Close()

	dstFile, err := t.os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer dstFile.Close()

	dstZip := zip.NewWriter(dstFile)
	defer dstZip.Close()

	err = t.replicateZip(srcZip, dstZip, src)
	if err != nil {
		return fmt.Errorf("failed to replicate zip: %w", err)
	}

	if len(artwork) != 0 {
		file, err := dstZip.Create("iTunesArtwork")
		if err != nil {
			return fmt.Errorf("failed to create artwork: %w", err)
		}

		if _, err := file.Write(artwork); err != nil {
			return fmt.Errorf("failed to write artwork: %w", err)
		}
	}

	err = t.writeMetadata(item.Metadata, acc, dstZip)
	if err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

func (t *appstore) writeMetadata(metadata map[string]interface{}, acc Account, zip *zip.Writer) error {
	metadata["apple-id"] = acc.Email
	metadata["userName"] = acc.Email

	metadataFile, err := zip.Create("iTunesMetadata.plist")
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	data, err := plist.Marshal(metadata, plist.BinaryFormat)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	_, err = metadataFile.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}
