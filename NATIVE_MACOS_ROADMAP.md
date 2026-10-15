# Roadmap: native macOS UI (Option A)

Goal: replace the Wails webview UI on macOS with a native AppKit/SwiftUI app,
keeping the Go backend (`internal/`, ~20k lines) unchanged. Linux keeps the
current Wails + Svelte build.

Ground rules (same as ROADMAP.md, plus native-specific):

- The terminal must be at least as fast as today. Measure it, don't assume it.
- Side-pane data loads only when the pane is shown, and only visible data is polled.
- Pulses play three times then hold still. No endless animations or timers.
- Every write or cloud change is confirmed in-app. Dev builds never touch the
  real `~/.aws` or `~/.ssh`.
- Every feature has a clickable home (side pane, menu or right-click), not just a
  shortcut.
- The manual is updated with every feature, and both builds share the same pages.

Status legend: ☐ not started · ◐ in progress · ☑ done

## Architecture

```
┌──────────────── wtf.app (Swift, AppKit + SwiftUI) ────────────────┐
│  Window / tabs / splits (AppKit)   Side pane views (SwiftUI)      │
│  Terminal view (SwiftTerm)         Manual (WKWebView, shared md)  │
│                  │                                                │
│           Backend.swift: call(service, method, json) → json       │
│                  │        events / PTY bytes → callback           │
└──────────────────┼────────────────────────────────────────────────┘
                   │ C ABI
┌──────────────────┴──────── libwtf.a (Go, -buildmode=c-archive) ───┐
│  Generic dispatcher over the same services main.go binds today:   │
│  config, terminal, state, notify, files, infra, certs,            │
│  awsprofiles, kube, snippets, about, sshhosts, tools, findings,   │
│  usage, netconn, procs, windows                                   │
└───────────────────────────────────────────────────────────────────┘
```

Key decisions:

- **Bridge.** Use one generic, reflection-based dispatcher (`service.Method` + JSON
  args) instead of hand-written C functions per method, so new Go methods need
  no bridge changes. PTY output goes through a dedicated byte callback, not JSON.
- **Terminal.** Use SwiftTerm 1.20.0, which is mature and MIT-licensed. Its
  CoreText renderer beat its Metal one in M0, so CoreText is the default. M0 benchmarks libghostty against it, but its embedding API isn't
  stable yet.
- **Build.** Use SwiftPM with full Xcode (installed 2026-10-14), which brings
  XCTest and Instruments. `macos/build.sh` (or `task native`) builds `libwtf.a`
  and then the app bundle.
- **Logic that lives in `frontend/src/lib`** (fuzzy match, secret masking, paste
  checks, prompts/blocks, log view, redaction, themes) is moved to Go where it
  can be, so Linux and macOS share it. The rest is ported to Swift along with
  its tests.

## Timeline

Twenty working days with active sessions, plus a 25% buffer. Dates assume a
start on Mon 2026-10-19. Gaps between sessions push the dates back but don't add
effort.

| # | Milestone | Days | Dates |
|---|---|---|---|
| M0 | Spike & go/no-go | 1 | Oct 19 |
| M1 | Go bridge & app skeleton | 2 | Oct 20–21 |
| M2 | Terminal core | 3 | Oct 22–26 |
| M3 | Window shell — **dogfood point** | 3 | Oct 27–29 |
| M4 | Side pane I: Files, SSH, Kube | 3 | Oct 30–Nov 3 |
| M5 | Side pane II: AWS, Net, Procs, Findings, History, Snippets, Pins, Share | 3 | Nov 4–6 |
| M6 | Viewers, generators, dialogs, manual | 2 | Nov 9–10 |
| M7 | Parity, perf, tests, release | 3 | Nov 11–13 |
| — | Buffer | 5 | Nov 16–20 |

## M0 — Spike & go/no-go ☑

- ☑ Build `libwtf.a` (`cmd/libwtf`) with terminal sessions only, plus a bare SwiftTerm
  window running a shell (`macos/`, built by `macos/build.sh`).
- ☑ Benchmark against v0.7.2 (`macos/bench/workload.sh`). It measures UI CPU and
  memory footprint (app plus WebKit helpers, not including the workload)
  across idle, `seq`, a coloured 42 MB `cat` and `yes`.
