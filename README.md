# wtf

A browser-like terminal for macOS and Linux. Go + Wails v3 + Svelte 5 + xterm.js.

What's next: see [ROADMAP.md](ROADMAP.md).

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
- `internal/sshhosts`: the SSH tab: hosts from `~/.ssh/config` and `known_hosts`, keys
  and ssh-agent, server host keys, and background tunnels.
- `internal/infra`: Kubernetes/AWS/gcloud/Terraform context for the context badges.
- `internal/kube`: the Kubernetes tab: deployments, pods and events, read with
  kubectl using the focused shell's variables.
- `internal/awsprofiles`: the AWS tab: profiles in `~/.aws/credentials` and
  `~/.aws/config`, checks and key rotation through the aws CLI.
- `internal/files`: directory listings (with git ignore rules) for the file tree.
- `internal/netconn`: connections and listening ports, attributed to terminal sessions.
- `internal/procs`: the user's processes, attributed to terminal sessions.
- `internal/windows`: opening/restoring windows, moving tabs between them,
  confirm-before-close/quit.
- `frontend/src/lib`: tab/group/pane state (`tabs.svelte.ts`), split geometry
  (`layout.ts`), the command registry (`commands.ts`), session autosave, themes.
- `frontend/src/components`: tab strip, address bar, panes, palette, search, menus.

## Manual

wtf has a user manual built in: **Help → wtf Manual** (⌘?, F1 on Linux), the
command palette, or the **?** at the top right of the side pane, which opens
the page for the tab on screen. It covers every feature, with search, and
its shortcuts come from the app's own command list. The pages are Markdown
in `frontend/src/manual/` (see `lib/manual.ts` for the subset they use);
tests check that every command and page they mention exists.

## Shortcuts

Every command is also in the command palette.

