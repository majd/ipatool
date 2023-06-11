## Changelog

### Version [2.1.3](https://github.com/majd/ipatool/releases/tag/v2.1.3)

- Fixed a bug where temporary files were not removed after downloading the app package.
- Fixed a bug where the `--keychain-passphrase` flag was marked as invalid.

### Version [2.1.2](https://github.com/majd/ipatool/releases/tag/v2.1.2)

- `FileBackend` keyring is now used when other options are not available.

### Version [2.1.1](https://github.com/majd/ipatool/releases/tag/v2.1.1)

- Fixed an issue when creating the config directory for the tool.

### Version [2.1.0](https://github.com/majd/ipatool/releases/tag/v2.1.0)

- Implemented `Lookup` API.
- Implemented `AccountInfo` API.
- Implemented `ReplicateSinf` API.
- Added storefront code for Georgia (GE).
- Build version is now set using linker flags.
- Refactored API interfaces.
- Improved tests for http package.
- Integrated `golangci-lint` linter.
- Added v2 suffix to module path.

### Version [2.0.3](https://github.com/majd/ipatool/releases/tag/v2.0.3)

- Added support for downloading Apple Arcade games.
- Fixed windows builds not having exe extension.


### Version [2.0.2](https://github.com/majd/ipatool/releases/tag/v2.0.2)

- Fixed Sinf patches when the app contains an Apple Watch app.
- Added storefront code for Albania.
- Fixed default output path to be current working directory.

### Version [2.0.1](https://github.com/majd/ipatool/releases/tag/v2.0.1)

- Linux & Windows releases now include the sha256 hash.
- Added storefront code for Mongolia.

### Version [2.0.0](https://github.com/majd/ipatool/releases/tag/v2.0.0)

- Added support for Windows.
- Added support for Linux.
- Added support for generating autocompletion script using the `completion` command.
- Implemented new `auth info` command.
- Implemented `--verbose` flag that replaces the `--debug-level` flag to enable verbose logging.
- Implemented `--format` which allows specifying logs output format to either text or json (default: text).
- Implemented `--non-interactive` flag to disable running the tool in an interactive session.
- The relevant command (i.e. purchase) will now automatically determine the country and the device family from the authenticated account. The following flags have been deprecated.
    - `--country`
    - `--device-family`
- Improved structured logging.
- Improved error handling.
- Improved support for automated systems.
- Added unit tests to cover the majority of the private App Store API logic.

### Version [1.1.4](https://github.com/majd/ipatool/releases/tag/v1.1.4)

- Add support for patching old code signature revisions as a fallback mechanism.

### Version [1.1.3](https://github.com/majd/ipatool/releases/tag/v1.1.3)

- Fixed keychain access on iOS.

### Version [1.1.2](https://github.com/majd/ipatool/releases/tag/v1.1.2)

- Improved error message for expired token.
- Disabled print buffering and output errors to stderr.
- Added linter.
- Add support for running on iOS.

### Version [1.1.1](https://github.com/majd/ipatool/releases/tag/v1.1.1)

- Update swift-argument-parser dependency.
- Remove usage of Swift concurrency.
- Add backward-compatibility for macOS 10.11+.

### Version [1.1.0](https://github.com/majd/ipatool/releases/tag/v1.1.0)

- Added support for purchasing apps.
- Implemented auth command.
- Implemented purchase command.
- Implemented --purchase flag in download command.
- Added price checks for purchase flow.


### Version [1.0.9](https://github.com/majd/ipatool/releases/tag/v1.0.9)

- Fixed building for older macOS versions.
- Fixed 2FA code not being requested.


### Version [1.0.8](https://github.com/majd/ipatool/releases/tag/v1.0.8)

- Added --output option to the download command for specifying the destination path for the downloaded app package.

### Version [1.0.7](https://github.com/majd/ipatool/releases/tag/v1.0.7)

- Fixed login requests to the store API when the 2FA code is required.

### Version [1.0.6](https://github.com/majd/ipatool/releases/tag/v1.0.6)

- Added support for supplying 2FA code in non-interactive sessions.

### Version [1.0.5](https://github.com/majd/ipatool/releases/tag/v1.0.5)

- Added support for specifying device family.

### Version [1.0.4](https://github.com/majd/ipatool/releases/tag/v1.0.4)

- Added support for specifying the iTunes Store region.

### Version [1.0.3](https://github.com/majd/ipatool/releases/tag/v1.0.3)

- Improved console logging.

### Version [1.0.2](https://github.com/majd/ipatool/releases/tag/v1.0.2)

- Improved error messages.

### Version [1.0.1](https://github.com/majd/ipatool/releases/tag/v1.0.1)

- Grammatical fixes.

### Version [1.0.0](https://github.com/majd/ipatool/releases/tag/v1.0.0)

- Initial release.
