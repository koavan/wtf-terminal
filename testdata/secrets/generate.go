//go:build ignore

// Writes cases.json: what internal/uicore's secret finder finds in each
// input, with offsets in UTF-16 code units. The Go tests
// (internal/uicore/secrets_test.go) and the Swift tests
// (macos/Tests/WtfCoreTests/SecretsTests.swift) both check against it, so
// the two implementations can't drift apart. After changing the rules or
// the inputs, run:  go run testdata/secrets/generate.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/dayanraj/wtf/internal/uicore"
)

// Made-up values in the shapes the real ones have.
const (
	akid   = "AKIAABCDEFGHIJKLMNOP"
	secret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	jwt    = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	key    = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----"
)

var inputs = []string{
	"aws_access_key_id = " + akid,
	"aws_secret_access_key = " + secret,
	"export AWS_SECRET_ACCESS_KEY=\"" + secret + "\"",
	"\"SecretAccessKey\": \"" + secret + "\",",
	"aws_session_token = " + strings.Repeat("IQoJb3JpZ2luX2VjEJr", 6),
	"use ghp_abcdefghijklmnopqrstuvwxyz0123456789 here",
	"use github_pat_11ABCDEFG0123456789_abcdefghijklmnop here",
	"use glpat-abcdefghij0123456789 here",
	"use xoxb-1234567890-abcdefghij here",
	"curl https://hooks.slack.com/services/T000/B000/XXXXXXXX -d @msg",
	"use sk_live_abcdefghij0123456789ab here",
	"use AIzaSyA1234567890abcdefghijklmnopqrstuv here",
	"use sk-ant-api03-abcdefghij0123456789 here",
	"use npm_abcdefghijklmnopqrstuvwxyz0123456789 here",
	"token " + jwt,
	"Authorization: Bearer " + jwt,
	"DATABASE_URL=postgres://app:s3cr3t-pw@db:5432/app",
	"Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345",
	"password: hunter2222",
	"export GITHUB_TOKEN=abc123def456",
	"\"client_secret\": \"q8~Zz.Y1\"",
	"db.password=correcthorse",
	"PASSWORD='quoted-value-1'",
	"password: null",
	"token: 1220 bytes",
	"api_key: ${API_KEY}",
	"password: ********",
	"password=<redacted>",
	"password: 12345678",
	"the password is set",
	"data:\n  username: aHVudGVyMjIyMg==\n  sha: 3f786850e387550fdab836ed7e6dc881de23001b\n  note: averylongwordwithoutdigitsorcaps\n  kind: ConfigMapListItems",
	"before\n" + key + "\nafter",
	"-----BEGIN RSA PRIVATE KEY-----\nMIIEow",
	"-----BEGIN PUBLIC KEY-----",
	"NAME READY STATUS RESTARTS AGE\nweb-1 1/1 Running 0 3d",
	"commit 3f786850e387550fdab836ed7e6dc881de23001b",
	"id=" + akid + " secret=" + secret,
	"two keys: " + akid + " and AKIAZZZZZZZZZZZZZZZZ",
	"unicode café ✓ password: pässwörd99 done",
	"",
	"nothing here",
}

type span struct {
	Kind  string `json:"kind"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type testCase struct {
	Text     string `json:"text"`
	Found    []span `json:"found"`
	Redacted string `json:"redacted"`
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func main() {
	cases := []testCase{}
	for _, text := range inputs {
		c := testCase{Text: text, Found: []span{}, Redacted: uicore.Redact(text)}
		for _, s := range uicore.FindSecrets(text) {
			c.Found = append(c.Found, span{s.Kind, utf16Len(text[:s.Start]), utf16Len(text[:s.End])})
		}
		cases = append(cases, c)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cases); err != nil {
		panic(err)
	}
	_, self, _, _ := runtime.Caller(0)
	if err := os.WriteFile(filepath.Join(filepath.Dir(self), "cases.json"), b.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %d cases\n", len(cases))
}
