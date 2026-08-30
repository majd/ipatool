package appstore

import (
	"errors"
	"fmt"
)

const macBackupSuffix = ".ipatool-backup"

func (t *appstore) publishMacPackage(stagedPath, destination string) error {
	backupPath, err := t.backupMacFile(destination)
	if err != nil {
		return fmt.Errorf("failed to preserve existing package: %w", err)
	}

	if err := t.os.Rename(stagedPath, destination); err != nil {
		publishErr := fmt.Errorf("failed to publish package: %w", err)

		return joinMacRollbackError(publishErr, t.restoreMacPackage(destination, backupPath))
	}

	if backupPath != "" {
		_ = t.os.Remove(backupPath)
	}

	return nil
}

func (t *appstore) backupMacFile(path string) (string, error) {
	_, err := t.os.Stat(path)
	if err != nil {
		if t.os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("failed to inspect destination package: %w", err)
	}

	backupPath, err := t.macBackupPath(path)
	if err != nil {
		return "", err
	}

	if err := t.os.Rename(path, backupPath); err != nil {
		return "", fmt.Errorf("failed to back up destination package: %w", err)
	}

	return backupPath, nil
}

func (t *appstore) macBackupPath(path string) (string, error) {
	for index := 0; ; index++ {
		backupPath := path + macBackupSuffix
		if index > 0 {
			backupPath = fmt.Sprintf("%s.%d", backupPath, index)
		}

		_, err := t.os.Stat(backupPath)
		if err != nil {
			if t.os.IsNotExist(err) {
				return backupPath, nil
			}

			return "", fmt.Errorf("failed to inspect package backup path: %w", err)
		}
	}
}

func (t *appstore) restoreMacPackage(destination, backupPath string) error {
	var rollbackErr error

	if err := t.os.Remove(destination); err != nil && !t.os.IsNotExist(err) {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("failed to remove partially published package: %w", err))
	}

	if backupPath != "" {
		if err := t.os.Rename(backupPath, destination); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("failed to restore package: %w", err))
		}
	}

	return rollbackErr
}

func joinMacRollbackError(err, rollbackErr error) error {
	if rollbackErr == nil {
		return err
	}

	return joinCleanupError(err, "failed to restore existing macOS download", rollbackErr)
}
