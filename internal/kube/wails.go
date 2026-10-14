//go:build !native

package kube

// Wails glue: typed events for the generated bindings, and the lifecycle
// hook. The native macOS build (-tags native) leaves this file out.

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return s.startup(ctx)
}
