package uicore

import (
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// What a terminal's output says, as tokens: log levels, timestamps, URLs,
// IP addresses, file:line paths, Kubernetes resources and JWTs. They're
// colored on screen and get a menu on Ctrl-click or right-click. Ports of
// lib/logs.ts and lib/logActions.ts (macos/Sources/WtfCore/Logs.swift).
// Offsets are bytes of the line's text, as in covers.go's logical lines.
//
// Go's regexp has no lookbehind or lookahead, so each pattern is its core,
// and logFind checks the character before a match and what follows it,
// backing off the optional tail of a match where the JavaScript pattern
// would.

// LogLevel is a log level's family.
type LogLevel string

const (
	LogError LogLevel = "error"
	LogWarn  LogLevel = "warn"
	LogInfo  LogLevel = "info"
	LogDebug LogLevel = "debug"
)

// LogKind is what a token is.
type LogKind string

const (
	LogKindLevel     LogKind = "level"
	LogKindTimestamp LogKind = "timestamp"
	LogKindTime      LogKind = "time"
	LogKindURL       LogKind = "url"
	LogKindJWT       LogKind = "jwt"
	LogKindIP        LogKind = "ip"
	LogKindPath      LogKind = "path"
	LogKindK8s       LogKind = "k8s"
)

// LogToken is a token in a line of output: bytes [Start, End) of it.
type LogToken struct {
	Kind LogKind
	// Level: its family.
	Level LogLevel
	// IP: the address, its port (0 for none), and whether it's a CIDR block.
	Address string
	Port    int
	CIDR    bool
	// Path: the path as written, its line, and its column (0 for none).
	Path      string
	Line, Col int
	// K8s: the resource as written (`deployment.apps`) and the name.
	Resource, Name string

	Start, End int
	Text       string
}

// LogMaxScan is the longest line scanned, in UTF-16 units as the web
// build counts (minified files and base64 blobs aren't).
const LogMaxScan = 4000

const logMonth = `(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`

var (
	logURL         = regexp.MustCompile(`\b(?:https?|ftp|file)://[^\s<>"'` + "`" + `]+`)
	logJWT         = regexp.MustCompile(InspectJWTPattern)
	logISO         = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2}(?:[.,]\d{1,9})?)?`)
	logISOZone     = regexp.MustCompile(`^ ?(?:Z|[+-]\d{2}:?\d{2})`)
	logAccess      = regexp.MustCompile(`\d{2}/` + logMonth + `/\d{4}:\d{2}:\d{2}:\d{2}(?: [+-]\d{4})?`)
	logSyslog      = regexp.MustCompile(logMonth + ` [ \d]\d \d{2}:\d{2}:\d{2}`)
	logTime        = regexp.MustCompile(`\d{2}:\d{2}:\d{2}(?:[.,]\d{1,9})?`)
	logOctet       = `(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)`
	logIPv4        = regexp.MustCompile(`((?:` + logOctet + `\.){3}` + logOctet + `)(?:(/\d{1,2})|:(\d{1,5}))?`)
	logHex         = `[0-9a-fA-F]{1,4}`
	logIPv6        = regexp.MustCompile(`((?:` + logHex + `(?::` + logHex + `)*)?::(?:` + logHex + `(?::` + logHex + `)*)?|(?:` + logHex + `:){7}` + logHex + `)(/\d{1,3})?`)
	logPath        = regexp.MustCompile(`((?:~|\.{1,2})?/?(?:[\w.@+-]+/)*[\w@+-][\w.@+-]*\.[A-Za-z]\w{0,9}):(\d+)(?::(\d+))?`)
	logPyPath      = regexp.MustCompile(`File "([^"]+)", line (\d+)`)
	logK8sKindsAlt = strings.Join([]string{"pods?", "deployments?", "deploy", "statefulsets?", "sts", "daemonsets?", "replicasets?", "jobs?", "cronjobs?",
		"services?", "svc", "configmaps?", "secrets?", "ingress(?:es)?", "nodes?", "namespaces?",
		"persistentvolumeclaims?", "pvc", "persistentvolumes?", "serviceaccounts?", "horizontalpodautoscalers?", "hpa",
		"networkpolic(?:y|ies)", "endpoints", "endpointslices?", "roles?", "rolebindings?", "clusterroles?",
		"clusterrolebindings?", "poddisruptionbudgets?", "pdb", "storageclass(?:es)?", "customresourcedefinitions?", "crd"}, "|")
	logK8s = regexp.MustCompile(`((?:` + logK8sKindsAlt + `)(?:\.[a-z0-9-]+(?:\.[a-z0-9-]+)*)?)/([a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?)`)
)

