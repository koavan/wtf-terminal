package uicore

import (
	"fmt"
	"strings"
)

// The Linux app's commands and shortcuts, in menu order. The IDs are the
// macOS app's (macos/Sources/Wtf/main.swift), so the palette, menus and
// manual name the same things.
//
// A terminal needs plain Ctrl+letter, so shortcuts follow GNOME Terminal:
// Ctrl+Shift for the terminal and its tabs, Ctrl+Alt to show a side-pane
// tab, Alt for panes and tab numbers. Ctrl+Alt+T, Ctrl+Alt+L, Ctrl+Alt+D
// and Ctrl+Alt+arrows belong to the desktop and stay unused.

// Command is one menu or palette entry.
type Command struct {
	ID, Title string
	// Shortcut, e.g. "Ctrl+Shift+T"; empty for none.
	Keys string
}

func cmd(id, title, keys string) Command { return Command{id, title, keys} }

// Commands lists every command, in menu order.
var Commands = func() []Command {
	c := []Command{
		cmd("app.about", "About wtf", ""),
		cmd("config.open", "Settings…", "Ctrl+Comma"),
		cmd("config.file", "Open config.toml", ""),
		cmd("theme.pick", "Change Theme…", ""),
		cmd("themes.folder", "Open Themes Folder", ""),
		cmd("tools.show", "Command-Line Tools…", ""),
		cmd("ai.providers", "AI Providers…", ""),
		cmd("inspect.tls", "Inspect TLS Certificate…", ""),
		cmd("paste.confirm", "Toggle Paste Confirmation", ""),
		cmd("generate.key", "Generate SSH Key…", ""),
		cmd("generate.password", "Generate Password…", ""),
		cmd("usage.toggle", "Toggle Resource Usage in Address Bar", ""),
		cmd("sidebar.autoRefresh", "Toggle Side Pane Auto-Refresh", ""),
		cmd("app.quit", "Quit", "Ctrl+Shift+Q"),

		cmd("tab.new", "New Tab", "Ctrl+Shift+T"),
		cmd("tab.newApi", "New API Tab", "Ctrl+Alt+Shift+A"),
		cmd("window.new", "New Window", "Ctrl+Shift+N"),
		cmd("pane.splitRight", "Split Right", "Ctrl+Shift+D"),
		cmd("pane.splitDown", "Split Down", "Ctrl+Alt+Shift+D"),
		cmd("tab.duplicate", "Duplicate Tab", ""),
		cmd("tab.rename", "Rename Tab…", ""),
		cmd("snippets.run", "Run Snippet…", "Ctrl+Shift+R"),
		cmd("snippets.open", "Open Snippets File…", ""),
		cmd("record.toggle", "Start or Stop Recording…", "Ctrl+Alt+Shift+S"),
		cmd("export.text", "Save Output as Text…", ""),
		cmd("export.markdown", "Save Commands as Markdown…", ""),
		cmd("findings.scan", "Run Security Scan…", ""),
		cmd("workspace.open", "Open Workspace…", ""),
		cmd("workspace.save", "Save Window as Workspace…", ""),
		cmd("broadcast.toggle", "Broadcast Input", "Ctrl+Alt+Shift+B"),
		cmd("tab.toNewWindow", "Move Tab to New Window", ""),
		cmd("pane.close", "Close", "Ctrl+Shift+W"),
		cmd("tab.close", "Close Tab", "Ctrl+Alt+Shift+W"),
		cmd("tab.reopen", "Reopen Closed Tab", "Ctrl+Alt+Shift+T"),
		cmd("window.close", "Close Window", ""),

		cmd("term.copy", "Copy", "Ctrl+Shift+C"),
		cmd("term.paste", "Paste", "Ctrl+Shift+V"),
		cmd("term.selectAll", "Select All", "Ctrl+Shift+A"),
		cmd("find", "Find…", "Ctrl+Shift+F"),
		cmd("find.next", "Find Next", "Ctrl+Shift+G"),
		cmd("find.prev", "Find Previous", "Ctrl+Shift+H"),
		cmd("term.copyOutput", "Copy Last Command Output", "Ctrl+Alt+Shift+C"),
		cmd("term.pinOutput", "Pin Last Command Output", ""),
		cmd("ai.explain", "Explain Last Failed Command…", ""),
		cmd("term.clear", "Clear Buffer", "Ctrl+Shift+K"),

		cmd("palette", "Command Palette…", "Ctrl+Shift+P"),
		cmd("sidebar.toggle", "Toggle Sidebar", "Ctrl+Shift+B"),
		cmd("sidebar.files", "Show Files", "Ctrl+Alt+E"),
		cmd("sidebar.ssh", "Show SSH Hosts", "Ctrl+Alt+S"),
		cmd("sidebar.aws", "Show AWS Profiles", "Ctrl+Alt+U"),
		cmd("sidebar.kube", "Show Kubernetes", "Ctrl+Alt+K"),
		cmd("sidebar.snippets", "Show Snippets", "Ctrl+Alt+Y"),
		cmd("sidebar.workspaces", "Show Workspaces", "Ctrl+Alt+W"),
		cmd("sidebar.network", "Show Network", "Ctrl+Alt+O"),
		cmd("sidebar.processes", "Show Processes", "Ctrl+Alt+A"),
		cmd("sidebar.history", "Show Command History", "Ctrl+Alt+H"),
		cmd("sidebar.pins", "Show Pinned Output", "Ctrl+Alt+I"),
		cmd("sidebar.findings", "Show Findings", "Ctrl+Alt+F"),
		cmd("sidebar.share", "Show Share & Record", "Ctrl+Alt+J"),
		cmd("sidebar.generate", "Show Generate Keys & Passwords", "Ctrl+Alt+G"),
		cmd("usage.show", "Resource Usage…", ""),
		cmd("secrets.toggle", "Hide Secrets", "Ctrl+Alt+Shift+R"),
		cmd("prompt.prev", "Previous Prompt", "Ctrl+Shift+Up"),
		cmd("prompt.next", "Next Prompt", "Ctrl+Shift+Down"),
		cmd("font.bigger", "Bigger Text", "Ctrl+Plus"),
		cmd("font.smaller", "Smaller Text", "Ctrl+Minus"),
		cmd("font.reset", "Actual Size", "Ctrl+0"),

		cmd("tab.overview", "Show All Tabs", "Ctrl+Bar"),
		cmd("tab.next", "Show Next Tab", "Ctrl+PageDown"),
		cmd("tab.prev", "Show Previous Tab", "Ctrl+PageUp"),
		cmd("tab.moveRight", "Move Tab Right", "Ctrl+Shift+PageDown"),
		cmd("tab.moveLeft", "Move Tab Left", "Ctrl+Shift+PageUp"),
		cmd("tab.group", "Add Tab to New Group", "Ctrl+Alt+Shift+G"),
	}
	for i := 1; i <= 8; i++ {
		c = append(c, cmd(fmt.Sprintf("tab.select.%d", i), fmt.Sprintf("Select Tab %d", i), fmt.Sprintf("Alt+%d", i)))
	}
	return append(c,
		cmd("tab.select.last", "Select Last Tab", "Alt+9"),
		cmd("pane.zoom", "Toggle Zoom", "Ctrl+Shift+Return"),
		cmd("pane.next", "Select Next Pane", "Ctrl+BraceRight"),
		cmd("pane.prev", "Select Previous Pane", "Ctrl+BraceLeft"),
		cmd("pane.left", "Select Pane Left", "Alt+Left"),
		cmd("pane.right", "Select Pane Right", "Alt+Right"),
		cmd("pane.up", "Select Pane Above", "Alt+Up"),
		cmd("pane.down", "Select Pane Below", "Alt+Down"),

		cmd("help.manual", "wtf Manual", "F1"),
		cmd("help.shortcuts", "Keyboard Shortcuts", "Ctrl+Question"),
	)
}()