| Action | macOS | Linux |
|---|---|---|
| Command palette | ⌘⇧P | Ctrl+Shift+P |
| Manual (at the page for the side-pane tab on screen) | ⌘? | F1 |
| Toggle sidebar | ⌘B | Ctrl+Shift+B |
| Sidebar: files / SSH / AWS / Kubernetes / snippets / network / processes / history | ⌘⇧E / ⌘⇧S / ⌘⇧U / ⌘⇧K / ⌘⇧Y / ⌘⇧O / ⌘⇧A / ⌘⇧H | Ctrl+Shift+Alt+E / S / U / K / Y / O / A / H |
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
| Broadcast input (group, or split panes) | ⌥⌘B | Ctrl+Shift+Alt+B |
| Run Snippet… | ⌘⇧R | Ctrl+Shift+Alt+R |
| Next / previous tab | ⌘⇧] / ⌘⇧[, Ctrl+Tab | Ctrl+PgDn / Ctrl+PgUp, Ctrl+Tab |
| Go to tab 1–8 / last | ⌘1–⌘8 / ⌘9 | Alt+1–8 / Alt+9 |
| Move tab | ⌘⇧← / ⌘⇧→ | Ctrl+Shift+PgUp / PgDn |
| Find / next / previous | ⌘F / ⌘G / ⌘⇧G | Ctrl+Shift+F / Enter / Shift+Enter |
| Jump to previous / next prompt | ⌘↑ / ⌘↓ | Ctrl+Shift+↑ / ↓ |
| Copy last command output | ⌥⌘C | Ctrl+Shift+Alt+C |
| Clear | ⌘K | Ctrl+Shift+K |
| Settings file | ⌘, | Ctrl+, |
| Text size | ⌘= / ⌘- / ⌘0 | Ctrl+Shift+= / - / 0 |

Double-click a tab (or right-click → Rename Tab…) to name it; an empty name
goes back to the automatic one (folder or program). Right-click a tab or a group chip for more. Double-click a split divider to even it out.

## Sidebar

⌘B (or the button next to the traffic lights) toggles a side pane with a file
tree of the focused pane's folder — pin it to keep a folder. Click a file to
insert its path at the prompt, double-click a folder to `cd` there (when the
shell is idle), and use ← (Back, or ⌫ in the tree, or the mouse's back
button) and ↑ (Up, or ⌥↑) to return to the folder shown before — each pane
keeps its own history — or go to the parent. Like double-click, they `cd` the
shell, or move the pinned folder when the tree is pinned. Drag a file into a
terminal to paste its path, and right-click for more (open in `$EDITOR` in a split, reveal in Finder, copy path…). Folders
load on demand, git-ignored files are dimmed, and the tree refreshes after
each command.

The **SSH Hosts** tab lists the `Host` entries of `~/.ssh/config` and the
files it `Include`s (grouped by file, with each host's user, address, port
and jump host worked out as ssh would), then **Known hosts**: servers in
`~/.ssh/known_hosts` your config doesn't name, most recent first. Git hosts
such as github.com are left out. Double-click a host (or press ▶ on it) to
connect in a new tab, or right-click to open it in a split or copy its `ssh`
command. ⌘-click or shift-click to select several, then **Open as Tab Group**
(or ↩) to connect to each in its own tab, grouped and named after their config
file. The list is read when the tab opens; ↻ reads it again.

**+** adds a host (name, address, user, port, key file, jump host, and any
other settings); right-click a host to edit or delete it, or a known host to
**Save as Host**. New hosts go into `~/.ssh/config`, before any `Host *` block
(ssh takes the first value it finds, so a host after `Host *` would get its
defaults instead). Edits change only the lines involved, keeping comments and
indentation. Before writing, wtf checks that the file hasn't changed since it
was listed and that `ssh -G` accepts the result, so a typo can't break ssh;
writes are atomic, follow a symlinked config, and keep a copy of the old file
in wtf's cache folder (`ssh-backups`, last 30). Hosts in files outside
`~/.ssh`, such as OrbStack's, belong to the program that writes them and are
read-only.

Right-click a host for the rest: **Upload Files…** and **Download…** (scp, in
a new tab; you can also drop a file from the file tree onto a host), **Open
sftp Session**, **New Tunnel…**, **Install Public Key…** (ssh-copy-id) and
**Host Keys…**. Host Keys shows the keys `known_hosts` holds for the server
(looked up the way ssh does, hashed entries and `HostKeyAlias` included),
asks the server which keys it presents now (ssh-keyscan), and says which
match. From there you can forget the stored keys (`ssh-keygen -R`, the fix
for "REMOTE HOST IDENTIFICATION HAS CHANGED") or store the server's keys; wtf
scans again before storing and keeps only the keys you were shown. A server
behind a jump host can't be scanned directly.

**Keys** lists the key pairs in `~/.ssh` (type, size, comment, fingerprint,
whether a passphrase protects it, which hosts use it) and keys only ssh-agent
holds, such as a password manager's. Keys other users can read are marked
with their permissions, since ssh refuses them; **Make Private** fixes that.
**+** generates a key, and the menu loads a key into the agent (on macOS also
with its passphrase in the Keychain), unloads it, changes its passphrase,
copies the public key (double-click), installs it on a host, or deletes it —
moving the pair to wtf's cache folder (`ssh-backups/keys`), not erasing it.
Anything that can ask for a passphrase or password (ssh-keygen, ssh-add,
ssh-copy-id, scp) runs as a real command in a new tab, so it's in your shell
history and the secret never passes through wtf.

**Tunnels** are saved port forwards: local (`-L`, reach a database behind a
bastion on localhost), remote (`-R`) and SOCKS (`-D`). Each runs as its own
background `ssh -N` while it's on, with its own connection, and reconnects if
a working connection drops; one that never connects shows ssh's error. They
run without prompts, so the host must accept a key from ssh-agent or one
without a passphrase. They stop when wtf quits and can start when it opens.
Saved in `ssh-tunnels.json` in wtf's config folder.

**Broadcast input** sends what you type in one pane to every pane of its tab
group (right-click a tab, or the group's chip) or of its tab's splits — run
the same commands on several hosts at once. Select hosts and choose **Open …
in a Tab Group with Broadcast Input** to start that way. While it's on, a bar
above the terminals says where your typing goes, the panes are outlined and
the tabs marked; ⌥⌘B (or Stop) ends it. Replies the terminal sends for a
program (cursor position, mouse reports) aren't broadcast, and it never
survives a restart.

The **AWS Profiles** tab (⌘⇧U) lists the AWS CLI's profiles from
`~/.aws/credentials` and `~/.aws/config` (or `$AWS_SHARED_CREDENTIALS_FILE` /
`$AWS_CONFIG_FILE`): access-key, assume-role, SSO and credential-process
profiles, with their key ID, region and account, and the one the focused
shell uses marked "in use". Double-click (or ▶) points the focused shell at a
profile by typing `export AWS_PROFILE=…` at its prompt — or opens a new tab
that does, if a program is running. The menu also opens a tab with the
profile, runs `aws sso login`, and asks AWS about it: **Check Identity**
(`sts get-caller-identity`) and, for access keys, **Check Access Keys** (each
key of the IAM user, its status, age and last use).

Changes always show what they'll do first and wait for a yes: **+** and
**Edit…** end in a review of every line that will change, and **Delete…**
and **Copy to Default…** (which copies a profile into `[default]`, used when
no profile is chosen) list their changes in a confirmation. Edits change only
the lines involved, keeping comments and each key's spelling
(`AWS_ACCESS_KEY_ID=` or `aws_access_key_id = `); renaming a profile updates
the profiles that use it as `source_profile`. Secrets are write-only: wtf
never shows a saved secret key or session token, and an empty secret field
keeps the current one. Before each write a copy of the file goes to wtf's
cache folder (`aws-backups`, last 20, readable only by you — they contain
keys).

**Rotate Access Key…** replaces an IAM user's long-term key: it checks the
key works and the user has room for a second key (AWS allows two), shows the
steps, and on confirmation creates a new key, writes it to the profile,
waits until AWS accepts it, then deactivates — and optionally deletes — the
old one. If AWS never accepts the new key, the old one is put back and the
new one deleted. The new secret goes straight from AWS into the file.

The **Kubernetes** tab (⌘⇧K) shows the focused shell's context and namespace:
deployments, StatefulSets, DaemonSets, Jobs, CronJobs and pods; Services and
Ingresses; ConfigMaps and Secrets; PersistentVolumeClaims; autoscalers;
nodes; and recent events — following the shell as you
switch — read with kubectl using that shell's own `KUBECONFIG`, AWS profile
and `PATH`, so auth plugins such as aws-iam-authenticator work as they do
there. Nothing is read from the cluster until you ask: opening the tab only
resolves the context from the kubeconfig, each section (Deployments, Pods,
Events) is fetched when you expand it, and only expanded sections refresh —
at most every 10 seconds (or the side pane's interval, if longer). The
namespace picker, which lists namespaces once you use it, views another
namespace without changing the shell. Pods show kubectl's STATUS, readiness,
restarts (with why the last run ended) and age; click a deployment to fetch
only its pods (by its label selector). Kinds other than deployments, pods
and events are listed the way `kubectl get` prints them (the cluster works
out the columns), which keeps answers small — Secrets are listed by name, type and key
count. A secret's values are read only when you ask: **View Decoded
Values…** (or double-click) opens that one secret with each key masked
until you reveal it, a copy button per key (base64 for binary data), and
everything masked again when wtf loses focus; the values live only in that
dialog. **Decode in Terminal** prints them in a new tab instead, with
kubectl's own `base64decode` template.

Everything else is a real kubectl command run in a new tab — with the
shell's `KUBECONFIG` and AWS profile and an explicit `--context` and `-n`,
so it does exactly what the view showed: logs (double-click; per container,
or from before the last restart), a shell (`exec`), describe, view YAML,
rollout status and history, port forwards (a Service's first port is filled
in), opening an Ingress's hosts in the browser. Commands that change the cluster — delete a pod,
restart a deployment, StatefulSet or DaemonSet, scale a deployment or
StatefulSet, run a CronJob now — show the exact command, context, cluster and
namespace and run only when you confirm.

The Kubernetes tab's **Contexts** view lists the contexts of the focused
shell's kubeconfig files (its `$KUBECONFIG`, else `~/.kube/config`) under
"Used by this shell", then those of every other kubeconfig in `~/.kube`
(any file there that is one; kubectl's cache folders are skipped), each file
on its own: cluster (an EKS cluster's name and region), namespace, how it logs
in (aws-iam-authenticator, `aws eks get-token`, certificate, token…) and
with which AWS profile — never tokens or certificates. A login helper that
isn't on the shell's PATH is said so. **Test Connection** asks the cluster
for its version and who you are, only when you click it.

Double-click (or ▶) switches the focused shell — only that shell — to a
context: wtf writes a small file holding just that context's name, cluster,
user and namespace (no credentials) and types `export KUBECONFIG=<that
file>:<your files>` at the prompt, so `kubectl` there uses it while other
tabs keep theirs, and `kubectl config` commands run there change that small
file, not your shared kubeconfig. These files go when wtf restarts. Picking a context from another file in
`~/.kube` points the shell at that file only, so same-named entries in
different files can't mix; editing or testing it works on that file. A
context's **default** pill marks the kubeconfig's `current-context`, which
new shells start with.

Rename, set the default namespace, and delete (with its cluster and user
when no other context uses them) run `kubectl config` after showing the
exact commands; **+** merges a kubeconfig file (adding only names not
already defined) or adds an EKS cluster with `aws eks update-kubeconfig`
for one of your AWS profiles (its cluster list fetched when you ask). For a
context that logs in with aws-iam-authenticator, **Log In with aws eks
get-token…** rewrites its user to need only the aws CLI, keeping its AWS
profile. Each change is confirmed first, and a copy of the kubeconfig is
kept in wtf's cache folder (`kube-backups`, last 20).

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

## Snippets and runbooks

**Run Snippet…** (⌘⇧R; also in the command palette) lists commands with
blanks to fill in, e.g. `kubectl -n {{ns=$kube_namespace}} rollout restart
deploy/{{name}}`, from `~/.config/wtf/snippets.toml` (**Open Snippets File**
creates it with examples), every `*.toml` in `~/.config/wtf/snippets/`, and
`.wtf/snippets.toml` in the shell's folder or any parent up to home — so a
repo can carry its own runbooks for everyone working in it. A form asks for
the values (defaults may follow the shell: `$kube_namespace`,
`$kube_context`, `$aws_profile`, `$aws_region`, `$cwd`), shows the exact
command, and inserts it at the prompt (Enter), runs it (⌘↩) or runs it in a
new tab; values are shell-quoted unless a param is `raw`. A runbook's steps
run one at a time.

The **Snippets** side-pane tab (⌘⇧Y) lists them by file (this project, yours,
team files) and manages them with a form: **+** / Edit / Duplicate / Delete,
a command or a runbook of steps (add, remove, reorder), and per-placeholder
description, default, choices and raw. **Review…** shows the exact TOML block
and file before anything is written; an edit replaces only that snippet's
block, keeping the file's other snippets and comments; a project file is
created at the repository root when there's none. **Save as Snippet…** on a
command (or in History) opens the same editor. The manual's Snippets page
documents the format.

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
files. That makes each command a block: a ✓/✗ mark in the margin (hover for
exit code, duration and finish time), the duration of slow commands (1s or
more) at the end of the prompt line when it has room, ⌘↑/⌘↓ between prompts,
the command status in the address bar, and a notification when a long
command finishes out of sight. Click a command's mark, or right-click in its
output, to copy the command, its output or both, select the output, or run it
again; the block is highlighted while the menu is open. Right-click also has
Copy, Paste, Select All, Find and Clear. Turn it off with
`shell-integration = false`.

## Context badges

With shell integration, the address bar shows what the focused shell is pointed
at: the Kubernetes cluster and namespace of the current context, the AWS
account, profile and region, the gcloud project, and the Terraform workspace.
At each prompt the shell reports the few environment variables involved
(`KUBECONFIG`, `AWS_PROFILE`, `CLOUDSDK_*`, `TF_WORKSPACE`, …, and whether AWS
keys are exported, never the keys); wtf reads the rest from the tools' own files
(`~/.kube/config`, `~/.aws/config`, gcloud's configurations,
`.terraform/environment`), without running any of the tools and without polling.

The AWS profile is the one the shell chose (`AWS_PROFILE`, aws-vault), else
`default` when that profile exists. Its account is the one the profile's config
names: the account of its `role_arn`, else `sso_account_id`, else
`aws_account_id`. When the config doesn't say (static keys,
`credential_process`), wtf asks AWS once in the background with
`aws sts get-caller-identity` (needs the aws CLI and no permissions) and keeps
the answer until `~/.aws/config` or `~/.aws/credentials` changes; a failed
lookup is retried after two minutes and its error shown on hover. From the
credentials file wtf reads only the profile names. Exported access keys, which
override any profile, show as "env credentials" with no account.
Hover a badge for the full names. Turn badges off with `context-badges = false`.

## Session

Each window's tabs, groups, split layouts, folders and text size are saved to
`session.json` in the user config dir (`~/Library/Application Support/wtf`
on macOS, `~/.config/wtf` on Linux) and restored on launch. Shells start
fresh in their saved folders.
