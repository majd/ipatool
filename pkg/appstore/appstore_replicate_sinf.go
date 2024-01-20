package appstore

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/majd/ipatool/v2/pkg/util"
	"howett.net/plist"
)

type Sinf struct {
	ID   int64  `plist:"id,omitempty"`
	Data []byte `plist:"sinf,omitempty"`
}

type ReplicateSinfInput struct {
	Sinfs       []Sinf
	PackagePath string
}

func (t *appstore) ReplicateSinf(input ReplicateSinfInput) error {
	zipReader, err := zip.OpenReader(input.PackagePath)
	if err != nil {
		return errors.New("failed to open zip reader")
	}

	tmpPath := fmt.Sprintf("%s.tmp", input.PackagePath)
	tmpFile, err := t.os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	zipWriter := zip.NewWriter(tmpFile)

	err = t.replicateZip(zipReader, zipWriter)
	if err != nil {
		return fmt.Errorf("failed to replicate zip: %w", err)
	}

	bundleName, err := t.readBundleName(zipReader)
	if err != nil {
		return fmt.Errorf("failed to read bundle name: %w", err)
	}

	manifest, err := t.readManifestPlist(zipReader)
	if err != nil {
		return fmt.Errorf("failed to read manifest plist: %w", err)
	}

	info, err := t.readInfoPlist(zipReader)
	if err != nil {
		return fmt.Errorf("failed to read info plist: %w", err)
	}

	if manifest != nil {
		err = t.replicateSinfFromManifest(*manifest, zipWriter, input.Sinfs, bundleName)
	} else {
		err = t.replicateSinfFromInfo(*info, zipWriter, input.Sinfs, bundleName)
	}

	if err != nil {
		return fmt.Errorf("failed to replicate sinf: %w", err)
	}

	zipReader.Close()
	zipWriter.Close()
	tmpFile.Close()

	err = t.os.Remove(input.PackagePath)
	if err != nil {
		return fmt.Errorf("failed to remove original file: %w", err)
	}

	err = t.os.Rename(tmpPath, input.PackagePath)
	if err != nil {
		return fmt.Errorf("failed to remove original file: %w", err)
	}

	return nil
}

type packageManifest struct {
	SinfPaths []string `plist:"SinfPaths,omitempty"`
}

type packageInfo struct {
	BundleExecutable string `plist:"CFBundleExecutable,omitempty"`
}

func (*appstore) replicateSinfFromManifest(manifest packageManifest, zip *zip.Writer, sinfs []Sinf, bundleName string) error {
	zipped, err := util.Zip(sinfs, manifest.SinfPaths)
	if err != nil {
		return fmt.Errorf("failed to zip sinfs: %w", err)
	}

	for _, pair := range zipped {
		sp := fmt.Sprintf("Payload/%s.app/%s", bundleName, pair.Second)

		file, err := zip.Create(sp)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		_, err = file.Write(pair.First.Data)
		if err != nil {
			return fmt.Errorf("failed to write data: %w", err)
		}
	}

	return nil
}

func (t *appstore) replicateSinfFromInfo(info packageInfo, zip *zip.Writer, sinfs []Sinf, bundleName string) error {
	sp := fmt.Sprintf("Payload/%s.app/SC_Info/%s.sinf", bundleName, info.BundleExecutable)

	file, err := zip.Create(sp)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	_, err = file.Write(sinfs[0].Data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

func (t *appstore) replicateZip(src *zip.ReadCloser, dst *zip.Writer) error {
	for _, file := range src.File {
		srcFile, err := file.OpenRaw()
		if err != nil {
			return fmt.Errorf("failed to open raw file: %w", err)
		}

		header := file.FileHeader
		dstFile, err := dst.CreateRaw(&header)

		if err != nil {
			return fmt.Errorf("failed to create raw file: %w", err)
		}

		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
	}

	return nil
}

func (*appstore) readInfoPlist(reader *zip.ReadCloser) (*packageInfo, error) {
	for _, file := range reader.File {
		if strings.Contains(file.Name, ".app/Info.plist") {
			src, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file: %w", err)
			}

			data := new(bytes.Buffer)
			_, err = io.Copy(data, src)

			if err != nil {
				return nil, fmt.Errorf("failed to copy data: %w", err)
			}

			var info packageInfo
			_, err = plist.Unmarshal(data.Bytes(), &info)

			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal data: %w", err)
			}

			return &info, nil
		}
	}

	return nil, nil
}

func (*appstore) readManifestPlist(reader *zip.ReadCloser) (*packageManifest, error) {
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".app/SC_Info/Manifest.plist") {
			src, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file: %w", err)
			}

			data := new(bytes.Buffer)
			_, err = io.Copy(data, src)

			if err != nil {
				return nil, fmt.Errorf("failed to copy data: %w", err)
			}

			var manifest packageManifest

			_, err = plist.Unmarshal(data.Bytes(), &manifest)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal data: %w", err)
			}

			return &manifest, nil
		}
	}

	return nil, nil
}

func (*appstore) readBundleName(reader *zip.ReadCloser) (string, error) {
	var bundleName string

	for _, file := range reader.File {
		if strings.Contains(file.Name, ".app/Info.plist") && !strings.Contains(file.Name, "/Watch/") {
			bundleName = filepath.Base(strings.TrimSuffix(file.Name, ".app/Info.plist"))

			break
		}
	}

	if bundleName == "" {
		return "", errors.New("could not read bundle name")
	}

	return bundleName, nil
}
