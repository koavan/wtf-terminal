// Headless check of libwtf's C interface: start the backend, call a
// service, run a shell, read its output, and wait for its exit event.
//   cmd/libwtf/smoke/run.sh
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include "libwtf.h"

static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static char output[1 << 16];
static size_t outlen;
static int exited, ended;
static char id[64];

static void on_event(void *ctx, const char *name, const char *json) {
	pthread_mutex_lock(&mu);
	if (strcmp(name, "terminal:exit") == 0 && strstr(json, id)) exited = 1;
	pthread_mutex_unlock(&mu);
}

static void on_output(void *ctx, const char *buf, int64_t len) {
	pthread_mutex_lock(&mu);
	if (len < 0) ended = 1;
	else if (outlen + len < sizeof output) { memcpy(output + outlen, buf, len); outlen += len; }
	pthread_mutex_unlock(&mu);
}

static int fail(const char *what, const char *detail) {
	fprintf(stderr, "FAIL %s: %s\n", what, detail ? detail : "");
	return 1;
}

int main(int argc, char **argv) {
	if (argc != 3) return fail("usage", "smoke <state-dir> <cache-dir>");
	char *err = wtf_init("smoke", "", "", argv[1], argv[2], on_event, NULL);
	if (err) return fail("init", err);

	char *about = wtf_call("about", "Info", "[]");
	if (!strstr(about, "\"version\":\"smoke\"")) return fail("about.Info", about);
	printf("ok   about.Info\n");
	wtf_free(about);

	char *bad = wtf_call("about", "Nope", "[]");
	if (!strstr(bad, "\"error\"")) return fail("unknown method", bad);
	printf("ok   unknown method -> error\n");
	wtf_free(bad);

	char *created = wtf_call("terminal", "Create", "[\"smoke\", 80, 24, \"\"]");
	char *q = strstr(created, "\"result\":\"");
	if (!q) return fail("terminal.Create", created);
	q += 10;
	snprintf(id, sizeof id, "\"%.*s\"", (int)(strchr(q, '"') - q), q);
	printf("ok   terminal.Create %s\n", id);
	char rawid[64]; snprintf(rawid, sizeof rawid, "%.*s", (int)strlen(id) - 2, id + 1);
	wtf_free(created);

	if ((err = wtf_attach(rawid, on_output, NULL))) return fail("attach", err);
	if (!(err = wtf_attach(rawid, on_output, NULL))) return fail("second attach", "accepted");
	wtf_free(err);
	printf("ok   attach (and second attach refused)\n");

	const char *cmd = "printf 'smoke-%s\\n' ok; exit 3\r";
	sleep(1);
	wtf_write(rawid, cmd, strlen(cmd));
	for (int i = 0; i < 100; i++) {
		pthread_mutex_lock(&mu);
		int done = exited && ended;
		pthread_mutex_unlock(&mu);
		if (done) break;
		usleep(100000);
	}
	output[outlen] = 0;
	if (!strstr(output, "smoke-ok")) return fail("shell output", output);
	printf("ok   shell output\n");
	if (!exited) return fail("terminal:exit event", "not received");
	if (!ended) return fail("end of stream", "not signalled");
	printf("ok   terminal:exit event and end of stream\n");

	wtf_shutdown();
	printf("ok   shutdown\n");
	return 0;
}
