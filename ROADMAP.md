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

### 15. Effective permissions panel ☑

`kubectl auth can-i --list` for the current namespace, and the AWS caller
identity with its attached policies. Loaded on request.

Built (2026-11-11): the Kubernetes tab's **Permissions** section (group
Access): `kubectl auth whoami` (1.28+, skipped when the cluster can't) and
`can-i --list` as rows of resource or URL with verbs and resource names, a
"Full access" row for `*` on `*.*`; read when expanded and when the context
or namespace changes, never on the poll (`kube.Permissions`). AWS profiles'
**Check Permissions (iam)…**: identity (user, assumed role and session, root,
other), the boundary, attached and inline policies of the user or role and
of the user's groups, each policy's document on **Show** (default version);
calls IAM refuses are listed, not fatal (`awsprofiles.Permissions`,
`PolicyDocument`). Lists what's attached; doesn't evaluate SCPs, resource or
session policies. Manual: Kubernetes, AWS profiles, Command-line tools.

### 16. SSH config facts ☑

Label hosts with `ForwardAgent`, `StrictHostKeyChecking no` and `ProxyJump`;
show each key's type, bit length and age in Keys. Display only, no warnings.
Built: **agent fwd** and **hostkey no/off/accept-new** pills (resolved like
ssh, `Host *` included; exact setting on hover), jump hosts as "via …";
keys show RSA/ECDSA sizes and "from Mar 2023" (the file's time).

### 17. Context-tagged history ☑

Record each command with the kube context, AWS account/region it ran under;
filter History by them ("what did I run against account X today?").

Built 2026-11-03: `internal/cmdhistory` (backend service `cmdhistory`)
saves each finished command, from both UIs, with the context its shell's
variables pointed at before it ran (infra.Resolve: Kubernetes context and
namespace, AWS profile/account/region, GCP project, Terraform workspace,
git branch; files only). Redacted with Hide Secrets' patterns, appended to
`history.jsonl` in the state folder (0600), kept `history-days` (default
90, decided by the user; 0 saves nothing and removes the file), at most
100,000 entries. Each entry has a random per-launch run id with the
session, so clearing a pane's History forgets only that shell's commands.
History's new **Saved** scope (beside This pane / All panes) searches it
with filters for time (today, 7, 30 days), Kubernetes context and AWS
account/profile/region, each listing the saved values with counts; rows
show the context as pills; Forget This Command, and 🗑 forgets all.
Self-checks write nothing to the real history (read-only under
WTF_NATIVE_DEBUG); the `history` self-check (both platforms) uses a
throwaway store.

### 18. Cloud secrets browser ☑

AWS Secrets Manager, SSM Parameter Store and Vault, like the k8s secrets view:
values masked until revealed, every write confirmed with a reviewed plan.

Built (2026-11-11): a **Cloud Secrets** side tab (⌘⇧X / Ctrl+Alt+X) with a
store picker (saved per window as `secretsSource`), following the focused
shell: its AWS profile and region (a region picked in the tab overrides it
until quit), and `VAULT_ADDR`, `VAULT_NAMESPACE`, `VAULT_CACERT`, newly
reported by the shell integration. `VAULT_TOKEN` is never reported, so the
vault CLI's token helper supplies the token. `internal/cloudsecrets` runs
the aws and vault CLIs. Lists hold names and metadata only (up to 2,000;
Vault: mounts, then folders). A value is read when its dialog opens, masked
until revealed, hidden on focus loss, dropped on close, and blocked
(reveal, copy, edit) while Hide Secrets is on. **Edit…** and **+** plan first
(`PlanChange`: secret, version, account and region or Vault server, which
keys change or the length, never values), then `ApplyChange` checks the
version again (Secrets Manager's AWSCURRENT VersionId, the parameter's
version, Vault KV 2 `-cas`; creating uses `-cas=0`). Values go to the CLIs on
stdin (`--cli-input-json file:///dev/stdin`, `vault kv put … -`). Overwriting
a parameter keeps its type, KMS key, tier and data type. The Vault CLI is in
Command-Line Tools (optional, so not on first start). Not done: deleting
secrets, Secrets Manager binary values (shown, not editable), versions and
rotation, and self-checks with real stores. Manual page: Cloud secrets.

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

Agreed 2026-10-28; moving to Claude Code as a plugin from 2026-11-02 (#32),
which replaces the API-key providers of #23. AI that works from what wtf already knows (command blocks,
context badges, logs, the Kubernetes and AWS tabs, Findings, recordings)
instead of guessing. Build in the order listed: the foundation first, then
#24 to prove the pipeline end to end; #27 waits until #24–#26 are solid.
Native apps only (macOS and Linux).

Rules for every item here:

- On demand only: nothing calls a model in the background, so no idle CPU,
  memory or network cost.
- Never runs a command that changes anything. Suggested commands are typed
  into the focused shell (runInPane) and wait for Enter. #27 runs read-only
  commands from an allowlist, and only each one the user approves (Run).
- Secrets are redacted with the Hide Secrets patterns before anything is sent,
  and the first send shows exactly what will leave the machine.
- No guessed danger labels: facts (resource names, counts, account IDs) are
  listed as facts; the model explains them, it doesn't raise alarms.
- Every feature gets a clickable home (right-click / side pane / palette) and a
  manual page.

### 23. AI foundation ☑ (replaced by #32: providers removed 2026-11-03)

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

### 26. Change reviewer ☑

For `terraform plan`, `kubectl diff` and `helm diff` output (a command block
or a plan file). Parse it first, deterministically: resources created,
changed, replaced and destroyed, IAM/security-group changes, listed as facts.
Then the model writes a plain-English summary of what will happen and why
(e.g. "replaces the RDS instance because `engine_version` changed").

Built: `internal/review` (service `review`): parsers for Terraform/OpenTofu/
Terragrunt plan text (top-level changed attributes, `# forces replacement`,
`(because …)`, moves, Terraform's own Plan line), `terraform show -json`
plans (actions, replace_paths, action_reason, changed keys; sensitive values
left out of the excerpt) and binary plans via read-only `terraform show
-json`/`tofu`, `kubectl diff` (object names from the LIVE/MERGED paths,
create/delete from the hunks) and `helm diff`. Changes are tagged IAM,
Security group, RBAC, Network policy or Secret by type/kind. `Reviewable`
(ported to WtfCore, shared cases in testdata/review/reviewable.json) gates
the block menu's **Review Changes…**; also **Review Last Command's
Changes…** and Files → **Review Terraform Plan…**. Facts work without AI;
with one, `ai-default` summarizes. Linux self-check `explain` covers a plan
block and a plan file.

### 27. Incident investigator ☑

An AI side-pane tab (or its own tab) where you ask "why is `checkout-api`
crashlooping in staging?". The agent investigates with an allowlist of
read-only commands (`kubectl get/describe/logs/events`, `aws … describe-*`,
`helm history`, `dig`, `curl -I`, …), each shown as a normal command block,
using the active kube context and AWS profile. It ends with a finding that
cites the evidence. Anything that changes state (rollback, scale, restart) is
typed into the focused shell for you to confirm. Step and cost limits per run;
Stop at any time.

Built: tool calling in `internal/ai` (`NewToolSession`/`Send`, anthropic
tool_use and OpenAI tool_calls, streamed; command providers refused with
`ErrNoTools`; everything redacted; first-send rule kept). `internal/investigate`:
the argv allowlist (`Check`: kubectl, helm, aws, gcloud, docker, dig/host/
nslookup, curl -I, ping; refuses context/credential switches, follow/watch,
secret-printing; adds --tail, --no-cli-pager, --max-time, -c; quotes every
argument) and the loop (`Start`/`Result`/`Stop`, events investigate:text/
run/refused/done; 12 commands, 250k tokens, then a forced finding). Per the
user's choice, commands are typed into the focused shell (full credentials,
normal command blocks); the UI waits for each block (Ctrl-C after 60 s) and
returns its output. After the first hands-on test (it ran `aws route53
list-hosted-zones` unasked for a how-to question), every command waits for
the user's Run or Skip (declined → the model is told and mustn't re-ask),
general questions are answered without commands, and the shell's last three
commands go with the question. Side-pane tab Investigate (hidden without a provider),
palette and right-click; the finding cites steps, Type Fix at Prompt.
Linux self-check `investigate` (fake Claude API in-process, stand-in kubectl).


### 32. AI through Claude Code (rearchitecture) ◐

Agreed 2026-11-02, after #23–#27 were built on API-key providers. wtf's AI
moves to the user's own Claude Code, run interactively in a wtf pane with a
bundled wtf plugin, like the VS Code extension. No API keys; wtf never calls
Claude Code in the background (`claude -p`): Anthropic's terms let a user
run the unmodified `claude` with their own login, not an app make requests
on that login. wtf's name and logo don't use Claude's or Anthropic's.

Design:

- **Ask Claude** (⌘I / Ctrl+Shift+I, palette, right-click; replaces #25's
  bar) opens a split running `claude --plugin-dir <bundled plugin>`
  attached to the shell it came from (the target). Nothing is installed
  into `~/.claude`.
- Plugin (`claude-plugin/`): an MCP server, `wtf-mcp`, a thin helper that
  relays to the running app over a private, token-protected local socket.
  Tools: `recent_commands`, `pane_context`, `review_changes` (#26's
  parsers), `run_readonly` (#27's allowlist; typed into the target shell
  as a normal command block), `type_at_prompt` (no Enter). Hooks report
  busy / idle / needs-you to the pane's badge and turn notifications into
  wtf's. Slash commands `/wtf:explain`, `/wtf:cmd`, `/wtf:review`,
  `/wtf:investigate`.
- Approval: a PreToolUse hook answers "ask" for `run_readonly`,
  `type_at_prompt` and Claude Code's own Bash in wtf-launched sessions, so
  every command shows Claude Code's Allow/Deny prompt each time, even in
  auto mode and after "don't ask again". The user may opt into Claude
  Code's auto mode with an explicit wtf setting; it's never on by default.
- Hide Secrets applies to everything a tool returns.

Phases:

0. ☑ Spike (2026-11-02, macOS, real Claude Code 2.1.316): the plugin loads
   with `--plugin-dir`; its MCP server gets wtf's per-pane variables; the
   forced "ask" works for MCP tools and Bash, in auto mode and after
   "don't ask again"; `/wtf:…` sent as one write or a bracketed paste is
   accepted; UserPromptSubmit / Notification / Stop give busy / needs-you /
   idle. Linux with real Claude Code not yet tried.
1. ☑ Bridge (2026-11-02): `internal/bridge` listens on a Unix socket in a
   private (0700) temp folder; every shell gets `WTF_PANE` (its session),
   `WTF_BRIDGE`, `WTF_BRIDGE_TOKEN` (an HMAC of the session, so one shell's
   token doesn't work for another) and `WTF_MCP`. `cmd/wtf-mcp` (Go MCP
   SDK v1.8.0, depends only on `internal/bridge/wire`) lists the app's
   tools and relays calls: `pane_context`, `recent_commands`,
   `review_changes`, `run_readonly` (#27's Check), `type_at_prompt`, all
   redacted. The UIs answer `bridge:request` (pane, blocks, type, run) with
   `bridge.Reply`; `SetTarget` points a Claude pane at another shell
   (phase 3). Bundled in `Contents/MacOS` and `/usr/lib/wtf/wtf-mcp`.
   Tested: Go tests drive the built helper with an MCP client; the Linux
   self-check `bridge` runs it in one shell, as Claude Code would, against
   another; macOS has the same check (`WTF_NATIVE_DEBUG=bridge`, run with
   `open -n -W --env … --stdout …`), passing. It found a pane just split
   while the window wasn't in front kept the launcher's name (wtf-exec) and
   looked busy; the session's leader still named wtf-exec now counts as
   the shell.
2. ☑ Plugin (2026-11-03): `claude-plugin/` (plugin `wtf`, server `wtf`, so
   tools are `mcp__plugin_wtf_wtf__*`). `.mcp.json` starts `${WTF_MCP}` with
   the pane's variables. Every hook runs `"${WTF_MCP}" hook` (quoted: the
   macOS path has a space): PreToolUse for Bash, Monitor, `run_readonly`
   and `type_at_prompt` answers "ask" without asking the app and fails
   closed (`|| exit 2`: no helper or unreadable input refuses the call);
   SessionStart / UserPromptSubmit / Notification / Stop / SessionEnd send
   the bridge's `hook` op, which emits `bridge:status` (idle, busy,
   waiting with the notification's text, ended) for phase 3's badge.
   Slash commands `/wtf:explain`, `/wtf:cmd`, `/wtf:review`,
   `/wtf:investigate` carry #24–#27's instructions. Bundled in
   `Contents/Resources/claude-plugin`, `/usr/share/wtf/claude-plugin` and
   next to a Linux dev build; `bridge.PluginDir` finds it. Go tests check
   the files (matcher, events, tools the commands name), run the hooks
   through sh as Claude Code does, and run `claude plugin validate` when
   it's installed; the Linux self-check `bridge` checks the app finds it.
3. ☑ Claude pane (2026-11-03): **Ask Claude** (⌘I / Ctrl+Shift+I, palette,
   Edit / window menu, right-click; Command from Plain English loses the
   shortcut) splits the shell to the right and types
   `claude --plugin-dir '<bundled plugin>'` (`bridge.ClaudeCommand`); the
   new pane's session targets the shell it came from (`SetTarget`), kept
   by session so it survives moving tabs between windows. Asking again from
   that shell goes back to its Claude pane (and starts claude again if it
   was quit); a Claude pane gets none of its own; closing it forgets the
   target. Hooks → `bridge:status`: the bridge keeps each pane's state and
   sends only changes; PostToolUse means busy again after an approval; a
   Notification while idle (Claude Code's "waiting for input" reminder)
   changes nothing. The tab shows working (accent sparkle / ✦), needs you
   (orange bubble / !) or idle (grey); wtf notifies when it needs you or
   finishes while you're elsewhere. `claude-auto-mode` (config.toml and
   Settings → General, off by default) puts `WTF_CLAUDE_AUTO=1` before
   claude, and `wtf-mcp hook` then leaves commands to Claude Code's own
   permission mode. Self-checks `claude` on both (a stand-in claude given
   by `WTF_CLAUDE_STANDIN`, honoured only under WTF_NATIVE_DEBUG=claude, so
   a check never starts the real one) cover launch, target, hooks → badge,
   asking again and closing. Manual page `ask-claude` (not `claude.md`:
   on a case-insensitive disk Claude Code reads that as CLAUDE.md). Not
   yet tried with real Claude Code (phase 6).
4. ☑ Rewire #24–#27 (2026-11-03): Explain Failure (block menu, Explain
   Last Failed Command) types `/wtf:explain block N` with Enter; Command
   from Plain English and Investigate type `/wtf:cmd ` / `/wtf:investigate `
   for you to finish; a review's **Summarize with Claude** (the parsed
   facts stay, without AI) types `/wtf:review block N` or `… plan file
   <path>`. Each opens the shell's Claude pane when needed, and types only
   when Claude Code is idle (after SessionStart, so never into its trust
   question; queued while it works). Their dialogs went too: the Explain
   sheet, the command bar, the Investigate side tab (an old session's
   Investigate tab opens Files) and Review's AI summary, with the
   explain/investigate self-checks; the `claude` self-check covers the
   new paths on both platforms. Seen there: a Claude pane split from a
   crowded tab can be very narrow.
