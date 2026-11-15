package agentctx

import "strings"

// word is one shell word: its text with quotes removed, and whether it
// depends on something only the shell knows when it runs ($VAR, $(…),
// backticks, globs aren't counted: they don't name a context).
type word struct {
	text    string
	dynamic bool
	// An operator (&&, ||, ;, |, &, a newline, ( or )) rather than a word.
	op bool
}

// words splits a command line the way a POSIX shell would, closely enough
// to read the commands in it: quotes, backslashes, operators. Redirections
// stay attached to their words; comments are dropped. Unbalanced quotes
// end the line.
func words(line string) []word {
	var out []word
	var cur strings.Builder
	have, dyn := false, false
	flush := func() {
		if have {
			out = append(out, word{text: cur.String(), dynamic: dyn})
		}
		cur.Reset()
		have, dyn = false, false
	}
	r := []rune(line)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\' && i+1 < len(r):
			i++
			if r[i] != '\n' {
				cur.WriteRune(r[i])
				have = true
			}
		case c == '\'':
			have = true
			for i++; i < len(r) && r[i] != '\''; i++ {
				cur.WriteRune(r[i])
			}
		case c == '"':
			have = true
			for i++; i < len(r) && r[i] != '"'; i++ {
				if r[i] == '\\' && i+1 < len(r) && strings.ContainsRune("\"\\$`", r[i+1]) {
					i++
				} else if r[i] == '$' || r[i] == '`' {
					dyn = true
				}
				cur.WriteRune(r[i])
			}
		case c == '$' || c == '`':
			dyn, have = true, true
			cur.WriteRune(c)
		case c == '#' && !have:
			for i < len(r) && r[i] != '\n' {
				i++
			}
			i--
		case c == ' ' || c == '\t':
			flush()
		case c == '\n' || c == ';' || c == '(' || c == ')':
			flush()
			out = append(out, word{text: string(c), op: true})
		case c == '&' || c == '|':
			flush()
			op := string(c)
			if i+1 < len(r) && (r[i+1] == c || (c == '|' && r[i+1] == '&')) {
				i++
				op += string(r[i])
			}
			out = append(out, word{text: op, op: true})
		default:
			cur.WriteRune(c)
			have = true
		}
	}
	flush()
	return out
}

// commands splits words into simple commands at the operators.
func commands(ws []word) [][]word {
	var out [][]word
	var cur []word
	for _, w := range ws {
		if w.op {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, w)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// assignment splits NAME=value; ok is false for anything else.
func assignment(s string) (name, value string, ok bool) {
	name, value, ok = strings.Cut(s, "=")
	if !ok || name == "" {
		return "", "", false
	}
	for i, c := range name {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return "", "", false
		}
	}
	return name, value, true
}

// flag finds an option's value in args: "--context x", "--context=x" and,
// for a one-letter short flag, "-nx". It's the last one given, as the
// tools themselves take it.
func flag(args []word, names ...string) (value word, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i].text
		if a == "--" {
			break
		}
		for _, n := range names {
			switch {
			case a == n && i+1 < len(args):
				value, ok = args[i+1], true
			case strings.HasPrefix(a, n+"="):
				value, ok = word{text: a[len(n)+1:], dynamic: args[i].dynamic}, true
			case len(n) == 2 && n[0] == '-' && n[1] != '-' && len(a) > 2 && strings.HasPrefix(a, n) && !strings.HasPrefix(a, "--"):
				value, ok = word{text: a[2:], dynamic: args[i].dynamic}, true
			}
		}
	}
	return value, ok
}

// has says whether any of the switches is given.
func has(args []word, names ...string) bool {
	for _, a := range args {
		if a.text == "--" {
			return false
		}
		for _, n := range names {
			if a.text == n {
				return true
			}
		}
	}
	return false
}
