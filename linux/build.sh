#!/bin/sh
# Builds the native Linux app in the dev image: bin/gtk/wtf-gtk.
set -eu
cd "$(dirname "$0")/.."
version=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo dev)
commit=$(git rev-parse --short HEAD 2>/dev/null || echo "")
if [ -n "$commit" ] && [ -n "$(git status --porcelain 2>/dev/null)" ]; then commit="$commit-dirty"; fi
date=$(date -u +%Y-%m-%d)
exec docker run --rm -v "$PWD":/src -w /src \
	-v wtf-gtk-gomod:/root/go/pkg/mod -v wtf-gtk-gocache:/root/.cache/go-build \
	wtf-gtk-dev:ubuntu24 go build -tags native \
	-ldflags "-X main.version=${version:-dev} -X main.commit=$commit -X main.buildDate=$date" \
	-o bin/gtk/wtf-gtk ./cmd/wtf-gtk
