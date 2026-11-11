#!/bin/sh
# Benchmark. Run inside the terminal under test (e.g. the native app,
# bin/native/wtf.app) from a fresh tab, with that window in front and left alone:
#
#   macos/bench/workload.sh <label>
#
# Measures the UI's CPU time and memory footprint (app + its WebKit helpers,
# not the shell or the workload) for each phase, plus how long each output
# phase took to reach the screen (until the UI goes quiet). Appends to macos/bench/results.csv.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
label=${1:?usage: workload.sh <label>}
meter=$here/respcpu
[ -x "$meter" ] || cc -O2 -o "$meter" "$here/respcpu.c"

# The shell running this script was started by the terminal app.
ui=$(ps -o ppid= -p "$PPID" | tr -d ' ')
echo "measuring $(ps -o comm= -p "$ui") (pid $ui)"

data=${TMPDIR:-/tmp}/wtf-bench-colored.log
if [ ! -f "$data" ]; then
	# ~40 MB of log-like lines with ANSI colours, like `kubectl logs` or a build.
	awk 'BEGIN { srand(1); for (i = 0; i < 400000; i++)
		printf "\033[2m2026-10-14T11:%02d:%02d\033[0m \033[%dm%-5s\033[0m svc-%d request id=%08x path=/api/v1/items/%d took=%dms\n",
			i % 60, i % 60, 31 + i % 6, (i % 7 ? "INFO" : "WARN"), i % 13, i * 2654435761 % 4294967296, i, i % 997 }' > "$data"
fi

results=$here/results.csv
[ -f "$results" ] || echo "label,phase,seconds,ui_cpu_seconds,ui_cpu_percent,footprint_mib" > "$results"

now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }

# Field n of the meter's output: 1 = UI CPU seconds, 2 = footprint MiB.
measure() { "$meter" "$ui" | cut -d' ' -f"$1"; }

phase() {
	name=$1 run=$2
	cpu0=$(measure 1)
	t0=$(now)
	$run
	# Output still buffered between the PTY and the screen keeps the UI busy
	# after the command exits: the phase ends once the UI goes quiet.
	cpu1=$(measure 1)
	while :; do
		sleep 0.5
		c=$(measure 1)
		if awk -v a="$cpu1" -v b="$c" 'BEGIN { exit !(b - a < 0.02) }'; then break; fi
		cpu1=$c
	done
	t1=$(now) mem=$(measure 2)
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
grep "^$label," "$results" | column -t -s,
