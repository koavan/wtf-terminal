#!/bin/sh
# Benchmark, the Linux port of macos/bench/workload.sh. Run inside the
# terminal under test (the Wails build or wtf-gtk), from a fresh tab, with
# that window left alone:
#
#   linux/bench/workload.sh <label>
#
# Measures the UI's CPU time and memory (PSS of the app plus its WebKit
# helpers, not the shell or the workload) for each phase, and how long each
# output phase took to reach the screen (until the UI goes quiet). Appends
# to linux/bench/results.csv.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
label=${1:?usage: workload.sh <label>}

# The shell running this script was started by the terminal app.
ui=$(ps -o ppid= -p "$PPID" | tr -d ' ')
echo "measuring $(cat /proc/$ui/comm) (pid $ui)"
hz=$(getconf CLK_TCK)

# The UI and its WebKit helpers (children named WebKit*).
pids() {
	echo "$ui"
	ps -eo pid=,ppid=,comm= | awk -v ui="$ui" '$2 == ui && $3 ~ /^WebKit/ { print $1 }'
}

# "cpu_seconds pss_mib" for the UI and its helpers.
measure() {
	cpu=0 pss=0
	for p in $(pids); do
		t=$(awk '{ print $14 + $15 }' "/proc/$p/stat" 2>/dev/null || echo 0)
		m=$(awk '/^Pss:/ { print $2 }' "/proc/$p/smaps_rollup" 2>/dev/null || echo 0)
		cpu=$((cpu + t)) pss=$((pss + m))
	done
	awk -v c="$cpu" -v hz="$hz" -v m="$pss" 'BEGIN { printf "%.2f %.0f\n", c / hz, m / 1024 }'
}

data=${TMPDIR:-/tmp}/wtf-bench-colored.log
if [ ! -f "$data" ]; then
	# ~40 MB of log-like lines with ANSI colours, like `kubectl logs` or a build.
	awk 'BEGIN { srand(1); for (i = 0; i < 400000; i++)
		printf "\033[2m2026-10-14T11:%02d:%02d\033[0m \033[%dm%-5s\033[0m svc-%d request id=%08x path=/api/v1/items/%d took=%dms\n",
			i % 60, i % 60, 31 + i % 6, (i % 7 ? "INFO" : "WARN"), i % 13, i * 2654435761 % 4294967296, i, i % 997 }' > "$data"
fi

results=$here/results.csv
[ -f "$results" ] || echo "label,phase,seconds,ui_cpu_seconds,ui_cpu_percent,pss_mib" > "$results"

now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }

phase() {
	name=$1 run=$2
	cpu0=$(measure | cut -d' ' -f1)
	t0=$(now)
	$run
	# Output still buffered between the PTY and the screen keeps the UI busy
	# after the command exits: the phase ends once the UI goes quiet.
	cpu1=$(measure | cut -d' ' -f1)
	while :; do
		sleep 0.5
		c=$(measure | cut -d' ' -f1)
		if awk -v a="$cpu1" -v b="$c" 'BEGIN { exit !(b - a < 0.02) }'; then break; fi
		cpu1=$c
	done
	t1=$(now) mem=$(measure | cut -d' ' -f2)
	echo "$label,$name,$t1,$t0,$cpu0,$cpu1,$mem" | awk -F, '{
		secs = $3 - $4 - 0.5; cpu = $6 - $5
		printf "%s,%s,%.2f,%.2f,%.0f,%.0f\n", $1, $2, secs, cpu, 100 * cpu / secs, $7 }' >> "$results"
}

idle() { sleep 30; }
seqrun() { seq 1 2000000; }
colored() { cat "$data"; }
yesrun() { yes 'the quick brown fox jumps over the lazy dog 0123456789' | head -n 2000000; }

phase idle-30s idle
phase seq-2M seqrun
phase colored-40MB colored
phase yes-2M yesrun
clear
echo "== $label =="
grep "^$label," "$results" | tr , "\t"
