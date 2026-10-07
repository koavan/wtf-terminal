# wtf

A browser-like terminal for macOS and Linux. Go + Wails v3 + Svelte 5 + xterm.js.

## Run

```sh
wails3 dev      # hot-reloading dev build
wails3 build    # production binary in bin/wtf
go test ./internal/...
cd frontend && npm test
```

Requires Go 1.25+, Node 20+, and `wails3` (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.28`).

## Layout

- `main.go`: window setup and native menu (menu items emit `app:command` events).
- `internal/terminal`: shell sessions (PTY), foreground-process/cwd polling, and the
  loopback WebSocket that streams PTY bytes with ack-based flow control.
- `internal/state`: atomic session file storage.
- `internal/config`: config file, themes (built-in, Ghostty, iTerm2), live reload.
- `internal/terminal/shellintegration`: the bash/zsh scripts.
- `internal/notify`: desktop notifications.
- `internal/files`: directory listings (with git ignore rules) for the file tree.
- `internal/netconn`: connections and listening ports, attributed to terminal sessions.
- `internal/procs`: the user's processes, attributed to terminal sessions.
- `internal/windows`: opening/restoring windows, moving tabs between them,
  confirm-before-close/quit.
- `frontend/src/lib`: tab/group/pane state (`tabs.svelte.ts`), split geometry
  (`layout.ts`), the command registry (`commands.ts`), session autosave, themes.
- `frontend/src/components`: tab strip, address bar, panes, palette, search, menus.

## Shortcuts

Every command is also in the command palette.

| Action | macOS | Linux |
|---|---|---|
| Command palette | ⌘⇧P | Ctrl+Shift+P |
| Toggle sidebar | ⌘B | Ctrl+Shift+B |
| Sidebar: files / network / processes / history | ⌘⇧E / ⌘⇧O / ⌘⇧A / ⌘⇧H | Ctrl+Shift+Alt+E / O / A / H |
| New tab | ⌘T | Ctrl+Shift+T |
| New window | ⌘N | Ctrl+Shift+N |
| Show all tabs | ⌘⇧\ | Ctrl+Shift+\ |
| Close window | ⌘⇧W | — |
| Split right / down | ⌘D / ⌘⇧D | Ctrl+Shift+E / Ctrl+Shift+O |
| Close pane (or tab, if it's the last pane) | ⌘W | Ctrl+Shift+W |
| Close tab | ⌥⌘W | — |
| Reopen closed tab | ⌘⇧T | — |
| Zoom pane | ⌘⇧↩ | Ctrl+Shift+Enter |
| Focus pane in a direction | ⌥⌘ + arrows | Ctrl+Alt + arrows |
| Next / previous pane | ⌘] / ⌘[ | — |
| Add tab to new group | ⌥⌘G | Ctrl+Shift+G |
| Next / previous tab | ⌘⇧] / ⌘⇧[, Ctrl+Tab | Ctrl+PgDn / Ctrl+PgUp, Ctrl+Tab |
| Go to tab 1–8 / last | ⌘1–⌘8 / ⌘9 | Alt+1–8 / Alt+9 |
| Move tab | ⌘⇧← / ⌘⇧→ | Ctrl+Shift+PgUp / PgDn |
| Find / next / previous | ⌘F / ⌘G / ⌘⇧G | Ctrl+Shift+F / Enter / Shift+Enter |
| Jump to previous / next prompt | ⌘↑ / ⌘↓ | Ctrl+Shift+↑ / ↓ |
| Copy last command output | ⌥⌘C | Ctrl+Shift+Alt+C |
| Clear | ⌘K | Ctrl+Shift+K |
| Settings file | ⌘, | Ctrl+, |
| Text size | ⌘= / ⌘- / ⌘0 | Ctrl+Shift+= / - / 0 |

Right-click a tab or a group chip for more. Double-click a split divider to even it out.

## Sidebar

⌘B (or the button next to the traffic lights) toggles a side pane with a file
tree of the focused pane's folder — pin it to keep a folder. Click a file to
insert its path at the prompt, double-click a folder to `cd` there (when the
shell is idle), drag a file into a terminal to paste its path, and right-click
for more (open in `$EDITOR` in a split, reveal in Finder, copy path…). Folders
load on demand, git-ignored files are dimmed, and the tree refreshes after
each command.

The **Network** tab lists listening ports and open connections — by default
those from programs in your terminals, labeled with their tab (click to jump
there); switch to **All** for everything you own. Listeners reachable from
other machines are marked. Right-click to open a port in the browser, copy
addresses, or end the process. Uses `lsof` (macOS) or `ss` (Linux) and only
refreshes while visible.

The **Processes** tab shows what's running in each terminal as a tree under
its shell (click the tab name to jump there), or **All** your processes
sorted by CPU, memory or name. Right-click to see a process's network
connections, copy its PID or command, end it, or force quit it.

The **History** tab lists the commands run in the focused pane's shell
(or every pane in the window), newest first, with when each finished, how
long it took and its exit code. Click to put a command at the prompt,
double-click to run it again, or right-click to scroll the terminal back to
it. It comes from shell integration (bash and zsh) and lasts as long as the
shell does.

🗑 clears the list and, after asking, removes the session's commands from
the shell's own history (↑ / Ctrl-R) via a key binding the integration sets
up (Ctrl-X Ctrl-]); nothing is typed into the terminal. History from earlier
sessions stays, and the history file is never rewritten without it. Busy
shells are left alone (only their list is cleared).

Nothing in the side pane runs while it's closed, only the visible tab
updates, and Network/Processes pause while the window is in the background.
To stop background updates entirely, press ⏸ in any side pane tab (or set
`sidebar-auto-refresh = false`): tabs then load when opened and refresh only
when you press ↻. `sidebar-refresh-interval` sets how often they update
otherwise (default 3 seconds).

## Windows

Drag a tab down out of the tab strip (or outside the window) and let go to
open it in a new window; its shells keep running and its scrollback comes
along. Right-click a tab to move it to another window. Closing a pane, tab,
group or window, or quitting, asks first when a program is still running.

## Settings and themes

`~/.config/wtf/config.toml` (or `$XDG_CONFIG_HOME/wtf/config.toml`). Open it
with ⌘, — it is created with every setting documented. Changes apply on save;
mistakes are reported in the window and the last good settings stay in effect.

Themes: pick one with **Change Theme…** in the command palette (live preview),
or set `theme = "Nord"` / `theme = "dark:Nord,light:Solarized Light"`. Add
Ghostty theme files or iTerm2 `.itermcolors` files to `~/.config/wtf/themes/`.
The window chrome takes its colors from the theme.

## Shell integration

bash (3.2+) and zsh load a small script (installed to the user cache dir) that
reports prompts and commands with OSC 133 marks, after your usual startup
files. That enables ✓/✗ marks beside commands, ⌘↑/⌘↓ between prompts, copying
a command's output, the command status in the address bar, and a notification
when a long command finishes out of sight. Turn it off with
`shell-integration = false`.

## Session

Each window's tabs, groups, split layouts, folders and text size are saved to
`session.json` in the user config dir (`~/Library/Application Support/wtf`
on macOS, `~/.config/wtf` on Linux) and restored on launch. Shells start
fresh in their saved folders.
