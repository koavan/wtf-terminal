// Package config loads ~/.config/wtf/config.toml and its themes, and
// reloads them when they change on disk.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the user's settings file.
type Config struct {
	Theme             string  `toml:"theme" json:"theme"`
	FontFamily        string  `toml:"font-family" json:"fontFamily"`
	FontSize          float64 `toml:"font-size" json:"fontSize"`
	LineHeight        float64 `toml:"line-height" json:"lineHeight"`
	CursorStyle       string  `toml:"cursor-style" json:"cursorStyle"`
	CursorBlink       bool    `toml:"cursor-blink" json:"cursorBlink"`
	Scrollback        int     `toml:"scrollback" json:"scrollback"`
	OptionAsMeta      bool    `toml:"option-as-meta" json:"optionAsMeta"`
	CopyOnSelect      bool    `toml:"copy-on-select" json:"copyOnSelect"`
	Shell             string  `toml:"shell" json:"shell"`
	ShellIntegration  bool    `toml:"shell-integration" json:"shellIntegration"`
	NotifyAfter       float64 `toml:"notify-after" json:"notifyAfter"`
	UnfocusedSplitDim float64 `toml:"unfocused-split-dim" json:"unfocusedSplitDim"`

	SidebarAutoRefresh     bool    `toml:"sidebar-auto-refresh" json:"sidebarAutoRefresh"`
	SidebarRefreshInterval float64 `toml:"sidebar-refresh-interval" json:"sidebarRefreshInterval"`

	ContextBadges bool `toml:"context-badges" json:"contextBadges"`

	LogHighlighting bool `toml:"log-highlighting" json:"logHighlighting"`
	RedactSecrets   bool `toml:"redact-secrets" json:"redactSecrets"`

	ClipboardClear float64 `toml:"clipboard-clear" json:"clipboardClear"`
	ConfirmPaste   bool    `toml:"confirm-paste" json:"confirmPaste"`

	ShowUsage bool `toml:"show-usage" json:"showUsage"`
}

func Defaults() Config {
	return Config{
		Theme:             "auto",
		FontFamily:        "JetBrains Mono",
		FontSize:          13,
		LineHeight:        1.22,
		CursorStyle:       "bar",
		CursorBlink:       true,
		Scrollback:        10000,
		ShellIntegration:  true,
		NotifyAfter:       10,
		UnfocusedSplitDim: 0.35,

		SidebarAutoRefresh:     true,
		SidebarRefreshInterval: 3,

		ContextBadges: true,

		LogHighlighting: true,

		ClipboardClear: 30,
		ConfirmPaste:   true,
	}
}

// template is written the first time the user opens their config file.
const template = `# wtf settings. Changes apply as soon as you save (or switch back to wtf).
# Remove the # in front of a line to change that setting.

# Theme name, or one for each system appearance:
#   theme = "Tokyo Night"
#   theme = "dark:Catppuccin Mocha,light:Catppuccin Latte"
# Built in: wtf Dark, wtf Light, Catppuccin Mocha, Catppuccin Latte,
# Tokyo Night, Dracula, Nord, Gruvbox Dark, One Dark, Rosé Pine,
# Solarized Dark, Solarized Light. Drop Ghostty theme files or iTerm2
# .itermcolors files into the themes folder next to this file to add more.
# Tip: "Change Theme" in the command palette previews them live.
# theme = "auto"

# font-family = "JetBrains Mono"
# font-size = 13
# line-height = 1.22

# cursor-style = "bar"     # bar, block or underline
# cursor-blink = true

# Lines of history kept per pane.
# scrollback = 10000

# Use Option as Meta (Alt) in the shell, instead of for typing symbols. macOS only.
# option-as-meta = false

# Copy text to the clipboard as soon as you select it.
# copy-on-select = false

# Shell for new panes; empty means your login shell.
# shell = ""

# Prompt marks, ⌘↑/⌘↓ between prompts, copying a command's output, and
# notifications. Supports bash and zsh. Applies to new panes.
# shell-integration = true

# Notify when a command that ran at least this many seconds finishes while
# you're looking elsewhere. 0 turns it off.
# notify-after = 10

# How much to fade split panes that don't have focus, 0 to 0.8.
# unfocused-split-dim = 0.35

# Keep the side pane's Files, Network and Processes tabs up to date on
# their own. When off, they update only when opened or when you press
# refresh, so they never do work in the background.
# sidebar-auto-refresh = true

# Seconds between updates of the Network and Processes tabs, 1 to 60.
# sidebar-refresh-interval = 3

# Show the Kubernetes cluster and namespace, AWS account, profile and
# region, gcloud project and Terraform workspace each shell is pointed at,
# next to the folder in the address bar. Needs shell integration.
# context-badges = true

# Color log levels (ERROR, WARN…), timestamps, URLs, IP addresses and
# Kubernetes resources in output — only text the program left uncolored,
# and never in full-screen programs. ⌘-click (Ctrl-click) works either way.
# log-highlighting = true

# Start with Hide Secrets on: access keys, tokens, passwords and private keys
# in output are covered on screen and replaced with [REDACTED] when copied —
# for sharing your screen. Best effort: it can miss a secret it doesn't
# recognize. Toggle it any time with ⌥⌘R (Ctrl+Shift+Alt+X).
# redact-secrets = false

# Seconds after copying a secret wtf revealed (a Kubernetes secret's value,
# decoded base64, a JWT, a generated password or passphrase) to empty the
# clipboard — only if it still holds that secret. 0 never empties it.
# clipboard-clear = 30

# Show text before pasting it when it has more than one line or control
# characters (escape sequences and the like), and paste only on Paste.
# confirm-paste = true

# Show wtf's own CPU and memory in the address bar. It's measured only
# while shown (every 2 seconds, while the window is in front).
# show-usage = false
`

