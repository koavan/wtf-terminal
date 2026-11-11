#!/bin/sh
# Builds the native Linux app's packages: a .deb and an .rpm per
# architecture, in bin/gtk/packages.
#
#   linux/package.sh [arm64] [amd64]     (default: both)
#   VERSION=1.1.0 linux/package.sh       (default: the VERSION file)
#
# Each architecture builds in its dev image (wtf-gtk-dev:ubuntu24 for arm64,
# wtf-gtk-dev:ubuntu24-amd64 for amd64: docker build --platform linux/amd64
# -t wtf-gtk-dev:ubuntu24-amd64 linux), so the binary links against Ubuntu
# 24.04's libraries, the oldest supported. nfpm (linux/package/nfpm.yaml)
# makes the packages.
set -eu
cd "$(dirname "$0")/.."
archs=${*:-arm64 amd64}
version=${VERSION:-$(cat VERSION)}
commit=$(git rev-parse --short HEAD)
if [ -n "$(git status --porcelain)" ]; then commit="$commit-dirty"; fi
date=$(date -u +%Y-%m-%d)
mkdir -p bin/gtk/packages

for arch in $archs; do
	case $arch in
	arm64) image=wtf-gtk-dev:ubuntu24 ;;
	amd64) image=wtf-gtk-dev:ubuntu24-amd64 ;;
	*) echo "unknown architecture $arch" >&2; exit 2 ;;
	esac
	stage=bin/gtk/pkg-$arch
	rm -rf "$stage"
	mkdir -p "$stage/fonts"
	echo "== $arch: building wtf $version ($commit)"
	docker run --rm --platform linux/$arch -v "$PWD":/src -w /src \
		-v wtf-gtk-gomod:/root/go/pkg/mod -v wtf-gtk-gocache-$arch:/root/.cache/go-build \
		"$image" sh -c '
		set -e
		go build -trimpath -buildvcs=false \
			-ldflags "-s -w -X main.version=$1 -X main.commit=$2 -X main.buildDate=$3" \
			-o "$4/wtf" ./cmd/wtf-gtk
		CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$1" \
			-o "$4/wtf-mcp" ./cmd/wtf-mcp
		for size in 32 48 64 128 256 512; do
			mkdir -p "$4/icons/${size}x${size}/apps"
			convert linux/package/appicon.png -resize ${size}x${size} "$4/icons/${size}x${size}/apps/com.dayanraj.wtf.png"
		done' sh "$version" "$commit" "$date" "$stage"
	cp macos/Resources/Fonts/*.ttf macos/Resources/Fonts/OFL.txt "$stage/fonts/"
	cp -R claude-plugin "$stage/claude-plugin"

	echo "== $arch: packaging"
	# nfpm expands variables in some fields but not in file paths.
	sed "s|\${STAGE}|$stage|g" linux/package/nfpm.yaml >"$stage/nfpm.yaml"
	docker run --rm -v "$PWD":/src -w /src \
		-e VERSION="$version" -e ARCH="$arch" \
		wtf-gtk-dev:ubuntu24 sh -c "nfpm package -f $stage/nfpm.yaml -p deb -t bin/gtk/packages/ &&
			nfpm package -f $stage/nfpm.yaml -p rpm -t bin/gtk/packages/"
done
ls -l bin/gtk/packages
