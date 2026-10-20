// Package manual holds the in-app manual's Markdown pages for the native
// Linux app, which renders them itself (internal/uicore/manual.go). The
// Wails build imports the same files from index.ts.
package manual

import "embed"

// Files are the pages, named <topic id>.md.
//
//go:embed *.md
var Files embed.FS
