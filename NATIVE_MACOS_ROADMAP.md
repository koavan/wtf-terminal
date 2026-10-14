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
│           WtfBridge.swift: call(service, method, json) → json     │
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
- **Build.** Use SwiftPM with Command Line Tools (installed). A new `darwin:native`
  task in the Taskfile builds `libwtf.a` and then the app bundle. Full Xcode is
  optional, but installing it gives XCTest and Instruments.
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

## M1 — Go bridge & app skeleton ☐

- Build `cmd/libwtf` with the c-archive dispatcher, an event callback and a PTY
  byte callback, and wire up startup and shutdown.
- `WtfBridge.swift` wraps it in typed async Swift functions, plus Codable models
  for the service types.
- Add the SwiftPM package, `Info.plist`, icon, ad-hoc signing and the
  `darwin:native` task.
- **Done when:** the app launches, calls `about.Info` and receives a `notify`
  event.

## M2 — Terminal core ☐

Replaces `TerminalPane.svelte`, `lib/terminals.ts` and the xterm add-ons.

- PTY I/O, resizing, themes and fonts (from `config.toml`), cursor-blink setting,
  unicode/emoji width.
- Search bar, clickable links, selection and copy, and paste confirm (#19).
- Clipboard clearing (#14) and secret masking/redaction.
- OSC 133 shell integration: prompt blocks and command status.
- Output batching that matches the v0.7.2 behaviour.
- **Done when:** vim, htop, tmux, ssh and kubectl all work, and the M0 benchmark
  still passes.

## M3 — Window shell (dogfood point) ☐

Replaces `TabStrip`, `TabPanes`, `TabOverview`, `AddressBar`, `Palette`,
`BroadcastBar`, `ContextMenu`, `Toast` and `Overlays`.

- Tabs and tab groups, splits, tab overview, rename.
- Address bar with context badges (Kube, AWS, gcloud, Terraform, SSH facts).
- Command palette, native menus, all shortcuts from `manual/shortcuts.md`.
- Broadcast input, session restore, workspaces (open and edit).
- Multiple windows, plus pausing background polling when no wtf window is in front.
- **Done when:** you switch to it as your daily terminal, still without the side pane.

## M4 — Side pane I ☐

- Side pane container: tabs, collapsing, and loading data only when shown.
- **Files:** `FileTree` with git status, file actions, `TransferDialog`.
- **SSH:** `SshView`, `SshHosts`, `HostEditor`, `GroupEditor`, `SshKeys`,
  `KeyGenerator`/`KeyGenForm`/`KeyInstall`, `SshTunnels`/`TunnelEditor`/
  `ForwardRow`, `HostKeysDialog`, `ScanDialog`, `GitIdentities`/`GitIdentityEditor`.
- **Kube:** `KubeView`, `KubeContexts`, `KubeCluster`, `EksAddDialog`, logs.
- Exec, describe and SSH actions type into the focused shell (runInPane).

## M5 — Side pane II ☐

- **AWS:** `AwsView`, `AwsProfileEditor`, `AwsRotate`, expiry clocks (#10).
- **Network** and **Processes** views, with refresh controls and the paused note.
- **Findings** (#12), **History**, **Snippets** (view, editor, dialog), **Pins**.
- **Share & Record:** recording and transcripts. This replaces
  `@xterm/addon-serialize`, so the recording format may need a backend change.
- Resource usage readout and dialog.

## M6 — Viewers, generators, dialogs, manual ☐

- `JsonViewer`, `JwtViewer`, `CertViewer`, `DecodedViewer`, `SecretViewer`
  (token/cert inspector #11).
- `GenerateView`, `PasswordGenerator`, `ToolsDialog` (tool check and install, #20),
  `ToolHint`.
- `AboutDialog`, `UsageDialog`, `CommandPromptDialog`, `Confirm`.
- Manual in a WKWebView, rendering the same `frontend/src/manual/*.md` pages.

## M7 — Parity, perf, tests, release ☐

- Go through a parity checklist covering every manual page and every right-click
  menu against the Svelte build.
- Port the TS unit tests for logic kept in Swift, and add Go tests for logic
  moved to the backend.
- Profile with Instruments (idle, heavy output, side pane open) and confirm
  there are no endless timers or animations.
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