5. ☑ Remove the API-key providers (2026-11-03): `internal/ai` (providers,
   keys, presets, streaming, the first-send preview), `internal/suggest`,
   investigate's agent loop (its allowlist stays, for `run_readonly`),
   explain's request builder (its shell facts stay, for `pane_context`),
   review's AI request (its parsers stay), Settings → AI (Settings is
   General only), the per-provider palette entries, AI Providers in the
   menus, and the `ai` self-check; go-keyring left go.mod. Old
   `ai-default`, `ai-fast` and `[ai.*]` load with one note instead of
   "unknown setting" errors; keys saved under "wtf AI" stay in the
   keychain/keyring for the user to remove (the manual says how).
6. ☐ Self-checks and hands-on: the `claude` and `bridge` self-checks on
   both platforms (done) drive wtf-mcp and a stand-in claude the way
   Claude Code does; still to do: the user's hands-on test with real
   Claude Code, and Linux on a real desktop.
7. ☑ Manual and roadmap (2026-11-03): the Ask Claude page, the AI page
   rewritten around it (with upgrading from providers), privacy and
   settings updated; #28–#31 below redone as plugin tools and slash
   commands.

Since #32, each of these is a tool on the wtf plugin's MCP server (Go, in
`internal/bridge`, redacted like the rest) and a `/wtf:` slash command,
with a clickable home in wtf that types the command in the shell's Claude
pane. wtf does the deterministic part (parsing, grouping, reading files it
already knows); Claude writes the prose.

