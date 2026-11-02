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

### 12. SARIF findings tab ☑

Side-pane tab that reads SARIF (trivy, semgrep, checkov, gitleaks, grype,
tfsec): findings grouped by severity and rule, click-through to `file:line`.
"Run scan…" runs the scanner as a visible command in a terminal tab. No polling.
Built: internal/findings + Findings tab (⌘⇧F). Per git repository; SARIF from
wtf's scans lives in the cache, `*.sarif` at the repo's top is read too (tfsec,
CodeQL… any producer). Severity is the scanner's (security-severity score,
else level). Run Scan types the command into the focused shell (new tab if
busy), like the other side-pane actions. Scanners are optional entries in Command-Line
Tools (Homebrew on macOS; Linux points to their sites). Reloads when a scan
command finishes.

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

### 19. Multi-line paste confirmation ☑

Preview before a paste that contains newlines or control characters is sent.
Triggered by content, never by guessing what the command does.
Built: lib/paste + PasteConfirm for ⌘V/Edit → Paste, Ctrl+Shift+V and the
right-click Paste; control characters shown (␛…), bracketed-paste state and
broadcast pane count stated; Paste Without Final Newline; `confirm-paste`
(default on), Don't Ask Again, and a palette toggle.

## Tier 5 — tab types

Tools you work in for minutes at a time get a tab of their own, not a side-pane
section: the side pane stays for glanceable helpers, and its width can't fit
long URLs, header tables or request bodies.

### 22. API client tab ☑

