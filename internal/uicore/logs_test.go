package uicore

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/files"
)

func logKinds(line string) []string {
	var out []string
	for _, t := range LogScan(line) {
		switch t.Kind {
		case LogKindLevel:
			out = append(out, "level:"+string(t.Level)+":"+t.Text)
		case LogKindJWT:
			out = append(out, "jwt")
		default:
			out = append(out, string(t.Kind)+":"+t.Text)
		}
	}
	return out
}

func orDash(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func logIPs(line string) []string {
	var out []string
	for _, t := range LogScan(line) {
		if t.Kind == LogKindIP {
			out = append(out, t.Address+"|"+orDash(t.Port)+"|"+strconv.FormatBool(t.CIDR))
		}
	}
	return out
}

func logPaths(line string) []string {
	var out []string
	for _, t := range LogScan(line) {
		if t.Kind == LogKindPath {
			out = append(out, t.Path+"|"+strconv.Itoa(t.Line)+"|"+orDash(t.Col))
		}
	}
	return out
}

func logK8sOf(line string) []string {
	var out []string
	for _, t := range LogScan(line) {
		if t.Kind == LogKindK8s {
			out = append(out, t.Resource+"|"+t.Name)
		}
	}
	return out
}

func expectList(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func TestLogScansALogLine(t *testing.T) {
	expectList(t, "line", logKinds("2026-10-09T12:34:56.789Z ERROR request from 10.0.3.7:52144 failed: see https://example.com/x."),
		[]string{"timestamp:2026-10-09T12:34:56.789Z", "level:error:ERROR", "ip:10.0.3.7:52144", "url:https://example.com/x"})
}

func TestLogLevels(t *testing.T) {
	expectList(t, "WARN", logKinds("[WARN] disk 91% full"), []string{"level:warn:WARN"})
	expectList(t, "level=", logKinds(`level=error msg="boom"`), []string{"level:error:error"})
	expectList(t, "json", logKinds(`{"level":"info","msg":"ok"}`), []string{"level:info:info"})
	expectList(t, "[debug]", logKinds("[debug] cache hit"), []string{"level:debug:debug"})
	expectList(t, "FATAL", logKinds("FATAL: out of memory"), []string{"level:error:FATAL"})
	expectList(t, "words", logKinds("ERROR_CODES.md INFOS warning: an error happened"), nil)
}

func TestLogTimestamps(t *testing.T) {
	expectList(t, "zoned", logKinds("2026-10-09 12:34:56,123 +02:00 started"), []string{"timestamp:2026-10-09 12:34:56,123 +02:00"})
	if got := logKinds(`1.2.3.4 - - [09/Oct/2026:13:55:36 -0700] "GET /"`); len(got) < 2 || got[1] != "timestamp:09/Oct/2026:13:55:36 -0700" {
		t.Errorf("access log: %q", got)
	}
	expectList(t, "syslog", logKinds("Oct  9 12:34:56 host sshd[42]: Accepted"), []string{"timestamp:Oct  9 12:34:56"})
	expectList(t, "time", logKinds("took until 12:34:56.5 today"), []string{"time:12:34:56.5"})
	for _, k := range logKinds("ether aa:bb:12:34:56:78") {
		if strings.HasPrefix(k, "time:") {
			t.Errorf("MAC address read as %q", k)
		}
	}
}

func TestLogIPAddresses(t *testing.T) {
	expectList(t, "v4", logIPs("peer 192.168.1.20:8443 in 10.0.0.0/16."), []string{"192.168.1.20|8443|false", "10.0.0.0|-|true"})
	expectList(t, "v6", logIPs("listen [::1]:80 and fe80::1c2:3 and 2001:db8:0:0:0:0:2:1"),
		[]string{"::1|-|false", "fe80::1c2:3|-|false", "2001:db8:0:0:0:0:2:1|-|false"})
	expectList(t, "not addresses", logIPs("version 999.1.1.1 and 1.2.3.4.5"), nil)
	expectList(t, "C++", logIPs("std::vector Foo::Bar"), nil)
}

func TestLogFilePaths(t *testing.T) {
	expectList(t, "go", logPaths("panic at /Users/me/app/main.go:42 +0x1d"), []string{"/Users/me/app/main.go|42|-"})
	expectList(t, "ts", logPaths("src/lib/app.ts:3:14 - error TS2304"), []string{"src/lib/app.ts|3|14"})
	expectList(t, "python", logPaths(`  File "app/views.py", line 88, in index`), []string{"app/views.py|88|-"})
	expectList(t, "relative", logPaths("./run.sh:7: command not found"), []string{"./run.sh|7|-"})
	expectList(t, "not paths", logPaths("at 12:30:45 and v1.2:3"), nil)
}

func TestLogKubernetesResources(t *testing.T) {
	expectList(t, "group", logK8sOf("deployment.apps/api-server restarted"), []string{"deployment.apps|api-server"})
	expectList(t, "two", logK8sOf("pod/web-7d9f8-x2x9z created, service/web unchanged."), []string{"pod|web-7d9f8-x2x9z", "service|web"})
	expectList(t, "not resources", logK8sOf("src/pods/list.ts and ./secret/key and pod/UPPER"), nil)
	for r, k := range map[string]string{"deploy": "deployment", "deployments.apps": "deployment", "pods": "pod", "ingresses": "ingress",
		"networkpolicies": "networkpolicy", "storageclasses": "storageclass", "endpoints": "endpoints", "job.batch": "job"} {
		if got := LogK8sKind(r); got != k {
			t.Errorf("LogK8sKind(%q) = %q, want %q", r, got, k)
		}
	}
}

func TestLogURLsAndSpans(t *testing.T) {
	expectList(t, "url with a port", logKinds("GET http://10.1.2.3:8080/health.json:12 ok"), []string{"url:http://10.1.2.3:8080/health.json:12"})
	expectList(t, "brackets", logKinds("(see https://en.wikipedia.org/wiki/Foo_(bar))"), []string{"url:https://en.wikipedia.org/wiki/Foo_(bar)"})
	line := "x  WARN  pod/a-1"
	for _, tok := range LogScan(line) {
		if line[tok.Start:tok.End] != tok.Text {
			t.Errorf("span %d–%d is %q, text %q", tok.Start, tok.End, line[tok.Start:tok.End], tok.Text)
		}
	}
	if got := LogScan(strings.Repeat("ERROR ", 1000)); len(got) != 0 {
		t.Errorf("long line: %d tokens", len(got))
	}
}

func TestLogParsesTimestamps(t *testing.T) {
	utc := func(s string) string {
		at, _, ok := LogParseTimestamp(s, time.Now(), time.Local)
		if !ok {
			return ""
		}
		return at.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	for in, want := range map[string]string{
		"2026-10-09T12:34:56.789Z":     "2026-10-09T12:34:56.789Z",
		"2026-10-09 14:34:56,5 +02:00": "2026-10-09T12:34:56.500Z",
		"09/Oct/2026:13:55:36 -0700":   "2026-10-09T20:55:36.000Z",
	} {
		if got := utc(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if _, zoned, _ := LogParseTimestamp("2026-10-09T12:34:56.789Z", time.Now(), time.Local); !zoned {
		t.Error("Z isn't zoned")
	}
	if at, zoned, ok := LogParseTimestamp("2026-10-09 12:34", time.Now(), time.Local); !ok || zoned || at.In(time.Local).Hour() != 12 {
		t.Errorf("no zone: %v %v %v", at, zoned, ok)
	}
	oct10 := time.Date(2026, 10, 10, 0, 0, 0, 0, time.Local)
	if at, _, _ := LogParseTimestamp("Oct  9 12:34:56", oct10, time.Local); at.In(time.Local).Year() != 2026 {
		t.Errorf("syslog year %v", at)
	}
	jan2 := time.Date(2027, 1, 2, 0, 0, 0, 0, time.Local)
	if at, _, _ := LogParseTimestamp("Dec 31 23:00:00", jan2, time.Local); at.In(time.Local).Year() != 2026 {
		t.Errorf("last December %v", at)
	}
	for _, bad := range []string{"2026-13-40T10:00:00Z", "12:34:56"} {
		if _, _, ok := LogParseTimestamp(bad, time.Now(), time.Local); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

var menuTarget = KubeTarget{Context: "prod", Namespace: "shop"}

func logMenuOf(line string, target KubeTarget, now time.Time) []LogEntry {
	for _, tok := range LogScan(line) {
		if tok.Kind != LogKindLevel {
			return LogMenu(tok, target, now, time.UTC)
		}
	}
	return nil
}

func logLabels(m []LogEntry) []string {
	var out []string
	for _, e := range m {
		switch e.Kind {
		case LogHeading:
			out = append(out, "# "+e.Label)
		case LogItem:
			out = append(out, e.Label)
		default:
			out = append(out, "—")
		}
	}
	return out
}

func logActionOf(m []LogEntry, label string) (LogAction, bool) {
	for _, e := range m {
		if e.Kind == LogItem && e.Label == label {
			return e.Action, true
		}
	}
	return LogAction{}, false
}

func expectAction(t *testing.T, m []LogEntry, label string, want LogAction) {
	t.Helper()
	if got, ok := logActionOf(m, label); !ok || got != want {
		t.Errorf("%q: %+v (%v), want %+v", label, got, ok, want)
	}
}

func TestLogKubeMenus(t *testing.T) {
	m := logMenuOf("pod/web-1 created", menuTarget, time.Now())
	expectList(t, "pod", logLabels(m), []string{"# pod/web-1", "# Context prod · namespace shop", "Logs", "Describe", "View YAML", "—", "Copy Name", "Copy pod/web-1"})
	expectAction(t, m, "Logs", LogAction{Kind: LogRunInTab, Text: "kubectl --context prod -n shop logs -f --tail=200 web-1"})
	expectAction(t, m, "Describe", LogAction{Kind: LogDescribe, ResourceKind: "pod", Name: "web-1"})
	d := logMenuOf("deployment.apps/api restarted", KubeTarget{}, time.Now())
	if l := logLabels(d); l[1] != "# kubectl's current context (this shell reported none)" {
		t.Errorf("no context: %q", l[1])
	}
	expectAction(t, d, "Logs", LogAction{Kind: LogRunInTab, Text: "kubectl logs -f --tail=200 --all-containers --prefix deployment/api"})
	s := logMenuOf("secret/db-creds configured", menuTarget, time.Now())
	if slices.Contains(logLabels(s), "View YAML") {
		t.Error("a secret's YAML")
	}
	expectAction(t, s, "View Decoded Values…", LogAction{Kind: LogShowSecret, Text: "db-creds"})
}

func TestLogOtherMenus(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 5, 0, 0, time.UTC)
	m := logMenuOf("2026-10-09T12:00:00Z start", menuTarget, now)
	if l := logLabels(m); !strings.HasSuffix(l[0], "your time · 5 min ago") || slices.Contains(l, "# No time zone in the text: read as your time") {
		t.Errorf("timestamp: %q", l)
	}
	expectAction(t, m, "Copy as Unix Time", LogAction{Kind: LogCopy, Text: "1791547200", What: "Unix time"})
	expectAction(t, m, "Copy as UTC (ISO 8601)", LogAction{Kind: LogCopy, Text: "2026-10-09T12:00:00.000Z", What: "time"})
	if l := logLabels(logMenuOf("2026-10-09 12:00:00 start", menuTarget, now)); !slices.Contains(l, "# No time zone in the text: read as your time") {
		t.Errorf("no zone: %q", l)
	}
	ip := logMenuOf("10.0.0.5:8080 up", menuTarget, now)
	expectAction(t, ip, "Open http://10.0.0.5:8080", LogAction{Kind: LogOpenURL, Text: "http://10.0.0.5:8080"})
	expectAction(t, ip, "SSH to 10.0.0.5", LogAction{Kind: LogRunHere, Text: "ssh 10.0.0.5"})
	expectList(t, "CIDR", logLabels(logMenuOf("10.0.0.0/8", menuTarget, now)), []string{"# 10.0.0.0/8", "Copy Address"})
	p := LogScan("main.go:12")[0]
	p.Path = "/src/main.go"
	expectAction(t, LogMenu(p, menuTarget, now, time.UTC), "Open in Editor at Line 12", LogAction{Kind: LogOpenInEditor, Text: "/src/main.go", Line: 12})
	if len(logMenuOf("ERROR", menuTarget, now)) != 0 || len(logMenuOf("at 10:00:00 ok", menuTarget, now)) != 0 {
		t.Error("levels and times have menus")
	}
	expectAction(t, logMenuOf("see https://api.example.com/v1", menuTarget, now), "Inspect TLS Certificate…", LogAction{Kind: LogInspectTLS, Text: "https://api.example.com/v1"})
	if _, ok := logActionOf(logMenuOf("see http://api.example.com/v1", menuTarget, now), "Inspect TLS Certificate…"); ok {
		t.Error("TLS for http")
	}
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZGEifQ.sig"
	j := logMenuOf(jwt, menuTarget, now)
	expectList(t, "JWT", logLabels(j), []string{"# JWT", "Inspect Token…", "Copy Token"})
	expectAction(t, j, "Copy Token", LogAction{Kind: LogCopySecret, Text: jwt, What: "token"})
	hint := "Ctrl-click"
	if runtime.GOOS == "darwin" {
		hint = "⌘-click"
	}
	if j[1].Hint != hint {
		t.Errorf("hint %q", j[1].Hint)
	}
}

func TestLogResolvesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolve := files.NewService().ResolveFile
	path := func(name string) LogToken { return LogToken{Kind: LogKindPath, Path: name, Line: 1} }
	if got, ok := LogResolve(path("main.go"), dir, resolve); !ok || got.Path != filepath.Join(dir, "main.go") {
		t.Errorf("main.go: %q %v", got.Path, ok)
	}
	if _, ok := LogResolve(path("nope.go"), dir, resolve); ok {
		t.Error("nope.go resolved")
	}
	if _, ok := LogResolve(path("main.go"), "", resolve); ok {
		t.Error("resolved without a folder")
	}
	if _, ok := LogResolve(path("."), dir, resolve); ok {
		t.Error("a folder resolved")
	}
	if got, ok := LogResolve(LogToken{Kind: LogKindURL, Text: "https://x"}, dir, resolve); !ok || got.Text != "https://x" {
		t.Error("a URL changed")
	}
}

// Where JavaScript's lookaheads back off an optional tail, so do the Go
// patterns.
func TestLogLookaheadBackoff(t *testing.T) {
	expectList(t, "fraction", logKinds("at 12:34:56.5x"), []string{"time:12:34:56"})
	expectList(t, "port", logIPs("10.0.0.1:123456"), []string{"10.0.0.1|-|false"})
	expectList(t, "prefix", logIPs("10.0.0.1/123"), []string{"10.0.0.1|-|false"})
	expectList(t, "v6 prefix", logIPs("fe80::1/1234"), []string{"fe80::1|-|false"})
	expectList(t, "name", logK8sOf("pod/web.xY"), []string{"pod|web"})
	expectList(t, "zone", logKinds("2026-10-09T12:34:56Zulu"), []string{"timestamp:2026-10-09T12:34:56"})
	expectList(t, "column", logPaths("a.go:1:2:3"), nil)
}
