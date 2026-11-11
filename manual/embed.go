// Package manual holds the in-app manual's Markdown pages, named <topic
// id>.md. The Linux app embeds them (internal/uicore/manual.go renders
// them); the macOS app copies them into its bundle (macos/build.sh).
package manual

import "embed"

// Files are the pages, named <topic id>.md.
//
//go:embed *.md
var Files embed.FS
