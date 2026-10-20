# Roadmap: native Linux UI

Goal: replace the Wails + WebKitGTK UI on Linux with a native GTK 4 app, the
way 1.0 replaced it on macOS. The Go backend (`internal/`) stays as it is, and
the app keeps the same features, manual and saved state as the macOS app.

Ground rules (same as NATIVE_MACOS_ROADMAP.md):

- The terminal must be at least as fast as the Wails build. Measure it, don't assume it.
- Side-pane data loads only when the pane is shown, and only visible data is polled.
- Pulses play three times then hold still. No endless animations or timers.
- Every write or cloud change is confirmed in-app. Dev builds never touch the
  real `~/.aws` or `~/.ssh`.
- Every feature has a clickable home (side pane, menu or right-click), not just a
  shortcut.
- The manual is updated with every feature, and all builds share the same pages.

Status legend: ☐ not started · ◐ in progress · ☑ done

## Architecture

```
┌──────────────── wtf (one Go binary, cgo) ─────────────────────────┐
│  internal/gtkui: windows, tab strip, splits, side pane (GTK 4)    │
│  Terminal view: VTE (vte-2.91-gtk4) via a thin cgo shim           │
│        │ direct Go calls                │ events → main loop      │
│  internal/uicore: pure-Go UI logic (secrets, prompts, layout,     │
│  fuzzy, inspect, workspaces…), tested against shared testdata     │
│        │                                                          │
│  internal/backend: the same 17 services the other UIs use         │
└───────────────────────────────────────────────────────────────────┘
```

Key decisions:

- **Language: Go, with GTK 4 through gotk4.** The UI lives in the same process
  and language as the backend, so:
  - There's no C bridge, no JSON dispatcher and no code generator: the UI calls
    services directly, with their real types.
  - There's no c-archive, so the 1.0.0 signal-mask problem can't happen. Shells
    are forked by a normal Go binary, as in today's Linux Wails build. M0 still
    checks the mask.
  - Fallback if gotk4 fails the M0 gate (crashes, leaks, slow callbacks): Rust
    with gtk4-rs and vte4, over the existing `libwtf` c-archive, like the macOS
    app, with a `wtf-exec` launcher.
- **Terminal: VTE**, the widget behind GNOME Terminal, Ptyxis and Tilix. gotk4
  doesn't ship VTE bindings, so `internal/gtkui/vte` is a small hand-written cgo
  shim (about 40 functions) that wraps a `VteTerminal` as a gotk4 `gtk.Widget`.
  Generating the bindings from `Vte-3.91.gir` with gotk4's generator is the
  alternative.
- **Who owns the PTY: Go.** Go keeps reading the PTY, as it does today, and
  feeds VTE with `vte_terminal_feed`. Keys come back through VTE's `commit`
  signal. Recording, shell integration and secret scanning keep working on the
  byte stream. M0 also measures VTE reading the PTY itself (`vte_pty_new_foreign_sync`),
  in case feeding costs too much.
- **Shared logic moves to Go.** `macos/Sources/WtfCore` (about 5,000 lines) and the
  remaining `frontend/src/lib` rules are ported into `internal/uicore`, and the
  shared `testdata` cases (secrets and others) run against it. The macOS app keeps
  its Swift copy for now. Moving it onto `uicore` over the bridge is a later,
  optional step.
- **No WebKit at all.** WebKitGTK is most of today's memory. The manual is
  rendered by `uicore` into GTK widgets (a `GtkTextView` with tags, plus native
  navigation), not a web view.
- **Toolkit look:** GTK 4 with libadwaita for the window chrome (header bar, dialogs,
  toasts, dark and light styles that follow the desktop). Tabs are custom-drawn
  in the header bar, as on macOS, not `AdwTabBar`, so groups, chips and dots
  match.
- **Build tags:** reuse `native` (no Wails), so the existing `native.go` files
  apply. The entry point is `cmd/wtf-gtk`. The Wails build still compiles until
  M7.
- **Supported systems:** GTK ≥ 4.14, VTE ≥ 0.76, libadwaita ≥ 1.5. That means
  Ubuntu 24.04+, Debian 13+, Fedora 40+, Arch. Both X11 and Wayland,
  amd64 and arm64.

## Linux-specific behaviour

