package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
)

// prepareStateDirectory resolves and migrates session storage before it is opened.
func prepareStateDirectory(os operatingsystem.OperatingSystem, homeDirectory string) (string, error) {
	legacyDirectory := filepath.Join(homeDirectory, ConfigDirectoryName)
	stateDirectory := legacyDirectory

	for _, variable := range []string{"XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if base := os.Getenv(variable); filepath.IsAbs(base) {
			stateDirectory = filepath.Join(base, "ipatool")

			break
		}
	}

	info, err := os.Stat(legacyDirectory)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("could not read legacy state directory metadata: %w", err)
	}

	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("legacy state path is not a directory: %s", legacyDirectory)
		}
		// Never merge sessions or move a directory into itself. Keeping the legacy
		// directory also preserves authentication when migration is unavailable.
		relative, err := filepath.Rel(legacyDirectory, stateDirectory)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return legacyDirectory, nil
		}

		if _, err := os.Stat(stateDirectory); err == nil || !os.IsNotExist(err) {
			return legacyDirectory, nil
		}

		if err := os.MkdirAll(filepath.Dir(stateDirectory), 0700); err != nil {
			return legacyDirectory, nil
		}
		// Rename keeps cookies and encrypted credentials together, with their
		// existing permissions. Cross-filesystem moves fall back to legacy storage.
		if err := os.Rename(legacyDirectory, stateDirectory); err != nil {
			return legacyDirectory, nil
		}

		return stateDirectory, nil
	}

	if err := os.MkdirAll(stateDirectory, 0700); err != nil {
		return "", fmt.Errorf("failed to create state directory: %w", err)
	}

	return stateDirectory, nil
}
