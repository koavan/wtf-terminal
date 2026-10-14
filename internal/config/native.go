//go:build native

package config

import "context"

// ServiceStartup is called by cmd/libwtf, the native UI's counterpart of
// Wails' lifecycle hook (see wails.go).
func (s *Service) ServiceStartup(ctx context.Context) error { return s.startup(ctx) }