### 28. Log summarizer ☑

`log_patterns`: a command block's output (default: the latest of the last
20 with at least 10 lines, also one still running, e.g. `kubectl logs -f`)
grouped into patterns by `internal/logsum`: timestamps (wtf's own log
scanner finds them), UUIDs, IPs, pod suffixes, hex ids, durations, quoted
values and numbers become placeholders; each pattern has its count, log
level (as wtf colours it), first and last time seen, and one example line.
Errors and warnings first, then by count; a timeline of error and warning
lines whose step (1 min … 1 day) keeps it to 30 rows. `/wtf:logs [block]`
has Claude summarize it. Homes: a block's menu → **Summarize Logs…
(Claude)** (10 lines or more) and the palette. Built 2026-11-03.

### 29. Session to runbook or postmortem ☑

`session_commands`: a range of command blocks (`from_block`/`to_block`
among the last 200, or the last `count`, default 20, at most 50), each with
its start time (the UIs now send `startedAt`), duration, exit code and the
first and last 12 lines of output. `/wtf:runbook` keeps the steps that did
the work, makes `{{placeholders}}` with shell-following defaults, and calls
`open_snippet_editor`, which opens wtf's own snippet editor filled in (a
new `draft` source: named as given, not "(copy)"); nothing is saved until
the user clicks Confirm & Save. `/wtf:postmortem` drafts a Markdown
timeline, evidence, root cause and follow-ups, shown in the pane. Homes: a
block's menu → **Runbook from Here… (Claude)**, and the palette (Runbook
from Recent Commands…, Postmortem Draft…). Built 2026-11-03; the bridge
self-checks open the editor through wtf-mcp on both platforms.

