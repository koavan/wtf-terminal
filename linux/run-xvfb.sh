#!/bin/sh
# Runs a command inside the dev image under Xvfb, with the repo at /src and
# a throwaway HOME (never the real one). Usage: linux/run-xvfb.sh <cmd>…
set -eu
cd "$(dirname "$0")/.."
exec docker run --rm -i -v "$PWD":/src -w /src \
	-v wtf-gtk-gomod:/root/go/pkg/mod -v wtf-gtk-gocache:/root/.cache/go-build \
	-e HOME=/tmp/home -e GTK_A11Y=none -e LIBGL_ALWAYS_SOFTWARE=1 -e SHELL=/bin/bash -e WTF_NATIVE_DEBUG -e WTF_GTK_PTY \
	wtf-gtk-dev:ubuntu24 sh -c 'mkdir -p $HOME; Xvfb :99 -screen 0 1600x1000x24 -nolisten tcp >/dev/null 2>&1 & sleep 0.5; export DISPLAY=:99; eval "$(dbus-launch --sh-syntax)"; "$@"' sh "$@"
