// respcpu: total CPU time and memory footprint of an app's UI: the app
// process plus the WebKit XPC helpers it is responsible for (launchd parents
// them, so ppid can't find them). Shells and their jobs are left out, so the
// workload itself doesn't count.
//   respcpu <app-pid>   prints: <cpu-seconds> <rss-MiB> <pids...>
#include <libproc.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <mach/mach_time.h>

extern int responsibility_get_pid_responsible_for_pid(pid_t);

int main(int argc, char **argv) {
	if (argc != 2) { fprintf(stderr, "usage: respcpu <pid>\n"); return 2; }
	pid_t target = atoi(argv[1]);
	mach_timebase_info_data_t tb; mach_timebase_info(&tb);
	int n = proc_listallpids(NULL, 0);
	pid_t *pids = calloc(n + 64, sizeof(pid_t));
	n = proc_listallpids(pids, (n + 64) * sizeof(pid_t));
	double cpu = 0, rss = 0; char list[4096] = ""; int off = 0;
	for (int i = 0; i < n; i++) {
		if (pids[i] != target) {
			if (responsibility_get_pid_responsible_for_pid(pids[i]) != target) continue;
			char path[PROC_PIDPATHINFO_MAXSIZE];
			if (proc_pidpath(pids[i], path, sizeof path) <= 0 || !strstr(path, "com.apple.WebKit.")) continue;
		}
		struct rusage_info_v4 ri;
		if (proc_pid_rusage(pids[i], RUSAGE_INFO_V4, (rusage_info_t *)&ri) != 0) continue;
		cpu += (double)(ri.ri_user_time + ri.ri_system_time) * tb.numer / tb.denom / 1e9;
		rss += ri.ri_phys_footprint / 1048576.0;
		off += snprintf(list + off, sizeof list - off, " %d", pids[i]);
	}
	printf("%.3f %.1f%s\n", cpu, rss, list);
	return 0;
}
