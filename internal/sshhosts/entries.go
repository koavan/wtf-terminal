package sshhosts

import "strings"

// The textual view of one config file, for showing a host's own settings
// and for editing them in place: which lines make up each Host entry.

// Option is one setting line of a Host entry, as written.
type Option struct {
	Key string `json:"key"`
	// Everything after the keyword, quotes included, e.g. `"~/my key"`.
	Value string `json:"value"`
}

type optLine struct {
	line int    // 0-based
	key  string // lowercased
	// The line up to where the value starts ("  User "), kept on edits.
	prefix string
	value  string
}

// entry is a Host block in one file: its Host line and the setting lines
// that follow it, up to the next Host or Match line. Include lines and
// comments inside it are not options and are left alone by edits.
type entry struct {
	line     int // the Host line, 0-based
	patterns []string
	opts     []optLine
	// Last line that belongs to the entry (its last setting, or the Host line).
	last int
}

func (e *entry) options() []Option {
	out := make([]Option, len(e.opts))
	for i, o := range e.opts {
		out[i] = Option{Key: keyOf(o.prefix), Value: o.value}
	}
	return out
}

// keyOf is the keyword as written in an option line's prefix ("  User " → "User").
func keyOf(prefix string) string {
	k := strings.TrimSpace(prefix)
	k = strings.TrimSpace(strings.TrimSuffix(k, "="))
	return k
}

// splitRaw splits a line into its keyword and the raw value text, and
// returns where the value starts. Comments and blank lines give "".
func splitRaw(line string) (key, value string, valueAt int) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || trimmed[0] == '#' {
		return "", "", 0
	}
	start := len(line) - len(trimmed)
	i := strings.IndexAny(trimmed, " \t=")
	if i < 0 {
		return strings.ToLower(trimmed), "", len(line)
	}
	key = strings.ToLower(trimmed[:i])
	rest := trimmed[i:]
	n := len(rest)
	rest = strings.TrimLeft(rest, " \t")
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	valueAt = start + i + (n - len(rest))
	return key, strings.TrimRight(rest, " \t\r"), valueAt
}

// parseEntries finds the Host entries of one file's lines.
func parseEntries(lines []string) []*entry {
	var out []*entry
	var cur *entry
	for i, line := range lines {
		key, value, at := splitRaw(line)
		switch key {
		case "":
		case "host":
			_, args := splitLine(line)
			cur = &entry{line: i, patterns: args, last: i}
			out = append(out, cur)
		case "match":
			cur = nil
		case "include":
		default:
			if cur != nil {
				cur.opts = append(cur.opts, optLine{line: i, key: key, prefix: line[:at], value: value})
				cur.last = i
			}
		}
	}
	return out
}
