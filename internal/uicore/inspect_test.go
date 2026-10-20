package uicore

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func b64(s string) string    { return base64.StdEncoding.EncodeToString([]byte(s)) }

var (
	testJWT = b64url(`{"alg":"RS256","typ":"JWT"}`) + "." +
		b64url(`{"sub":"ada","iss":"https://idp","exp":1791829800,"iat":1791826200,"aud":["api"]}`) + ".c2lnbmF0dXJl"
	testPEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
)

func TestInspectBase64Rules(t *testing.T) {
	if b, ok := InspectBase64Bytes("+/8="); !ok || !slices.Equal(b, []byte{251, 255}) {
		t.Errorf("standard: %v %v", b, ok)
	}
	if b, ok := InspectBase64Bytes("-_8"); !ok || !slices.Equal(b, []byte{251, 255}) {
		t.Errorf("url-safe: %v %v", b, ok)
	}
	for _, bad := range []string{"abcde", "not base64!"} {
		if _, ok := InspectBase64Bytes(bad); ok {
			t.Errorf("%q decoded", bad)
		}
	}
	if s, ok := InspectUTF8([]byte{0xEF, 0xBB, 0xBF, 0x41}); !ok || s != "A" {
		t.Errorf("BOM: %q", s)
	}
	for _, bad := range [][]byte{{0, 0x41}, {0xFF}} {
		if _, ok := InspectUTF8(bad); ok {
			t.Errorf("%v is text", bad)
		}
	}
	if s, ok := InspectUTF8([]byte("a\tb\nc")); !ok || s != "a\tb\nc" {
		t.Errorf("tabs: %q", s)
	}
}

func TestInspectJWTDecoding(t *testing.T) {
	j, ok := InspectDecodeJWT(testJWT)
	if !ok || j.Alg != "RS256" || !j.Signed {
		t.Fatalf("decoded %+v %v", j, ok)
	}
	if sub, _ := j.Claims.Get("sub"); sub.Str != "ada" {
		t.Errorf("sub %+v", sub)
	}
	var claims []string
	for _, c := range j.Times {
		claims = append(claims, c.Claim)
	}
	if !slices.Equal(claims, []string{"exp", "iat"}) || !j.Times[0].At.Equal(time.Unix(1791829800, 0)) {
		t.Errorf("times %v", j.Times)
	}
	if j, ok := InspectDecodeJWT(b64url(`{"alg":"none"}`) + "." + b64url(`{"a":1}`) + "."); !ok || j.Signed {
		t.Errorf("unsigned: %+v %v", j, ok)
	}
	for _, bad := range []string{"", "a.b", "a.b.c", b64url("[1]") + "." + b64url("{}") + ".x", b64url("{}") + "." + b64url("nope") + ".x"} {
		if _, ok := InspectDecodeJWT(bad); ok {
			t.Errorf("%q decoded", bad)
		}
	}
	if got := InspectFirstJWT("Authorization: Bearer " + testJWT + " sent"); got != testJWT {
		t.Errorf("first JWT %q", got)
	}
}

func TestInspectBase64Decoding(t *testing.T) {
	check := func(in string, want InspectDecoded) {
		t.Helper()
		if got, ok := InspectDecodeBase64(in); !ok || got != want {
			t.Errorf("%q: %+v %v, want %+v", in, got, ok, want)
		}
	}
	check(b64("hunter2-password"), InspectDecoded{InspectText, 16, "hunter2-password"})
	check(b64(`{"a":1}`), InspectDecoded{InspectJSON, 7, "{\n  \"a\": 1\n}"})
	if d, _ := InspectDecodeBase64(b64(testPEM)); d.Kind != InspectCertificate {
		t.Errorf("PEM: %+v", d)
	}
	if d, _ := InspectDecodeBase64(b64(testJWT + "\n")); d.Kind != InspectJWTKind || d.Text != testJWT {
		t.Errorf("JWT: %+v", d)
	}
	bin, _ := InspectDecodeBase64(base64.StdEncoding.EncodeToString(append([]byte{0, 1, 2}, "hello"...)))
	if bin.Kind != InspectBinary {
		t.Errorf("binary: %+v", bin)
	}
	if want := "00000000  " + fmt.Sprintf("%-47s", "00 01 02 68 65 6c 6c 6f") + "  |...hello|"; bin.Text != want {
		t.Errorf("hex dump:\n%q\nwant\n%q", bin.Text, want)
	}
	if n := len(strings.Split(InspectHexDump(make([]byte, 17)), "\n")); n != 2 {
		t.Errorf("17 bytes: %d rows", n)
	}
	if InspectHexDump(nil) != "" {
		t.Error("empty dump")
	}
}

