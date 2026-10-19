package uicore

import (
	"regexp"
	"strings"
)

// ParseEnv reads "NAME=value" lines, as the shell scripts report them at
// each prompt; lines without "=" (or starting with it) are skipped, later
// duplicates win. Like Env.parse in lib/infra.ts.
func ParseEnv(text string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok && name != "" {
			env[name] = value
		}
	}
	return env
}

// terminalReport is what a terminal answers a program on its own: cursor
// position, device attributes, focus, mouse, mode reports, OSC and DCS
// replies.
var terminalReport = regexp.MustCompile(`^(?:` + strings.Join([]string{
	`\x1b\[\d+;\d+R`,
	`\x1b\[[?>=]?[\d;]*c`,
	`\x1b\[[IO]`,
	`\x1b\[M[\s\S]{3}`,
	`\x1b\[<\d+;\d+;\d+[Mm]`,
	`\x1b\[\??[\d;]*\$y`,
	`\x1b\][\s\S]*(?:\x07|\x1b\\)`,
	`\x1bP[\s\S]*\x1b\\`,
}, "|") + `)$`)

// IsTerminalReport says input is a terminal's own answer to a program, which
// broadcast leaves in its pane. Like isTerminalReport in
// lib/broadcast.svelte.ts.
func IsTerminalReport(s string) bool { return terminalReport.MatchString(s) }