- ☑ Hands-on check: vim, htop, tmux, ssh, resizing, copy/paste, IME (2026-10-14).
- Skipped libghostty. SwiftTerm passed the gate, so there was no need to try it.
- **Gate:** if native isn't clearly better than v0.7.2 on CPU or memory, stop
  here and fall back to Option B or profiling-only work.

Results, 2026-10-14. UI CPU seconds per phase; memory is the largest footprint
seen.

| Phase | Wails 0.7.2 | Native CoreText | Native Metal |
|---|---|---|---|
| idle 30 s | 3.05 s | 0.01 s | 0.09 s |
| `seq 1 2000000` | 7.27 s | 6.66 s (−8%) | 6.87 s |
| `cat` 42 MB coloured | 0.78 s, 0.94 s wall | 0.67 s (−14%), 0.62 s wall | 0.69 s |
| `yes` × 2M lines | 7.62 s | 6.41 s (−16%) | 6.61 s |
| memory | 371–589 MiB | 28–86 MiB | 91–120 MiB |

The 0.7.2 run used the everyday instance with its other tabs and state open, so
its idle CPU and memory are upper bounds. A clean 0.7.2 instance would be a
fairer comparison. Throughput is limited by how fast the PTY delivers output,
so wall times are close and the CPU gap under load is moderate.

## M1 — Go bridge & app skeleton ☑

- ☑ `cmd/libwtf` (built with `-tags native`):
  - `wtf_call(service, method, argsJSON)` calls any exported method of the 17
    services by reflection, with the same result and error shape as Wails.
  - `wtf_init` / `wtf_shutdown` run the services' startup and shutdown hooks.
  - Events reach Swift through a callback. PTY output has its own callback with
    per-frame backpressure and an end-of-stream signal.
  - `wtf_set_focused` lets polling pause while no window is in front.
- ☑ Backend separated from Wails:
  - `internal/backend` builds the services for both UIs.
  - `internal/events` replaces direct `application.Get().Event.Emit` calls.
  - Wails lifecycle hooks and event registration are in `wails.go` files
    (`//go:build !native`), with `native.go` counterparts.
  - The native archive has no Wails dependencies. The regenerated Wails bindings
    are byte-identical.
- ☑ Separate profile until M7: state in `~/Library/Application Support/wtf-native`
  and caches in `~/Library/Caches/wtf-native` (`internal/paths`). Running both
  apps side by side can't clear the other's kube overlays or start the same
  tunnels. `config.toml` is shared.
- ☑ Swift: `Backend.swift` provides typed async `call`/`run`, `on(event)`,
  `attach`, `write` and focus. `Models.swift` holds hand-written models (about,
  terminal events). A Swift model generator from the Go types is deferred to
  M4, when many more types are needed.
- ☑ `bin/native/wtf.app` is built by `macos/build.sh` (or `task native`): it
  reuses the Wails `Info.plist` with bundle id `com.dayanraj.wtf.native`, plus
  icon, commit/date and ad-hoc signing. The minimum macOS is 14.
- ☑ Tests: dispatcher unit tests, and a headless C smoke test
  (`cmd/libwtf/smoke/run.sh`, or `task native:test`) covering init, a call, an
  error, a shell, output, the exit event and shutdown.
- ☑ Launch check (2026-10-14): `about.Info` was read, the title followed
  `terminal:info` (`~`, then `sleep — /private/tmp`), the app quit on the exit
  event, and only the `wtf-native` directories were written.

## M2 — Terminal core ☑

Replaces `TerminalPane.svelte`, `lib/terminals.ts` and the xterm add-ons.

- ☑ Look from `config.toml`, following `config:changed` and the system
  appearance: theme colours, font, line height, cursor style and blink,
  scrollback, Option as Meta, copy-on-select. Bigger, Smaller and Actual Size
  text work (⌘= ⌘- ⌘0).
- ☑ Find bar (⌘F, ⌘G, ⇧⌘G, Esc, match case). Links open with ⌘-click
  (http, https, mailto, file).
