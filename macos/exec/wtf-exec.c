// wtf-exec: starts a shell with a clean signal state.
//
//   wtf-exec <path> <argv0> [args...]
//
// The native app's backend is Go built as a library, and Go's threads there
// block the signals Go doesn't handle. A shell forked from one of them would
// inherit that mask and pass it on to every program it runs: no Ctrl-C
// (SIGINT), no hang-up when wtf quits (SIGHUP), no resize signal (SIGWINCH).
// This unblocks everything, puts the usual signals back to their default
// actions, and execs the shell as asked (argv0 may be "-bash" for a login shell).
#include <signal.h>
#include <stdio.h>
#include <errno.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
	if (argc < 3) {
		fprintf(stderr, "usage: wtf-exec <path> <argv0> [args...]\n");
		return 2;
	}
	sigset_t none;
	sigemptyset(&none);
	sigprocmask(SIG_SETMASK, &none, NULL);
	int sigs[] = {SIGHUP, SIGINT, SIGQUIT, SIGTERM, SIGPIPE, SIGALRM, SIGCHLD, SIGTSTP, SIGTTIN, SIGTTOU,
		SIGCONT, SIGWINCH, SIGUSR1, SIGUSR2, SIGINFO, SIGIO, SIGXCPU, SIGXFSZ, SIGVTALRM, SIGPROF};
	for (size_t i = 0; i < sizeof sigs / sizeof sigs[0]; i++) signal(sigs[i], SIG_DFL);
	execv(argv[1], argv + 2);
	fprintf(stderr, "wtf-exec: %s: %s\n", argv[1], strerror(errno));
	return 127;
}
