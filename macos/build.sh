#!/bin/sh
# Builds the native macOS app: the Go backend as a C archive, the Swift UI,
# then bin/native/wtf.app (ad-hoc signed).
#   macos/build.sh [release|debug] [universal] [dmg]
# universal: arm64 and x86_64 in one app. dmg: also bin/native/wtf-<version>.dmg.
set -eu
# Full Xcode is needed (SwiftTerm's Metal shader; the Metal renderer is opt-in).
export DEVELOPER_DIR=${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}
config=release universal= dmg=
for a in "$@"; do
	case $a in
	release | debug) config=$a ;;
	universal) universal=1 ;;
	dmg) dmg=1 ;;
	*) echo "usage: macos/build.sh [release|debug] [universal] [dmg]" >&2; exit 2 ;;
	esac
done
cd "$(dirname "$0")/.."
version=$(cat VERSION)
commit=$(git describe --always --dirty 2>/dev/null || echo unknown)
date=$(date -u +%Y-%m-%d)

goarchive() { # goarchive <goarch> <clang arch> <out>
	# Go won't overwrite a fat archive from a universal build.
	rm -f "$3"
	CGO_ENABLED=1 GOARCH=$1 CC="clang -arch $2" go build -trimpath -buildvcs=false -buildmode=c-archive \
		-o "$3" ./cmd/libwtf
}
if [ -n "$universal" ]; then
	# One fat archive, then SwiftPM builds and links both architectures.
	goarchive arm64 arm64 bin/native/arm64/libwtf.a
	goarchive amd64 x86_64 bin/native/amd64/libwtf.a
	lipo -create bin/native/arm64/libwtf.a bin/native/amd64/libwtf.a -output bin/native/libwtf.a
	cp bin/native/arm64/libwtf.h bin/native/libwtf.h
	archs="--arch arm64 --arch x86_64"
else
	goarchive "$(go env GOARCH)" "$(uname -m)" bin/native/libwtf.a
	archs=
fi
mkdir -p macos/Sources/CWtf/include
cp bin/native/libwtf.h macos/Sources/CWtf/include/libwtf.h
# shellcheck disable=SC2086
(cd macos && swift build -c "$config" $archs)
# shellcheck disable=SC2086
bin=$(cd macos && swift build -c "$config" $archs --show-bin-path)

app=bin/native/wtf.app
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin/wtf" "$app/Contents/MacOS/wtf"
# Starts shells with a clean signal mask (see macos/exec/wtf-exec.c).
if [ -n "$universal" ]; then helper_archs="-arch arm64 -arch x86_64"; else helper_archs=; fi
# shellcheck disable=SC2086
cc -O2 -Wall $helper_archs -mmacosx-version-min=14.0 -o "$app/Contents/MacOS/wtf-exec" macos/exec/wtf-exec.c
# The wtf plugin's MCP server, which Claude Code starts in a wtf shell (#32).
gohelper() { # gohelper <goarch> <out>
	CGO_ENABLED=0 GOARCH=$1 go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X main.version=$version" -o "$2" ./cmd/wtf-mcp
}
if [ -n "$universal" ]; then
	gohelper arm64 bin/native/arm64/wtf-mcp
	gohelper amd64 bin/native/amd64/wtf-mcp
	lipo -create bin/native/arm64/wtf-mcp bin/native/amd64/wtf-mcp -output "$app/Contents/MacOS/wtf-mcp"
else
	gohelper "$(go env GOARCH)" "$app/Contents/MacOS/wtf-mcp"
fi
cp macos/Resources/icons.icns macos/Resources/Assets.car "$app/Contents/Resources/"
# JetBrains Mono (OFL), the terminal's default font.
mkdir -p "$app/Contents/Resources/Fonts"
cp macos/Resources/Fonts/*.ttf macos/Resources/Fonts/OFL.txt "$app/Contents/Resources/Fonts/"
# The wtf Claude Code plugin, for `claude --plugin-dir` (#32).
cp -R claude-plugin "$app/Contents/Resources/claude-plugin"
# The manual: the pages in manual/, shared with the Linux app.
mkdir -p "$app/Contents/Resources/manual"
cp manual/*.md "$app/Contents/Resources/manual/"
if [ -d "$bin/SwiftTerm_SwiftTerm.bundle" ]; then
	cp -R "$bin/SwiftTerm_SwiftTerm.bundle" "$app/Contents/Resources/"
fi
# wtf's Info.plist (bundle id com.dayanraj.wtf): this app is wtf on macOS.
plist=$app/Contents/Info.plist
cp macos/Resources/Info.plist "$plist"
pb() { /usr/libexec/PlistBuddy -c "$1" "$plist"; }
pb "Set :CFBundleVersion $version"
pb "Set :CFBundleShortVersionString $version"
pb "Set :LSMinimumSystemVersion 14.0.0"
pb "Add :ATSApplicationFontsPath string Fonts"
pb "Add :WtfCommit string $commit"
pb "Add :WtfBuildDate string $date"
codesign --force --sign - --timestamp=none "$app/Contents/MacOS/wtf-exec" >/dev/null
codesign --force --sign - --timestamp=none "$app/Contents/MacOS/wtf-mcp" >/dev/null
codesign --force --sign - --timestamp=none "$app" >/dev/null
echo "built: $app ($version, $commit)"

if [ -n "$dmg" ]; then
	# The app and a link to Applications, compressed. Ad-hoc signed: on another
	# Mac, Gatekeeper asks once (System Settings → Privacy & Security → Open Anyway).
	out=bin/native/wtf-$version.dmg
	stage=$(mktemp -d)
	ditto "$app" "$stage/wtf.app"
	ln -s /Applications "$stage/Applications"
	rm -f "$out"
	hdiutil create -quiet -volname "wtf $version" -srcfolder "$stage" -format UDZO -fs HFS+ "$out"
	rm -rf "$stage"
	echo "dmg: $out ($(lipo -archs "$app/Contents/MacOS/wtf"))"
fi
