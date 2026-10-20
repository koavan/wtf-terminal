package uicore

import (
	"encoding/base64"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The token and certificate inspector's rules: JWTs, base64 values,
// certificates and TLS servers found in a terminal's output. Port of
// lib/inspect.ts (and isCertFile from lib/fileActions.ts), as
// macos/Sources/WtfCore/Inspect.swift.

// InspectJWTPattern is a JWT-shaped run of text.
const InspectJWTPattern = `\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`

var (
	inspectJWT       = regexp.MustCompile(InspectJWTPattern)
	inspectBase64    = regexp.MustCompile(`^[A-Za-z0-9+/_-]+={0,2}$`)
	inspectPEM       = regexp.MustCompile(`-----BEGIN (?:TRUSTED )?CERTIFICATE-----`)
	inspectYAMLValue = regexp.MustCompile(`^(\s*[\w.-]+:\s+)([A-Za-z0-9+/]+={0,2})\s*$`)
	inspectTLSURL    = regexp.MustCompile(`(?i)^(?:https|wss|ldaps)://[^/\s?#]+`)
	inspectTLSHost   = regexp.MustCompile(`(?i)^((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:[a-z]{2,63}|xn--[a-z0-9-]+)|\d{1,3}(?:\.\d{1,3}){3})(?::(\d{1,5}))?/?$`)
	inspectFileLike  = regexp.MustCompile(`(?i)\.(go|ts|js|py|rb|rs|md|txt|json|ya?ml|toml|lock|sh|log|conf|cfg|ini|pem|crt|key)$`)
	inspectCertFile  = regexp.MustCompile(`(?i)\.(?:pem|crt|cer|cert|der|ca-bundle)$`)
	inspectScheme    = regexp.MustCompile(`^\w+://`)
)

// InspectFirstJWT is the first JWT-shaped run of text, or "".
func InspectFirstJWT(text string) string { return inspectJWT.FindString(text) }

func withoutSpace(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

// InspectBase64Bytes decodes standard or URL-safe base64, padding optional;
// false if it isn't base64.
func InspectBase64Bytes(text string) ([]byte, bool) {
	s := withoutSpace(text)
	if len(s) < 2 || !inspectBase64.MatchString(s) {
		return nil, false
	}
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	s = strings.TrimRight(s, "=")
	if len(s)%4 == 1 {
		return nil, false
	}
	s += strings.Repeat("=", (4-len(s)%4)%4)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	return b, true
}

// InspectUTF8 is strict UTF-8 that looks like text (no control characters
// but tab and line breaks); false if it's binary.
func InspectUTF8(b []byte) (string, bool) {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	// Strict: invalid sequences make it binary, not U+FFFD.
	if !utf8.Valid(b) {
		return "", false
	}
	s := string(b)
	for _, u := range s {
		if u <= 8 || u >= 0x0E && u <= 0x1F || u == 0x7F {
			return "", false
		}
	}
	return s, true
}

// InspectJWT is what a JWT says.
type InspectJWT struct {
	Header, Claims JSONValue
	// The time claims it has, of exp, nbf, iat and auth_time, in that order.
	Times  []InspectJWTTime
	Alg    string
	Signed bool
}

// InspectJWTTime is a time claim.
type InspectJWTTime struct {
	Claim string
	At    time.Time
}

func inspectJSONObject(part string) (JSONValue, bool) {
	b, ok := InspectBase64Bytes(part)
	if !ok {
		return JSONValue{}, false
	}
	text, ok := InspectUTF8(b)
	if !ok {
		return JSONValue{}, false
	}
	v, err := ParseJSON(text)
	if err != nil || v.Kind != JSONObject {
		return JSONValue{}, false
	}
	return v, true
}

// InspectDecodeJWT reads a JWT's header and claims (the signature isn't
// checked).
func InspectDecodeJWT(token string) (InspectJWT, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return InspectJWT{}, false
	}
	header, ok := inspectJSONObject(parts[0])
	if !ok {
		return InspectJWT{}, false
	}
	claims, ok := inspectJSONObject(parts[1])
	if !ok {
		return InspectJWT{}, false
	}
	j := InspectJWT{Header: header, Claims: claims, Signed: parts[2] != ""}
	for _, c := range []string{"exp", "nbf", "iat", "auth_time"} {
		if v, ok := claims.Get(c); ok && v.Kind == JSONNumber && !math.IsInf(v.Num, 0) && !math.IsNaN(v.Num) {
			sec, frac := math.Modf(v.Num)
			j.Times = append(j.Times, InspectJWTTime{c, time.Unix(int64(sec), int64(frac*1e9))})
		}
	}
	if alg, ok := header.Get("alg"); ok && alg.Kind == JSONString {
		j.Alg = alg.Str
	}
	return j, true
}

// InspectKind is what a decoded base64 value turned out to be.
type InspectKind string

const (
	InspectCertificate InspectKind = "certificate"
	InspectJWTKind     InspectKind = "jwt"
	InspectJSON        InspectKind = "json"
	InspectText        InspectKind = "text"
	InspectBinary      InspectKind = "binary"
)

// InspectDecoded is a decoded base64 value: its kind, its size in bytes, and
// the text to show (pretty JSON, a hex dump for binary).
type InspectDecoded struct {
	Kind InspectKind
	Size int
	Text string
}

// InspectHasPEMCertificate says the text holds a PEM certificate.
func InspectHasPEMCertificate(text string) bool { return inspectPEM.MatchString(text) }

// InspectDecodeBase64 decodes a base64 value, and says what it turned out
// to be.
func InspectDecodeBase64(input string) (InspectDecoded, bool) {
	b, ok := InspectBase64Bytes(input)
	if !ok || len(b) == 0 {
		return InspectDecoded{}, false
	}
	text, ok := InspectUTF8(b)
	if !ok {
		return InspectDecoded{InspectBinary, len(b), InspectHexDump(b[:min(len(b), 256)])}, true
	}
	trimmed := strings.TrimSpace(text)
	if InspectHasPEMCertificate(trimmed) {
		return InspectDecoded{InspectCertificate, len(b), text}, true
	}
	if t := InspectFirstJWT(trimmed); t != "" {
		if _, ok := InspectDecodeJWT(t); ok {
			return InspectDecoded{InspectJWTKind, len(b), trimmed}, true
		}
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		if v, err := ParseJSON(trimmed); err == nil {
			return InspectDecoded{InspectJSON, len(b), v.Pretty()}, true
		}
	}
	return InspectDecoded{InspectText, len(b), text}, true
}

// InspectHexDump is `00000000  00 01 02 …  |...|`, 16 bytes a row.
func InspectHexDump(b []byte) string {
	var rows []string
	for off := 0; off < len(b); off += 16 {
		row := b[off:min(off+16, len(b))]
		hex := make([]string, len(row))
		ascii := make([]byte, len(row))
		for i, c := range row {
			hex[i] = fmt.Sprintf("%02x", c)
			ascii[i] = '.'
			if c >= 0x20 && c < 0x7F {
				ascii[i] = c
			}
		}
		rows = append(rows, fmt.Sprintf("%08x  %-47s  |%s|", off, strings.Join(hex, " "), ascii))
	}
	return strings.Join(rows, "\n")
}

var (
	hasUpper = regexp.MustCompile(`[A-Z]`)
	hasLower = regexp.MustCompile(`[a-z]`)
)

// InspectLooksBase64 says some text is likely base64 (not just a word that
// happens to fit).
func InspectLooksBase64(text string) bool {
	s := withoutSpace(text)
	if len(s) < 8 || !inspectBase64.MatchString(s) {
		return false
	}
	mixed := hasUpper.MatchString(s) && hasLower.MatchString(s)
	if !mixed && !strings.HasSuffix(s, "=") {
		return false
	}
	_, ok := InspectDecodeBase64(s)
	return ok
}

// InspectYAMLBase64At is the base64 value of a YAML `key: value` line if
// the byte index is on it, or "".
func InspectYAMLBase64At(line string, index int) string {
	m := inspectYAMLValue.FindStringSubmatchIndex(line)
	if m == nil {
		return ""
	}
	start, value := m[3]-m[2], line[m[4]:m[5]]
	if index < start || index >= start+len(value) || !InspectLooksBase64(value) {
		return ""
	}
	return value
}

// InspectTLSTarget is a server to ask for its TLS certificate: `https://host`,
// `host:port` or a domain name; "" if the text isn't one.
func InspectTLSTarget(text string) string {
	s := strings.TrimSpace(text)
	if s == "" || utf8.RuneCountInString(s) > 300 || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return ""
	}
	if u := inspectTLSURL.FindString(s); u != "" {
		return u
	}
	m := inspectTLSHost.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	host, port := m[1], m[2]
	if port == "" && !(host[0] >= '0' && host[0] <= '9') && inspectFileLike.MatchString(host) {
		return ""
	}
	if port != "" {
		return host + ":" + port
	}
	return host
}

