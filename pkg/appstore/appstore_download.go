package appstore

import (
	"archive/zip"
	"bytes"
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/util"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"howett.net/plist"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DownloadSinfResult struct {
	ID   int64  `plist:"id,omitempty"`
	Data []byte `plist:"sinf,omitempty"`
}

type DownloadItemResult struct {
	HashMD5  string                 `plist:"md5,omitempty"`
	URL      string                 `plist:"URL,omitempty"`
	Sinfs    []DownloadSinfResult   `plist:"sinfs,omitempty"`
	Metadata map[string]interface{} `plist:"metadata,omitempty"`
}

type DownloadResult struct {
	FailureType     string               `plist:"failureType,omitempty"`
	CustomerMessage string               `plist:"customerMessage,omitempty"`
	Items           []DownloadItemResult `plist:"songList,omitempty"`
}

type PackageManifest struct {
	SinfPaths []string `plist:"SinfPaths,omitempty"`
}

type PackageInfo struct {
	BundleExecutable string `plist:"CFBundleExecutable,omitempty"`
}

func (a *appstore) Download(bundleOrAppID any, outputPath string, acquireLicense bool, skipExisting bool, dryRun bool) error {
	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrGetAccount.Error())
	}

	var appID int64
	if val, ok := bundleOrAppID.(int64); ok {
		appID = val
	} else {
		countryCode, err := a.countryCodeFromStoreFront(acc.StoreFront)
		if err != nil {
			return errors.Wrap(err, ErrInvalidCountryCode.Error())
		}
		app, err := a.Lookup(bundleOrAppID, countryCode)
		if err != nil {
			return errors.Wrap(err, ErrAppLookup.Error())
		}
		if app.Price > 0 {
			return ErrPaidApp
		}
		appID = app.ID
	}

	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, ErrGetMAC.Error())
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Verbose().Str("mac", macAddr).Str("guid", guid).Send()

	item, err := a.downloadItem(acc, appID, outputPath, guid, acquireLicense, true, false)
	if err != nil {
		return errors.Wrap(err, ErrDownloadFile.Error())
	}

	// bundleShortVersionString is optional, but if present, it is the public-facing version
	// these versions may not match the iTunes version, and there's no way to fetch the latter for unlisted apps
	versionString := item.Metadata["bundleVersion"].(string)
	if val, ok := item.Metadata["bundleShortVersionString"]; ok {
		versionString = val.(string)
	}
	app := App{
		ID:       appID,
		BundleID: item.Metadata["softwareVersionBundleId"].(string),
		// there is also bundleDisplayName, but this matches lookup name
		Name:    item.Metadata["itemName"].(string),
		Version: versionString,
		Price:   0,
	}

	dst, err := a.resolveDestinationPath(app, outputPath)
	if err != nil {
		return errors.Wrap(err, ErrResolveDestinationPath.Error())
	}

	if _, err := os.Stat(dst); err == nil {
		if skipExisting {
			a.logger.Log().Str("output", dst).Bool("success", true).Msg("already exists")
			return errors.Wrap(err, ErrResolveDestinationPath.Error())
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap(err, ErrResolveDestinationPath.Error())
	}

	if !dryRun {
		err = a.downloadFile(fmt.Sprintf("%s.tmp", dst), item.URL)
		if err != nil {
			return errors.Wrap(err, ErrDownloadFile.Error())
		}

		err = a.applyPatches(item, acc, fmt.Sprintf("%s.tmp", dst), dst)
		if err != nil {
			return errors.Wrap(err, ErrPatchApp.Error())
		}

		err = a.os.Remove(fmt.Sprintf("%s.tmp", dst))
		if err != nil {
			return errors.Wrap(err, ErrRemoveTempFile.Error())
		}
	}

	a.logger.Log().Str("output", dst).Bool("success", true).Send()

	return nil
}