A new kind of tab, opened from the new-tab (`+`) menu, the right-click menu and
the command palette. Nothing loads until one is open. Native apps only (macOS
and Linux); not the Wails UI (#21).

First version:

- Method, URL, query parameters, headers (key/value table), body (raw text or
  JSON, with formatting).
- Response: status, time, size, headers, body with JSON pretty-printing and find.
- Copy as curl, and Run in Shell: type the equivalent `curl` into the focused
  shell (runInPane), a new tab only if busy.
- Paste a `curl` command into the URL bar to fill in the request.
- Hide Secrets masks tokens in the response; Authorization values are masked
  until revealed.

Later: saved requests (a small collapsible side-pane list that opens them in a
tab), environment variables from the focused shell, history, splitting an API
tab beside a terminal.

Built for macOS (2026-10-26): internal/apiclient (Send/Cancel, FormatJSON,
Curl, ParseCurl — options it can't represent are refused, not dropped);
an API pane kind in WindowModel, so splits, groups, moving windows, Duplicate
and Reopen work; credential header values are left out of the saved session.
Linux (2026-10-26): the same pane kind in uicore.WindowModel (sessions are
shared with macOS), gtkui/apiview.go, Ctrl+Alt+Shift+A, self-check
`WTF_NATIVE_DEBUG=api` in linux/check.sh.

## Tier 6 — AI assistance

Agreed 2026-10-28. AI that works from what wtf already knows (command blocks,
context badges, logs, the Kubernetes and AWS tabs, Findings, recordings)
instead of guessing. Build in the order listed: the foundation first, then
#24 to prove the pipeline end to end; #27 waits until #24–#26 are solid.
Native apps only (macOS and Linux).

Rules for every item here:

- On demand only: nothing calls a model in the background, so no idle CPU,
  memory or network cost.
- Never runs a command that changes anything. Suggested commands are typed
  into the focused shell (runInPane) and wait for Enter; the only exception is
  #27's allowlist of read-only commands.
- Secrets are redacted with the Hide Secrets patterns before anything is sent,
  and the first send shows exactly what will leave the machine.
- No guessed danger labels: facts (resource names, counts, account IDs) are
  listed as facts; the model explains them, it doesn't raise alarms.
- Every feature gets a clickable home (right-click / side pane / palette) and a
  manual page.

### 23. AI foundation ☑

Works with any AI tool, not one vendor. Features talk to one interface; which
model or tool answers is configuration.

- `internal/ai`: one provider interface with streaming replies, cancel, and
  token/cost counts per request when the provider reports them.
- Three provider kinds, which between them cover any AI tool:
  - `anthropic`: the Claude Messages API.
  - `openai`: any OpenAI-compatible chat endpoint, with a configurable base
    URL. Covers OpenAI, Azure OpenAI, Gemini (its OpenAI endpoint), Mistral,
    Groq, DeepSeek, OpenRouter, and local servers (Ollama, LM Studio, vLLM,
    llama.cpp), where nothing leaves the machine.
  - `command`: any CLI tool. wtf writes the prompt to its stdin and streams
    its stdout, e.g. `claude -p`, `codex exec`, `gemini -p`, `llm`, `aichat`.
    No API key in wtf; the tool's own login, model and settings apply.
    No token/cost counts unless the tool prints them.
- Several named providers in `config.toml`; pick one for quick inline
  features (`ai-fast`) and one for heavier ones (`ai-default`), switchable
  from the palette. Tested against a fixed set (Claude API, OpenAI, Ollama,
  one CLI); others work through the same kinds.
- Tool calling (needed by #27) is used when the provider supports it
  (`anthropic`, most `openai` endpoints); #27 says so and is unavailable
  otherwise, never faked by parsing free text.
- API keys in the macOS Keychain / Linux secret service, per provider, never
  in `config.toml`.
- Redaction pass shared with Hide Secrets, plus a "what's being sent" preview
  (shown the first time per provider, then on request). Applies to `command`
  providers too: wtf redacts before the text reaches the tool.
- AI features hidden entirely when no provider is set.

```toml
ai-default = "claude"
ai-fast = "local"

[ai.providers.claude]
kind = "anthropic"
model = "claude-sonnet-5-5"

[ai.providers.local]
kind = "openai"
base-url = "http://localhost:11434/v1"
model = "qwen3:8b"

[ai.providers.codex]
kind = "command"
command = ["codex", "exec", "-"]
```

Built: `internal/ai` (Status, Preview, Ask/Cancel streaming as `ai:chunk` /
`ai:done`, Presets, Models, keys via go-keyring), provider tables edited in
place by `config.SaveAIProvider`/`DeleteAIProvider`, per-provider extra
`headers` (e.g. `anthropic-workspace-id`; key headers refused). A real
Settings window (⌘, / Ctrl+,) with AI and General sections: add from presets
(cloud APIs, Ollama/LM Studio, Claude Code/Codex/Gemini CLI/llm, custom),
edit, delete, Fetch Models, keys, Test, default/quick; the first-send preview
and a shared request helper for #24+ (AIRequest on macOS, askAI on Linux).
Command providers run in an empty temp folder, through wtf-exec on macOS.
Self-check `ai` on Linux. Not done: the Linux keyring path is untested on a
real desktop (the check container has no Secret Service); tool calling for
#27 is left to #27.

### 24. Explain this failure ☑

Right-click a failed command block → Explain. Sends the command, exit code,
the tail of its output, cwd, relevant tool versions and the active context
(kube context, AWS profile). Returns the likely cause and a suggested fix
command, typed into the focused shell but not run.

Built: `internal/explain` (service `explain`: `Request` builds the facts —
command, exit code with the shell's meaning for 126/127/signals, duration,
last 150 lines of output, folder as `~`, system and shell, the shell's
Kubernetes/AWS/GCP/Terraform/git context from infra.Resolve, and versions
of an allowlist of well-known tools found on the shell's PATH; `Parse`
splits the reply into cause and the last fenced block as the fix, with
control characters dropped). Block menu **Explain Failure…** (failed blocks,
only with a provider set up), palette/menu **Explain Last Failed Command…**,
a dialog that streams the reply with What's Sent, Copy, Close (stops it) and
**Type Fix at Prompt** (Ctrl-U then the fix, no Enter; a multi-line fix is
bracketed, or copied when the shell has no bracketed paste). Uses
`ai-default`. Linux self-check `explain`.

### 25. Plain English to command ☑

A prompt bar (⌘I / Ctrl+Shift+I, palette, right-click) that turns "delete
evicted pods in all namespaces" into a command for the current shell and
context. Inserted at the prompt, never executed; a short explanation of each
flag on request. (⌘K stays Clear Buffer; Ctrl+K belongs to the shell.)

Built: `internal/suggest` (service `suggest`: `Request` sends the words with
explain's shared `ShellFacts` — folder, system, shell, Kubernetes/AWS/GCP/
Terraform/git context — and the well-known tools on the PATH, to `ai-fast`;
`Parse` takes the last fenced block as the command, made safe to type;
`FlagsRequest` continues the conversation to explain each part). A bar in
the palette's style: Return asks, Return again (same words) inserts with
Ctrl-U first, Explain Flags, What's Sent, Esc stops or closes. The typing
helper (`typeAtPrompt`) is shared with #24. Linux self-check `explain`
covers both.

### 26. Change reviewer ☐

For `terraform plan`, `kubectl diff` and `helm diff` output (a command block
or a plan file). Parse it first, deterministically: resources created,
changed, replaced and destroyed, IAM/security-group changes, listed as facts.
Then the model writes a plain-English summary of what will happen and why
(e.g. "replaces the RDS instance because `engine_version` changed").

### 27. Incident investigator ☐

An AI side-pane tab (or its own tab) where you ask "why is `checkout-api`
crashlooping in staging?". The agent investigates with an allowlist of
read-only commands (`kubectl get/describe/logs/events`, `aws … describe-*`,
`helm history`, `dig`, `curl -I`, …), each shown as a normal command block,
using the active kube context and AWS profile. It ends with a finding that
cites the evidence. Anything that changes state (rollback, scale, restart) is
typed into the focused shell for you to confirm. Step and cost limits per run;
Stop at any time.

### 28. Log summarizer ☐

On a logs pane or a selection: group repeated errors into patterns with
counts, first/last seen, and a short timeline.

### 29. Session to runbook or postmortem ☐

From a recording or a range of command blocks: a parameterized runbook saved
to Snippets, or a postmortem draft (timeline of commands, output and
decisions).

### 30. Findings triage ☐

For a SARIF finding in the Findings tab: what it means, whether the code path
looks reachable, and a proposed patch shown as a diff (applied only on
confirm).

### 31. Ask your history ☐

Natural-language search over context-tagged history ("the command I used to
rotate the RDS cert last month"). Depends on #17.

## Also built

- Resource usage (2026-10-13): wtf's own CPU and memory in the address bar
  (`show-usage`) and a per-process breakdown (app, WebKit windows, and the
  programs run in it, counted apart). internal/usage samples via libproc and
  /proc only while shown.

## Maintenance

### 21. Retire the Wails/Svelte UI ☐

Since 1.1.0 both platforms ship native apps (macOS: AppKit + SwiftTerm;
Linux: GTK 4 + VTE), so nothing ships the Wails build any more. Agreed
2026-10-20 to remove it later, not now. What it takes:

- Delete the Wails app: `main.go`, the `wails.go` glue in `internal/*`
  (`//go:build !native`), `frontend/` (Svelte), the Wails parts of `build/`
  and `Taskfile.yml`. Drop the `native` build tag once nothing builds without it.
- Move the manual pages out of `frontend/src/manual` (to `manual/`) and update
  `macos/build.sh`, `macos/Sources/WtfCore/Manual.swift` and its tests,
  `internal/uicore/manual*.go` and `internal/gtkui/manual.go`.
- Move the version out of `main.go` (`macos/build.sh` reads it there) and
  update the release recipe; `build/linux/nfpm`, `build/darwin`,
  `build/windows` go if nothing else uses them.
- Keep shared test data (`testdata/secrets/cases.json` was also read by
  vitest) and port any vitest-only cases to Go or Swift tests.
- README: drop the Wails build steps.
- Check after: `go vet ./...`, `go test ./internal/...`, `linux/check.sh`,
  `swift test` and both app builds.

## Previously deferred (not DevOps-specific)

- Packaging: .app / DMG and .deb done (v0.1.0, v0.2.0); AppImage not.
- Dropping a tab onto another window's tab strip (today: "Move to window" menu).