| Area | macOS app | Linux app |
|---|---|---|
| Shortcuts | ⌘ | Ctrl+Shift for terminal-conflicting keys (Ctrl+Shift+C/V/T/W/F…), Ctrl elsewhere; one keymap table in `uicore` |
| Clipboard | NSPasteboard change count | `GdkClipboard`, plus the PRIMARY selection (select to copy, middle-click to paste); secret copies are cleared only if we still own the clipboard |
| Tab drag-out | window placed at the cursor | new window; Wayland can't position it, so it opens where the compositor puts it |
| Font | bundled via ATSApplicationFontsPath | JetBrains Mono in `/usr/share/wtf/fonts`, added with `FcConfigAppFontAddDir` |
| Theme | system appearance | `AdwStyleManager` (follows the desktop's dark preference) |
| Notifications | NSUserNotification | `GNotification` (portal-friendly) |
| Usage readout | task_info | `/proc/self/stat` and `smaps_rollup` (PSS) |
| Packaging | DMG + .app.zip | .deb and .rpm (nfpm), AppImage; no Flatpak, because a sandboxed terminal needs `flatpak-spawn --host` |

## Terminal features on VTE: how each maps

- **Look from config:** font, colours (`vte_terminal_set_colors`), cursor shape
  and blink, scrollback, bold-is-bright, zoom (`set_font_scale`).
- **Find:** `search_set_regex` (PCRE2) plus next and previous. VTE has no match
  count, so "N of M" comes from counting over `get_text_range_format`, only
  while the find bar is open.
- **Links:** `match_add_regex` for URL, file and JWT tokens, and OSC 8 hyperlinks
  turned on. Ctrl-click opens them, and right-click asks `check_match_at` what's under the mouse.
- **OSC 133 blocks:** Go splits each feed at the 133 markers, and reads
  `get_cursor_position` right after each part, so mark rows are exact. VTE
  rewraps lines on resize, so marks re-anchor when the column count changes.
  This is the biggest unknown, and M0 tests it.
- **Hide Secrets and log tints:** a `GtkOverlay` with a drawing layer over the
  VTE, using cell metrics (`get_char_width/height`). Tints use a GSK blend node
  (darken or lighten). The macOS M2 lesson applies: recompute at most once per
  frame clock tick, and only when something changed.
- **Program titles, bell, OSC 52:** VTE's `window-title-changed` signal; the bell
  is silent; OSC 52 clipboard writes stay off.
- **Paste confirmation and broadcast:** catch paste before VTE does
  (`paste-clipboard` signal) and route through `uicore.Paste`. Broadcast uses
  `commit`.

## Milestones

Same shape as the macOS plan. Days are working sessions. The macOS app's 20
planned days took far fewer, so treat these as an upper bound.

| # | Milestone | Days |
|---|---|---|
| M0 | Spike & go/no-go | 1–2 |
| M1 | App skeleton, `uicore` start, build & test setup | 2 |
| M2 | Terminal core | 3 |
| M3 | Window shell — **dogfood point** | 3 |
| M4 | Side pane I: Files, SSH, Kubernetes | 3 |
| M5 | Side pane II: AWS, Network, Processes, Findings, History, Snippets, Pins, Share, Generate, Workspaces | 3 |
| M6 | Viewers, tools, About, manual | 2 |
| M7 | Parity, perf, packaging, release | 3 |
| — | Buffer | 5 |

### M0 — Spike & go/no-go ◐

- `cmd/wtf-gtk`: one window, gotk4 + the VTE shim, one shell from
  `internal/terminal`, fed by Go.
- Benchmark against the Wails 1.0.1 Linux build in the same VM: a Linux port of
  `macos/bench/workload.sh` (CPU from `/proc/<pid>/stat`, memory as PSS from
  the app plus its WebKit helpers). Phases: idle 30 s, `seq`, coloured 42 MB
  `cat`, `yes`. Also try VTE reading the PTY itself.
- Check: the shell's signal mask is empty (`grep SigBlk /proc/<pid>/status`),
  Ctrl-C, resize and quitting kill the programs.
- Check: OSC 133 marks survive scrollback trimming and a resize rewrap.
- Hands-on: vim, htop, tmux, ssh, resize, copy/paste (CLIPBOARD and PRIMARY),
  IME (ibus), on GNOME Wayland and on X11.
- **Gate:** clearly lower idle CPU and memory than the Wails build, and heavy
  output no worse. Otherwise try the Rust/vte4 fallback, or stop.

**Status (2026-10-16): the headless part is done and the gate passes; the
hands-on check on a real Linux desktop is still to do.**

What's built (uncommitted): `linux/Dockerfile` (`wtf-gtk-dev:ubuntu24`,
arm64), `linux/build.sh`, `linux/run-xvfb.sh`, `linux/bench/` (the
`workload.sh` port plus an Xvfb driver), `internal/gtkui/vte` (the cgo shim:
a `WtfTerm` subclass of `VteTerminal` that signals `before-allocate` and
`grid-changed`), and `cmd/wtf-gtk` (one window, one shell from
`internal/terminal`, fed by Go; `WTF_GTK_PTY=vte` lets VTE read the PTY).
Self-checks: `WTF_NATIVE_DEBUG=marks` and `=signals` under `linux/run-xvfb.sh`.