func (a *appstore) downloadItem(acc Account, appID int64, dst, guid string, acquireLicense, attemptToRenewCredentials, dryRun bool) (DownloadItemResult, error) {
	req := a.downloadRequest(acc, appID, guid)

	res, err := a.downloadClient.Send(req)
	if err != nil {
		return DownloadItemResult{}, errors.Wrap(err, ErrRequest.Error())
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		if attemptToRenewCredentials {
			a.logger.Verbose().Msg("retrieving new password token")
			acc, err = a.login(acc.Email, acc.Password, "", guid, 0, true)
			if err != nil {
				return DownloadItemResult{}, errors.Wrap(err, ErrPasswordTokenExpired.Error())
			}

			return a.downloadItem(acc, appID, dst, guid, acquireLicense, false, dryRun)
		}

		return DownloadItemResult{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound && acquireLicense {
		a.logger.Verbose().Msg("attempting to acquire license")
		err = a.purchase(acc, appID, guid, true)
		if err != nil {
			return DownloadItemResult{}, errors.Wrap(err, ErrPurchase.Error())
		}

		return a.downloadItem(acc, appID, dst, guid, false, attemptToRenewCredentials, dryRun)
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return DownloadItemResult{}, ErrLicenseRequired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		a.logger.Verbose().Interface("response", res).Send()
		return DownloadItemResult{}, errors.New(res.Data.CustomerMessage)
	}

	if res.Data.FailureType != "" {
		a.logger.Verbose().Interface("response", res).Send()
		return DownloadItemResult{}, ErrGeneric
	}

	if len(res.Data.Items) == 0 {
		a.logger.Verbose().Interface("response", res).Send()
		return DownloadItemResult{}, ErrInvalidResponse
	}

	return res.Data.Items[0], nil
}

func (a *appstore) downloadFile(dst, sourceURL string) (err error) {
	req, err := a.httpClient.NewRequest("GET", sourceURL, nil)
	if err != nil {
		return errors.Wrap(err, ErrCreateRequest.Error())
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, ErrRequest.Error())
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != err && err == nil {
			err = closeErr
		}
	}()

	file, err := a.os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrap(err, ErrOpenFile.Error())
	}

	defer func() {
		if closeErr := file.Close(); closeErr != err && err == nil {
			err = closeErr
		}
	}()

	sizeMB := float64(res.ContentLength) / (1 << 20)
	a.logger.Verbose().Str("size", fmt.Sprintf("%.2fMB", sizeMB)).Msg("downloading")

	if a.interactive {
		bar := progressbar.NewOptions64(res.ContentLength,
			progressbar.OptionSetDescription("downloading"),
			progressbar.OptionSetWriter(os.Stdout),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(20),
			progressbar.OptionFullWidth(),
			progressbar.OptionThrottle(65*time.Millisecond),
			progressbar.OptionShowCount(),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionSpinnerType(14),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetElapsedTime(false),
			progressbar.OptionSetPredictTime(false),
		)

		_, err = io.Copy(io.MultiWriter(file, bar), res.Body)
	} else {
		_, err = io.Copy(file, res.Body)
	}

	if err != nil {
		return errors.Wrap(err, ErrFileWrite.Error())
	}

	return nil
}

func (*appstore) downloadRequest(acc Account, appID int64, guid string) http.Request {
	host := fmt.Sprintf("%s-%s", PriavteAppStoreAPIDomainPrefixWithoutAuthCode, PrivateAppStoreAPIDomain)
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
				"salableAdamId": appID,
			},
		},
	}
}

func (a *appstore) resolveDestinationPath(app App, path string) (string, error) {
	file := app.GetIPAName()

	if path == "" {
		workdir, err := a.os.Getwd()
		if err != nil {
			return "", errors.Wrap(err, ErrGetCurrentDirectory.Error())
		}

		return fmt.Sprintf("%s/%s", workdir, file), nil
	}

	isDir, err := a.isDirectory(path)
	if err != nil {
		return "", errors.Wrap(err, ErrCheckDirectory.Error())
	}

	if isDir {
		return fmt.Sprintf("%s/%s", path, file), nil
	}

	return path, nil
}

func (a *appstore) isDirectory(path string) (bool, error) {
	info, err := a.os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return false, errors.Wrap(err, ErrGetFileMetadata.Error())
	}

	if info == nil {
		return false, nil
	}

	return info.IsDir(), nil
}

func (a *appstore) applyPatches(item DownloadItemResult, acc Account, src, dst string) (err error) {
	dstFile, err := a.os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrap(err, ErrOpenFile.Error())
	}

	srcZip, err := zip.OpenReader(src)
	if err != nil {
		return errors.Wrap(err, ErrOpenZipFile.Error())
	}
	defer func() {
		if closeErr := srcZip.Close(); closeErr != err && err == nil {
			err = closeErr
		}
	}()

	dstZip := zip.NewWriter(dstFile)
	defer func() {
		if closeErr := dstZip.Close(); closeErr != err && err == nil {
			err = closeErr
		}
	}()

	manifestData := new(bytes.Buffer)
	infoData := new(bytes.Buffer)

	appBundle, err := a.replicateZip(srcZip, dstZip, infoData, manifestData)
	if err != nil {
		return errors.Wrap(err, ErrReplicateZip.Error())
	}

	err = a.writeMetadata(item.Metadata, acc, dstZip)
	if err != nil {
		return errors.Wrap(err, ErrWriteMetadataFile.Error())
	}

	if manifestData.Len() > 0 {
		err = a.applySinfPatches(item, dstZip, manifestData.Bytes(), appBundle)
		if err != nil {
			return errors.Wrap(err, ErrApplyPatches.Error())
		}
	} else {
		err = a.applyLegacySinfPatches(item, dstZip, infoData.Bytes(), appBundle)
		if err != nil {
			return errors.Wrap(err, ErrApplyLegacyPatches.Error())
		}
	}

	return nil
}

