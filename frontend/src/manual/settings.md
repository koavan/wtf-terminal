# Settings and themes

{{cmd:config.open}} opens the Settings window. Its **General** section has the theme, the folders below, and whether [Ask Claude](#ask-claude) asks before every command. Everything else is set in `~/.config/wtf/config.toml` (or `$XDG_CONFIG_HOME/wtf/config.toml`): {{cmd:config.file}} opens it, created with every setting documented. Changes apply when you save (if wtf is in the background, as soon as you switch back to it); mistakes are reported in the window and the last good settings stay in effect.

| Setting | What it does |
|---|---|
| `theme` | a theme name, or `"dark:Nord,light:Solarized Light"` to follow the system |
| `font-family`, `font-size`, `line-height` | the terminal's text |
| `cursor-style`, `cursor-blink` | block, bar or underline; blinking (off by default: it redraws the window twice a second, about 1% CPU) |
| `scrollback` | lines kept per pane |
| `option-as-meta` | macOS: ⌥ sends Meta (for Emacs, readline) |
| `copy-on-select` | copy whatever you select |
| `shell` | the shell to run (your login shell by default) |
| `shell-integration` | prompt and command marks — see [Commands, prompts and output](#shell-integration) |
| `notify-after` | notify when a command running at least this many seconds finishes out of sight; 0 turns it off |
| `unfocused-split-dim` | how much unfocused panes are dimmed |
| `sidebar-auto-refresh`, `sidebar-refresh-interval` | side pane updates — see [The side pane](#side-pane) |
| `context-badges` | the address bar's Kubernetes/AWS/gcloud/Terraform badges |
| `redact-secrets` | start with Hide Secrets on — see [Hide Secrets](#secrets) |
| `show-usage` | show wtf's own CPU and memory in the address bar (default off) — see [Resource usage](#usage) |
| `confirm-paste` | show a paste first when it has more than one line, a final newline or control characters (default on) — see [Privacy](#privacy) |
| `claude-auto-mode` | let Claude Code's own permission mode decide which commands in a Claude pane run without asking (off: wtf asks every time) — see [Ask Claude](#ask-claude) |
| `clipboard-clear` | seconds before a copied secret leaves the clipboard (default 30, up to 600); 0 keeps it — see [Privacy](#privacy) |
| `log-highlighting` | colors for log levels, timestamps, URLs, IPs and Kubernetes resources — see [Logs in the output](#logs) |

## Themes

{{cmd:theme.pick}} previews themes as you move through the list. Add Ghostty theme files or iTerm2 `.itermcolors` files to the themes folder ({{cmd:themes.folder}}). The window's colors follow the theme.
