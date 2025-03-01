# IPATool

[![Release](https://img.shields.io/github/release/majd/ipatool.svg?label=Release)](https://GitHub.com/majd/ipatool/releases/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/majd/ipatool/blob/main/LICENSE)

`ipatool` is a command line tool that allows you to search for iOS apps on the [App Store](https://apps.apple.com) and download a copy of the app package, known as an _ipa_ file.

![Demo](./demo.gif)

- [Requirements](#requirements)
- [Installation](#installation)
  - [Manual](#manual)
  - [Package Manager (macOS)](#package-manager-macos)
- [Usage](#usage)
- [Compiling](#compiling)
- [License](#license)
- [Releases](https://github.com/majd/ipatool/releases)
- [FAQ](https://github.com/majd/ipatool/wiki/FAQ)

## Requirements
- Supported operating system (Windows, Linux or macOS).
- Apple ID set up to use the App Store.

## Installation

### Manual

You can grab the latest version of `ipatool` from [GitHub releases](https://github.com/majd/ipatool/releases).

### Package Manager (macOS)

You can install `ipatool` using [Homebrew](https://brew.sh).

```shell
$ brew tap majd/repo
$ brew install ipatool
```

## Usage

To authenticate with the App Store, use the `auth` command.

```
Authenticate with the App Store

Usage:
  ipatool auth [command]

Available Commands:
  info        Show current account info
  login       Login to the App Store
  revoke      Revoke your App Store credentials

Flags:
  -h, --help   help for auth

Global Flags:
      --format format     sets output format for command; can be 'text', 'json' (default text)
      --non-interactive   run in non-interactive session
      --verbose           enables verbose logs

Use "ipatool auth [command] --help" for more information about a command.
```

To search for apps on the App Store, use the `search` command.

```
Search for iOS apps available on the App Store

Usage:
  ipatool search <term> [flags]

Flags:
  -h, --help        help for search
  -l, --limit int   maximum amount of search results to retrieve (default 5)

Global Flags:
      --format format     sets output format for command; can be 'text', 'json' (default text)
      --non-interactive   run in non-interactive session
      --verbose           enables verbose logs
```

To obtain a license for an app, use the `purchase` command.

```
Obtain a license for the app from the App Store

Usage:
  ipatool purchase [flags]

Flags:
  -b, --bundle-identifier string   Bundle identifier of the target iOS app (required)
  -h, --help                       help for purchase

Global Flags:
      --format format     sets output format for command; can be 'text', 'json' (default text)
      --non-interactive   run in non-interactive session
      --verbose           enables verbose logs
```

To download a copy of the ipa file, use the `download` command.

```
Download (encrypted) iOS app packages from the App Store

Usage:
  ipatool download [flags]

Flags:
  -i, --app-id int                 ID of the target iOS app (required)
  -b, --bundle-identifier string   The bundle identifier of the target iOS app (overrides the app ID)
  -h, --help                       help for download
  -o, --output string              The destination path of the downloaded app package
      --purchase                   Obtain a license for the app if needed

Global Flags:
      --format format                sets output format for command; can be 'text', 'json' (default text)
      --keychain-passphrase string   passphrase for unlocking keychain
      --non-interactive              run in non-interactive session
      --verbose                      enables verbose logs
```

**Note:** the tool runs in interactive mode by default. Use the `--non-interactive` flag
if running in an automated environment.

## Compiling

The tool can be compiled using the Go toolchain.

```shell
$ go build -o ipatool
```

Unit tests can be executed with the following commands.

```shell
$ go generate github.com/majd/ipatool/...
$ go test -v github.com/majd/ipatool/...
```

## License

IPATool is released under the [MIT license](https://github.com/majd/ipatool/blob/main/LICENSE).