// CommandByID finds a command.
func CommandByID(id string) (Command, bool) {
	for _, c := range Commands {
		if c.ID == id {
			return c, true
		}
	}
	return Command{}, false
}

// Key names in Keys, with their GDK keysym names and how menus show them.
// Shifted symbols are named by what Shift makes (Bar, not Shift+Backslash):
// that's the key GTK sees, and the label says Shift.
var keyNames = map[string][2]string{
	"Comma": {"comma", ","}, "Plus": {"plus", "+"}, "Minus": {"minus", "−"},
	"Backslash": {"backslash", "\\"}, "BracketLeft": {"bracketleft", "["}, "BracketRight": {"bracketright", "]"},
	"Question": {"question", "?"}, "Return": {"Return", "Enter"},
	"Bar": {"bar", "Shift+\\"}, "BraceLeft": {"braceleft", "Shift+["}, "BraceRight": {"braceright", "Shift+]"},
	"PageUp": {"Page_Up", "Page Up"}, "PageDown": {"Page_Down", "Page Down"},
	"Up": {"Up", "↑"}, "Down": {"Down", "↓"}, "Left": {"Left", "←"}, "Right": {"Right", "→"},
}

var modifierNames = map[string]string{"Ctrl": "<Control>", "Shift": "<Shift>", "Alt": "<Alt>", "Super": "<Super>"}

// splitKeys returns a shortcut's modifiers and key, checking each part.
func splitKeys(keys string) (mods []string, key string, err error) {
	parts := strings.Split(keys, "+")
	key = parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		if _, ok := modifierNames[m]; !ok {
			return nil, "", fmt.Errorf("%q: unknown modifier %q", keys, m)
		}
		mods = append(mods, m)
	}
	switch _, named := keyNames[key]; {
	case named:
	case len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9'):
	case len(key) >= 2 && key[0] == 'F' && strings.Trim(key[1:], "0123456789") == "":
	default:
		return nil, "", fmt.Errorf("%q: unknown key %q", keys, key)
	}
	return mods, key, nil
}

// Accel turns Keys into a GTK accelerator, e.g. "<Control><Shift>t".
func Accel(keys string) string {
	if keys == "" {
		return ""
	}
	mods, key, err := splitKeys(keys)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, m := range mods {
		b.WriteString(modifierNames[m])
	}
	if n, ok := keyNames[key]; ok {
		key = n[0]
	} else if len(key) == 1 {
		key = strings.ToLower(key)
	}
	b.WriteString(key)
	return b.String()
}

// KeyLabel is how menus and the manual show Keys, e.g. "Ctrl+Shift+T".
func KeyLabel(keys string) string {
	mods, key, err := splitKeys(keys)
	if err != nil || keys == "" {
		return ""
	}
	if n, ok := keyNames[key]; ok {
		key = n[1]
	}
	return strings.Join(append(mods, key), "+")
}