- ☑ Paste confirmation (#19): the same rules, text and buttons as the Svelte
  build, including Paste Without Final Newline and Don't Ask Again, plus
  bracketed paste.
- ☑ Hide Secrets (⌥⌘R, and on at startup with `redact-secrets`):
  - Covers over secrets on screen, recomputed for each output frame and scroll.
  - Copies, the block menus and the window title are redacted.
  - `WtfCore/Secrets.swift` ports `lib/secrets.ts` (ICU regex adjusted to
    behave like JavaScript). Both implementations are tested against the shared
    `testdata/secrets/cases.json`, written by `node testdata/secrets/generate.ts`.
- ☑ Clipboard clearing (#14): `Clipboard.copySecret` uses the pasteboard change
  count. It has no callers until the secret viewers arrive in M6.
- ☑ OSC 133 command blocks (`WtfCore/PromptTracker.swift`):
  - ✓/✗ marks in the gutter, with tooltips; click one for the block's actions.
  - Duration labels on slow commands.
  - Block menu: Copy Command / Output / Both, Select Output, Run Again.
  - Previous/Next Prompt (⌘↑ ⌘↓), Copy Last Command Output (⌥⌘C), Clear Buffer
    (⌘K).
  - Marks use absolute rows with `BufferLine` identity, so they survive
    scrollback trimming and reflow.
- ☑ Right-click menu: the block's actions, then Copy, Paste, Select All, Find,
  Clear and Hide/Show Secrets. Pin, snippet and recording items come with
  M5.
- ☑ Tests: 26 Swift tests, covering the shared secret cases, paste, durations,
  prompt tracking (trimming and reflow) and secret covers (wrapping, wide
  characters, a key that started above the screen).
- ☑ Benchmark (`native-m2-fix1`, 2026-10-14): CPU is within 2–7% of the M0
  spike (seq 6.79 s, coloured 0.72 s, yes 6.62 s), and idle is 0.05 s per
  30 s. Memory is 52–166 MiB, higher than M0 because of the configured 10,000
  lines of scrollback (M0 had 500). The first M2 build was 2–7× slower because
  the overlay refreshed (tooltips, cursor rects, a full-window redraw) for every
  output frame and every scrolled line. Now marks refresh at most once per
  main-loop pass and redraw only when they change, and the overlay is hidden
  when empty. Covers stay synchronous while Hide Secrets is on.
- → Bundling JetBrains Mono moved to M7. npm only ships WOFF2 subsets, so the
  native app needs the TTF. Until then it uses JetBrains Mono if installed,
  otherwise Menlo.
- ☑ Done (2026-10-14): your hands-on check passed (vim, htop, tmux, ssh,
  kubectl, paste, Hide Secrets, blocks, find). The benchmark passes (see above).
  `WTF_NATIVE_DEBUG=paste` runs a self-closing check of ⌘V → confirmation
  sheet, using a private pasteboard.

## M3 — Window shell (dogfood point) ◐

Specs for this milestone were written from the Svelte and Go sources (tabs, address bar,
palette), and the port follows them.

- ☑ `WtfCore/WindowModel` + `Layout`: tabs, panes, the split tree, groups, titles,
  rename, reorder with group settling, collapse, zoom, directional focus,
  detach/adopt. `SavedSession` uses the same `session.json` shapes as the Svelte
  build (version 2 per window), and unknown fields (sidebar) are kept. Ported
  `tabs.test.ts`, including the 2,000-operation random invariant check.
- ☑ Tab strip in the title bar (custom-drawn): process icons, activity and finished
  dots, group chips and underline, close button, middle-click, double-click
  rename, right-click menu, drag to reorder, drag off the strip for a new window,
  and empty space moves or zooms the window.
- ☑ Pane area: split tree layout, draggable dividers (double-click for 50/50),
  dimmed unfocused panes (`unfocused-split-dim`), and a zoom pill.
- ☑ Address bar: group chip, process chip, path breadcrumbs, context badges with
  expiry clocks (`infra.Resolve`, newest request per pane wins, re-run on
  `infra:changed`), the running/✓/✗ status chip, and a "Secrets hidden" toggle.
  The badge logic is ported from `infra.ts` with its tests.
- ☑ Broadcast input (⌥⌘B, tab menu, group editor): per window. Only the pane
  with keyboard focus fans out, terminal reports are filtered (ported tests),
  and there's the orange bar, pane outlines and a count in the paste
  confirmation.
- ☑ Palette (⌘⇧P): commands, tabs, groups and workspaces with the exact fuzzy
  ranking (ported tests). Themes are previewed live and reverted on Esc.
- ☑ Workspaces: open dialog with placeholders (`$kube_namespace` and the like),
  live plan, here or in a new window. Splits are chained then evened out, and
  groups matched within the plan (ported logic, with tests).
- ☑ Windows: names `main`, `w2`…; saved windows reopen at launch; a moved tab
  keeps its live terminal (scrollback, running program) instead of replaying a
  buffer; close confirmations; quit asks when programs run (`RunningProcesses`)
  and saves synchronously; polling pauses while no window is in front.
- ☑ Menus as in `main.go` (Shell, Edit, View, Tab, Pane, Window), with Ctrl-Tab.
- ☑ `libwtf` `session` service (saved windows, forget) for the native window manager.
- ☑ Self-checks: `WTF_NATIVE_DEBUG=tabs` then `=restore` round-trip the session
  through a real quit and relaunch (passed 2026-10-14).
- → Moved to M5, next to the Workspaces side pane: the workspace editor (Save
  Window as Workspace, edit, duplicate, delete, review).
- ◐ Hands-on (2026-10-14, ongoing daily use) fixed so far: typing a group name
  lost focus after each character (sync moved focus on every change; now only
  when the front pane changes), and double-clicking a tab to rename also
  zoomed the window (the title bar saw it too; strip double-clicks now go to
  the strip only).

## M4 — Side pane I ☑

Specs for this milestone were written from the Svelte and Go sources (side pane and Files, SSH,
Kubernetes), and the port follows them.

- ☑ `cmd/swiftgen`: typed Swift for every backend service. It generates Codable
  models for the 74 Go types (lenient: `null` becomes empty) and an async
  function per method (106). `TestGeneratedSwiftIsCurrent` fails when they're
  stale.
- ☑ Side pane container: icon rail (Files, SSH Hosts, Kubernetes so far), drag
  to resize (220–520, double-click for 300), ⌘B and a toggle in the strip, and
  ⌘⇧E/S/K (pressing the shown tab's shortcut closes the pane). The state is
  saved in the session's `sidebar` object (`SidebarState`, unknown fields
  kept). Closed means gone, and only the shown tab exists.
- ☑ Files: follows the focused pane or a pinned root, with Back and Up history
  per pane, filter, keyboard navigation, git letters and folder dots
  (`GitIndex`), the changes list, file, folder and git menus, a Remote session
  note for ssh panes, and drag a path onto a terminal. It reads on root change
  and 250 ms after each command; there's no timer.
- ☑ SSH:
  - Hosts: grouped by file plus known hosts, multi-select, open here, as a
    split, in tabs or as a tab group (with broadcast), sftp, scp (dropping a
    file on a host opens the upload), the host editor, delete, host keys
    (scan, forget, trust).
  - Keys: agent load and unload, passphrase, chmod, `.pub`, install on a host,
    delete, and generate in a terminal.
  - Git identities: use, stop, test login, add to GitHub, editor; also in the
    tab menu.
  - Tunnels: live from `ssh:tunnels`, with the tunnel editor.
  - Rules ported with tests (`WtfCore/Ssh.swift`).
- ☑ Kubernetes:
  - Resources: 17 sections, read only while open and polled at most every
    max(setting, 10 s) while the window is in front. Namespace override,
    deployment → pods, row menus with exact kubectl commands, confirmations
    for anything that changes the cluster, scale, secret viewer (masked,
    hidden on blur) and port-forward.
  - Contexts: per-shell switching through overlay files, plan → confirm →
    back up → apply edits, Test Connection, Add EKS Cluster, merge.
  - Rules ported with tests (`WtfCore/Kube.swift`).
- ☑ Shared pieces: `SideList` (sections, collapsible headings, pills, menus,
  drops) and `FormSheet` (dialogs).
- ☑ Self-check `WTF_NATIVE_DEBUG=sidebar` (read-only) passed 2026-10-14.
- → Moved to M5: the in-app SSH key generator form (it lives in the Generate
  tab); until then "+ New key" uses the generate-in-terminal dialog.
- ☑ Hands-on (2026-10-14), with these fixes:
  - Any command crashed the app: the address bar's running chip had a
    constraint tied to a view it wasn't attached to yet.
  - File names were cut early (a stale cached size on reused rows).
  - Empty-state notes didn't wrap to the pane.
  - Section headings had stray bars behind them.
  - The side pane's fields took the keyboard from the terminal.
  - Kubernetes headings didn't open: the table swallowed their clicks.

  New self-checks: `commands`, `kubeclicks` (no cluster reads), and `snapshot`
  (the app renders its window to PNGs for layout checks).

## M5 — Side pane II ☑ (pushed 2159b9f)

- **AWS:** `AwsView`, `AwsProfileEditor` (form → planned changes → confirm),
  `AwsRotate`. Expiry clocks were already on the address-bar badge (M3).
- **Network** and **Processes** views, with refresh controls and the paused
  note. A shared `Poller` refreshes only the visible tab while its window is in
  front, and catches up when the window comes back to the front.
- **Findings** (#12) with the Scan dialog, **History** (scope saved with the
  window), **Snippets** (tab, run dialog with runbook steps, editor, palette
  mode ⌘⇧R, Save as Snippet… from command blocks, History and Pins) and
  **Pins** (following running commands every 1.5 s, only while shown).
- **Share & Record:** Hide Secrets, recording (`terminal.StartRecording`: the
  Go side writes asciicast, so no format change was needed), REC pill in the
  address bar and a red dot on the tab, Save Output as Text, Save Commands as
  Markdown.
- Resource usage readout (sampled only while shown) and dialog. The Windows
  group shows only if something lands there; the native app draws its own
  windows.
- **Generate** tab: SSH key form (made in Go, with a passphrase), password
  generator (`SecRandomCopyBytes`). The SSH tab's + New key opens it.
- **Workspaces** tab and workspace editor (moved here from M3).
- Self-check: `WTF_NATIVE_DEBUG=m5 WTF_NATIVE_SNAPSHOT_DIR=…` (set
  `AWS_CONFIG_FILE`/`AWS_SHARED_CREDENTIALS_FILE` to throwaway files) renders
  every M5 tab and the usage dialog.
- Known: bash integration reports `$BASH_COMMAND`, so a pipeline shows only its
  first command in History and Pins (zsh is fine; same in the Wails build).
  The Tools dialog (“Install…” links) comes with M6; until then, the hints say
  how to install.

## M6 — Viewers, generators, dialogs, manual ☑ (built, awaiting hands-on check)

- **Viewers** (`Viewers.swift`): JSON (key-order-preserving, JS-style
  printing in `WtfCore/OrderedJSON.swift`), JWT, Decoded Base64 (masked,
  re-masked on focus loss), Certificate/TLS, and the Kubernetes secret viewer
  rewritten on the same base (Reveal All ⇄ Hide All, eye/copy icons, scroll
  limits, masks when another wtf window takes focus). Rules in
  `WtfCore/Inspect.swift`, tests ported from `inspect.test.ts`.
- **Entry points:** terminal right-click (URL or JWT under the mouse, what the
  selection can be inspected as, a YAML base64 value, Pretty-print JSON), Files
  → Inspect Certificate…, palette Inspect TLS Certificate…. Not yet: ⌘-click
  on a JWT (right-click works) and Kubernetes-resource tokens in output (they
  need the output token scanner, M7 parity).
- **Command-Line Tools** dialog with Install view (commands/scripts shown, run
  in a new tab), first-run welcome, Install… links (Network, Kubernetes,
  Scan dialog; AWS and kube contexts via the row menu), wtf menu item, rail
  wrench button. **About** sheet (true native credits). **Help** menu, ⌘?,
  rail ? button.
- **Manual** (`ManualWindow.swift`): the same `frontend/src/manual/*.md`,
  bundled into the app, rendered in Swift (`WtfCore/Manual.swift`, tests
  ported, plus a check that every page's links resolve), native nav and
  search, WKWebView with JavaScript off, made on open and released on close.
- Self-check: `WTF_NATIVE_DEBUG=m6 WTF_NATIVE_SNAPSHOT_DIR=…` (manual command
  ids, inspector menus over real output, every dialog rendered).
- Lesson: an NSBox doesn't size to auto-layout contents; cards are bordered
  NSStackViews.

## M7 — Parity, perf, tests, release ◐

- **Font:** JetBrains Mono bundled (static TTFs, OFL) via `ATSApplicationFontsPath`.
- **Release build:** `macos/build.sh [release|debug] [universal] [dmg]` makes a
  universal app (fat Go archive + `swift build --arch arm64 --arch x86_64`) and
  a DMG (ad-hoc signed; no Developer ID, so no notarization).
- **Performance** (`WTF_NATIVE_DEBUG=perf`, results in `macos/bench/results.csv`
  as `native-m6`): idle 0% CPU, 49 MB; heavy output on par with M0; every
  side-pane tab 0% over 30 s; the manual costs ~40 MB while open, released on close.
- **Parity:** all 77 palette commands match. Gap audit done; fixed so far:
  - Terminal: program titles (OSC 0/2), a silent bell, and no clipboard writes from programs (OSC 52), all as in Wails.
  - Find bar: N of M, every match highlighted, typing refines the search.
  - Mouse: ⌥-drag selects in mouse-using programs; right-click selects the word.
  - Log colours (`log-highlighting`): blended over the text, no buffer change; ⌘-click and right-click token menus for timestamps, IPs, file:line, Kubernetes resources, URLs and JWTs.
  - Menus: the tab menu's recording/export section; red destructive items; Services; Pin Last Command Output; the Pin Selection item.
  - Settings: warnings shown at launch; Network/Processes view saved per window; the pause button follows the setting.
  - SSH: the tunnel editor's Port field, plus SSH dialogs waiting for their lists to load.
  - Workspaces: the editor's placeholders update live.
  - Pins: the output box resizes.
  - Plus many smaller strings and fixes.
- Shipped as **v1.0.0** (the native app took over the real `wtf` bundle id and folders).
- Polish after 1.0.0: Tab Overview card ×, group-tinted icons and ↑/↓ moves;
  a tear-off ghost window; palette section headers and tinted tab rows; SSH
  editor labels, ⌘↩ to save, Host Keys states and busy buttons; Files git
  tooltips; Kubernetes "Pods of deployment" filter, saved test results and
  one-line problems; AWS and Findings results one per line with severity dots;
  Network forward counts; many smaller strings.
- **Signal fix (after 1.0.0):** shells started by the native app inherited a
  signal mask from Go's c-archive threads, with SIGINT, SIGHUP, SIGTERM,
  SIGWINCH, SIGTSTP and more blocked. So programs survived quitting, and Ctrl-C
  and resizes could be lost. A tiny `wtf-exec` helper (`macos/exec/wtf-exec.c`,
  in Contents/MacOS) clears the mask, resets signal handlers and then execs the
  shell; Go uses it through `terminal.Launcher`. Closing a terminal also hangs up
  its foreground process group, because on macOS the hang-up only reaches the
  session leader.
- Left: ⌥-click cursor moves (needs shell-integration `cl=`).


- Go through a parity checklist covering every manual page and every right-click
  menu against the Svelte build.
- Port the TS unit tests for logic kept in Swift, and add Go tests for logic
  moved to the backend.
- Profile with Instruments (idle, heavy output, side pane open) and confirm
  there are no endless timers or animations.
- Bundle JetBrains Mono (TTF, OFL) and register it at launch, to match the
  Wails build's default font.
- Release pipeline: universal binary, codesign, notarize, DMG. Update README and
  the build/release recipes.
- Ship as **v0.8.0** with the native build on macOS and Wails on Linux.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Native terminal isn't faster than tuned xterm/WebKit | Whole effort is wasted | M0 go/no-go gate |
| Two UIs (Swift + Svelte) drift apart | Every feature costs about 2× | Move logic into Go, share the manual, keep a parity checklist in M7 |
| SwiftTerm gaps (serialize, some OSC codes) | Recording or shell integration breaks | Handle in the backend, or upstream patches |
| Keyboard and IME edge cases (dead keys, CJK input, Option-as-Meta) | Daily-use annoyances | Dogfood from M3 onward, two weeks before release |
| No full Xcode (no XCTest or Instruments) | Weaker testing and profiling | Install Xcode before M7, or use swift-testing and `sample`/`top` |
| Feedback loop: each GUI check needs your hands-on time | Calendar time slips | Batch the review checkpoints at the end of each milestone |