// Dir is $XDG_CONFIG_HOME/wtf, or ~/.config/wtf, on every platform.
func Dir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "wtf"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "wtf"), nil
}

// Parse reads a config file's contents over the defaults. Problems that
// don't prevent loading (unknown keys, out-of-range values) are returned
// alongside the config; a syntax error is returned as err.
func Parse(data string) (cfg Config, problems []string, err error) {
	cfg = Defaults()
	md, err := toml.Decode(data, &cfg)
	if err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return Defaults(), nil, fmt.Errorf("line %d: %s", perr.Position.Line, perr.Message)
		}
		return Defaults(), nil, err
	}
	for _, key := range md.Undecoded() {
		problems = append(problems, fmt.Sprintf("unknown setting %q", key.String()))
	}
	return validate(cfg, problems)
}

func validate(c Config, problems []string) (Config, []string, error) {
	d := Defaults()
	bad := func(key string, got any, want string) {
		problems = append(problems, fmt.Sprintf("%s = %v: %s", key, got, want))
	}
	if c.FontSize < 6 || c.FontSize > 72 {
		bad("font-size", c.FontSize, "must be between 6 and 72")
		c.FontSize = d.FontSize
	}
	if c.LineHeight < 1 || c.LineHeight > 3 {
		bad("line-height", c.LineHeight, "must be between 1 and 3")
		c.LineHeight = d.LineHeight
	}
	switch c.CursorStyle {
	case "bar", "block", "underline":
	default:
		bad("cursor-style", c.CursorStyle, `must be "bar", "block" or "underline"`)
		c.CursorStyle = d.CursorStyle
	}
	if c.Scrollback < 0 || c.Scrollback > 1_000_000 {
		bad("scrollback", c.Scrollback, "must be between 0 and 1000000")
		c.Scrollback = d.Scrollback
	}
	if c.NotifyAfter < 0 {
		bad("notify-after", c.NotifyAfter, "must be 0 or more")
		c.NotifyAfter = d.NotifyAfter
	}
	if c.UnfocusedSplitDim < 0 || c.UnfocusedSplitDim > 0.8 {
		bad("unfocused-split-dim", c.UnfocusedSplitDim, "must be between 0 and 0.8")
		c.UnfocusedSplitDim = d.UnfocusedSplitDim
	}
	if c.SidebarRefreshInterval < 1 || c.SidebarRefreshInterval > 60 {
		bad("sidebar-refresh-interval", c.SidebarRefreshInterval, "must be between 1 and 60")
		c.SidebarRefreshInterval = d.SidebarRefreshInterval
	}
	if c.ClipboardClear < 0 || c.ClipboardClear > 600 {
		bad("clipboard-clear", c.ClipboardClear, "must be between 0 and 600")
		c.ClipboardClear = d.ClipboardClear
	}
	if strings.TrimSpace(c.FontFamily) == "" {
		c.FontFamily = d.FontFamily
	}
	return c, problems, nil
}

// ThemePair resolves a theme setting to the themes for dark and light mode.
//
//	"auto" or ""                      → wtf Dark / wtf Light
//	"Nord"                            → Nord for both
//	"dark:Nord,light:Solarized Light" → one each
func ThemePair(spec string, themes []Theme) (dark, light Theme, problems []string) {
	dark, _ = FindTheme(themes, DefaultDarkTheme)
	light, _ = FindTheme(themes, DefaultLightTheme)
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "auto") {
		return dark, light, nil
	}
	lookup := func(name string, into *Theme) {
		name = strings.TrimSpace(name)
		if t, ok := FindTheme(themes, name); ok {
			*into = t
		} else {
			problems = append(problems, fmt.Sprintf("theme %q not found", name))
		}
	}
	if !strings.Contains(spec, ":") {
		if t, ok := FindTheme(themes, spec); ok {
			return t, t, nil
		}
		return dark, light, []string{fmt.Sprintf("theme %q not found", spec)}
	}
	for _, part := range strings.Split(spec, ",") {
		mode, name, _ := strings.Cut(part, ":")
		switch strings.TrimSpace(strings.ToLower(mode)) {
		case "dark":
			lookup(name, &dark)
		case "light":
			lookup(name, &light)
		default:
			problems = append(problems, fmt.Sprintf("theme %q: expected dark:<name> or light:<name>", strings.TrimSpace(part)))
		}
	}
	return dark, light, problems
}

func withTheme(data, spec string) string {
	return withSetting(data, "theme", fmt.Sprintf("%q", spec))
}

// withSetting returns the config file text with `key = value` set (value
// is a TOML literal), keeping everything else, comments included, as it
// was. It replaces the active line if there is one, else the commented-out
// default ("# key = …"), else appends. Documentation examples, which are
// indented after the "#", are left alone.
func withSetting(data, key, value string) string {
	line := key + " = " + value
	k := regexp.QuoteMeta(key)
	active := regexp.MustCompile(`(?m)^[ \t]*` + k + `[ \t]*=.*$`)
	if loc := active.FindStringIndex(data); loc != nil {
		return data[:loc[0]] + line + data[loc[1]:]
	}
	commented := regexp.MustCompile(`(?m)^#[ ]?` + k + `[ \t]*=.*$`)
	if loc := commented.FindStringIndex(data); loc != nil {
		return data[:loc[0]] + line + data[loc[1]:]
	}
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	return data + line + "\n"
}