Benchmark: Xvfb in Docker on this Mac (arm64 VM, software GL through
llvmpipe for both apps). CPU is in seconds, memory is PSS in MiB (app plus
WebKit helpers).

| Phase | Wails 1.0.2 | Wails, no DMA-BUF | wtf-gtk (Go feeds) | wtf-gtk (VTE reads PTY) | wtf-gtk, cairo renderer |
|---|---|---|---|---|---|
| idle 30 s: CPU | 27.6 | 8.8 | 0.03 | 0.01 | 0.03 |
| seq 2M: s / CPU | 1.72 / 6.7 | 1.90 / 2.5 | 0.48 / 0.56 | 0.47 / 0.35 | 0.53 / 0.48 |
| coloured 40 MB: s / CPU | 1.11 / 3.0 | 3.93 / 4.8 | 0.91 / 0.69 | 1.02 / 0.48 | 0.86 / 0.49 |
| yes 2M: s / CPU | 2.40 / 9.9 | 6.14 / 8.2 | 1.03 / 1.13 | 1.68 / 0.78 | 1.00 / 1.09 |
| memory | 782–895 | 573–688 | 247–286 | 245–246 | 113–168 |

The Wails idle number is inflated by software rendering under Xvfb (on
macOS it was 3 s per 30 s), but the order of magnitude matches the macOS M0.
A soak test (six rounds of 3M `yes` lines, typing, Ctrl+L) kept memory at
204–260 MiB with 42 threads, and idle CPU at 0 afterwards. No gotk4 leaks or
crashes showed up.

Checks passed:

- The shell's signal mask is empty (`SigBlk 0`), and so is a child's.
  Ctrl-C stops `sleep`, SIGWINCH reaches the foreground program, and
  quitting (or SIGTERM/SIGHUP to the app) kills a running `sleep`.
- OSC 133: 160 marks over 40 prompts land on their exact rows. They stay
  there after rewrapping narrower and wider (15–25 ms, relayout included)
  and after the scrollback trims; trimmed marks are dropped.
- vim, htop and tmux splits render correctly (screenshots via
  `xwd | convert`).

Findings that change the plan:

- **gotk4 v0.4.x is generated against GTK 4.20.** It calls 4.18 and 4.20
  functions, so it won't build on Ubuntu 24.04 (GTK 4.14). Pinned to
  **v0.3.1**, which matches 4.14. Upgrade once the oldest supported distro
  has a newer GTK. gotk4's generic `Connect` leaves out the instance
  argument in handlers.
- **`vte_terminal_feed` only queues.** VTE parses its whole queue in the
  frame clock's *update* phase. So Go feeds in *before-paint* (same frame,
  no added latency) with an adaptive per-frame budget, which is also the
  backpressure. Each feed is cut just after an OSC 133 mark, so in the next
  frame the cursor is exactly at the mark (at most one mark per frame).
  This replaces "read `get_cursor_position` right after each part".
- **VTE has no API for where the scrollback starts.** Its adjustment is
  relative (lower bound always 0). So each mark keeps its row's text, and a
  mark whose row no longer matches is dropped. That handles trimming, and
  `clear` too (the macOS 1.0.2 bug).
