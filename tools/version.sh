#!/bin/sh -e

VERSION=$(git describe --abbrev=0 --tags | cut -c 2-)
BASEDIR=$(dirname "$0")

cat <<EOF >"$BASEDIR/../cmd/version.go"
package cmd

// Automatically generated
const version = "$VERSION"
EOF

echo "Version ${VERSION}"