#!/bin/sh
set -eu

# Build a standalone executable for jailbroken arm64 devices running iOS 15+.
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUTPUT=${1:-ipatool-ios-arm64}
VERSION=${VERSION:-dev}

for TOOL in ldid cmake; do
  command -v "$TOOL" >/dev/null 2>&1 || {
    echo "$TOOL is required; install it with 'brew install ldid cmake'." >&2
    exit 1
  }
done

SDKROOT=$(xcrun --sdk iphoneos --show-sdk-path)
CC=$(xcrun --sdk iphoneos --find clang)
export SDKROOT CC
export IPHONEOS_DEPLOYMENT_TARGET=15.0
export GOOS=ios GOARCH=arm64 CGO_ENABLED=1

BUILD_DIR=$(mktemp -d /tmp/ipatool-ios.XXXXXX)
trap 'rm -rf "$BUILD_DIR"' EXIT HUP INT TERM

# Keep this version in sync with internal/sap/unicorn/artifact.go.
curl -fL --retry 3 https://github.com/unicorn-engine/unicorn/archive/refs/tags/2.1.4.tar.gz -o "$BUILD_DIR/unicorn.tar.gz"
echo "ea8863f095a0136388694e5a6063afd9bb7650e30243dd6251af59c5ce5601f4  $BUILD_DIR/unicorn.tar.gz" | shasum -a 256 -c -
tar -xzf "$BUILD_DIR/unicorn.tar.gz" -C "$BUILD_DIR"
# Use iOS W^X memory protection for Unicorn, including nested Go callbacks.
patch -d "$BUILD_DIR/unicorn-2.1.4" -p1 < "$ROOT/tools/patches/unicorn-ios.patch"
ARCHFLAGS='-arch arm64' cmake -S "$BUILD_DIR/unicorn-2.1.4" -B "$BUILD_DIR/build" \
  -DCMAKE_SYSTEM_NAME=iOS -DCMAKE_OSX_SYSROOT="$SDKROOT" \
  -DCMAKE_OSX_ARCHITECTURES=arm64 -DCMAKE_OSX_DEPLOYMENT_TARGET=15.0 \
  -DCMAKE_C_FLAGS='-target arm64-apple-ios15.0' -DCMAKE_BUILD_TYPE=Release \
  -DBUILD_SHARED_LIBS=OFF -DUNICORN_ARCH=x86 -DUNICORN_BUILD_TESTS=OFF -DUNICORN_INSTALL=OFF
cmake --build "$BUILD_DIR/build" --parallel "$(sysctl -n hw.ncpu)"

# purego resolves the statically linked Unicorn API through dlsym.
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -Wl,-force_load,$BUILD_DIR/build/libunicorn.a -Wl,-export_dynamic"
cd "$ROOT"
go build -ldflags="-X github.com/majd/ipatool/v2/cmd.version=$VERSION" -o "$OUTPUT" .
ldid -S"$ROOT/resources/ios-entitlements.plist" "$OUTPUT"
