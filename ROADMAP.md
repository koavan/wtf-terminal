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

### 2. SSH host manager (side-pane tab) ☑

The SSH tab (⌘⇧S): `~/.ssh/config` hosts (with `Include`, grouped by
file) plus known_hosts servers; filter, double-click to connect, open in a
split, or multi-select and open as a named tab group; add/edit/delete hosts;
keys and ssh-agent; server host keys (forget, scan, trust); scp upload and
download, sftp; tunnels (see #8). See README.

### 3. Broadcast input ☑

- Send keystrokes to all panes in a split, or all tabs in a tab group
  (right-click group → "Broadcast input").
- Loud, always-visible indicator while active; one shortcut to stop.

### 4. Command blocks ☑

✓/✗ marks with exit code, duration and finish time on hover; the duration of
slow commands (≥1s) at the end of their prompt line; jump between prompts
(⌘↑/⌘↓); for any command, from its mark or a right-click in its output:
copy command / output / both, select output, run again (block highlighted
while the menu is open); long-command notifications.

- Using OSC 133 marks: ✓/✗ gutter mark with exit status and duration per command.
- Actions: copy this command's output, jump to previous/next command, re-run.
- Desktop notification (via `internal/notify`) when a command running longer than
  N seconds finishes while the window is unfocused.

## Tier 2 — differentiators

### 5. Kubernetes side-pane tab ☑

Resources (14 kinds in groups, each loaded only when its section is open;
actions as real kubectl commands in tabs, changes confirmed; secret values
on request, masked), and Contexts (every kubeconfig in ~/.kube, per-shell
switching, rename/delete/namespace/merge, Add EKS Cluster, fix
aws-iam-authenticator auth). See README.

### 6. Workspaces / launch profiles ☑

`[[workspace]]` blocks in the snippets files (user, project, team): tabs with
names, groups and colors, panes (side by side or stacked) with folders and
startup commands, `{{placeholders}}`. Open Workspace… (palette) or the
Workspaces side-pane tab (⌘⇧L); a dialog previews every tab and command, then
opens them here or in a new window. Save Window as Workspace… captures the
current tabs (running commands become startup commands); form editor with
reviewed, block-level writes.

### 7. Snippets / runbooks ☑

Run Snippet… (⌘⇧R) and the palette list snippets from
`~/.config/wtf/snippets.toml`, `~/.config/wtf/snippets/*.toml` and the
project's `.wtf/snippets.toml` (shell's folder and up); `{{name}}` /
`{{name=default}}` placeholders (defaults can be `$kube_namespace`, …) are
filled in a form with a live preview, then inserted, run, or run in a new
tab; runbooks run step by step; Save as Snippet… from a command or History.
The Snippets side-pane tab (⌘⇧Y) manages them with a form editor (reviewed
TOML, block-level edits that keep the rest of the file).

### 8. Port-forward and tunnel manager ☑

ssh tunnels (SSH tab → Tunnels) and kubectl port-forwards run by the same
background manager (status, reconnect after a drop or pod restart, start
with wtf, stop on quit). kubectl forwards keep the creating shell's
KUBECONFIG, AWS profile and PATH. The Network tab's Forwards lists both,
with open-in-browser, and labels their listening ports; the Kubernetes tab's
Port Forward… opens the editor (save & start, or run once in a terminal).

- Start/stop/monitor `kubectl port-forward` and `ssh -L` tunnels from the Network
  tab, which already lists listening ports.
- "Open in browser" for localhost ports.

### 9. AWS profile manager ☑

AWS Profiles tab (⌘⇧U): list/add/edit/rename/delete profiles with a reviewed
plan for every change, use a profile in the focused shell, check identity
and key age, rotate access keys with rollback. See README.

## Tier 3 — polish

- ☑ **Log-friendly rendering:** colors for log levels, timestamps, URLs, IPs
  and k8s resources (on-screen rows only, after output settles; programs' own
  colors and full-screen apps untouched; `log-highlighting`); ⌘-click or
  right-click actions for timestamps (local/UTC/relative, copy), IPs (ssh,
  browser), k8s resources (logs/describe/YAML in a new tab), `file:line`
  (editor at line, local files only); "Pretty-print JSON" on a line or selection.
- ☑ **Pin output:** the Pinned side-pane tab (⌘⇧I): Pin Output from a command's
  menu, Pin Selection, Pin Last Command Output; running commands' pins follow
  them; in memory only.
- ☑ **Secret redaction:** Hide Secrets (⌥⌘R): covers AWS keys, tokens, JWTs,
  passwords and private keys on screen (before the frame is drawn) and redacts
  them on copy and in the window's other text; `redact-secrets`.
