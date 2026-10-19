// Package uicore holds the native UIs' rules that don't touch a toolkit:
// what counts as a secret, when a paste is shown first, fuzzy matching, the
// keymap. Each is a port of the matching frontend/src/lib file (and of
// macos/Sources/WtfCore), tested against the same cases.
package uicore

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// SecretSpan is one secret found in a text.
type SecretSpan struct {
	// Byte offsets into the text.
	Start, End int
	// What it looks like, e.g. "AWS access key".
	Kind string
}

// Redacted is what copied text shows instead of a secret.
const Redacted = "[REDACTED]"

// Finds secrets in text, for Hide Secrets (screen sharing): access keys,
// tokens, JWTs, passwords in URLs and in key=value settings, private keys.
// Best effort, and on the side of hiding too much: hiding a harmless value
// costs little while sharing a screen; showing a key costs a lot.
//
// The patterns are frontend/src/lib/secrets.ts's, which need lookarounds and
// backreferences, so they run on regexp2 rather than RE2.
type secretRule struct {
	kind string
	re   *regexp2.Regexp
	// Which group is the secret (0: all of it).
	group int
	// Extra check on the secret found.
	accept func(string) bool
}

func rule(kind, pattern string, opts regexp2.RegexOptions, group int, accept func(string) bool) secretRule {
	return secretRule{kind, regexp2.MustCompile(jsPattern(pattern), opts), group, accept}
}

var (
	upper, lower, digit = regexp.MustCompile(`[A-Z]`), regexp.MustCompile(`[a-z]`), regexp.MustCompile(`[0-9]`)
	placeholder         = regexp.MustCompile(`^(?i:true|false|null|none|nil|undefined|\*+|<[^>]*>|\$\{?\w+\}?)$`)
	allDigits           = regexp.MustCompile(`^[0-9]+$`)
)

// random: upper case, lower case and digits mixed: a key or a hash, not a word.
func random(v string) bool {
	return upper.MatchString(v) && lower.MatchString(v) && digit.MatchString(v)
}

// base64ish: encoded data rather than a name: random-looking, or mixed case
// with base64's = padding.
func base64ish(v string) bool {
	return random(v) || (strings.HasSuffix(v, "=") && upper.MatchString(v) && lower.MatchString(v))
}

var secretRules = []secretRule{
	rule("private key", `-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|$(?![\s\S]))`, 0, 0, nil),
	rule("AWS access key", `\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`, 0, 0, nil),
	rule("AWS secret key", `\b(?:aws_?secret_?access_?key|secret_?access_?key)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})(?![A-Za-z0-9/+=])`,
		regexp2.IgnoreCase, 1, nil),
	rule("AWS session token", `\b(?:aws_?session_?token|session_?token)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{80,})`,
		regexp2.IgnoreCase, 1, nil),
	rule("JWT", `\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`, 0, 0, nil),
	rule("GitHub token", `\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`, 0, 0, nil),
	rule("GitLab token", `\bglpat-[A-Za-z0-9_-]{20,}`, 0, 0, nil),
	rule("Slack token", `\bxox[abposr]-[A-Za-z0-9-]{10,}`, 0, 0, nil),
	rule("Slack webhook", `https://hooks\.slack\.com/services/[A-Za-z0-9/]+`, 0, 0, nil),
	rule("Stripe key", `\b[sr]k_(?:live|test)_[A-Za-z0-9]{20,}`, 0, 0, nil),
	rule("Google API key", `\bAIza[0-9A-Za-z_-]{35}`, 0, 0, nil),
	rule("API key", `\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}`, 0, 0, nil),
	rule("npm token", `\bnpm_[A-Za-z0-9]{36}\b`, 0, 0, nil),
	// https://user:password@host
	rule("password in URL", `\b[a-z][a-z0-9+.-]*://[^\s:/@]+:([^\s@/]+)@`, regexp2.IgnoreCase, 1, nil),
	rule("bearer token", `\b(?:bearer|token)\s+([A-Za-z0-9._~+/-]{20,}=*)`, regexp2.IgnoreCase, 1, nil),
	// password=…, "client_secret": "…", API_KEY: …, export GITHUB_TOKEN=…
	rule("secret setting",
		`\b[\w.-]*(?:passw(?:or)?d|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credentials?)["']?\s*[:=]\s*(["']?)([^\s"'&,;]{6,})\1`,
		regexp2.IgnoreCase, 2,
		func(v string) bool { return !placeholder.MatchString(v) && !allDigits.MatchString(v) }),
	// Base64 values in YAML, as in a secret's `data:` (kubectl get secret -o yaml).
	rule("encoded value", `^\s+[\w.-]+:\s+([A-Za-z0-9+/]{12,}={0,2})\s*$`, regexp2.Multiline, 1, base64ish),
}

// jsPattern spells out \w and \b as ASCII, as JavaScript has them; regexp2
// (like .NET) makes them Unicode-aware.
func jsPattern(p string) string {
	const word = "A-Za-z0-9_"
	var out strings.Builder
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			i++
			switch next := p[i]; {
			case next == 'w' && inClass:
				out.WriteString(word)
			case next == 'w':
				out.WriteString("[" + word + "]")
			case next == 'b' && !inClass:
				out.WriteString("(?:(?<![" + word + "])(?=[" + word + "])|(?<=[" + word + "])(?![" + word + "]))")
			default:
				out.WriteByte('\\')
				out.WriteByte(next)
			}
			continue
		}
		switch c {
		case '[':
			inClass = true
		case ']':
			inClass = false
		}
		out.WriteByte(c)
	}
	return out.String()
}

// FindSecrets returns the secrets in a text, in order and never overlapping.
func FindSecrets(text string) []SecretSpan {
	if text == "" {
		return nil
	}
	// regexp2 counts runes; spans count bytes.
	runes := []rune(text)
	byteAt := make([]int, len(runes)+1)
	for i, r := range runes {
		byteAt[i+1] = byteAt[i] + utf8.RuneLen(r)
	}
	var out []SecretSpan
	for _, r := range secretRules {
		m, _ := r.re.FindRunesMatch(runes)
		for ; m != nil; m, _ = r.re.FindNextMatch(m) {
			if m.Length == 0 {
				continue
			}
			g := m.GroupByNumber(r.group)
			if g == nil || g.Length == 0 {
				continue
			}
			start, end := byteAt[g.Index], byteAt[g.Index+g.Length]
			if r.accept != nil && !r.accept(text[start:end]) {
				continue
			}
			if slices.ContainsFunc(out, func(s SecretSpan) bool { return start < s.End && end > s.Start }) {
				continue
			}
			out = append(out, SecretSpan{start, end, r.kind})
		}
	}
	slices.SortFunc(out, func(a, b SecretSpan) int { return a.Start - b.Start })
	return out
}

// Redact returns the text with its secrets replaced, for copying while
// secrets are hidden.
func Redact(text string) string {
	spans := FindSecrets(text)
	if len(spans) == 0 {
		return text
	}
	var out strings.Builder
	at := 0
	for _, s := range spans {
		out.WriteString(text[at:s.Start])
		out.WriteString(Redacted)
		at = s.End
	}
	out.WriteString(text[at:])
	return out.String()
}
