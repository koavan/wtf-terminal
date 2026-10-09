# Roadmap: wtf for DevOps engineers

Goal: make wtf the terminal DevOps engineers want — safe around production, fast
across fleets of hosts, and aware of the infrastructure tools they live in.

Ground rules that apply to every item:

- Nothing here may cost terminal rendering performance. Background work (polling
  kube, cloud CLIs, etc.) runs only while its UI is visible and can be switched off
  in `config.toml`, like `sidebar-auto-refresh`.
- macOS and Linux both.
- Build on what exists: OSC 133 shell integration, tab groups, the side pane, the
  command palette, `internal/notify`, session restore.

Status legend: ☐ not started · ◐ in progress · ☑ done

## Tier 1 — build first

### 1. Context badges ☑

The address bar shows the Kubernetes cluster/namespace, AWS account/profile/
region, gcloud project and Terraform workspace of the focused shell (see README,
"Context badges"). Reported by the shell at each prompt; no polling.

Decided against (2026-10-09): flagging "production" contexts and a
confirm-before-destructive-command guard. Detection by name patterns isn't
fool-proof, and a warning that is sometimes wrong is worse than none. Revisit
only with a reliable signal.

### 2. SSH host manager (side-pane tab) ☐

- Parse `~/.ssh/config` (including `Include`); searchable list, grouped by host
  patterns or tags.
- Click → new tab with `ssh <host>`; multi-select → open as a tab group.
- Browser metaphor: hosts are bookmarks.

### 3. Broadcast input ☐

- Send keystrokes to all panes in a split, or all tabs in a tab group
  (right-click group → "Broadcast input").
- Loud, always-visible indicator while active; one shortcut to stop.

### 4. Command blocks ◐ (second priority)

Already there: ✓/✗ marks, jump between prompts (⌘↑/⌘↓), copy last output,
long-command notifications. Still to do: duration in the gutter, copy/re-run
for any command (not just the last).

- Using OSC 133 marks: ✓/✗ gutter mark with exit status and duration per command.
- Actions: copy this command's output, jump to previous/next command, re-run.
- Desktop notification (via `internal/notify`) when a command running longer than
  N seconds finishes while the window is unfocused.

## Tier 2 — differentiators

### 5. Kubernetes side-pane tab ☐

- Pods, deployments, recent events for the current context/namespace.
- Click a pod → logs / exec / describe, each opening a new tab with the real
  `kubectl` command (so it lands in history and is reproducible).
- Polls only while visible. A lightweight, built-in k9s.

### 6. Workspaces / launch profiles ☐

- Named layouts in `config.toml`: tabs, groups, splits, each with cwd, env and a
  startup command (e.g. "payments-oncall": 3 SSH tabs + log tail + kubectl watch).
- Open from the command palette. Builds on session restore.

### 7. Snippets / runbooks ☐

- Parameterized commands, e.g. `kubectl -n {{ns}} rollout restart deploy/{{name}}`,
  filled in from the command palette.
- Stored in a plain file so a team can share them via git.

### 8. Port-forward and tunnel manager ☐

- Start/stop/monitor `kubectl port-forward` and `ssh -L` tunnels from the Network
  tab, which already lists listening ports.
- "Open in browser" for localhost ports.

## Tier 3 — polish

- ☐ **Log-friendly rendering:** highlight and make clickable timestamps, levels
  (ERROR/WARN), IPs, URLs, `file:line`, k8s resource names; "pretty-print JSON"
  on a selected line.
- ☐ **Pin output:** pin a command's output to a side panel while you keep working.
- ☐ **Secret redaction:** mask AWS keys, tokens, JWTs on screen and on copy — for
  screen-sharing during incidents.
- ☐ **Session recording:** export a tab as asciinema/text for postmortems.
- ☐ **Git status in the file tree** (previously deferred).

## Previously deferred (not DevOps-specific)

- Packaging (.app / DMG / AppImage).
- Dropping a tab onto another window's tab strip (today: "Move to window" menu).