- **Rewrap:** before a width change (in `size_allocate`, before VTE's own),
  each mark notes how many line ends lie between it and the cursor.
  Afterwards it walks up to that line again. `get_text_range` gives a `\n`
  only between rows of a range, so "row ends a line" reads `(r,0)–(r+1,0)`.
- **Hidden tabs (M2):** VTE parses at 10 Hz when unmapped (its fallback
  scheduler), so frame-paced feeding and mark cuts must work differently
  for tabs that aren't shown.
- Go feeding costs about 40–60% more UI CPU than VTE reading the PTY on
  heavy output, but it's faster in wall time (yes: 1.03 vs 1.68 s) and keeps
  recording and secret scanning on the byte stream. Removing a copy didn't
  change it; profile in M2.
- GL memory dominates under llvmpipe; check PSS on a real GPU in the
  hands-on.

Still to do for M0 (needs a Linux desktop, for example a UTM Ubuntu 24.04
VM): GNOME Wayland and X11 hands-on with vim, htop, tmux and ssh, resizing,
CLIPBOARD and PRIMARY copy/paste, IME (ibus), and the benchmark with a real
GPU. The spike has no copy/paste shortcuts yet (VTE's defaults only).

The `WTF_GTK_PTY=vte` comparison mode was removed in M1: Go feeds VTE.

### M1 — Skeleton, shared core, tooling ☑

- `internal/gtkui` app shell: `AdwApplication`, single instance (a second
  `wtf` opens a window in the running one), the `native` lifecycle hooks, and
  `internal/events` delivered on the GTK main loop (`glib.IdleAdd`, batched).
- `internal/uicore` begins: secrets (shared `cases.json`), paste rules, fuzzy,
  keymap. Port the Swift and TS tests.
- Build: extend the `wtf-linux-build:ubuntu24` Docker image with
  `libgtk-4-dev libadwaita-1-dev libvte-2.91-gtk4-dev`, cache `GOCACHE` in a
  volume (gotk4's first build is slow), and add `task linux:native`.
- Self-checks run headless: Xvfb in an OrbStack Ubuntu machine,
  `WTF_NATIVE_DEBUG=snapshot` renders windows to PNG through
  `gtk_widget_snapshot` → `gsk_renderer_render_texture`, so I can check layouts
  without your time.
- Separate profile until M7 (`~/.config/wtf-native`, `~/.cache/wtf-native`),
  as on macOS, so a running Wails wtf keeps its kube overlays and tunnels.

**Status (2026-10-19): built and checked headless; the hands-on part waits
for a Linux desktop, with M0's.**

What's built:

- `internal/gtkui`: `App` (AdwApplication `com.dayanraj.wtf.Native`, its own
  ID until M7; single instance, so a second `wtf-gtk` opens a window in the
  first and exits; backend started in *startup*, so only the first instance
  runs one; SIGTERM/SIGHUP quit), `Window` (AdwApplicationWindow, header
  bar, one pane for now), `Pane` (VTE + the M0 feeder and marks on a
  backend session), `eventPump` (`internal/events` queued from any
  goroutine and handed out in one idle callback per batch, as Go values, no
  JSON). Window focus feeds `activity.Gate`. The spike moved here;
  `cmd/wtf-gtk` is now just `main`.
- `internal/backend`: `Start`/`Shutdown` run the services' lifecycle hooks,
  shared by `cmd/libwtf` and the GTK app.
- `internal/uicore`: secrets (on regexp2, since the rules need lookarounds
  and backreferences; passes every shared `cases.json` case), paste facts,
  fuzzy, and the keymap: every macOS menu command with a Linux shortcut,
  GNOME Terminal style (Ctrl+Shift for the terminal, Ctrl+Alt+letter for
  side-pane tabs, Alt for panes and tab numbers). Tests check there are no
  duplicates, no plain Ctrl+letter, none of the desktop's Ctrl+Alt keys, and
  that every command in `macos/Sources/Wtf/main.swift` has an entry.
  Shortcuts run in a capture-phase `GtkShortcutController`, before VTE
  turns keys into input.
- Separate profile: `~/.config/wtf-native` (state) and `~/.cache/wtf-native`.
  `config.toml` and snippets are shared.
- Build and checks: `task gtk` (`linux/build.sh`, version stamped from
  git) and `task gtk:check` (`linux/check.sh`: uicore tests, the `app`,
  `snapshot`, `marks` and `signals` self-checks, single instance, SIGTERM,
  no shells left behind). Docker image `wtf-gtk-dev:ubuntu24` instead of an
  OrbStack machine; the gotk4 caches live in volumes.
- `WTF_NATIVE_DEBUG=snapshot` writes `bin/gtk/snapshots/<window>.png`. It's
  in C (`snapshot.go`): gotk4 v0.3.1 treats `GskRenderNode` as a GObject
  and crashes in `gsk_renderer_render_texture`.
- libadwaita bindings: `gotk4-adwaita` pinned to 824c3ce (libadwaita 1.5,
  the last before 1.6).

Benchmark (Xvfb, llvmpipe), M0 → M1: idle 0.03 → 0.05 s CPU per 30 s, PSS
247 → 264 MiB (libadwaita). With the GL renderer, `seq` takes 0.95–1.02 s
(M0 0.48) at the same CPU: painting the Adwaita window (rounded corners,
shadow) in software makes frames longer, so less output fits a frame. With
`GSK_RENDERER=cairo` it's 0.48 s, as in M0. Check on a real GPU in the
hands-on; if it holds, consider a square window without a shadow.

### M2 — Terminal core ☑

Ports the macOS M2 list onto VTE: look from `config.toml` and `config:changed`,
zoom, find bar, links, paste confirmation, bracketed paste, Hide Secrets
covers, clipboard clearing, OSC 133 blocks (marks, durations, block menu,
previous/next prompt, copy last output, clear), right-click menu. `uicore`
gains `PromptTracker` and `SecretCovers`. Benchmark again after this milestone.

**Status (2026-10-19): done and checked headless** (`WTF_NATIVE_DEBUG=terminal`
and `=view` join `task gtk:check`; screenshots in `bin/gtk/snapshots`).

What's built:

- `uicore`: `Lines` (the M0 marks, now on a `Grid` interface: text
  fingerprints, rewrap re-anchoring), `PromptTracker` (blocks, durations,
  output, jumps, "prompts only move down"), `SecretCells` (soft-wrapped
  lines, private keys from above, cell widths), `FindIn`, theme colors
  (cover, accent) and durations. All tested with a fake grid.
- `gtkui`: `settings` (config, `config:changed`, `AdwStyleManager` dark,
  text size for every window as on macOS, Hide Secrets), the pane's look
  (font, line height, cursor, scrollback, palette and selection colors),
  `overlay` (gutter marks with tooltips, duration labels, block highlight,
  find highlights, covers; hidden when empty, updated once per main-loop
  pass), find bar, right-click and block menus (one kept popover per pane),
  paste confirmation (`AdwMessageDialog`, "Don't Ask Again" saves
  `confirm-paste`), CLIPBOARD and PRIMARY paste through the checks,
  Ctrl-click links (URL match and OSC 8), copy-on-select, copy with secrets
  replaced while hidden, `copySecret` with clipboard clearing, toasts
  (`AdwToastOverlay`), slow-command notifications.
- Input to the shell goes through a per-pane writer goroutine, so a shell
  that isn't reading can't block the window.

Findings:

- **Where the screen is.** VTE's adjustment counts from the oldest row
  still kept, while its cursor and text calls use absolute rows, and
  nothing says how many rows were trimmed (trimmed and blank rows read the
  same, and text ranges aren't clamped). So the feeder sends a cursor
  position report (`CSI 6n`) after each frame's output; VTE answers through
  `commit` with the cursor's row on screen, which gives the screen's first
  absolute row, and from that the oldest row: `screenTop + rows - upper`.
  The answer is kept from the shell; reports the program asked for in the
  same frame are counted and passed on first. `=view` checks it after
  trimming.
- The feeder tracks modes from the byte stream: alternate screen (no marks
  there), mouse reporting (right/middle click go to the program unless
  Shift is held) and bracketed paste (the confirmation says which).
- Bash reports the first simple command of a line (`echo one` for
  `echo one; echo two`), as in the other builds.
- A `GtkPopoverMenu` unparented after closing crashed GTK; a kept popover
  whose model changes doesn't.
- gotk4's `ContentProvider.Value` passes an untyped `GValue`, so clipboard
  text can't be read back synchronously; clearing uses the macOS rule (only
  if nothing changed the clipboard since).
- Under Xvfb with no window manager, the keyboard doesn't return to the
  window after a dialog; the self-check answers the paste dialog last.

Not done here: **Select Output** (VTE has no API to select a range; Copy
Output is there), hidden-tab feeding (needs tabs, M3), split dimming (M3),
log tints and the token menus from `Logs.swift` (with the viewers, M6), Pin
and Save as Snippet in the block menu (M5).

Benchmark (Xvfb, llvmpipe), M1 → M2: idle 0.05 → 0.02 s CPU per 30 s, `seq`
0.95 → 0.97 s (0.62 → 0.58 s CPU), coloured 40 MB 0.92 → 0.98 s (0.82 →
0.96 s CPU), `yes` 0.99 s both (1.14 → 1.23 s CPU), PSS 264–312 MiB. The
extra CPU on heavy output is the report and the overlay scan per frame.

### M3 — Window shell (dogfood point) ☑

`uicore` gains `WindowModel`, `Layout`, `SavedSession` (same `session.json`), the
infra badges and workspaces, with their tests. GTK: tab strip in the header bar
(drag, groups, dots, rename, tear-off), split panes with `GtkPaned`-style
dividers, address bar, broadcast, palette, workspace open, several windows,
restore, menus as a header-bar menu plus a shortcuts window, quit confirmations.

**Status (2026-10-19): done and checked headless** (`WTF_NATIVE_DEBUG=tabs`
then `=restore` join `task gtk:check`).

What's built:

- `uicore`: `WindowModel`, `Layout`, `SavedSession` (same `session.json`,
  lenient like the Swift), `TabPayload`, the address bar `Badges`,
  `Workspaces` (placeholders, plans, `OpenPlan`, `CaptureWorkspace`),
  `ShellQuote` and `UseIdentity`, `ParseEnv`, `IsTerminalReport`. All the
  Swift tests are ported, the random-operations invariant test included.
- `gtkui`: wtf's own title bar (`GtkWindowHandle`: tabs from the left, +,
  the menu, the window buttons); the tab strip (activity dot, ✓/✗, group
  chips and colors, click / double-click to rename / middle-click / right-click
  menus, drag to reorder, drag between windows, drag out to tear off); a
  `GtkStack` page per tab with its split tree in `GtkPaned`s (rebuilt only
  when the shape changes; dragged dividers write the ratio back);
  unfocused split panes veiled (`unfocused-split-dim`); zoom; the address
  bar (group, process, folder crumbs, context badges with expiry clocks,
  running command or last result, Secrets hidden); broadcast input (bar,
  outlines, tab and group menus, terminal reports kept in their pane); the
  command palette (commands with shortcuts, tabs, groups, workspaces; theme
  mode with preview); Show All Tabs; the workspace dialog (placeholders,
  plan preview, here or in a new window); the Keyboard Shortcuts window
  (generated from the keymap, only what works); several windows named
  `main`, `w2`… as on macOS; session save 400ms after a change and on
  quit, restore at launch; moving tabs between windows with their shells;
  close/quit confirmations listing running programs; git identity switch
  when a pane starts.
- The feeder now waits for each report's answer before feeding more: the
  answer places marks exactly, and a pane that isn't showing (VTE parses
  it about ten times a second) is fed from a 100ms timer instead of the
  frame clock. The `tabs` check runs 2,000 lines in a hidden tab and gets
  all of them and its marks.

Findings:

- `AdwHeaderBar` centers its title widget, so tabs can't start at the
  left; a `GtkWindowHandle` with `GtkWindowControls` does it.
- Shifted symbols are matched by the key GTK sees: Ctrl+Shift+\ is
  `<Control>bar`; the keymap names them that way (Bar, BraceLeft,
  BraceRight) and shows them as Ctrl+Shift+\.
- A window that never changed after opening was never saved; only
  restored windows skip the first save now (the macOS app has the same gap).
- The folder in the address bar follows `PWD` from each prompt's report:
  the process poll pauses while no wtf window is in front.

Not checked headless (needs the desktop): dragging tabs (reorder, between
windows, tear-off), keyboard focus moving between windows and dialogs with a
real window manager.

Benchmark (Xvfb, llvmpipe), M2 → M3: idle 0.02 → 0.03 s CPU per 30 s,
`seq` 0.97 → 0.50 s, coloured 0.98 → 0.92 s, `yes` 0.99 → 0.98 s, PSS
270–324 MiB. (`seq` is back to M0's time: the Adwaita header bar was what
made llvmpipe frames slow.)

### M4 — Side pane I ☑

Rail and container, Files (git letters, drag a path into a terminal), SSH
(hosts, keys, identities, tunnels and their dialogs), Kubernetes (resources,
contexts, port-forward, secret viewer). Lists are `GtkListView` with
recycled rows, so long lists cost nothing. `uicore` gains the SSH and Kube rules.

**Status (2026-10-19): done and checked headless** (`WTF_NATIVE_DEBUG=sidebar`
joins `task gtk:check`, with a test `~/.ssh/config`, key and kubeconfig in
the check's own home).

What's built:

- `uicore`: `SidebarState` (the session's `sidebar` object, unknown fields
  kept), Files (`FileTree`, `GitIndex`, paths), SSH (commands, host and
  tunnel drafts, key rules) and Kube (`KubeTarget` commands, tones,
  details, table kinds, contexts) rules, with the macOS tests.
- `gtkui`: the side pane (icon rail, the shown tab only, made when chosen
  and dropped when not; a divider for its width, saved per window; a
  command and shortcut per tab, which closes the pane when it's already
  showing that tab); a shared `sideList` (sections, collapsible headings,
  rows with icons, dots, pills, letters and buttons, menus, drag and drop);
  the Files tab (tree, git letters and folder dots, changes view, filter,
  back/up/pin, click to type a path, double-click to cd, menus with git
  commands, drag a path into a terminal); the SSH tab (Hosts, Keys, Git
  identities, Tunnels, every dialog: host editor, upload/download, install
  key, new key, tunnel editor, host keys, identity editor); the Kubernetes
  tab (Resources by section with polling of open sections only while the
  window is in front, Contexts with edit plans, the secret viewer with
  values hidden on blur, scale, EKS, port forward). Side-pane actions that
  type into a shell use `runInPane`.
- Event subscriptions can be dropped (`events.on` returns `off`).
- Dev image: `librsvg2-common`, `adwaita-icon-theme` (theme icons are
  SVG) and `openssh-client`. The packages must depend on them too (M7).

Findings:

- A `GtkPopover` parented to a `GtkListBox` is one of its children:
  clearing the list (even with `RemoveAll`) loops on it forever. The
  list's menu is parented to its scrolled window.
- Labels that wrap must not ellipsize too (`wrapLabel`).

Not done here: the "Install…" links for missing tools (the tools dialog is
M6); Keychain options (macOS only); "New key" opens the key dialog directly
until the Generate tab (M5); the Kubernetes port-forward dialog is its own,
not the SSH tab's tunnel editor; row icons aren't tinted. Not checked
headless: the Kubernetes Resources view against a real cluster, real SSH
connections, the agent.

### M5 — Side pane II ☑

AWS, Network, Processes (one shared poller: only the visible tab, only while a
window is in front), Findings, History, Snippets, Pins, Share & Record,
usage readout, Generate, Workspaces and the editor.

**Status (2026-10-19): done and checked headless** (`WTF_NATIVE_DEBUG=panes`
joins `task gtk:check`, with a test `~/.aws/config` and snippets file).

What's built:

- `uicore`: AWS, network and process, findings, history and pins, export
  (names, Markdown transcripts), snippets and password rules, with the
  macOS tests; `PromptTracker.Transcript`.
- `gtkui`: the window's records (history, pins that follow a running
  command, recordings, saved files) with Pin Output / Pin Selection, Start
  and Stop Recording (GTK save dialog, REC pill in the address bar, a red dot
  on the tab), Save Output as Text and Save Commands as Markdown; a shared
  `poller` (only the visible tab, only while its window is in front, again
  when it comes back); the History, Pinned, Share & Record, Network,
  Processes, AWS Profiles, Generate Keys & Passwords, Findings (and Run
  Security Scan), Snippets (run dialog, runbooks, editor) and Workspaces
  (editor, Save Window as Workspace) tabs; the resource-usage readout and
  dialog; Save as Snippet in block, history and pin menus; snippets in the
  palette and its own Run Snippet mode. Network, Processes, AWS, snippets
  and usage keep one store per app, so windows share one poll.
- Side-pane form dialogs re-show after a failed save on the next main-loop
  turn: a message dialog hides itself after its response handler returns.

Not done here: the "Install…" links (tools dialog, M6); per-line colors in a
row's alert and tinted row icons (`sideRow` has one alert line and no icon
class); clickable process-group headings; a window-level tunnel editor (the
Network and Generate tabs borrow the SSH tab's dialogs through a detached
tab). Not checked headless: recording (needs the save dialog), the
editors' Review/Confirm flows, AWS SSO and key rotation, killing processes.

