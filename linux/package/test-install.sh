#!/bin/sh
# Installs a built package on clean distribution containers and starts wtf
# under Xvfb there: the package's dependencies must be enough to run it.
#
#   linux/package/test-install.sh <arch>     (arm64 or amd64)
#
# Uses the .deb on Ubuntu 24.04 and Debian 13, the .rpm on Fedora.
#
# When a distribution's default mirror is slow, point its container at
# another one (the amd64 containers are emulated, so a crawling mirror can
# stall a run for many minutes):
#   UBUNTU_MIRROR=http://mirrors.mit.edu/ubuntu/   replaces every Ubuntu URI,
#     security included (arm64 needs an ubuntu-ports mirror, e.g.
#     http://mirrors.edge.kernel.org/ubuntu-ports/)
#   DEBIAN_MIRROR=http://mirrors.edge.kernel.org/debian/   replaces
#     deb.debian.org/debian (debian-security stays as it is)
set -eu
cd "$(dirname "$0")/../.."
arch=${1:-arm64}
rpmarch=$arch
[ "$arch" = amd64 ] && rpmarch=x86_64
[ "$arch" = arm64 ] && rpmarch=aarch64
deb=$(ls bin/gtk/packages/wtf_*_"$arch".deb | tail -1)
rpm=$(ls bin/gtk/packages/wtf-*."$rpmarch".rpm | tail -1)
fail=0

# The check, the same everywhere: it starts, opens a window, uses the real
# folders and the bundled font, and its shell runs.
check='
set -u
export HOME=/tmp/home SHELL=/bin/bash LIBGL_ALWAYS_SOFTWARE=1
mkdir -p $HOME/.config/wtf && touch $HOME/.config/wtf/tools-checked
Xvfb :99 -screen 0 1280x800x24 -nolisten tcp >/dev/null 2>&1 &
sleep 1
export DISPLAY=:99
eval "$(dbus-launch --sh-syntax)"
test -x /usr/bin/wtf || { echo "FAIL no /usr/bin/wtf"; exit 1; }
test -f /usr/share/applications/com.dayanraj.wtf.desktop || { echo "FAIL no desktop file"; exit 1; }
test -f /usr/share/icons/hicolor/256x256/apps/com.dayanraj.wtf.png || { echo "FAIL no icon"; exit 1; }
missing=$(ldd /usr/bin/wtf | grep "not found" || true)
[ -z "$missing" ] || { echo "FAIL missing libraries: $missing"; exit 1; }
wtf >/tmp/wtf.log 2>&1 &
pid=$!
for _ in $(seq 100); do
	pgrep -P $pid bash >/dev/null 2>&1 && break
	kill -0 $pid 2>/dev/null || break
	sleep 0.2
done
sleep 2
if ! kill -0 $pid 2>/dev/null; then echo "FAIL wtf exited"; cat /tmp/wtf.log; exit 1; fi
pgrep -P $pid bash >/dev/null || { echo "FAIL no shell"; cat /tmp/wtf.log; exit 1; }
grep -q "state $HOME/.config/wtf, cache $HOME/.cache/wtf" /tmp/wtf.log || { echo "FAIL folders"; cat /tmp/wtf.log; exit 1; }
grep -q "fonts from /usr/share/wtf/fonts" /tmp/wtf.log || { echo "FAIL bundled fonts not added"; cat /tmp/wtf.log; exit 1; }
grep -E "CRITICAL|ERROR" /tmp/wtf.log | head -5
kill -TERM $pid
for _ in $(seq 50); do kill -0 $pid 2>/dev/null || break; sleep 0.1; done
kill -0 $pid 2>/dev/null && { echo "FAIL still running after SIGTERM"; exit 1; }
echo "PASS $(. /etc/os-release; echo $PRETTY_NAME): installs, starts, runs a shell, quits"
'

run() { # image, install command
	echo "== $1"
	if ! docker run --rm --platform "linux/$arch" -v "$PWD/bin/gtk/packages":/pkgs:ro "$1" sh -c "$2 && $check"; then
		echo "FAIL $1"; fail=1
	fi
}
xtools='xvfb dbus-x11 procps'
ubuntu_src= debian_src=
if [ -n "${UBUNTU_MIRROR:-}" ]; then
	echo "Ubuntu mirror: $UBUNTU_MIRROR"
	ubuntu_src="sed -i 's#^URIs: .*#URIs: $UBUNTU_MIRROR#' /etc/apt/sources.list.d/ubuntu.sources && "
fi
if [ -n "${DEBIAN_MIRROR:-}" ]; then
	echo "Debian mirror: $DEBIAN_MIRROR"
	debian_src="sed -i 's#^URIs: http://deb.debian.org/debian\$#URIs: $DEBIAN_MIRROR#' /etc/apt/sources.list.d/debian.sources && "
fi
run ubuntu:24.04 "${ubuntu_src}apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq /pkgs/$(basename "$deb") $xtools >/dev/null"
run debian:trixie "${debian_src}apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq /pkgs/$(basename "$deb") $xtools >/dev/null"
run fedora:41 "dnf install -y -q /pkgs/$(basename "$rpm") xorg-x11-server-Xvfb dbus-x11 procps-ng >/dev/null"
[ $fail -eq 0 ] && echo "ALL PASSED" || echo "SOME FAILED"
exit $fail
