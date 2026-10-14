//go:build !native

package sshhosts

// Wails glue: typed events for the generated bindings, and the lifecycle
// hook. The native macOS build (-tags native) leaves this file out.

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func init() {
	application.RegisterEvent[[]TunnelStatus](EventTunnels)
}

func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return s.startup(ctx)
}
