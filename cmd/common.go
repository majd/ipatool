package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/byteness/keyring"
	cookiejar "github.com/juju/persistent-cookiejar"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/log"
	"github.com/majd/ipatool/v2/pkg/util"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var dependencies = Dependencies{}
var keychainPassphrase string

type Dependencies struct {
	Logger    log.Logger
	OS        operatingsystem.OperatingSystem
	Machine   machine.Machine
	CookieJar http.CookieJar
	Keychain  keychain.Keychain
	AppStore  appstore.AppStore
}

// newLogger returns a new logger instance.
func newLogger(format OutputFormat, verbose bool) log.Logger {
	var writer io.Writer

	switch format {
	case OutputFormatJSON:
		writer = zerolog.SyncWriter(os.Stdout)
	case OutputFormatText:
		writer = log.NewWriter()
	}

	return log.NewLogger(log.Args{
		Verbose: verbose,
		Writer:  writer,
	},
	)
}

// newCookieJar returns a new cookie jar instance.
func newCookieJar(machine machine.Machine) http.CookieJar {
	cookiePath := filepath.Join(machine.HomeDirectory(), ConfigDirectoryName, CookieJarFileName)
	removeStaleZeroByteLock(cookiePath + ".lock")
	return util.Must(cookiejar.New(&cookiejar.Options{
		Filename: cookiePath,
	}))
}

// removeStaleZeroByteLock removes a zero-byte lock file left behind when a process
// is killed between O_TRUNC and writing its PID. The juju/go4/lock library only
// performs PID-based stale detection when the file has content.
func removeStaleZeroByteLock(lockPath string) {
	fi, err := os.Stat(lockPath)
	if err != nil || fi.Size() != 0 {
		return
	}

	_ = os.Remove(lockPath)
}

// newKeychain returns a new keychain instance.
func newKeychain(machine machine.Machine, logger log.Logger, interactive bool) keychain.Keychain {
	ring := util.Must(keyring.Open(keyring.Config{
		AllowedBackends: []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.SecretServiceBackend,
			keyring.FileBackend,
		},
		ServiceName: KeychainServiceName,
		FileDir:     filepath.Join(machine.HomeDirectory(), ConfigDirectoryName),
		FilePasswordFunc: func(s string) (string, error) {
			if keychainPassphrase == "" && !interactive {
				return "", errors.New("keychain passphrase is required when not running in interactive mode; use the \"--keychain-passphrase\" flag")
			}

			if keychainPassphrase != "" {
				return keychainPassphrase, nil
			}

			path := strings.Split(s, " unlock ")[1]
			logger.Log().Msgf("enter passphrase to unlock %s (this is separate from your Apple ID password): ", path)
			bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return "", fmt.Errorf("failed to read password: %w", err)
			}

			password := string(bytes)
			password = strings.Trim(password, "\n")
			password = strings.Trim(password, "\r")

			return password, nil
		},
	}))

	return keychain.New(keychain.Args{Keyring: ring})
}

// initWithCommand initializes the dependencies of the command.
func initWithCommand(cmd *cobra.Command) {
	verbose := cmd.Flag("verbose").Value.String() == "true"
	interactive, _ := cmd.Context().Value(interactiveKey).(bool)
	format := util.Must(OutputFormatFromString(cmd.Flag("format").Value.String()))

	dependencies.Logger = newLogger(format, verbose)
	dependencies.OS = operatingsystem.New()
	dependencies.Machine = machine.New(machine.Args{OS: dependencies.OS})
	dependencies.CookieJar = newCookieJar(dependencies.Machine)
	dependencies.Keychain = newKeychain(dependencies.Machine, dependencies.Logger, interactive)
	dependencies.AppStore = appstore.NewAppStore(appstore.Args{
		CookieJar:       dependencies.CookieJar,
		OperatingSystem: dependencies.OS,
		Keychain:        dependencies.Keychain,
		Machine:         dependencies.Machine,
	})

	util.Must("", createConfigDirectory(dependencies.OS, dependencies.Machine))
}

// createConfigDirectory creates the configuration directory for the CLI tool, if needed.
func createConfigDirectory(os operatingsystem.OperatingSystem, machine machine.Machine) error {
	configDirectoryPath := filepath.Join(machine.HomeDirectory(), ConfigDirectoryName)
	_, err := os.Stat(configDirectoryPath)

	if err != nil && os.IsNotExist(err) {
		err = os.MkdirAll(configDirectoryPath, 0700)
		if err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("could not read metadata: %w", err)
	}

	return nil
}
