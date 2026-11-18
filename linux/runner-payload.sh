#!/bin/sh
# Builds what wtf installs on an agent runner (#41): wtf-runner and wtf-mcp
# for linux-amd64 and linux-arm64, in <out>/linux-<arch>/. Pure Go, so it
# cross-compiles on the build machine.
#
#   linux/runner-payload.sh <out> <version>
set -eu
cd "$(dirname "$0")/.."
out=$1
version=$2
rm -rf "$out"
for arch in amd64 arm64; do
	mkdir -p "$out/linux-$arch"
	for cmd in wtf-runner wtf-mcp; do
		GOOS=linux GOARCH=$arch CGO_ENABLED=0 go build -trimpath -buildvcs=false \
			-ldflags "-s -w -X main.version=$version" -o "$out/linux-$arch/$cmd" ./cmd/$cmd
	done
done
