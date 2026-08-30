package appstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/blacktop/go-macho/pkg/xar"
	"github.com/schollz/progressbar/v3"
)

const (
	macDPInfoSuffix         = ".dpInfo"
	macHWInfoSuffix         = ".hwInfo"
	macEncryptedStageSuffix = ".ipatool-encrypted"
	macDecryptedStageSuffix = ".ipatool-decrypted"
)

type macPackageDecrypter interface {
	Decrypt(ctx context.Context, dst io.Writer, src io.Reader) (int64, error)
	Close() error
}

type macPackageDecrypterFactory func(ctx context.Context, hardwareID, dpInfo []byte) (macPackageDecrypter, error)

func (t *appstore) downloadMacPackage(
	ctx context.Context,
	item downloadItemResult,
	destination string,
	hardwareID []byte,
	progress *progressbar.ProgressBar,
) (_ DownloadOutput, err error) {
	dpInfo, err := macDPInfo(item.Sinfs)
	if err != nil {
		return DownloadOutput{}, err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	encryptedPath := destination + macEncryptedStageSuffix
	decryptedPath := destination + macDecryptedStageSuffix
	stagePaths := []string{encryptedPath, decryptedPath}
	published := false

	if err := t.cleanupMacPaths(stagePaths...); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to prepare macOS download staging: %w", err)
	}

	defer func() {
		cleanupErr := t.cleanupMacPaths(stagePaths...)
		if !published && cleanupErr != nil {
			err = joinCleanupError(err, "failed to clean up macOS download staging", cleanupErr)
		}
	}()

	if err := t.downloadFile(ctx, item.URL, encryptedPath, progress); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to download file: %w", err)
	}

	factory := t.macDecrypterFactory
	if factory == nil {
		factory = defaultMacPackageDecrypterFactory
	}

	decrypter, err := factory(ctx, append([]byte(nil), hardwareID...), append([]byte(nil), dpInfo...))
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to initialize macOS package decrypter: %w", err)
	}

	if err := t.decryptMacPackage(ctx, decrypter, encryptedPath, decryptedPath); err != nil {
		return DownloadOutput{}, err
	}

	if err := t.validateMacPackage(decryptedPath); err != nil {
		return DownloadOutput{}, fmt.Errorf("failed to validate decrypted macOS package: %w", err)
	}

	if err := t.publishMacPackage(decryptedPath, destination); err != nil {
		return DownloadOutput{}, err
	}

	published = true

	// The validated package is already published at this point. Legacy sidecars
	// are obsolete metadata, so their cleanup must not turn a successful package
	// replacement into an error.
	_ = t.removeLegacyMacSidecars(destination)

	return DownloadOutput{DestinationPath: destination}, nil
}

func (t *appstore) decryptMacPackage(ctx context.Context, decrypter macPackageDecrypter, encryptedPath, decryptedPath string) (err error) {
	decrypterClosed := false

	var source *os.File

	var destination *os.File

	defer func() {
		if destination != nil {
			if closeErr := destination.Close(); closeErr != nil {
				err = joinCleanupError(err, "failed to close decrypted macOS package staging", closeErr)
			}
		}

		if source != nil {
			if closeErr := source.Close(); closeErr != nil {
				err = joinCleanupError(err, "failed to close encrypted macOS package", closeErr)
			}
		}

		if !decrypterClosed {
			if closeErr := decrypter.Close(); closeErr != nil {
				err = joinCleanupError(err, "failed to close macOS package decrypter", closeErr)
			}
		}
	}()

	source, err = t.os.OpenFile(encryptedPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open encrypted macOS package: %w", err)
	}

	destination, err = t.os.OpenFile(decryptedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open decrypted macOS package staging: %w", err)
	}

	_, decryptErr := decrypter.Decrypt(ctx, destination, source)
	destinationCloseErr := destination.Close()
	destination = nil
	sourceCloseErr := source.Close()
	source = nil
	decrypterCloseErr := decrypter.Close()
	decrypterClosed = true

	if decryptErr != nil {
		err = fmt.Errorf("failed to decrypt macOS package: %w", decryptErr)
	}

	if destinationCloseErr != nil {
		err = joinCleanupError(err, "failed to close decrypted macOS package staging", destinationCloseErr)
	}

	if sourceCloseErr != nil {
		err = joinCleanupError(err, "failed to close encrypted macOS package", sourceCloseErr)
	}

	if decrypterCloseErr != nil {
		err = joinCleanupError(err, "failed to close macOS package decrypter", decrypterCloseErr)
	}

	return err
}

func (t *appstore) validateMacPackage(path string) (err error) {
	file, err := t.os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open decrypted macOS package: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = joinCleanupError(err, "failed to close decrypted macOS package", closeErr)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to inspect decrypted macOS package: %w", err)
	}

	if info.Size() == 0 {
		return errors.New("decrypted package is empty")
	}

	archive, err := xar.NewReader(file, info.Size())
	if err != nil {
		return fmt.Errorf("failed to parse XAR archive: %w", err)
	}

	if len(archive.Files) == 0 {
		return errors.New("XAR archive does not contain files")
	}

	for _, entry := range archive.Files {
		if !entry.VerifyChecksum() {
			return fmt.Errorf("XAR entry %q failed checksum validation", entry.Name)
		}
	}

	return nil
}

func macDPInfo(sinfs []Sinf) ([]byte, error) {
	var dpInfo []byte

	for _, sinf := range sinfs {
		if len(sinf.DPInfo) == 0 {
			continue
		}

		if dpInfo != nil && !bytes.Equal(dpInfo, sinf.DPInfo) {
			return nil, errors.New("download response contains conflicting dpInfo values")
		}

		dpInfo = sinf.DPInfo
	}

	if len(dpInfo) == 0 {
		return nil, errors.New("download response does not contain dpInfo")
	}

	return append([]byte(nil), dpInfo...), nil
}

func (t *appstore) cleanupMacPaths(paths ...string) error {
	var cleanupErr error

	for _, path := range paths {
		if err := t.os.Remove(path); err != nil && !t.os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failed to remove %s: %w", path, err))
		}
	}

	return cleanupErr
}

func (t *appstore) removeLegacyMacSidecars(destination string) error {
	return t.cleanupMacPaths(destination+macDPInfoSuffix, destination+macHWInfoSuffix)
}