### 30. Findings triage ☑

`findings`: the SARIF results the Findings tab shows for the target
shell's repository (`internal/findings`, the same files and wtf's own scan
results), most severe first (critical, high/error, medium/warning,
low/note), each numbered with tool, rule, file:line:column, message, help
link and the two lines of code either side; filters `rule`, `path`,
`severity` (that or worse), `limit` (30, at most 100); the reports' tools,
versions and dates first. `/wtf:triage [rule, file or number]` has Claude
explain it for this code, judge reachability from the code it reads, and
propose a unified diff, applied only through Claude Code's edit tools,
which ask in Claude panes (Edit, MultiEdit, Write and NotebookEdit are
forced to "ask" like Bash, decided 2026-11-03). Home: a finding's menu →
**Triage with Claude…** (`/wtf:triage <rule> at <path:line>`). Built
2026-11-03.

### 31. Ask your history ☑

`search_history`: #17's saved history (all shells, across windows and
restarts), filtered by words in the command or folder, `days`, Kubernetes
context/namespace, AWS account/profile/region and `failed`; newest first
(30, at most 100), each with when, exit code, duration, folder and the
context it ran under; the answer starts with the contexts saved (with
counts) so Claude can map "staging" or "the prod account" to real values.
`/wtf:history <question>` tries a few wordings, answers with when and where
each command ran, and types one at the prompt on request (stating both
contexts as facts when the shell now points elsewhere). Homes: the palette
(Ask Your History…) and a button in History's Saved view. Built
2026-11-03.

