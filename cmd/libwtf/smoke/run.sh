#!/bin/sh
# Builds libwtf and runs the headless C smoke test against it, in throwaway
# state and cache directories.
set -eu
cd "$(dirname "$0")/../../.."
CGO_ENABLED=1 go build -buildmode=c-archive -o bin/native/libwtf.a ./cmd/libwtf
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cc -o "$tmp/smoke" -Ibin/native cmd/libwtf/smoke/smoke.c bin/native/libwtf.a \
	-framework CoreFoundation -framework Security -lresolv 2>&1 | grep -v 'ld: warning' || true
mkdir -p "$tmp/state" "$tmp/cache"
"$tmp/smoke" "$tmp/state" "$tmp/cache"