- ☑ **Session recording:** record a terminal to asciicast v2 (⌥⌘S, written by the
  backend as output arrives); save the scrollback as text or the commands as a
  Markdown transcript.
- ☑ **Git status in the file tree:** branch with ahead/behind, changed files
  colored with M/A/U/D/R/! and folders dotted, a changes list (deleted files
  too), and read-only Show Changes / Show History in a new tab.

## Tier 4 — DevSecOps

Same ground rules, plus: show facts (expiry times, key sizes, permissions), never
guessed danger levels; nothing loads until its UI is opened.

### 10. Credential expiry clocks ☑

The AWS and Kubernetes badges, and an ssh badge, show how long the focused
shell's credentials have left (`42m`, `expired`) from a day before: exported
temporary keys' expiry, SSO logins, saml2aws stamps, kube client certificates
and JWT/OIDC tokens, ssh-agent certificates. Read at each prompt; counts down
in the frontend. Not done: Vault tokens (needs a network call with the token).

### 11. Token and certificate inspector ☑

⌘-click / right-click actions, alongside the log-rendering ones:

- JWT → decoded header and claims, `exp`/`iat`/`nbf` as local times.
- Base64 / kube Secret data → decode in place, masked until revealed.
- Hostname or URL → TLS chain, expiry, SANs, issuer, protocol/cipher.
- PEM file in the file tree → the same certificate details.

Reuses Hide Secrets' JWT and key detection. Also from a selection (JWT,
base64, PEM block, host or host:port) and the palette (Inspect TLS
Certificate…). Decoded base64 is masked until revealed; JWTs and base64 can't
be opened while Hide Secrets is on. Manual page: Tokens and certificates.

### 20. Command-line tools: check and install ☑

Done before #12, whose scanners will use it. Side-pane features run kubectl, the
AWS CLI, git, OpenSSH, lsof/ss and notify-send. One lookup (`internal/tools`:
shell PATH, wtf's PATH, install folders) shared by the features and the
**Command-Line Tools** dialog (palette, About wtf), which shows each one's path,
version and what uses it. Missing ones install per OS and CPU via Homebrew, the
official release (checksum or signature checked where the vendor publishes one;
user-level into `~/.local/bin` where possible), Apple's Command Line Tools, or
the distro's package manager. The exact command or script is shown first and runs
in a new tab; nothing installs on its own. Side-pane errors about a missing
program get an **Install…** link. On first start, opens once if something is
missing (looks only, runs nothing). The `.deb`/`.rpm` recommend git, OpenSSH,
lsof, libnotify. Manual page: Command-line tools. Not done: scanners (with #12),
Vault (with #18).

### 12. SARIF findings tab ☐

Side-pane tab that reads SARIF (trivy, semgrep, checkov, gitleaks, grype,
tfsec): findings grouped by severity and rule, click-through to `file:line`.
"Run scan…" runs the scanner as a visible command in a terminal tab. No polling.

### 13. Incognito tab ☐

No shell history, no session restore, no saved scrollback, recording blocked,
Hide Secrets forced on. Clearly marked; for break-glass access and handling
secrets.

### 14. Clipboard auto-clear ☑

Clear the clipboard N seconds after copying a revealed secret, generated
password or private key, only if it still holds that value.
Built: `clipboard-clear` (default 30 s, 0 off) applies to Kubernetes secret
values, decoded base64, Copy Token on a JWT in output, generated passwords
and passphrases (lib/clipboard.copySecret). wtf never copies a private key.

### 15. Effective permissions panel ☐

`kubectl auth can-i --list` for the current namespace, and the AWS caller
identity with its attached policies. Loaded on request.

### 16. SSH config facts ☑

Label hosts with `ForwardAgent`, `StrictHostKeyChecking no` and `ProxyJump`;
show each key's type, bit length and age in Keys. Display only, no warnings.
Built: **agent fwd** and **hostkey no/off/accept-new** pills (resolved like
ssh, `Host *` included; exact setting on hover), jump hosts as "via …";
keys show RSA/ECDSA sizes and "from Mar 2023" (the file's time).

### 17. Context-tagged history ☐

Record each command with the kube context, AWS account/region it ran under;
filter History by them ("what did I run against account X today?").

### 18. Cloud secrets browser ☐

AWS Secrets Manager, SSM Parameter Store and Vault, like the k8s secrets view:
values masked until revealed, every write confirmed with a reviewed plan.

### 19. Multi-line paste confirmation ☐

Preview before a paste that contains newlines or control characters is sent.
Triggered by content, never by guessing what the command does.

## Previously deferred (not DevOps-specific)

- Packaging: .app / DMG and .deb done (v0.1.0, v0.2.0); AppImage not.
- Dropping a tab onto another window's tab strip (today: "Move to window" menu).
