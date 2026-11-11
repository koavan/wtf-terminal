package config

import "context"

// ServiceStartup is called once at launch, by backend.Start.
func (s *Service) ServiceStartup(ctx context.Context) error { return s.startup(ctx) }