func TestInspectLooksAndYAML(t *testing.T) {
	for _, s := range []string{"cGFzc3dvcmQxMjM=", "U2VjcmV0VmFsdWU"} {
		if !InspectLooksBase64(s) {
			t.Errorf("%q isn't base64", s)
		}
	}
	for _, s := range []string{"deployment", "12345678", "short="} {
		if InspectLooksBase64(s) {
			t.Errorf("%q is base64", s)
		}
	}
	if got := InspectYAMLBase64At("  password: cGFzc3dvcmQxMjM=", 14); got != "cGFzc3dvcmQxMjM=" {
		t.Errorf("on the value: %q", got)
	}
	if got := InspectYAMLBase64At("  password: cGFzc3dvcmQxMjM=", 3); got != "" {
		t.Errorf("on the key: %q", got)
	}
	if got := InspectYAMLBase64At("  name: payments", 10); got != "" {
		t.Errorf("a word: %q", got)
	}
}

func TestInspectTLSTargets(t *testing.T) {
	if got := InspectTLSTarget("https://api.example.com/v1/x?y=1"); got != "https://api.example.com" {
		t.Errorf("URL: %q", got)
	}
	for _, s := range []string{"db.internal:5432", "example.com", "10.0.0.5:8443"} {
		if got := InspectTLSTarget(s); got != s {
			t.Errorf("%q: %q", s, got)
		}
	}
	for _, s := range []string{"main.go", "config.yaml", "hello", "two words.com", "http://plain.example.com", ""} {
		if got := InspectTLSTarget(s); got != "" {
			t.Errorf("%q: %q", s, got)
		}
	}
	for _, p := range []string{"a.pem", "ca.crt", "x.CER", "b.der"} {
		if !InspectIsCertFile(p) {
			t.Errorf("%q isn't a cert file", p)
		}
	}
	for _, p := range []string{"id_rsa", "a.key", "a.pub"} {
		if InspectIsCertFile(p) {
			t.Errorf("%q is a cert file", p)
		}
	}
}

func TestInspectSelectionMenus(t *testing.T) {
	labels := func(s string, secrets bool) []string {
		var out []string
		for _, e := range InspectSelectionEntries(s, secrets) {
			out = append(out, e.Label)
		}
		return out
	}
	for _, c := range []struct {
		sel     string
		secrets bool
		want    []string
	}{
		{testPEM, true, []string{"Inspect Certificate…"}},
		{"token=" + testJWT, true, []string{"Inspect JWT…"}},
		{"cGFzc3dvcmQxMjM=", true, []string{"Decode Base64…"}},
		{"https://example.com:8443/", true, []string{"Inspect TLS Certificate of example.com:8443…"}},
		{testJWT, false, nil},
		{"cGFzc3dvcmQxMjM=", false, nil},
		{testPEM, false, []string{"Inspect Certificate…"}},
		{"just words", true, nil},
	} {
		if got := labels(c.sel, c.secrets); !slices.Equal(got, c.want) {
			t.Errorf("%q (secrets %v): %q, want %q", c.sel, c.secrets, got, c.want)
		}
	}
	if e := InspectSelectionEntries(" "+testJWT+" ", true); e[0].Action != (InspectAction{InspectActionJWT, testJWT}) {
		t.Errorf("JWT action %+v", e[0].Action)
	}
	if e := InspectSelectionEntries("https://example.com:8443/", true); e[0].Action != (InspectAction{InspectActionTLS, "https://example.com:8443"}) {
		t.Errorf("TLS action %+v", e[0].Action)
	}
}

func TestInspectSizes(t *testing.T) {
	if InspectSize(16, "") != "16 bytes" || InspectSize(1536, "") != "1.5 KB" || InspectSize(5, "B") != "5 B" {
		t.Error(InspectSize(16, ""), InspectSize(1536, ""), InspectSize(5, "B"))
	}
}
