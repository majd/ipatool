# IPATool
[![Release](https://img.shields.io/github/release/majd/ipatool.svg)](https://GitHub.com/majd/ipatool/releases/)
![Swift](https://img.shields.io/badge/Swift-5.4-green.svg)
![macOS](https://img.shields.io/badge/macOS-10.15%2B-green.svg)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/majd/ipatool/blob/main/LICENSE)

`ipatool` is a command line tool that allows you to search for iOS apps on the [App Store](https://apps.apple.com) and download a copy of the app package, known as an _ipa_ file.

![Demo](./demo.gif)

## Requirements
* macOS 10.15 or later.
* Apple ID set up to use the App Store.

## Installation

### Manual Install

You can grab the latest version of `ipatool` from [GitHub releases](https://github.com/majd/ipatool/releases).

### Homebrew

You can install `ipatool` using [Homebrew](https://brew.sh).

```
$ brew tap majd/repo
$ brew install ipatool
```

## Usage

To search for apps on the App Store, use the `search` command.

```
OVERVIEW: Search for iOS apps available on the App Store.

USAGE: ipatool search <term> [--limit <limit>] [--country <country>] [--device-family <device-family>] [--log-level <log-level>]

ARGUMENTS:
  <term>                  The term to search for. 

OPTIONS:
  -l, --limit <limit>     The maximum amount of search results to retrieve.
                          (default: 5)
  -c, --country <country> The two-letter (ISO 3166-1 alpha-2) country code for
                          the iTunes Store. (default: US)
  -d, --device-family <device-family>
                          The device family to limit the search query to.
                          (default: iPhone)
  --log-level <log-level> The log level. (default: info)
  --version               Show the version.
  -h, --help              Show help information.
```

To download a copy of the ipa file, use the `download` command.

```
OVERVIEW: Download (encrypted) iOS app packages from the App Store.

USAGE: ipatool download --bundle-identifier <bundle-identifier> [--email <email>] [--password <password>] [--auth-code <auth-code>] [--country <country>] [--device-family <device-family>] [--log-level <log-level>]

OPTIONS:
  -b, --bundle-identifier <bundle-identifier>
                          The bundle identifier of the target iOS app. 
  -e, --email <email>     The email address for the Apple ID. 
  -p, --password <password>
                          The password for the Apple ID. 
  --auth-code <auth-code> The 2FA code for the Apple ID. 
  -c, --country <country> The two-letter (ISO 3166-1 alpha-2) country code for the iTunes Store. (default: US)
  -d, --device-family <device-family>
                          The device family to limit the search query to. (default: iPhone)
  --log-level <log-level> The log level. (default: info)
  --version               Show the version.
  -h, --help              Show help information.
```

**Note:** You can specify the Apple ID email address and username as arguments when using the tool or by setting them as environment variables (`IPATOOL_EMAIL` and `IPATOOL_PASSWORD`). If you do not specify this information using either of those methods, the tool will prompt for user input in an interactive session. Similarly, you can supply the 2FA code interactively or using the environment variable `IPATOOL_2FA_CODE`.

## Common Knowledge

**Are my Apple ID credentials stored safely?**

The tool does not store your credentials anywhere and it only communicates with Apple servers directly. Feel free to go through the source code.

**Will my Apple ID get flagged for using this tool?**

Maybe, but probably not. While this tool communicates with iTunes and the App Store directly, mimicking the behavior of iTunes running on macOS, I cannot guarantee its safety. I recommend using a throwaway Apple ID. **Use this tool at your own risk**.

**Can I use this tool to download paid apps without paying for them?**

**No**. This is is not a piracy tool; you can only download apps that you have previously installed on your iOS device. This limitation applies to free apps as well. Essentially, your account must already have a license for the app you are trying to download.

**Can I use this tool to sideload unsupported iOS apps on Apple Silicon Macs?**

While it was previously possible to download ipa files using this tool and install them on Macs running on Apple Silicon, this is no longer the case as of recently. Apple stopped serving macOS compatible `sinf` data for the app package. You could, however, use this tool to get a copy of the iOS app and use a jailbroken iOS device to strip any codesigning requirements then codesign the app again using an adhoc signature to run on Apple Silicon.

## Troubleshooting

If `ipatool download` fails with the following error despite providing
correct email/password/auth code:

```
==> ℹ️  [Info] Querying the iTunes Store for '<bundle-identifier>' in country '<country>'...
==> ℹ️  [Info] Authenticating with the App Store...
==> ❌ [Error] An unknown error has occurred.
```

then see [issue #30](https://github.com/majd/ipatool/issues/30)
(particularly [this
comment](https://github.com/majd/ipatool/issues/30#issuecomment-977852648))
for a fix. Note that you may need version 1.07.
