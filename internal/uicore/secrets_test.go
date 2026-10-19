package uicore

import (
	"encoding/json"
	"os"
	"testing"
	"unicode/utf16"
)

// testdata/secrets/cases.json is written by the TypeScript implementation
// (node testdata/secrets/generate.ts); the Go port must agree with it.
type sharedCase struct {
	Text  string `json:"text"`
	Found []struct {
		Kind       string `json:"kind"`
		Start, End int    // UTF-16 offsets, as JavaScript counts
	} `json:"found"`
	Redacted string `json:"redacted"`
}

func TestSecretsMatchTypeScript(t *testing.T) {
	b, err := os.ReadFile("../../testdata/secrets/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []sharedCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 30 {
		t.Fatalf("only %d shared cases", len(cases))
	}
	for _, c := range cases {
		got := FindSecrets(c.Text)
		ok := len(got) == len(c.Found)
		for i := 0; ok && i < len(got); i++ {
			w := c.Found[i]
			ok = got[i].Kind == w.Kind && utf16Len(c.Text[:got[i].Start]) == w.Start && utf16Len(c.Text[:got[i].End]) == w.End
		}
		if !ok {
			t.Errorf("%.50q: found %+v, want %+v", c.Text, got, c.Found)
		}
		if r := Redact(c.Text); r != c.Redacted {
			t.Errorf("%.50q: redacted %q, want %q", c.Text, r, c.Redacted)
		}
	}
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func TestSecretsKeepWordsASCII(t *testing.T) {
	// In JavaScript \b sits between é and A; a Unicode \b would see no boundary.
	if got := FindSecrets("éAKIAABCDEFGHIJKLMNOP"); len(got) != 1 || got[0].Kind != "AWS access key" || got[0].Start != 2 {
		t.Errorf("got %+v", got)
	}
	want := `[A-Za-z0-9_.-](?:(?<![A-Za-z0-9_])(?=[A-Za-z0-9_])|(?<=[A-Za-z0-9_])(?![A-Za-z0-9_]))\s`
	if got := jsPattern(`[\w.-]\b\s`); got != want {
		t.Errorf("jsPattern = %s", got)
	}
}