Benchmark (Xvfb, llvmpipe): idle 0.03 s CPU per 30 s, `seq` 0.51 s,
coloured 0.96 s, `yes` 1.07 s, PSS 278–325 MiB (side pane closed: as M3).

### M6 — Viewers, tools, About, manual ☑

JSON, JWT, Base64, certificate/TLS and secret viewers (masked, hidden on blur),
the Command-Line Tools dialog (install commands for apt, dnf and pacman come from
`internal/tools`), About, and the manual rendered natively from
`frontend/src/manual`, with a check that every link resolves.

**Status (2026-10-19): done and checked headless** (`WTF_NATIVE_DEBUG=viewers`
joins `task gtk:check`; the `terminal` check covers log colors).

What's built:

- `uicore`: inspection (JWT, Base64, certificates, TLS targets, selection
  entries), log tokens and their menus, log tints (with `PlainGrid`, so a
  program's own colors win), the manual parser (blocks of styled runs for
  a native renderer; every page and link checked against
  `frontend/src/manual`), ordered JSON; `MissingTool`.
- `gtkui`: the JSON, JWT, Base64 and certificate/TLS viewers (decoded
  values hidden while wtf loses focus, copied with clipboard clearing); the
  right-click menu's inspect entries and the token menus (paths open in the
  editor, Kubernetes names describe, URLs and TLS); Ctrl-click on tokens;
  log colors, blended over the text by the terminal widget's own snapshot
  (a GSK blend node: darken on dark themes, lighten on light), read 120ms
  after output from VTE's HTML export so program-colored text is left
  alone; the Command-Line Tools dialog (install commands for apt, dnf and
  pacman from `internal/tools`, run in a tab after confirming; the once-ever
  first-run check); About (with diagnostics); the manual rendered with GTK
  widgets (no WebKit), with search, back/forward, links, and the keymap's
  shortcuts filled in; the rail's tools and help buttons; F1. The manual's
  pages are embedded from `frontend/src/manual` (`embed.go` there).
