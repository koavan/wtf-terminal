#!/bin/sh
# Builds the native Linux app in the dev image: bin/gtk/wtf-gtk, and the
# plugin's MCP helper next to it, bin/gtk/wtf-mcp.
set -eu
cd "$(dirname "$0")/.."
version=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo dev)
commit=$(git rev-parse --short HEAD 2>/dev/null || echo "")
if [ -n "$commit" ] && [ -n "$(git status --porcelain 2>/dev/null)" ]; then commit="$commit-dirty"; fi
date=$(date -u +%Y-%m-%d)
exec docker run --rm -v "$PWD":/src -w /src \
	-v wtf-gtk-gomod:/root/go/pkg/mod -v wtf-gtk-gocache:/root/.cache/go-build \
	wtf-gtk-dev:ubuntu24 sh -c '
	set -e
	go build -tags native -ldflags "-X main.version=$1 -X main.commit=$2 -X main.buildDate=$3" \
		-o bin/gtk/wtf-gtk ./cmd/wtf-gtk
	# The wtf plugin'"'"'s MCP server, found next to the app (#32).
	CGO_ENABLED=0 go build -ldflags "-X main.version=$1" -o bin/gtk/wtf-mcp ./cmd/wtf-mcp' \
	sh "${version:-dev}" "$commit" "$date"
