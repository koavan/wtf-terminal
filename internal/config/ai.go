package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// validateAI drops providers that can't work, saying why.
func validateAI(ai AIConfig, problems []string) (AIConfig, []string) {
	if len(ai.Providers) == 0 {
		return AIConfig{}, problems
	}
	ok := map[string]AIProvider{}
	names := make([]string, 0, len(ai.Providers))
	for name := range ai.Providers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		p := ai.Providers[name]
		bad := func(why string) {
			problems = append(problems, fmt.Sprintf("ai.providers.%s: %s", name, why))
		}
		p.Kind = strings.TrimSpace(p.Kind)
		p.Model = strings.TrimSpace(p.Model)
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		switch p.Kind {
		case "anthropic", "openai":
			if p.Model == "" {
				bad("model is missing")
				continue
			}
			if p.BaseURL != "" {
				u, err := url.Parse(p.BaseURL)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					bad(fmt.Sprintf("base-url %q isn't an http or https URL", p.BaseURL))
					continue
				}
			}
			if len(p.Command) > 0 {
				bad(`command is only for kind = "command"`)
			}
			if err := checkHeaders(p.Headers); err != nil {
				bad(err.Error())
				continue
			}
		case "command":
			if len(p.Command) == 0 || strings.TrimSpace(p.Command[0]) == "" {
				bad(`command is missing, e.g. command = ["llm"]`)
				continue
			}
			if p.BaseURL != "" || p.APIKeyEnv != "" || len(p.Headers) > 0 {
				bad(`base-url, api-key-env and headers aren't used with kind = "command"`)
			}
		case "":
			bad(`kind is missing: "anthropic", "openai" or "command"`)
			continue
		default:
			bad(fmt.Sprintf(`kind = %q: must be "anthropic", "openai" or "command"`, p.Kind))
			continue
		}
		ok[name] = p
	}
	if len(ok) == 0 {
		return AIConfig{}, problems
	}
	return AIConfig{Providers: ok}, problems
}

// aiChoices resolves ai-default and ai-fast to provider names: a lone
// provider needs neither, and ai-fast falls back to ai-default.
func aiChoices(c Config, problems []string) (def, fast string, _ []string) {
	exists := func(key, name string) bool {
		if _, ok := c.AI.Providers[name]; ok {
			return true
		}
		problems = append(problems, fmt.Sprintf("%s = %q: no [ai.providers.%s]", key, name, name))
		return false
	}
	def, fast = strings.TrimSpace(c.AIDefault), strings.TrimSpace(c.AIFast)
	if def != "" && !exists("ai-default", def) {
		def = ""
	}
	if fast != "" && !exists("ai-fast", fast) {
		fast = ""
	}
	if def == "" {
		switch len(c.AI.Providers) {
		case 0:
		case 1:
			for name := range c.AI.Providers {
				def = name
			}
		default:
			if fast != "" {
				def = fast
			} else {
				problems = append(problems, "ai-default isn't set: name one of the [ai.providers] tables")
			}
		}
	}
	if fast == "" {
		fast = def
	}
	return def, fast, problems
}