// InspectIsCertFile says the Files tab offers to inspect a file as a
// certificate.
func InspectIsCertFile(path string) bool { return inspectCertFile.MatchString(path) }

// InspectActionKind is what an inspector menu entry opens.
type InspectActionKind int

const (
	InspectActionJWT InspectActionKind = iota
	InspectActionBase64
	InspectActionCertText
	InspectActionTLS
)

// InspectAction is an inspector to open, and what for: the token, the
// base64 text, the text holding a certificate, or the TLS target.
type InspectAction struct {
	Kind InspectActionKind
	Text string
}

// InspectEntry is one entry of a selection's menu.
type InspectEntry struct {
	Label  string
	Action InspectAction
}

// InspectSelectionEntries is what a selection can be inspected as. secrets
// is false while Hide Secrets is on: no JWTs or base64 values then.
func InspectSelectionEntries(selection string, secrets bool) []InspectEntry {
	if strings.TrimSpace(selection) == "" {
		return nil
	}
	var out []InspectEntry
	if InspectHasPEMCertificate(selection) {
		out = append(out, InspectEntry{"Inspect Certificate…", InspectAction{InspectActionCertText, selection}})
	}
	jwt := ""
	if secrets {
		jwt = InspectFirstJWT(selection)
	}
	if _, ok := InspectDecodeJWT(jwt); jwt != "" && ok {
		out = append(out, InspectEntry{"Inspect JWT…", InspectAction{InspectActionJWT, jwt}})
	} else if secrets && len(out) == 0 && InspectLooksBase64(selection) {
		out = append(out, InspectEntry{"Decode Base64…", InspectAction{InspectActionBase64, selection}})
	}
	if len(out) == 0 && InspectFirstJWT(selection) == "" {
		if target := InspectTLSTarget(selection); target != "" {
			shown := inspectScheme.ReplaceAllString(target, "")
			out = append(out, InspectEntry{"Inspect TLS Certificate of " + shown + "…", InspectAction{InspectActionTLS, target}})
		}
	}
	return out
}

// InspectSize is `16 bytes` or `1.5 KB`; unit names bytes ("" is "bytes").
func InspectSize(n int, unit string) string {
	if unit == "" {
		unit = "bytes"
	}
	if n < 1024 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