type logLevelWord struct {
	word  string
	level LogLevel
}

var logLevelWords = []logLevelWord{
	{"FATAL", LogError}, {"PANIC", LogError}, {"CRITICAL", LogError}, {"CRIT", LogError}, {"EMERG", LogError}, {"ALERT", LogError},
	{"ERROR", LogError}, {"ERR", LogError}, {"SEVERE", LogError}, {"WARNING", LogWarn}, {"WARN", LogWarn},
	{"INFO", LogInfo}, {"NOTICE", LogInfo}, {"DEBUG", LogDebug}, {"TRACE", LogDebug},
}

var logLevelAlt = func() string {
	var w []string
	for _, l := range logLevelWords {
		w = append(w, l.word)
	}
	return strings.Join(w, "|")
}()

var (
	logLevelBare  = regexp.MustCompile(`(` + logLevelAlt + `)`)
	logLevelField = regexp.MustCompile(`(?i)(?:\b(?:level|lvl|severity)["']?\s*[=:]\s*["']?|\[)(` + logLevelAlt + `)`)
)

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// logByteBefore is the byte before i, or 0 at the start.
func logByteBefore(text string, i int) byte {
	if i == 0 {
		return 0
	}
	return text[i-1]
}

// logByteAt is the byte at i, or 0 at the end.
func logByteAt(text string, i int) byte {
	if i >= len(text) {
		return 0
	}
	return text[i]
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

// logFind runs a pattern over the text as a JavaScript regex with
// lookarounds would: each match the core pattern finds goes to check, which
// gives back its submatches (perhaps shortened) or nil to try again one
// character further on.
func logFind(re *regexp.Regexp, text string, check func(m []int) []int, each func(m []int)) {
	for pos := 0; pos < len(text); {
		m := re.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			return
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		if got := check(m); got != nil {
			each(got)
			pos = max(got[1], got[0]+1)
			continue
		}
		_, n := utf8.DecodeRuneInString(text[m[0]:])
		pos = m[0] + max(n, 1)
	}
}

// cut ends a match at end, dropping the groups from first on.
func cut(m []int, end, first int) []int {
	out := slices.Clone(m)
	out[1] = end
	for i := first * 2; i < len(out); i++ {
		out[i] = -1
	}
	return out
}

func group(text string, m []int, i int) (string, bool) {
	if m[2*i] < 0 {
		return "", false
	}
	return text[m[2*i]:m[2*i+1]], true
}

// LogTrimURL is a URL without the punctuation after it, keeping a closing
// bracket it opened.
func LogTrimURL(url string) string {
	u := url
	pairs := map[byte]string{')': "(", ']': "[", '}': "{"}
	for u != "" && strings.IndexByte(".,;:!?'\")]}>", u[len(u)-1]) >= 0 {
		last := u[len(u)-1]
		if open, ok := pairs[last]; ok && strings.Count(u, open) >= strings.Count(u, string(last)) {
			return u
		}
		u = u[:len(u)-1]
	}
	return u
}

func logUTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// LogScan is the tokens in a line of output, in order; where they'd
// overlap, the first found wins.
func LogScan(text string) []LogToken {
	if text == "" || len(text) > 4*LogMaxScan || logUTF16Len(text) > LogMaxScan {
		return nil
	}
	var taken []LogToken
	add := func(tok LogToken, start, end int) {
		if end <= start {
			return
		}
		for _, t := range taken {
			if end > t.Start && start < t.End {
				return
			}
		}
		tok.Start, tok.End, tok.Text = start, end, text[start:end]
		taken = append(taken, tok)
	}
	all := func(m []int) []int { return m }

	for _, m := range logURL.FindAllStringIndex(text, -1) {
		add(LogToken{Kind: LogKindURL}, m[0], m[0]+len(LogTrimURL(text[m[0]:m[1]])))
	}
	for _, m := range logJWT.FindAllStringIndex(text, -1) {
		add(LogToken{Kind: LogKindJWT}, m[0], m[1])
	}
	// 2026-10-09T12:34:56.789Z: not after a word or a dot; the zone only if
	// no word or colon follows it.
	logFind(logISO, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || c == '.' {
			return nil
		}
		if z := logISOZone.FindStringIndex(text[m[1]:]); z != nil {
			if c := logByteAt(text, m[1]+z[1]); !isWordByte(c) && c != ':' {
				m[1] += z[1]
			}
		}
		return m
	}, func(m []int) { add(LogToken{Kind: LogKindTimestamp}, m[0], m[1]) })
	// 09/Oct/2026:13:55:36 -0700
	logFind(logAccess, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || c == '/' {
			return nil
		}
		return m
	}, func(m []int) { add(LogToken{Kind: LogKindTimestamp}, m[0], m[1]) })
	// Oct  9 12:34:56
	logFind(logSyslog, text, func(m []int) []int {
		if isWordByte(logByteBefore(text, m[0])) {
			return nil
		}
		if c := logByteAt(text, m[1]); isWordByte(c) || c == ':' {
			return nil
		}
		return m
	}, func(m []int) { add(LogToken{Kind: LogKindTimestamp}, m[0], m[1]) })
	logFind(logPyPath, text, all, func(m []int) {
		line, _ := group(text, m, 2)
		n, _ := strconv.Atoi(line)
		add(LogToken{Kind: LogKindPath, Path: text[m[2]:m[3]], Line: n}, m[2], m[3])
	})
	// path:line[:col], not inside a longer word or path, and not followed
	// by more of one (a word, a dot, or :digits).
	logFind(logPath, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || strings.IndexByte("/.~@+-", c) >= 0 {
			return nil
		}
		if c := logByteAt(text, m[1]); isWordByte(c) || c == '.' || c == ':' && isDigitByte(logByteAt(text, m[1]+1)) {
			return nil
		}
		return m
	}, func(m []int) {
		p, _ := group(text, m, 1)
		line, _ := group(text, m, 2)
		tok := LogToken{Kind: LogKindPath, Path: p}
		tok.Line, _ = strconv.Atoi(line)
		if col, ok := group(text, m, 3); ok {
			tok.Col, _ = strconv.Atoi(col)
		}
		add(tok, m[0], m[1])
	})
	// kind/name; a name followed by a word gives back up to a dot in it.
	logFind(logK8s, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || c == '.' || c == '/' || c == '-' {
			return nil
		}
		ok := func(end int) bool { c := logByteAt(text, end); return !isWordByte(c) && c != '/' && c != '-' }
		for end := m[1]; end > m[4]; end-- {
			if c := text[end-1]; (c >= 'a' && c <= 'z' || isDigitByte(c)) && ok(end) {
				out := slices.Clone(m)
				out[1], out[5] = end, end
				return out
			}
		}
		return nil
	}, func(m []int) {
		r, _ := group(text, m, 1)
		n, _ := group(text, m, 2)
		add(LogToken{Kind: LogKindK8s, Resource: r, Name: n}, m[0], m[1])
	})
	// a.b.c.d, a.b.c.d/nn or a.b.c.d:port; a bad port or prefix length
	// leaves the address alone.
	logFind(logIPv4, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || c == '.' {
			return nil
		}
		ok := func(end int) bool {
			c := logByteAt(text, end)
			return !isWordByte(c) && !(c == '.' && isDigitByte(logByteAt(text, end+1)))
		}
		if ok(m[1]) {
			return m
		}
		if m[1] != m[3] && ok(m[3]) {
			return cut(m, m[3], 2)
		}
		return nil
	}, func(m []int) {
		tok := LogToken{Kind: LogKindIP, Address: text[m[2]:m[3]]}
		if p, ok := group(text, m, 3); ok {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				return
			}
			tok.Port = n
		}
		_, tok.CIDR = group(text, m, 2)
		add(tok, m[0], m[1])
	})
	logFind(logIPv6, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || c == ':' {
			return nil
		}
		ok := func(end int) bool { c := logByteAt(text, end); return !isWordByte(c) && c != ':' }
		if ok(m[1]) {
			return m
		}
		if m[1] != m[3] && ok(m[3]) {
			return cut(m, m[3], 2)
		}
		return nil
	}, func(m []int) {
		a := text[m[2]:m[3]]
		if len(a) < 3 {
			return
		}
		_, cidr := group(text, m, 2)
		add(LogToken{Kind: LogKindIP, Address: a, CIDR: cidr}, m[0], m[1])
	})
	// 12:34:56[.5]: a fraction followed by a word is dropped.
	logFind(logTime, text, func(m []int) []int {
		if c := logByteBefore(text, m[0]); isWordByte(c) || c == ':' || c == '.' {
			return nil
		}
		ok := func(end int) bool { c := logByteAt(text, end); return !isWordByte(c) && c != ':' }
		if ok(m[1]) {
			return m
		}
		if m[1] > m[0]+8 && ok(m[0]+8) {
			return cut(m, m[0]+8, 1)
		}
		return nil
	}, func(m []int) { add(LogToken{Kind: LogKindTime}, m[0], m[1]) })
	level := func(m []int) {
		w, _ := group(text, m, 1)
		for _, l := range logLevelWords {
			if l.word == strings.ToUpper(w) {
				add(LogToken{Kind: LogKindLevel, Level: l.level}, m[2], m[3])
				return
			}
		}
	}
	notWordOrDash := func(c byte) bool { return !isWordByte(c) && c != '-' }
	logFind(logLevelBare, text, func(m []int) []int {
		if !notWordOrDash(logByteBefore(text, m[0])) || !notWordOrDash(logByteAt(text, m[1])) {
			return nil
		}
		return m
	}, level)
	logFind(logLevelField, text, func(m []int) []int {
		if !notWordOrDash(logByteAt(text, m[1])) {
			return nil
		}
		return m
	}, level)

	sort.SliceStable(taken, func(i, j int) bool { return taken[i].Start < taken[j].Start })
	return taken
}

