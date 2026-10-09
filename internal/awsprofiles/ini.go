package awsprofiles

import (
	"strings"
)

// A line-preserving view of ~/.aws/credentials and ~/.aws/config, for
// editing them the way a person would: a changed setting keeps its line,
// its key's spelling (AWS_ACCESS_KEY_ID or aws_access_key_id) and its
// "key = value" or "key=value" style; comments and blank lines stay.
//
// Keys are matched case-insensitively, as the AWS CLI does. A key followed
// by indented lines (a nested block, like `s3 =` with its sub-settings) is
// left exactly as it is.

type section struct {
	// "default", "profile prod", "sso-session corp", or a credentials
	// file's plain profile name; whitespace inside the brackets collapsed.
	name   string
	header int // its [header] line
	keys   []*keyLine
	// Last line belonging to the section: its last setting (or nested
	// line), else the header.
	last int
}

type keyLine struct {
	line int
	// Lowercased, for matching.
	key string
	// The line up to where the value starts, kept on edits: "region = ".
	prefix string
	value  string
	// Indented lines below it belong to it (a nested block).
	nested bool
}

func (s *section) get(key string) *keyLine {
	for _, k := range s.keys {
		if k.key == key {
			return k
		}
	}
	return nil
}

type doc struct {
	lines []string
}

func newDoc(data []byte) *doc {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return &doc{}
	}
	return &doc{lines: strings.Split(s, "\n")}
}

func (d *doc) bytes() []byte {
	// Drop trailing blank lines, end with one newline.
	lines := d.lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func isComment(t string) bool { return t == "" || t[0] == '#' || t[0] == ';' }

// sections parses the document afresh; line numbers are 0-based.
func (d *doc) sections() []*section {
	var out []*section
	var cur *section
	var lastKey *keyLine
	for i, line := range d.lines {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || isComment(t):
		case t[0] == '[' && strings.HasSuffix(t, "]"):
			cur = &section{name: strings.Join(strings.Fields(t[1:len(t)-1]), " "), header: i, last: i}
			out = append(out, cur)
			lastKey = nil
		case cur == nil:
		case line[0] == ' ' || line[0] == '\t':
			// A nested line belongs to the key above.
			if lastKey != nil {
				lastKey.nested = true
				cur.last = i
			}
		default:
			eq := strings.IndexByte(line, '=')
			if eq < 0 {
				continue
			}
			valueAt := eq + 1
			for valueAt < len(line) && (line[valueAt] == ' ' || line[valueAt] == '\t') {
				valueAt++
			}
			k := &keyLine{
				line:   i,
				key:    strings.ToLower(strings.TrimSpace(line[:eq])),
				prefix: line[:valueAt],
				value:  strings.TrimSpace(line[valueAt:]),
			}
			cur.keys = append(cur.keys, k)
			cur.last = i
			lastKey = k
		}
	}
	return out
}

func (d *doc) find(name string) *section {
	for _, s := range d.sections() {
		if s.name == name {
			return s
		}
	}
	return nil
}

// style is how the file writes settings: "key = value" or "key=value".
func (d *doc) style() string {
	// Follow the file's first setting; " = " is the AWS CLI's own style.
	for _, s := range d.sections() {
		if len(s.keys) > 0 {
			if strings.Contains(s.keys[0].prefix, " =") || strings.Contains(s.keys[0].prefix, "\t=") {
				return " = "
			}
			return "="
		}
	}
	return " = "
}

// set gives a key a value in a section (which must exist), reusing the
// key's line when it has one.
func (d *doc) set(name, key, value string) {
	s := d.find(name)
	if k := s.get(key); k != nil {
		d.lines[k.line] = k.prefix + value
		return
	}
	d.insert(s.last+1, key+d.style()+value)
}

// unset removes a key's line (and its nested block) from a section.
func (d *doc) unset(name, key string) {
	s := d.find(name)
	if s == nil {
		return
	}
	k := s.get(key)
	if k == nil {
		return
	}
	end := k.line + 1
	for end < len(d.lines) && end <= s.last && (strings.HasPrefix(d.lines[end], " ") || strings.HasPrefix(d.lines[end], "\t")) {
		end++
	}
	d.lines = append(d.lines[:k.line], d.lines[end:]...)
}

func (d *doc) insert(at int, line string) {
	d.lines = append(d.lines[:at], append([]string{line}, d.lines[at:]...)...)
}

// add creates an empty section at the end of the file.
func (d *doc) add(name string) {
	if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
		d.lines = append(d.lines, "")
	}
	d.lines = append(d.lines, "["+name+"]")
}

// rename changes a section's header, keeping its indentation.
func (d *doc) rename(from, to string) {
	s := d.find(from)
	line := d.lines[s.header]
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	d.lines[s.header] = indent + "[" + to + "]"
}

// remove deletes a section, with the comments directly above it, and
// doesn't leave two blank lines where it was.
func (d *doc) remove(name string) {
	s := d.find(name)
	if s == nil {
		return
	}
	from, to := s.header, s.last+1
	for from > 0 && strings.HasPrefix(strings.TrimSpace(d.lines[from-1]), "#") {
		from--
	}
	for to < len(d.lines) && strings.TrimSpace(d.lines[to]) == "" && (from == 0 || strings.TrimSpace(d.lines[from-1]) == "") {
		to++
	}
	d.lines = append(d.lines[:from], d.lines[to:]...)
}