- Keymap: Rename Tab, Open Snippets File, Open Themes Folder (the manual
  names them).

Findings:

- VTE's HTML export marks a program's colors (`<font color>`,
  `<span style="background-color">`): enough to keep log tints off them.
- A `WtfTerm` snapshot override draws in VTE's content coordinates (inside
  its CSS padding), unlike the overlay drawing area.

Manual wording still Mac-only on Linux (for M7): files.md "⌫ for Back",
settings.md `option-as-meta`, tools.md (wtf menu, Homebrew/Apple methods),
tabs.md `Ctrl+Tab`.

Not done: "Install…" links from each side-pane error (`showTools(tool)` is
there to call); hints in menus (`menuItem` has no tooltip).

Benchmark (Xvfb, llvmpipe), two runs: idle 0.02–0.03 s CPU per 30 s, `seq`
0.40–0.92 s (it varies run to run under llvmpipe), coloured 0.97–0.99 s,
`yes` 0.99–1.06 s, PSS 281–324 MiB: as M5, log colors on. The benchmark's
profile skips the first-run tools check (`tools-checked`), as a returning
user's does: its dialog would take the keyboard.

### M7 — Parity, perf, packaging, release ◐

- Parity checklist against the macOS app (all palette commands, every
  right-click menu, every manual page). The manual gets Linux shortcut text.
