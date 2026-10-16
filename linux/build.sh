#!/bin/sh
# Builds the native Linux app in the dev image: bin/gtk/wtf-gtk.
set -eu
cd "$(dirname "$0")/.."
exec docker run --rm -v "$PWD":/src -w /src \
	-v wtf-gtk-gomod:/root/go/pkg/mod -v wtf-gtk-gocache:/root/.cache/go-build \
	wtf-gtk-dev:ubuntu24 go build -tags native -o bin/gtk/wtf-gtk ./cmd/wtf-gtk
