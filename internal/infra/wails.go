//go:build !native

package infra

// Wails glue: typed events for the generated bindings, and the lifecycle
// hook. The native macOS build (-tags native) leaves this file out.

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func init() {
	application.RegisterEvent[string](EventChanged)
}