- Perf: idle 0%, every side-pane tab 0% over 30 s, heavy output vs M0.
- Packaging: .deb and .rpm for amd64 and arm64 (nfpm: depends on
  `libgtk-4-1`, `libadwaita-1-0`, `libvte-2.91-gtk4-0`), `.desktop` file, icon,
  font, AppImage if it bundles cleanly. Test-install on clean Ubuntu 24.04,
  Debian 13 and Fedora containers.
- Switch to the real folders. Release as the next minor version, with Linux
  native and macOS native.
- Afterwards: decide whether to delete the Wails/Svelte UI. Nothing would ship it
  any more; the manual pages move to `manual/`.

**Status (2026-10-20): parity, perf and packaging done and checked headless;
the release is waiting.**

- Real folders and ID: the app is `com.dayanraj.wtf` and uses `~/.config/wtf`
  and `~/.cache/wtf`, shared with the Wails build. Self-checks (and
  `WTF_NATIVE_PROFILE=separate`) keep `wtf-native` and the `.Native` ID.
  `linux/check.sh` confirms the real folders.
- Parity, from an audit against the macOS app: the tab menu (git identity
  items, Use Automatic Name, Close Tabs to the Right, macOS order, no panes
  broadcast while the group broadcasts), the group menu's broadcast item,
  naming a new group, the six missing palette commands (Inspect TLS, paste
  confirmation, Generate key/password, usage toggle, auto-refresh toggle),
  Settings… (Ctrl+,), Ctrl+Tab, Install… links (Kubernetes and Network notes,
  Kubernetes section errors, context and AWS menus, Findings at its scanner),
  and the Files tab's pause button. Manual: Backspace for Back, the tab menu's
  close items.