func (a *appstore) writeMetadata(metadata map[string]interface{}, acc Account, zip *zip.Writer) error {
	metadata["apple-id"] = acc.Email
	metadata["userName"] = acc.Email

	metadataFile, err := zip.Create("iTunesMetadata.plist")
	if err != nil {
		return errors.Wrap(err, ErrCreateMetadataFile.Error())
	}

	data, err := plist.Marshal(metadata, plist.BinaryFormat)
	if err != nil {
		return errors.Wrap(err, ErrEncodeMetadataFile.Error())
	}

	_, err = metadataFile.Write(data)
	if err != nil {
		return errors.Wrap(err, ErrWriteMetadataFile.Error())
	}

	return nil
}

func (a *appstore) replicateZip(src *zip.ReadCloser, dst *zip.Writer, info *bytes.Buffer, manifest *bytes.Buffer) (appBundle string, err error) {
	for _, file := range src.File {
		srcFile, err := file.OpenRaw()
		if err != nil {
			return "", errors.Wrap(err, ErrOpenFile.Error())
		}

		if strings.HasSuffix(file.Name, ".app/SC_Info/Manifest.plist") {
			srcFileD, err := file.Open()
			if err != nil {
				return "", errors.Wrap(err, ErrDecompressManifestFile.Error())
			}

			_, err = io.Copy(manifest, srcFileD)
			if err != nil {
				return "", errors.Wrap(err, ErrGetManifestFile.Error())
			}
		}

		if strings.Contains(file.Name, ".app/Info.plist") {
			srcFileD, err := file.Open()
			if err != nil {
				return "", errors.Wrap(err, ErrDecompressInfoFile.Error())
			}

			if !strings.Contains(file.Name, "/Watch/") {
				appBundle = filepath.Base(strings.TrimSuffix(file.Name, ".app/Info.plist"))
			}

			_, err = io.Copy(info, srcFileD)
			if err != nil {
				return "", errors.Wrap(err, ErrGetInfoFile.Error())
			}
		}

		header := file.FileHeader
		dstFile, err := dst.CreateRaw(&header)
		if err != nil {
			return "", errors.Wrap(err, ErrCreateDestinationFile.Error())
		}

		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			return "", errors.Wrap(err, ErrFileWrite.Error())
		}
	}

	if appBundle == "" {
		return "", ErrGetBundleName
	}

	return appBundle, nil
}

func (a *appstore) applySinfPatches(item DownloadItemResult, zip *zip.Writer, manifestData []byte, appBundle string) error {
	var manifest PackageManifest
	_, err := plist.Unmarshal(manifestData, &manifest)
	if err != nil {
		return errors.Wrap(err, ErrUnmarshal.Error())
	}

	zipped, err := util.Zip(item.Sinfs, manifest.SinfPaths)
	if err != nil {
		return errors.Wrap(err, ErrZipSinfs.Error())
	}

	for _, pair := range zipped {
		sp := fmt.Sprintf("Payload/%s.app/%s", appBundle, pair.Second)
		a.logger.Verbose().Str("path", sp).Msg("writing sinf data")

		file, err := zip.Create(sp)
		if err != nil {
			return errors.Wrap(err, ErrCreateSinfFile.Error())
		}

		_, err = file.Write(pair.First.Data)
		if err != nil {
			return errors.Wrap(err, ErrWriteSinfData.Error())
		}
	}

	return nil
}

func (a *appstore) applyLegacySinfPatches(item DownloadItemResult, zip *zip.Writer, infoData []byte, appBundle string) error {
	a.logger.Verbose().Msg("applying legacy sinf patches")

	var info PackageInfo
	_, err := plist.Unmarshal(infoData, &info)
	if err != nil {
		return errors.Wrap(err, ErrUnmarshal.Error())
	}

	sp := fmt.Sprintf("Payload/%s.app/SC_Info/%s.sinf", appBundle, info.BundleExecutable)
	a.logger.Verbose().Str("path", sp).Msg("writing sinf data")

	file, err := zip.Create(sp)
	if err != nil {
		return errors.Wrap(err, ErrCreateSinfFile.Error())
	}

	_, err = file.Write(item.Sinfs[0].Data)
	if err != nil {
		return errors.Wrap(err, ErrWriteSinfData.Error())
	}

	return nil
}
