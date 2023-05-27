package cmd

import (
	"errors"
	"fmt"
	"github.com/99designs/keyring"
	"github.com/juju/persistent-cookiejar"
	"github.com/majd/ipatool/pkg/appstore"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	"github.com/majd/ipatool/pkg/util"
	"github.com/majd/ipatool/pkg/util/machine"
	"github.com/majd/ipatool/pkg/util/operatingsystem"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
	"golang.org/x/term"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var dependencies = Dependencies{}
var keychainPassphrase string

type Dependencies struct {
	Logger             log.Logger
	OS                 operatingsystem.OperatingSystem
	Machine            machine.Machine
	CookieJar          http.CookieJar
	Keychain           keychain.Keychain
	AppStore           appstore.AppStore
	KeyringBackendType keyring.BackendType
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
	})
}

// newCookieJar returns a new cookie jar instance.
func newCookieJar(machine machine.Machine) http.CookieJar {
	path := filepath.Join(machine.HomeDirectory(), ConfigDirectoryName, CookieJarFileName)
	return util.Must(cookiejar.New(&cookiejar.Options{
		Filename: path,
	}))
}

// newKeychain returns a new keychain instance.
func newKeychain(machine machine.Machine, logger log.Logger, backendType keyring.BackendType, interactive bool) keychain.Keychain {
	ring := util.Must(keyring.Open(keyring.Config{
		AllowedBackends: []keyring.BackendType{
			backendType,
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

// keyringBackendType returns the backend type for the keyring.
func keyringBackendType() keyring.BackendType {
	allowedBackends := []keyring.BackendType{
		keyring.KeychainBackend,
		keyring.SecretServiceBackend,
	}

	for _, backend := range allowedBackends {
		if slices.Contains(keyring.AvailableBackends(), backend) {
			return backend
		}
	}

	return keyring.FileBackend
}

// initWithCommand initializes the dependencies of the command.
func initWithCommand(cmd *cobra.Command) {
	verbose := cmd.Flag("verbose").Value.String() == "true"
	interactive, _ := cmd.Context().Value("interactive").(bool)
	format := util.Must(OutputFormatFromString(cmd.Flag("format").Value.String()))

	dependencies.Logger = newLogger(format, verbose)
	dependencies.OS = operatingsystem.New()
	dependencies.Machine = machine.New(machine.Args{OS: dependencies.OS})
	dependencies.CookieJar = newCookieJar(dependencies.Machine)
	dependencies.KeyringBackendType = keyringBackendType()
	dependencies.Keychain = newKeychain(dependencies.Machine, dependencies.Logger, dependencies.KeyringBackendType, interactive)
	dependencies.AppStore = appstore.NewAppStore(appstore.Args{
		CookieJar:       dependencies.CookieJar,
		OperatingSystem: dependencies.OS,
		Keychain:        dependencies.Keychain,
		Machine:         dependencies.Machine,
	})

	util.Must("", createConfigDirectory())
}

// createConfigDirectory creates the configuration directory for the CLI tool, if needed.
func createConfigDirectory() error {
	configDirectoryPath := filepath.Join(dependencies.Machine.HomeDirectory(), ConfigDirectoryName)
	_, err := dependencies.OS.Stat(configDirectoryPath)
	if err != nil && dependencies.OS.IsNotExist(err) {
		err = dependencies.OS.MkdirAll(configDirectoryPath, 0700)
		if err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("could not read metadata: %w", err)
	}

	return nil
}