- Packaging: `linux/package.sh` builds in the dev image per architecture
  (`wtf-gtk-dev:ubuntu24-amd64` for amd64) and runs nfpm
  (`linux/package/nfpm.yaml`): /usr/bin/wtf, `com.dayanraj.wtf.desktop`,
  hicolor icons, AppStream metadata, JetBrains Mono in /usr/share/wtf/fonts
  (added for this process with `FcConfigAppFontAddDir`). The program name is
  the app ID, so X11's WM_CLASS matches the desktop file.
  `linux/package/test-install.sh <arch>` installs on clean Ubuntu 24.04,
  Debian 13 and Fedora 41 and starts it under Xvfb: all pass, amd64 and
  arm64. Upgrading from the 1.0.3 Wails .deb removes /usr/local/bin/wtf and
  the old desktop file.
- AppImage: not made. Bundling GTK 4, libadwaita and VTE with linuxdeploy
  isn't clean, and every supported distribution takes the .deb or .rpm.
- Perf (`WTF_NATIVE_DEBUG=perf`, 30 s per tab, Xvfb/llvmpipe): idle 0.02 s;
  each tab 0.01–0.08 s, Network 0.22 s (it polls `ss` while shown); the
  manual 0.26 s for its first render, then 0. Heavy output as M0: `seq`
  0.52 s, coloured 0.94 s, `yes` 1.11 s, PSS 280–325 MiB.

Findings:

- nfpm expands variables in fields but not in file paths, so `package.sh`
  writes a concrete config per architecture.
- Fedora's minimal image has no libGLESv2, and GTK aborts when it opens
  GL; the rpm depends on `libglvnd-gles`.

Not done: "Select Output" (VTE can't select a range from Go); menu hints;
the hands-on list under M0.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| gotk4 leaks, crashes or slow cgo callbacks | Unstable app | M0 gate; keep callbacks off the output path; Rust/vte4 fallback |
| VTE rewrap moves OSC 133 marks | Wrong block marks after resize | Tested in M0; re-anchor on column change |
| Overlays (covers, tints) slow VTE | Terminal slower than Wails | Once per frame, only on change; benchmark after M2 |
| Wayland limits (window placement, global shortcuts) | Small gaps vs macOS | Accept; document in the manual |
| Distro spread (old GTK/VTE) | Won't start on older systems | Clear minimum versions in package deps |
| Hands-on needs a Linux desktop | Calendar slips | A UTM Ubuntu 24.04 desktop VM on this Mac; headless snapshot checks for everything else |
| Three UIs until Wails is retired | Features cost 3× | Most logic goes to `uicore`; no new features until M7 is done |
