#!/bin/sh
# Builds the Go backend as a C archive, then the native app.
#   macos/build.sh [debug|release]
set -eu
# Full Xcode builds SwiftTerm's Metal shader (Metal renderer is opt-in).
export DEVELOPER_DIR=${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}
cd "$(dirname "$0")/.."
CGO_ENABLED=1 go build -buildmode=c-archive -o bin/native/libwtf.a ./cmd/libwtf
cp bin/native/libwtf.h macos/Sources/CWtf/include/libwtf.h
cd macos
swift build -c "${1:-release}"
echo "built: macos/.build/${1:-release}/WtfSpike"
