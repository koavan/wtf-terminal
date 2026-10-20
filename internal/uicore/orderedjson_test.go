package uicore

import (
	"errors"
	"testing"
)

func TestJSONPretty(t *testing.T) {
	v, err := ParseJSON(`{"b":1,"a":[true,null,"x/y\n"],"e":{},"f":[],"b":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.Pretty(), "{\n  \"b\": 2,\n  \"a\": [\n    true,\n    null,\n    \"x/y\\n\"\n  ],\n  \"e\": {},\n  \"f\": []\n}"; got != want {
		t.Errorf("pretty:\n%s\nwant:\n%s", got, want)
	}
	for _, bad := range []string{`{"a":1,}`, "[1] x"} {
		if _, err := ParseJSON(bad); !errors.Is(err, ErrJSONParse) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	v, err = ParseJSON(`"😀 é\u0001"`)
	if err != nil || v.Pretty() != `"😀 é\u0001"` {
		t.Errorf("surrogates: %q %v", v.Pretty(), err)
	}
}

func TestJSNumbers(t *testing.T) {
	for _, c := range []struct {
		d    float64
		want string
	}{
		{1.0, "1"}, {1e21, "1e+21"}, {1e-7, "1e-7"}, {0.000001, "0.000001"}, {negZero(), "0"},
		{123.45, "123.45"}, {1791829800, "1791829800"}, {1.5e300, "1.5e+300"}, {-2.5, "-2.5"},
		{100, "100"}, {0.1, "0.1"}, {12345678901234567890, "12345678901234567000"},
	} {
		if got := JSNumber(c.d); got != c.want {
			t.Errorf("JSNumber(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func negZero() float64 {
	z := 0.0
	return -z
}

func TestFindJSON(t *testing.T) {
	if got := FindJSON(`2026-10-09 INFO {"a":1,"b":[true]}`); got != "{\n  \"a\": 1,\n  \"b\": [\n    true\n  ]\n}" {
		t.Errorf("object: %q", got)
	}
	if got := FindJSON(`[INFO] payload=["x"]`); got != "[\n  \"x\"\n]" {
		t.Errorf("array: %q", got)
	}
	if got := FindJSON("[INFO] nothing {here}"); got != "" {
		t.Errorf("nothing: %q", got)
	}
	if got := FindJSON("42"); got != "" {
		t.Errorf("number: %q", got)
	}
}