// LogK8sKind is a resource's kind: `deployments.apps` → `deployment`,
// `svc` → `service`.
func LogK8sKind(resource string) string {
	base, _, _ := strings.Cut(resource, ".")
	short := map[string]string{"deploy": "deployment", "sts": "statefulset", "svc": "service", "pvc": "persistentvolumeclaim",
		"hpa": "horizontalpodautoscaler", "pdb": "poddisruptionbudget", "crd": "customresourcedefinition"}
	switch {
	case short[base] != "":
		return short[base]
	case base == "endpoints":
		return base
	case strings.HasSuffix(base, "ies"):
		return strings.TrimSuffix(base, "ies") + "y"
	case strings.HasSuffix(base, "sses") || strings.HasSuffix(base, "xes"):
		return base[:len(base)-2]
	case strings.HasSuffix(base, "s") && !strings.HasSuffix(base, "ss"):
		return base[:len(base)-1]
	}
	return base
}

var (
	logMonths      = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	logParseISO    = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})(?::(\d{2})(?:[.,](\d{1,9}))?)? ?(Z|[+-]\d{2}:?\d{2})?$`)
	logParseAccess = regexp.MustCompile(`^(\d{2})/(\w{3})/(\d{4}):(\d{2}):(\d{2}):(\d{2})(?: ([+-]\d{4}))?$`)
	logParseSyslog = regexp.MustCompile(`^(\w{3}) [ \d]?(\d{1,2}) (\d{2}):(\d{2}):(\d{2})$`)
)

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// LogParseTimestamp reads a timestamp token's moment, and whether it said
// its time zone (else it's read as loc's time). now picks a syslog
// timestamp's year.
func LogParseTimestamp(text string, now time.Time, loc *time.Location) (t time.Time, zoned, ok bool) {
	build := func(y, mo, d, h, mi, s, ms int, z string, hasZone bool) (time.Time, bool, bool) {
		if mo < 0 || mo > 11 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 60 {
			return time.Time{}, false, false
		}
		where, offset := loc, 0
		if hasZone {
			where = time.UTC
			if z != "Z" {
				digits := strings.ReplaceAll(z[1:], ":", "")
				if len(digits) != 4 {
					return time.Time{}, false, false
				}
				offset = atoi(digits[:2])*60 + atoi(digits[2:])
				if z[0] == '-' {
					offset = -offset
				}
			}
		}
		t := time.Date(y, time.Month(mo+1), d, h, mi, s, 0, where)
		return t.Add(time.Duration(ms)*time.Millisecond - time.Duration(offset)*time.Minute), hasZone, true
	}
	if m := logParseISO.FindStringSubmatch(text); m != nil {
		frac := 0
		if m[7] != "" {
			f, _ := strconv.ParseFloat("0."+m[7], 64)
			frac = int(f*1000 + 0.5)
		}
		sec := 0
		if m[6] != "" {
			sec = atoi(m[6])
		}
		return build(atoi(m[1]), atoi(m[2])-1, atoi(m[3]), atoi(m[4]), atoi(m[5]), sec, frac, m[8], m[8] != "")
	}
	if m := logParseAccess.FindStringSubmatch(text); m != nil {
		mo := slices.Index(logMonths, m[2])
		if mo < 0 {
			return time.Time{}, false, false
		}
		return build(atoi(m[3]), mo, atoi(m[1]), atoi(m[4]), atoi(m[5]), atoi(m[6]), 0, m[7], m[7] != "")
	}
	if m := logParseSyslog.FindStringSubmatch(text); m != nil {
		mo := slices.Index(logMonths, m[1])
		if mo < 0 {
			return time.Time{}, false, false
		}
		year := now.In(loc).Year()
		t, z, ok := build(year, mo, atoi(m[2]), atoi(m[3]), atoi(m[4]), atoi(m[5]), 0, "", false)
		// December's log read in January was last year.
		if ok && t.Sub(now) > 31*24*time.Hour {
			return build(year-1, mo, atoi(m[2]), atoi(m[3]), atoi(m[4]), atoi(m[5]), 0, "", false)
		}
		return t, z, ok
	}
	return time.Time{}, false, false
}

// Menus

// LogActionKind is what a menu item does.
type LogActionKind int

const (
	LogOpenURL LogActionKind = iota
	LogCopy
	// LogCopySecret copies text the clipboard forgets after a while.
	LogCopySecret
	// LogRunInTab runs the command in a new tab.
	LogRunInTab
	// LogRunHere runs the command in the focused shell, or a new tab if
	// it's busy.
	LogRunHere
	// LogDescribe is kubectl describe: in the shell as is, or with the
	// shell's variables in a new tab.
	LogDescribe
	LogOpenInEditor
	LogOpenFile
	LogReveal
	// LogShowSecret opens the masked secret viewer (the Kubernetes tab's).
	LogShowSecret
	LogInspectJWT
	LogInspectTLS
)

// LogAction is what a menu item does, and with what.
type LogAction struct {
	Kind LogActionKind
	// The URL, text to copy, command, path, secret name, token or TLS target.
	Text string
	// Copy: what was copied, for the message ("URL", "path", …).
	What string
	// Describe: the resource's kind and name.
	ResourceKind, Name string
	// OpenInEditor: the line.
	Line int
}

// LogEntryKind is a menu entry's kind.
type LogEntryKind int

const (
	LogHeading LogEntryKind = iota
	LogItem
	LogSeparator
)

// LogEntry is a menu entry: a heading (Label), an item, or a separator.
type LogEntry struct {
	Kind  LogEntryKind
	Label string
	// A hint shown at the item's end (the shortcut that does it too).
	Hint   string
	Action LogAction
}

func logHeading(s string) LogEntry { return LogEntry{Kind: LogHeading, Label: s} }
func logItem(label string, a LogAction) LogEntry {
	return LogEntry{Kind: LogItem, Label: label, Action: a}
}

var logSeparator = LogEntry{Kind: LogSeparator}

func logClickHint() string {
	if runtime.GOOS == "darwin" {
		return "⌘-click"
	}
	return "Ctrl-click"
}

// logLongTime is `Thu, Oct 9, 2026, 12:34:56`.
func logLongTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon, Jan 2, 2006, 15:04:05")
}

// LogMenu is the menu for a token (paths already made absolute, see
// LogResolve), against loc's time. Levels and bare times have none.
func LogMenu(tok LogToken, target KubeTarget, now time.Time, loc *time.Location) []LogEntry {
	text := tok.Text
	switch tok.Kind {
	case LogKindURL:
		heading := text
		if utf8.RuneCountInString(text) > 60 {
			heading = string([]rune(text)[:59]) + "…"
		}
		open := logItem("Open in Browser", LogAction{Kind: LogOpenURL, Text: text})
		open.Hint = logClickHint()
		out := []LogEntry{logHeading(heading), open, logItem("Copy URL", LogAction{Kind: LogCopy, Text: text, What: "URL"})}
		if strings.HasPrefix(strings.ToLower(text), "https://") {
			out = append(out, logItem("Inspect TLS Certificate…", LogAction{Kind: LogInspectTLS, Text: text}))
		}
		return out
	case LogKindJWT:
		inspect := logItem("Inspect Token…", LogAction{Kind: LogInspectJWT, Text: text})
		inspect.Hint = logClickHint()
		return []LogEntry{logHeading("JWT"), inspect, logItem("Copy Token", LogAction{Kind: LogCopySecret, Text: text, What: "token"})}
	case LogKindTimestamp:
		t, zoned, ok := LogParseTimestamp(text, now, loc)
		if !ok {
			return nil
		}
		out := []LogEntry{
			logHeading(logLongTime(t, loc) + " your time · " + RelativeTime(t, now)),
			logHeading(logLongTime(t, time.UTC) + " UTC"),
		}
		if !zoned {
			out = append(out, logHeading("No time zone in the text: read as your time"))
		}
		secs := t.Unix()
		return append(out,
			logItem("Copy as UTC (ISO 8601)", LogAction{Kind: LogCopy, Text: t.UTC().Format("2006-01-02T15:04:05.000Z07:00"), What: "time"}),
			logItem("Copy as Your Time", LogAction{Kind: LogCopy, Text: logLongTime(t, loc), What: "time"}),
			logItem("Copy as Unix Time", LogAction{Kind: LogCopy, Text: strconv.FormatInt(secs, 10), What: "Unix time"}))
	case LogKindIP:
		out := []LogEntry{logHeading(text), logItem("Copy Address", LogAction{Kind: LogCopy, Text: tok.Address, What: "address"})}
		if tok.Port != 0 {
			out = append(out, logItem("Copy "+text, LogAction{Kind: LogCopy, Text: text, What: "address and port"}))
		}
		if !tok.CIDR {
			host := tok.Address
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			if tok.Port != 0 {
				u := fmt.Sprintf("http://%s:%d", host, tok.Port)
				out = append(out, logItem("Open "+u, LogAction{Kind: LogOpenURL, Text: u}))
			}
			out = append(out, logItem("SSH to "+tok.Address, LogAction{Kind: LogRunHere, Text: "ssh " + tok.Address}))
		}
		return out
	case LogKindPath:
		reveal := "Open Containing Folder"
		if runtime.GOOS == "darwin" {
			reveal = "Reveal in Finder"
		}
		at := fmt.Sprintf("%s:%d", tok.Path, tok.Line)
		return []LogEntry{
			logHeading(at),
			logItem(fmt.Sprintf("Open in Editor at Line %d", tok.Line), LogAction{Kind: LogOpenInEditor, Text: tok.Path, Line: tok.Line}),
			logItem("Open with Default App", LogAction{Kind: LogOpenFile, Text: tok.Path}),
			logItem(reveal, LogAction{Kind: LogReveal, Text: tok.Path}),
			logSeparator,
			logItem("Copy Path", LogAction{Kind: LogCopy, Text: tok.Path, What: "path"}),
			logItem(fmt.Sprintf("Copy Path:%d", tok.Line), LogAction{Kind: LogCopy, Text: at, What: "path and line"}),
		}
	case LogKindK8s:
		kind, name := LogK8sKind(tok.Resource), tok.Name
		where := "kubectl's current context (this shell reported none)"
		if target.Context != "" {
			where = "Context " + target.Context
			if target.Namespace != "" {
				where += " · namespace " + target.Namespace
			}
		}
		out := []LogEntry{logHeading(kind + "/" + name), logHeading(where)}
		if kind == "pod" {
			out = append(out, logItem("Logs", LogAction{Kind: LogRunInTab, Text: target.Logs(name, "", false)}))
		} else if slices.Contains([]string{"deployment", "statefulset", "daemonset", "replicaset", "job"}, kind) {
			out = append(out, logItem("Logs", LogAction{Kind: LogRunInTab, Text: target.WorkloadLogs(kind, name)}))
		}
		out = append(out, logItem("Describe", LogAction{Kind: LogDescribe, ResourceKind: kind, Name: name}))
		// A secret's YAML would print its values in the clear.
		if kind == "secret" {
			out = append(out, logItem("View Decoded Values…", LogAction{Kind: LogShowSecret, Text: name}))
		} else {
			out = append(out, logItem("View YAML", LogAction{Kind: LogRunInTab, Text: target.YAML(kind, name)}))
		}
		return append(out, logSeparator,
			logItem("Copy Name", LogAction{Kind: LogCopy, Text: name, What: "name"}),
			logItem("Copy "+kind+"/"+name, LogAction{Kind: LogCopy, Text: kind + "/" + name, What: "resource"}))
	}
	return nil
}

// LogIsLink says Ctrl-click opens a menu for the kind (URLs open directly;
// levels and times aren't links).
func LogIsLink(k LogKind) bool {
	switch k {
	case LogKindTimestamp, LogKindIP, LogKindPath, LogKindK8s, LogKindJWT:
		return true
	}
	return false
}

// LogResolve makes a path token's path absolute, if it names a file on
// this computer: resolve is files.Service.ResolveFile (`~` is home,
// relative paths start at the shell's folder cwd; "" for no such file).
// Other tokens come back as they are.
func LogResolve(tok LogToken, cwd string, resolve func(dir, name string) string) (LogToken, bool) {
	if tok.Kind != LogKindPath {
		return tok, true
	}
	p := resolve(cwd, tok.Path)
	if p == "" {
		return tok, false
	}
	tok.Path = p
	return tok, true
}