## Tier 7 — agents

### 33. Agents tab ☑

Like herdr's agent sidebar: every coding agent running in wtf's shells, with
whether it's working or waiting for you, so you don't poll tabs. State
comes from the agent's own hooks, never from reading the screen.

Phases:

0. ☐ Hands-on with real Claude Code: hooks from `--settings` add to the
   user's own (`~/.claude`) hooks rather than replacing them, and the hook
   input carries `prompt` (UserPromptSubmit) and `message` (Notification).
   (`claude --help`, 2.1.319: "additional settings".)
1. ☑ Status for any `claude` (2026-11-06): `claude-plugin/status-settings.json`
   holds only the status hooks (no PreToolUse: your own claude keeps its
   permission mode; no tools or commands). Every shell gets
   `WTF_AGENT_SETTINGS` while `agent-status` is on (default; config.toml and
   Settings → General); the bash/zsh integration defines `claude` as
   `command claude --settings "$WTF_AGENT_SETTINGS" "$@"`, left alone with
   its own `--settings`/`--plugin-dir` or a user's alias or function. The
   bridge keeps an agent per session (`Status`: state, redacted message and
   first line of the prompt, since, unseen = done until its pane is seen),
   `Agents()`, `Seen`, `Forget`; the terminal service forgets one when its
   shell exits or is back at its prompt (a killed claude). Go tests:
   registry, settings file, env, the shell wrapper in bash and zsh.