var providerName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// CheckAIProvider tidies a provider as the Settings window sends it and
// says what's wrong with it, if anything.
func CheckAIProvider(name string, p AIProvider) (AIProvider, error) {
	if !providerName.MatchString(name) {
		return p, errors.New("a name uses letters, digits, - and _ only (up to 40)")
	}
	p.APIKeyEnv = strings.TrimSpace(p.APIKeyEnv)
	if p.Kind == "command" {
		p.Model, p.BaseURL, p.APIKeyEnv, p.Headers = "", "", "", nil
	} else {
		p.Command = nil
	}
	if p.APIKeyEnv != "" && !envName.MatchString(p.APIKeyEnv) {
		return p, fmt.Errorf("%q isn't an environment variable name", p.APIKeyEnv)
	}
	ok, problems := validateAI(AIConfig{Providers: map[string]AIProvider{name: p}}, nil)
	if len(problems) > 0 {
		return p, errors.New(strings.TrimPrefix(problems[0], "ai.providers."+name+": "))
	}
	return ok.Providers[name], nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// withAIProvider returns the config file text with provider old replaced by
// name = p, or added (old == ""), or removed (p == nil). ai-default and
// ai-fast follow a rename and a removal.
func withAIProvider(data, old, name string, p *AIProvider) (string, error) {
	cfg, _, err := Parse(data)
	if err != nil {
		return "", fmt.Errorf("config.toml has an error to fix first: %v", err)
	}
	if p != nil {
		clean, err := CheckAIProvider(name, *p)
		if err != nil {
			return "", err
		}
		p = &clean
		if _, taken := cfg.AI.Providers[name]; taken && name != old {
			return "", fmt.Errorf("there's already a provider named %q", name)
		}
	}
	// Before the change: which provider answers, so it still does after.
	def := cfg.AIDefault

	if old != "" {
		start, end, ok := providerTable(data, old)
		if !ok {
			return "", fmt.Errorf("couldn't find [ai.providers.%s] in config.toml: change it there", old)
		}
		repl := ""
		if p != nil {
			repl = renderProvider(name, *p) + "\n"
		}
		data = data[:start] + repl + data[end:]
	} else {
		data = strings.TrimRight(data, "\n") + "\n\n" + renderProvider(name, *p)
	}

	// Point ai-default / ai-fast where they should.
	raw := struct {
		Default string `toml:"ai-default"`
		Fast    string `toml:"ai-fast"`
	}{}
	_, _ = toml.Decode(data, &raw)
	after, _, err := Parse(data)
	if err != nil {
		return "", err
	}
	switch {
	case old != "" && old != name && p != nil:
		if raw.Default == old {
			data = withSetting(data, "ai-default", strconv.Quote(name))
		}
		if raw.Fast == old {
			data = withSetting(data, "ai-fast", strconv.Quote(name))
		}
	case p == nil:
		if raw.Fast == old {
			data = withoutSetting(data, "ai-fast")
		}
		if raw.Default == old || (def == old && raw.Default == "") {
			data = withoutSetting(data, "ai-default")
			if names := sortedKeys(after.AI.Providers); len(names) > 1 {
				data = withSetting(data, "ai-default", strconv.Quote(names[0]))
			}
		}
	}
	// A second provider: the one that answered alone keeps answering.
	if raw.Default == "" && def != "" && def != old && len(after.AI.Providers) > 1 {
		data = withSetting(data, "ai-default", strconv.Quote(def))
	}
	return data, nil
}

// providerTable finds [ai.providers.<name>]: from its header to the next
// table's header (or the end).
func providerTable(data, name string) (start, end int, ok bool) {
	q := regexp.QuoteMeta(name)
	header := regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*ai[ \t]*\.[ \t]*providers[ \t]*\.[ \t]*("` + q + `"|'` + q + `'|` + q + `)[ \t]*\][ \t]*(#.*)?$\n?`)
	loc := header.FindStringIndex(data)
	if loc == nil {
		return 0, 0, false
	}
	end = len(data)
	if next := tableHeader.FindStringIndex(data[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	return loc[0], end, true
}

func renderProvider(name string, p AIProvider) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[ai.providers.%s]\n", name)
	fmt.Fprintf(&b, "kind = %s\n", tomlString(p.Kind))
	line := func(key, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", key, tomlString(v))
		}
	}
	line("model", p.Model)
	line("base-url", p.BaseURL)
	line("api-key-env", p.APIKeyEnv)
	if len(p.Headers) > 0 {
		names := make([]string, 0, len(p.Headers))
		for n := range p.Headers {
			names = append(names, n)
		}
		slices.Sort(names)
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = tomlString(n) + " = " + tomlString(p.Headers[n])
		}
		fmt.Fprintf(&b, "headers = { %s }\n", strings.Join(parts, ", "))
	}
	if len(p.Command) > 0 {
		parts := make([]string, len(p.Command))
		for i, a := range p.Command {
			parts[i] = tomlString(a)
		}
		fmt.Fprintf(&b, "command = [%s]\n", strings.Join(parts, ", "))
	}
	return b.String()
}

// tomlString is s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// withoutSetting returns the config file text without an active `key = …`
// line.
func withoutSetting(data, key string) string {
	active := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=.*\n?`)
	return active.ReplaceAllString(data, "")
}

func sortedKeys(m map[string]AIProvider) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// SplitCommand splits a command line into its program and arguments, with
// shell-style quotes ('…', "…") and backslashes, for the Settings window's
// Command field. Nothing is expanded.
func SplitCommand(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote != 0:
			if r == quote {
				quote = 0
			} else if r == '\\' && quote == '"' {
				escaped = true
			} else {
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inArg = true, true
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote isn't closed")
	}
	if escaped {
		return nil, errors.New("the line ends with a backslash")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// JoinCommand is SplitCommand's reverse, for showing a command to edit.
func JoinCommand(args []string) string {
	plain := regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
	out := make([]string, len(args))
	for i, a := range args {
		if plain.MatchString(a) {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

var headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// checkHeaders refuses header names HTTP doesn't allow, and the ones that
// carry the API key or that wtf sets itself.
func checkHeaders(h map[string]string) error {
	for name, value := range h {
		if !headerName.MatchString(name) {
			return fmt.Errorf("header %q isn't a valid header name", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("header %s: the value can't span lines", name)
		}
		switch strings.ToLower(name) {
		case "authorization", "x-api-key", "api-key":
			return fmt.Errorf("header %s: put the key in the API key field (the keychain) instead", name)
		case "content-type", "accept", "content-length", "host", "anthropic-version":
			return fmt.Errorf("header %s is set by wtf", name)
		}
	}
	return nil
}

// ParseHeaders reads the Settings window's Extra headers field: one
// "Name: value" per line.
func ParseHeaders(text string) (map[string]string, error) {
	h := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%q: write headers as Name: value", line)
		}
		h[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	if len(h) == 0 {
		return nil, nil
	}
	return h, checkHeaders(h)
}

// FormatHeaders is ParseHeaders' reverse, sorted by name.
func FormatHeaders(h map[string]string) string {
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, n)
	}
	slices.Sort(names)
	lines := make([]string, len(names))
	for i, n := range names {
		lines[i] = n + ": " + h[n]
	}
	return strings.Join(lines, "\n")
}
