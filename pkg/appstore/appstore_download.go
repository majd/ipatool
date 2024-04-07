package appstore

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/schollz/progressbar/v3"
	"howett.net/plist"
)

var (
	ErrLicenseRequired = errors.New("license is required")
)

type DownloadInput struct {
	Account    Account
	App        App
	OutputPath string
	Progress   *progressbar.ProgressBar
}

type DownloadOutput struct {
	DestinationPath string
	Sinfs           []Sinf
}

func (t *appstore) Download(input DownloadInput) (DownloadOutput, error) {
	destination, err := t.resolveDestinationPath(input.App, input.OutputPath)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to resolve destination path: %w", err)
	}

	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	req := t.downloadRequest(input.Account, input.App, guid)

	res, err := t.downloadClient.Send(req)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		return DownloadOutput{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return DownloadOutput{}, ErrLicenseRequired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		return DownloadOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return DownloadOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.FailureType), res)
	}

	if len(res.Data.Items) == 0 {
		return DownloadOutput{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	}

	item := res.Data.Items[0]

	err = t.downloadFile(item.URL, fmt.Sprintf("%s.tmp", destination), input.Progress)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to download file: %w", err)
	}

	err = t.applyPatches(item, input.Account, fmt.Sprintf("%s.tmp", destination), destination)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to apply patches: %w", err)
	}

	err = t.os.Remove(fmt.Sprintf("%s.tmp", destination))
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to remove file: %w", err)
	}

	return DownloadOutput{
		DestinationPath: destination,
		Sinfs:           item.Sinfs,
	}, nil
}

type downloadItemResult struct {
	HashMD5  string                 `plist:"md5,omitempty"`
	URL      string                 `plist:"URL,omitempty"`
	Sinfs    []Sinf                 `plist:"sinfs,omitempty"`
	Metadata map[string]interface{} `plist:"metadata,omitempty"`
}

type downloadResult struct {
	FailureType     string               `plist:"failureType,omitempty"`
	CustomerMessage string               `plist:"customerMessage,omitempty"`
	Items           []downloadItemResult `plist:"songList,omitempty"`
}

func (t *appstore) downloadFile(src, dst string, progress *progressbar.ProgressBar) error {
	req, err := t.httpClient.NewRequest("GET", src, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	file, err := t.os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer file.Close()

	if progress != nil {
		progress.ChangeMax64(res.ContentLength)
		_, err = io.Copy(io.MultiWriter(file, progress), res.Body)
	} else {
		_, err = io.Copy(file, res.Body)
	}

	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (*appstore) downloadRequest(acc Account, app App, guid string) http.Request {
	host := fmt.Sprintf("%s-%s", PrivateAppStoreAPIDomainPrefixWithoutAuthCode, PrivateAppStoreAPIDomain)

	return http.Request{
		URL:            fmt.Sprintf("https://%s%s?guid=%s", host, PrivateAppStoreAPIPathDownload, guid),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"Content-Type": "application/x-apple-plist",
			"iCloud-DSID":  acc.DirectoryServicesID,
			"X-Dsid":       acc.DirectoryServicesID,
		},
		Payload: &http.XMLPayload{
			Content: map[string]interface{}{
				"creditDisplay": "",
				"guid":          guid,
				"salableAdamId": app.ID,
			},
		},
	}
}

func fileName(app App) string {
	var parts []string

	if app.BundleID != "" {
		parts = append(parts, app.BundleID)
	}

	if app.ID != 0 {
		parts = append(parts, strconv.FormatInt(app.ID, 10))
	}

	if app.Version != "" {
		parts = append(parts, app.Version)
	}

	return fmt.Sprintf("%s.ipa", strings.Join(parts, "_"))
}

func (t *appstore) resolveDestinationPath(app App, path string) (string, error) {
	file := fileName(app)

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
		return fmt.Sprintf("%s/%s", path, file), nil
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

func (t *appstore) applyPatches(item downloadItemResult, acc Account, src, dst string) error {
	srcZip, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open zip reader: %w", err)
	}
	defer srcZip.Close()

	dstFile, err := t.os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	dstZip := zip.NewWriter(dstFile)
	defer dstZip.Close()

	err = t.replicateZip(srcZip, dstZip)
	if err != nil {
		return fmt.Errorf("failed to replicate zip: %w", err)
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
