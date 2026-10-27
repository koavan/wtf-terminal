#!/bin/sh
# Runs the native Linux app's headless checks under Xvfb in the dev image:
# the uicore tests, each WTF_NATIVE_DEBUG self-check, single instance, and
# quitting. Build first (linux/build.sh). Snapshots land in bin/gtk/snapshots.
#
#   linux/check.sh
set -eu
cd "$(dirname "$0")/.."
exec linux/run-xvfb.sh sh -s <<'EOF'
set -u
bin=bin/gtk/wtf-gtk
fail=0
run() { # name, then the command
	name=$1; shift
	echo "== $name"
	if ! "$@"; then echo "FAIL $name"; fail=1; fi
}

run "uicore tests" go test ./internal/uicore/
for c in app snapshot view terminal tabs restore sidebar panes viewers api marks signals; do
	run "self-check $c" env WTF_NATIVE_DEBUG=$c timeout 60 $bin
done

echo "== single instance"
rm -rf "$HOME/.config/wtf" "$HOME/.cache/wtf" # a fresh real profile
$bin >/tmp/first.log 2>&1 &
first=$!
for _ in $(seq 50); do xdotool search --onlyvisible --pid $first >/dev/null 2>&1 && break; sleep 0.1; done
timeout 10 $bin >/tmp/second.log 2>&1
status=$?
sleep 1
# Terminal windows only: a fresh profile also gets the first-run tools check.
windows=$(( $(xdotool search --onlyvisible --pid $first 2>/dev/null | wc -l) - $(xdotool search --onlyvisible --name 'Command-Line Tools' 2>/dev/null | wc -l) ))
if [ $status -eq 0 ] && [ "$windows" -eq 2 ]; then
	echo "PASS a second wtf-gtk exits ($status) and the first opens a window ($windows)"
else
	echo "FAIL second exit $status, first has $windows windows"; fail=1
fi
if grep -q "state $HOME/.config/wtf, cache $HOME/.cache/wtf" /tmp/first.log; then
	echo "PASS it uses the real folders"
else
	echo "FAIL folders: $(grep -o 'state .*' /tmp/first.log)"; fail=1
fi
shells=$(pgrep -P $first | wc -l)
kill -TERM $first
for _ in $(seq 50); do kill -0 $first 2>/dev/null || break; sleep 0.1; done
if kill -0 $first 2>/dev/null; then
	echo "FAIL still running after SIGTERM"; fail=1
else
	echo "PASS SIGTERM quits ($shells shells before)"
fi
left=$(pgrep -x bash -P 1 | wc -l)
[ "$left" -eq 0 ] && echo "PASS no shells left behind" || { echo "FAIL $left shells left"; fail=1; }

[ $fail -eq 0 ] && echo "ALL PASSED" || echo "SOME FAILED"
exit $fail
EOF
