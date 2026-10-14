package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fake struct{}

type pair struct {
	A int    `json:"a"`
	B string `json:"b"`
}

func (fake) Nothing()                  {}
func (fake) Echo(p pair) pair          { return p }
func (fake) Add(a, b int) (int, error) { return a + b, nil }
func (fake) Fail() error               { return errors.New("boom") }
func (fake) Two() (string, int)        { return "x", 2 }
func (fake) Panics()                   { panic("oops") }
func (fake) ServiceShutdown() error    { return nil }

func withFake(t *testing.T) {
	t.Helper()
	services = map[string]any{"fake": fake{}}
	t.Cleanup(func() { services = nil })
}

func TestCall(t *testing.T) {
	withFake(t)
	for _, tc := range []struct {
		method, args, want, err string
	}{
		{"Nothing", `[]`, `null`, ""},
		{"Nothing", ``, `null`, ""},
		{"Echo", `[{"a":1,"b":"hi"}]`, `{"a":1,"b":"hi"}`, ""},
		{"Add", `[2,3]`, `5`, ""},
		{"Two", `[]`, `["x",2]`, ""},
		{"Fail", `[]`, "", "boom"},
		{"Add", `[1]`, "", "takes 2 arguments, got 1"},
		{"Add", `["1",2]`, "", "argument 1"},
		{"Add", `{}`, "", "must be a JSON array"},
		{"Missing", `[]`, "", "no method fake.Missing"},
		{"ServiceShutdown", `[]`, "", "not callable"},
		{"Panics", `[]`, "", "panicked: oops"},
	} {
		got, err := call("fake", tc.method, []byte(tc.args))
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s(%s): err = %v, want %q", tc.method, tc.args, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s(%s): %v", tc.method, tc.args, err)
			continue
		}
		b, _ := json.Marshal(got)
		if string(b) != tc.want {
			t.Errorf("%s(%s) = %s, want %s", tc.method, tc.args, b, tc.want)
		}
	}
}

func TestCallUnknownService(t *testing.T) {
	withFake(t)
	if _, err := call("nope", "X", nil); err == nil || !strings.Contains(err.Error(), `no service "nope"`) {
		t.Fatalf("err = %v", err)
	}
}
