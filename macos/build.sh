#!/bin/sh
# Builds the native macOS app: the Go backend as a C archive, the Swift UI,
# then bin/native/wtf.app (ad-hoc signed).
#   macos/build.sh [release|debug]
set -eu
# Full Xcode is needed (SwiftTerm's Metal shader; the Metal renderer is opt-in).
export DEVELOPER_DIR=${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}
config=${1:-release}
cd "$(dirname "$0")/.."
version=$(sed -n 's/^const version = "\(.*\)"/\1/p' main.go)
commit=$(git describe --always --dirty 2>/dev/null || echo unknown)
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)

CGO_ENABLED=1 go build -tags native -trimpath -buildvcs=false -buildmode=c-archive \
	-o bin/native/libwtf.a ./cmd/libwtf
cp bin/native/libwtf.h macos/Sources/CWtf/include/libwtf.h
(cd macos && swift build -c "$config")
bin=$(cd macos && swift build -c "$config" --show-bin-path)

app=bin/native/wtf.app
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin/wtf" "$app/Contents/MacOS/wtf"
cp build/darwin/icons.icns build/darwin/Assets.car "$app/Contents/Resources/"
if [ -d "$bin/SwiftTerm_SwiftTerm.bundle" ]; then
	cp -R "$bin/SwiftTerm_SwiftTerm.bundle" "$app/Contents/Resources/"
fi
# The Wails app's Info.plist, as a separate app (own bundle id) until the
# native one replaces it.
plist=$app/Contents/Info.plist
cp build/darwin/Info.plist "$plist"
pb() { /usr/libexec/PlistBuddy -c "$1" "$plist"; }
pb "Set :CFBundleIdentifier com.dayanraj.wtf.native"
pb "Set :CFBundleName wtf native"
pb "Set :CFBundleVersion $version"
pb "Set :CFBundleShortVersionString $version"
pb "Set :LSMinimumSystemVersion 14.0.0"
pb "Add :WtfCommit string $commit"
pb "Add :WtfBuildDate string $date"
codesign --force --sign - --timestamp=none "$app" >/dev/null
echo "built: $app ($version, $commit)"
