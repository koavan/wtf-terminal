package main

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

// The generated Swift must match the Go services: after changing a
// service's methods or types, run `go run ./cmd/swiftgen`.
func TestGeneratedSwiftIsCurrent(t *testing.T) {
	g := &gen{structs: map[string]reflect.Type{}}
	svc, _ := g.services()
	models := g.models()
	for path, want := range map[string][]byte{
		"../../macos/Sources/WtfCore/Generated/Models.swift": models,
		"../../macos/Sources/Wtf/Generated/Services.swift":   svc,
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date: run `go run ./cmd/swiftgen`", path)
		}
	}
}
