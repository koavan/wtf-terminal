#!/bin/sh
# Runs workload.sh inside a terminal app under Xvfb in the dev image, typing
# the command into its window with xdotool.
#
#   linux/bench/run-xvfb.sh <label> <app> [VAR=value…]
#
# e.g. linux/bench/run-xvfb.sh gtk-feed bin/gtk/wtf-gtk
#      linux/bench/run-xvfb.sh wails bin/gtk/wtf-wails
set -eu
label=$1 app=$2
shift 2
cd "$(dirname "$0")/../.."
exec docker run --rm -v "$PWD":/src -w /src wtf-gtk-dev:ubuntu24 sh -c '
	label=$1 app=$2; shift 2
	export HOME=/tmp/home SHELL=/bin/bash GTK_A11Y=none LIBGL_ALWAYS_SOFTWARE=1 \
		WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 "$@"
	mkdir -p $HOME
	# As a returning user: no first-run tools check taking the keyboard.
	mkdir -p $HOME/.config/wtf && touch $HOME/.config/wtf/tools-checked
	Xvfb :99 -screen 0 1600x1000x24 -nolisten tcp >/dev/null 2>&1 &
	sleep 0.5
	export DISPLAY=:99
	eval "$(dbus-launch --sh-syntax)"
	"$app" >/tmp/app.log 2>&1 &
	pid=$!
	win=$(xdotool search --sync --onlyvisible --pid $pid | head -1)
	sleep 4
	xdotool windowfocus --sync "$win"
	xdotool type --delay 20 "/src/linux/bench/workload.sh $label"
	xdotool key Return
	# Wait for the four phases.
	done=0
	while [ "$done" -lt 4 ]; do
		kill -0 $pid 2>/dev/null || { echo "app exited early"; tail -20 /tmp/app.log; exit 1; }
		sleep 2
		done=$(grep -c "^$label," linux/bench/results.csv 2>/dev/null) || done=0
	done
	kill $pid 2>/dev/null || true
	grep "^$label," linux/bench/results.csv | tr , "\t"
' sh "$label" "$app" "$@"
