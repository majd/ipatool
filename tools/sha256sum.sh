#!/bin/sh -e

if which sha256sum >/dev/null 2>&1; then
  sha256sum "$1" | awk '{ print $1 }'
else
  shasum -a256 "$1" | awk '{ print $1 }'
fi