2. ☑ Both UIs: badges for every Claude Code (needs you ! / bubble, done ✓,
   working ✦, idle), notifications for any of them while you look
   elsewhere, Seen when its pane is in front; macOS Dock badge counts the
   ones that need you. Ask Claude's own state map only queues its `/wtf:`
   messages now.
3. ☑ Agents side tab (first in the rail; ⌘⇧M / Ctrl+Alt+M): rows most urgent
   first with tab, folder, prompt or what it asks, time in state; click goes
   there; a 30 s redraw only while shown.
4. ☑ Go to Agent… (⌥⌘A / Ctrl+Alt+Shift+M, the palette in an agents mode)
   and Next Agent Needing You (⌥⌘N / Ctrl+Alt+Shift+N).
5. ☐ Manual page `agents` (done), self-checks (done 2026-11-06: `claude` on
   both types a stand-in in a new tab; passing on macOS and Linux), hands-on.
   The macOS check found a session "back at its prompt" was judged by the
   process name, so a bash script run from bash (the user's shell) counted
   as the shell: now the shell's own pid must be in front (`Info.AtShell`).

Later: Codex (its `notify` hook), and agents that report through OSC
9/777/7501.

### 34. Background agents ☑

Close a tab running Claude Code and it can keep running; the Agents tab
manages it. Decided with the user (2026-11-06): it lives while wtf runs (not
after quitting); closing asks Keep Running in Background / End (Don't ask
again → `agent-close` ask/background/end); manage = open, end, start, peek,
reply.

Built 2026-11-06:

- A background window, never shown or saved: parking moves the agent's pane
  there the way tabs move between windows (new `DetachPane` in both models),
  so output, hooks, badges and notifications carry on. Spike: a never-shown
  window takes output as fast as a hidden tab (Linux, 200k lines in 0.4 s).
- Closing a tab, pane or window with agents asks first (`confirmIfBusy`);
  the last window you can see, with agents in it or in the background, quits
  instead (wtf asks, listing them). The last tab parked keeps a fresh tab.
- Agents tab: a Background section; per agent Open in Tab, Peek and Reply…
  (screen with Hide Secrets, updated every second; text + Return, Return,
  Esc, only on your click), End…; Start Claude Code… (folder, first prompt,
  in a tab or in the background). A background agent that quits closes its
  shell.
- Fixed on the way: macOS adopting a tab ran sync first, which started a new
  shell for the moved pane and took over its session (moving tabs between
  windows had this too); Linux moving a window's last tab could hang its
  shells up (CloseWindow before Adopt).
- Self-checks `background` on both (stand-in claude, guarded so no other
  claude can run): park, not shown, Agents tab, peek, reply, open, end,
  start in background. Passing on macOS and Linux.

Still to do: hands-on with real Claude Code.

### 35. Sub-agents in the Agents tab ☑

See the sub-agents an agent spins up and what they do, folded away by
default (asked 2026-11-09): a small "▸ 3 sub-agents · 1 working" toggle under
the agent opens them.

Built 2026-11-09:

- From hooks only: SubagentStart/SubagentStop added to the plugin's hooks and
  status-settings.json (still no PreToolUse); the Agent call's PostToolUse
  names the sub-agent (`agentId` in its response) with its description; tool
  events from inside a sub-agent carry `agent_id` and become its latest step
  ("Read deploy_test.go") instead of changing the parent's state. Payloads
  checked against Claude Code 2.1.321 (a background Explore sub-agent).
- bridge.Status gains `subagents` (type, task, step, result, running,
  started/ended; at most 20, finished ones cleared by the next prompt).
- Both UIs: the toggle and the sub-agent rows (shown state remembered while
  wtf runs), right-click Show/Hide Sub-agents. Self-check `claude` covers
  it, passing on macOS and Linux.

Still to do: hands-on with real Claude Code.

### 36. Spotting agents at a glance ☑

Asked 2026-11-09: hear an agent that needs you, and find one in the list or
the tab strip without reading every row.

Built 2026-11-09:

- `agent-sound` (on): a short sound with an agent's notification, one for
  needs you and one for finished (macOS Glass / Pop, played in the app;
  Linux the freedesktop message-new-instant / complete through the
  notification daemon when it has the sound capability, else
  canberra-gtk-play). One sound per 1.5 s.
- The Agents tab lists agents in the order they started (bridge.Status
  `started`); rows don't move. Go to Agent… stays most urgent first.
- Names: double-click or Rename… (bridge.Rename); shown in the Agents tab,
  the picker and notifications; kept with the shell across claude restarts,
  dropped when it closes. Naming an agent also names its tab; clearing the
  name puts the tab back to automatic if it still carries that name.
- `agent-group` (on): a tab running Claude Code joins its window's "agents"
  tab group (made when needed) and leaves it when Claude Code ends, unless
  another agent runs in it. Background agents are left out.
- Self-check `claude` covers names and the group on macOS and Linux; a
  stand-in notify-send / canberra test covers the Linux sound.

Still to do: hands-on with real Claude Code; listening to the sounds.

## Also built

- Resource usage (2026-10-13): wtf's own CPU and memory in the address bar
  (`show-usage`) and a per-process breakdown (app, WebKit windows, and the
  programs run in it, counted apart). internal/usage samples via libproc and
  /proc only while shown.

## Maintenance

### 21. Retire the Wails/Svelte UI ☑

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

Done 2026-11-11, after 1.7.1: `main.go`, `internal/windows`, the `wails.go`
glue, `frontend/` and `build/` are gone, and so is the `native` build tag
(the services' startup hooks are plain `startup.go`); go.mod has no Wails.
The manual is in `manual/` (Go package `manual`, embedded by the Linux app,
copied into the macOS bundle). The version is the `VERSION` file, read by
`macos/build.sh`, `linux/build.sh` and `linux/package.sh` (a release bumps
only it and the README). The icons and Info.plist moved to
`macos/Resources/`, the Linux icon to `linux/package/`.
`testdata/secrets/cases.json` is now written by
`go run testdata/secrets/generate.go` (identical output). 35 vitest cases
with no Go or Swift test were ported (22 Go, 16 Swift); logic that only
lived in Svelte (xterm decorations, blocks menus, broadcast targets) went
with it. The manual test now matches the Go topic list against
`Manual.swift`. About no longer reports a Wails version. Taskfile.yml runs
the native scripts only. Left: `linux/Dockerfile` still installs WebKitGTK
(harmless; drop it on the next image rebuild).

## Previously deferred (not DevOps-specific)

- Packaging: .app / DMG and .deb done (v0.1.0, v0.2.0); AppImage not.
- Dropping a tab onto another window's tab strip (today: "Move to window" menu).
