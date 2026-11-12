# wtf

A browser-like terminal for macOS and Linux. A Go backend with a native UI on
each: AppKit + SwiftTerm on macOS (`macos/`, see NATIVE_MACOS_ROADMAP.md), GTK 4 +
libadwaita + VTE on Linux (`internal/gtkui`, see NATIVE_LINUX_ROADMAP.md).
Before 1.0 it was a Wails v3 + Svelte 5 + xterm.js app; that UI was removed
after 1.7.1 (roadmap #21).

What's next: see [ROADMAP.md](ROADMAP.md).

## Install on Linux

The `.deb` (Ubuntu 24.04+, Debian 13+) or `.rpm` (Fedora 40+) for your
processor from the release, e.g. `sudo apt install ./wtf_1.1.0_amd64.deb` or
`sudo dnf install ./wtf-1.1.0-1.x86_64.rpm`. It replaces an older Wails build
of wtf and keeps its windows and settings (`~/.config/wtf`).

## Build the macOS app

Needs Xcode (full, not only the Command Line Tools) and Go 1.25+:

```sh
macos/build.sh                   # bin/native/wtf.app (release, this Mac's processor)
macos/build.sh debug             # a debug build
macos/build.sh release universal dmg   # arm64 + x86_64, and bin/native/wtf-<version>.dmg
(cd macos && swift test)         # the Swift core's tests
```

The version comes from the `VERSION` file; the build stamps the git commit and
date into the app.

## Build the native Linux app

In Docker, on any machine (`linux/Dockerfile` is Ubuntu 24.04 with the GTK,
libadwaita and VTE headers):

```sh
docker build -t wtf-gtk-dev:ubuntu24 linux
linux/build.sh                   # bin/gtk/wtf-gtk
linux/check.sh                   # tests and headless self-checks under Xvfb
linux/package.sh arm64 amd64     # .deb and .rpm in bin/gtk/packages
linux/package/test-install.sh amd64   # installs them on clean Ubuntu, Debian, Fedora
```

If a distribution's mirror is slow, `UBUNTU_MIRROR=http://mirrors.mit.edu/ubuntu/`
or `DEBIAN_MIRROR=…` points `test-install.sh` at another one.

## Tests

```sh
go vet ./... && go test ./...    # the Go backend and shared UI logic
(cd macos && swift test)         # the macOS app's Swift core
linux/check.sh                   # the Linux app, headless, in its dev image
```

`testdata/secrets/cases.json` holds what the secret finder must find; both the
Go and Swift tests check against it. After changing the rules, regenerate it
with `go run testdata/secrets/generate.go`.

At run time the Linux app uses `ss` (or `lsof`) for the Network tab,
`notify-send` for notifications and `xdg-open` to open files.

## Command-line tools

Side-pane features run the programs you'd run yourself — `kubectl`, the AWS
CLI, `git`, OpenSSH, `lsof`/`ss`, `notify-send` — found on the focused shell's
PATH, then wtf's, then the usual install folders (`/opt/homebrew/bin`,
`/usr/local/bin`, `~/.local/bin`…). None is needed to start; a feature whose
program is missing says so with an **Install…** link.

The wrench at the bottom of the side pane's icon rail (also the palette's
**Command-Line Tools**, the macOS wtf menu, and About wtf → Tools) lists each one with
its path and version, and installs a missing one for this OS and processor:
Homebrew, the official release (kubectl SHA-256-checked into `~/.local/bin`; the
AWS CLI's signed macOS package or Linux zip into `~/.local`), Apple's Command
Line Tools, or the distribution's package manager. It shows the exact command or
script first and runs it in a new tab, where sudo can ask for a password. On
first start it opens once if anything is missing. The `.deb`/`.rpm` recommend
`git`, OpenSSH, `lsof` and libnotify. Code: `internal/tools`.

## Layout

- `macos/`: the macOS app (AppKit + SwiftTerm); `macos/Sources/WtfCore` is its
  shared, tested logic. `cmd/libwtf` builds the Go backend as a C archive for it.
- `internal/gtkui`, `cmd/wtf-gtk`: the Linux app (GTK 4 + libadwaita + VTE);
  `internal/uicore` is its shared, tested logic (a Go port of WtfCore).
- `internal/backend`: builds every service once, for both apps.
- `cmd/wtf-mcp`: the wtf Claude Code plugin's MCP server (`claude-plugin/`).
- `manual/`: the in-app manual's pages.
- `internal/terminal`: shell sessions (PTY) and foreground-process/cwd polling.
- `internal/state`: atomic session file storage.
- `internal/config`: config file, themes (built-in, Ghostty, iTerm2), live reload.
- `internal/terminal/shellintegration`: the bash/zsh scripts.
- `internal/notify`: desktop notifications.
- `internal/sshhosts`: the SSH tab: hosts from `~/.ssh/config` and `known_hosts`, keys
  and ssh-agent, server host keys, git identities, and background tunnels.
- `internal/infra`: Kubernetes/AWS/gcloud/Terraform/git context for the context badges.
- `internal/kube`: the Kubernetes tab: deployments, pods and events, read with
  kubectl using the focused shell's variables.
- `internal/awsprofiles`: the AWS tab: profiles in `~/.aws/credentials` and
  `~/.aws/config`, checks and key rotation through the aws CLI.
- `internal/files`: directory listings (with git ignore rules) for the file tree.
- `internal/netconn`: connections and listening ports, attributed to terminal sessions.
- `internal/procs`: the user's processes, attributed to terminal sessions.
- `internal/cloudsecrets`: the Cloud Secrets tab: Secrets Manager, Parameter
  Store and Vault through the aws and vault CLIs.

## Manual

wtf has a user manual built in: **Help → wtf Manual** (⌘?, F1 on Linux), the
command palette, or the **?** at the top right of the side pane, which opens
the page for the tab on screen. It covers every feature, with search, and
its shortcuts come from the app's own command list. The pages are Markdown
in `manual/` (`internal/uicore/manual.go` describes the subset they use);
tests check that every command and page they mention exists.

**About wtf** (the **wtf** menu on macOS, the command palette anywhere) shows the version, the commit and date of the build
(the build stamps them in from git), the system, the shell and whether
shell integration is on, and where settings, snippets, the session and
backups live. **Copy Diagnostics** copies the version and system facts, with
no paths, for a bug report.

## Shortcuts

Every command is also in the command palette.

| Action | macOS | Linux |
|---|---|---|
| Command palette | ⌘⇧P | Ctrl+Shift+P |
| Manual (at the page for the side-pane tab on screen) | ⌘? | F1 |
| Toggle sidebar | ⌘B | Ctrl+Shift+B |
| Sidebar: files / SSH / AWS / Kubernetes / snippets / workspaces / network / processes / history / findings | ⌘⇧E / ⌘⇧S / ⌘⇧U / ⌘⇧K / ⌘⇧Y / ⌘⇧L / ⌘⇧O / ⌘⇧A / ⌘⇧H / ⌘⇧F | Ctrl+Shift+Alt+E / S / U / K / Y / L / O / A / H / F |
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
| Ask Claude (Claude Code beside the shell) | ⌘I | Ctrl+Shift+I |
| Agents tab / Go to Agent… / Next agent needing you | ⌘⇧M / ⌥⌘A / ⌥⌘N | Ctrl+Alt+M / Ctrl+Alt+Shift+M / Ctrl+Alt+Shift+N |
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
each command. In a git repository the tree shows the branch (↑/↓ against its
upstream), colors changed files with M/A/U/D/R/! and dots folders holding
changes; **N changes** lists them all (deleted ones too), and right-click has
Show Changes (`git diff`) and Show History (`git log`) in a new tab. Read-only:
`git status` runs with optional locks off, only when the tree refreshes.

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
**+** opens the Generate tab's key form (below), and the menu loads a key into the agent (on macOS also
with its passphrase in the Keychain), unloads it, changes its passphrase,
copies the public key (double-click), installs it on a host, or deletes it —
moving the pair to wtf's cache folder (`ssh-backups/keys`), not erasing it.
Anything else that can ask for a passphrase or password (ssh-add,
ssh-copy-id, scp, changing a passphrase) runs as a real command in a new tab,
so it's in your shell history and the secret never passes through wtf.

The **Generate** tab (⌘⇧N, key icon) makes SSH keys and passwords from a
form. **SSH Key**: type (Ed25519, RSA 4096, ECDSA P-521), file name (never
replacing a file), comment and passphrase (typed twice, or **Suggest one**);
wtf makes the key with Go's crypto and `golang.org/x/crypto/ssh` and writes
it in OpenSSH's format (encrypted with bcrypt-pbkdf and AES-256 like
ssh-keygen, `0600`), and can load it into ssh-agent. The passphrase is used
once in memory and never stored; **Generate in a terminal instead** runs
ssh-keygen in a tab. The result shows the fingerprint and public key, with
Copy Public Key, Add to GitHub…, Install on Host… and Use for a Git
Identity…. **Password**: length 4–128, lowercase/uppercase/digits/symbols
(each at least once; no quotes, spaces, backslashes or backticks), leave out
look-alikes, and roughly how many bits of randomness it has. Made with
`crypto.getRandomValues`, never saved; copying clears the clipboard after 30
seconds, and Hide Secrets shows it as dots until you click Show.

**Git** holds git identities, for working on GitHub as more than one user:
each pairs an SSH key with the name and email to commit as. Using one in a tab
(double-click, or right-click a tab → **Use …**) types an `export` of
`GIT_SSH_COMMAND='ssh -i <key> -o IdentitiesOnly=yes'`, `GIT_AUTHOR_*` and
`GIT_COMMITTER_*` into that tab's shell, so git there pushes with only that
key and commits as that author; other tabs are untouched, and nothing in
`~/.ssh` or `~/.gitconfig` changes. Splits and duplicates of the tab keep the
identity, and so does the tab across restarts. **Add Key to GitHub…** copies
the public key and opens GitHub's new-key page; **Test GitHub Login** runs
`ssh -T git@github.com` with only that key and shows the account GitHub
names. The git badge shows the identity a tab uses. Saved in
`git-identities.json` in wtf's config folder.

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

**Check Permissions (iam)…** shows who a profile is (user, role or root),
its permissions boundary, and every policy attached to it, inline and through
its groups; click one to read its document. The Kubernetes tab's **Access**
section does the same for a cluster: `kubectl auth whoami` and
`kubectl auth can-i --list`, as rules you can scan.

The **Cloud Secrets** tab (⌘⇧X / Ctrl+Alt+X) browses AWS Secrets Manager,
SSM Parameter Store and HashiCorp Vault (KV 1 and 2) with the focused
shell's AWS profile, region and `VAULT_ADDR`. Values stay masked until you
reveal one, and Hide Secrets blocks revealing, copying and editing. New
values and edits go through a plan you confirm, and are checked against the
version you read first, so you can't overwrite someone else's change. Values
reach the CLIs on stdin, never on a command line.

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

The **Network** tab starts with **Forwards**: every port forward wtf runs —
ssh tunnels and **kubectl port-forwards** — with status, start/stop and
**Open in Browser**; **+** adds either kind. A kubectl forward (context,
namespace, `svc/…`, `deploy/…` or `pod/…`, ports) runs in the background with
the `KUBECONFIG`, AWS profile and `PATH` of the shell it was made in,
listening on 127.0.0.1, and is restarted when its pod restarts. The
Kubernetes tab's **Port Forward…** opens the same editor (or runs a one-off
in a terminal). Listening ports that belong to a forward are labeled with it.
Below, the tab lists listening ports and open connections — by default
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
shell does. **Saved** lists every shell's commands across windows and
restarts (90 days, `history-days`), each with the Kubernetes context and AWS
account and region it ran under, filtered by time and context; secrets are
redacted before saving (`internal/cmdhistory`, `history.jsonl` in the state
folder, mode 0600).

🗑 clears the list and, after asking, removes the session's commands from
the shell's own history (↑ / Ctrl-R) via a key binding the integration sets
up (Ctrl-X Ctrl-]); nothing is typed into the terminal. History from earlier
sessions stays, and the history file is never rewritten without it. Busy
shells are left alone (only their list is cleared).

The **Pinned** tab (⌘⇧I) keeps output in view while you work: **Pin Output**
from a command's mark or right-click menu (or **Pin Last Command Output** in
the palette), or **Pin Selection** with text selected. A pin is a copy of the
text that outlives the scrollback and its tab; a command still running keeps
its pin up to date (refreshed only while the tab is on screen, and once more
when it finishes). Copy, collapse, unpin, or right-click for Show in Terminal.
Pins stay in memory, per window, and are never written to disk.

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

## Workspaces

A workspace opens a set of tabs in one go: names, tab groups and colors,
split panes (side by side or stacked) with their folders, and a startup
command each — e.g. two SSH tabs grouped "payments", a repo shell, and
`kubectl get pods -w` beside `kubectl get events -w`. **Open Workspace…**
(command palette) or the **Workspaces** side-pane tab (⌘⇧L) opens one; a
dialog asks for its `{{placeholders}}` (defaults may follow the shell, like
snippets), lists every tab and command, and opens them here or in a new
window. **Save Window as Workspace…** captures the current window — commands
running in panes (ssh, kubectl … -w) become startup commands — into the same
form editor snippets use, with the same review before writing. Workspaces are
`[[workspace]]` blocks in the snippets files (yours, a project's
`.wtf/snippets.toml`, team files), documented in the manual.

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

## Log-friendly output

Output is scanned, only on the screen you're looking at and only once it
settles, for log levels (ERROR red, WARN yellow, INFO green, DEBUG faded),
timestamps (faded), URLs, IP addresses and Kubernetes resources
(`pod/web-1`, `deployment.apps/api`), which get colors from the theme. The
output itself is untouched. Text a program colored keeps its colors, and
full-screen programs are left alone. ⌘-click (Ctrl-click) or right-click one
for actions: a timestamp in your time zone and UTC, with copy as ISO/Unix time;
an IP's ssh or `http://ip:port`; a resource's logs, describe or YAML as
kubectl commands in a new tab, against the shell's reported context and
namespace (secrets open the masked viewer instead); a `file:line` that exists
on this machine opens in `$EDITOR +line`. Right-click a line holding JSON for
**Pretty-print JSON**. Turn colors off with `log-highlighting = false`.

## Tokens and certificates

- **JWT** in the output: ⌘-click (or right-click → Inspect Token…) for its
  header and claims, with `exp`/`iat`/`nbf` as local times. The signature
  isn't verified.
- **Base64**: select it, or right-click a value in `kubectl get secret -o yaml`,
  → Decode Base64…; shown masked until revealed (hidden again on blur), as
  text, JSON, a hex dump, or a PEM certificate / JWT to open next.
- **Certificates**: a `.pem`/`.crt`/`.cer`/`.der` file in Files, or a selected
  PEM block → Inspect Certificate…: subject, issuer, names, validity and time
  left, key, signature, serial, SHA-256 fingerprint.
- **TLS servers**: an `https://` URL's menu, a selected host or `host:port`, or
  the palette's Inspect TLS Certificate… connects (handshake only) and shows the
  protocol, cipher, ALPN and chain, and whether it verifies against the system's
  roots — with the TLS library's error when it doesn't.

JWTs and base64 can't be opened while Hide Secrets is on.

## Findings (security scans)

The side pane's **Findings** tab (⌘⇧F / Ctrl+Shift+Alt+F, bug icon) shows
what security scanners found in the focused shell's git repository, grouped by
severity (the scanner's own: its security-severity score, else the SARIF
level) and then by rule; click a location to open the file at that line.
**Run Scan…** (▶, or the palette) offers Trivy, Semgrep, Checkov, Gitleaks
(`--redact`) and Grype: it shows the exact command and types it into the
focused shell. The SARIF goes to wtf's cache, not the repository. `.sarif`
files at the repository's top (from CI, say) are read too. Read when the tab
opens, on refresh, and when a scan finishes; never polled. Missing scanners
get an **Install…** (Homebrew on macOS). Code: `internal/findings`.

## Ask Claude

**Ask Claude** (⌘I / Ctrl+Shift+I, the palette, or right-click) splits the
shell and runs your own [Claude Code](https://claude.com/claude-code) there
with wtf's bundled plugin (`claude --plugin-dir`; nothing is installed into
`~/.claude`). It uses your Claude Code login; wtf holds no AI keys and never
runs Claude in the background. The plugin's MCP server (`wtf-mcp`, talking
to the app over a private socket) gives Claude tools for the shell it came
from: its context, recent commands and output, change previews (terraform
plan, kubectl/helm diff), log patterns, a range of commands, security
findings, saved history, read-only commands from an allowlist (run where you
see them), and typing a command at the prompt without Enter. Everything
they return is redacted. Claude Code asks before every command it runs or
types and every file it edits, even in its auto mode, unless you set
`claude-auto-mode`. The tab shows whether Claude is working, needs you or
is idle, and wtf notifies when it needs you.

wtf's menus type the plugin's slash commands for you: Explain Failure
(`/wtf:explain`), Command from Plain English (`/wtf:cmd`), Investigate
(`/wtf:investigate`), Summarize with Claude in a review (`/wtf:review`),
Summarize Logs (`/wtf:logs`), Runbook from Here (`/wtf:runbook`, opening
wtf's snippet editor filled in), Postmortem Draft (`/wtf:postmortem`),
Triage with Claude on a finding (`/wtf:triage`) and Ask Your History
(`/wtf:history`). Code: `claude-plugin/`, `cmd/wtf-mcp`, `internal/bridge`.

## Agents

The **Agents** tab (⌘⇧M / Ctrl+Alt+M, first in the side pane) lists every
Claude Code running in wtf's shells, in every window: needs you, done
(finished while you looked elsewhere), working or idle, with its tab,
folder, last prompt or what it asks, and how long. Click one to go there;
**Go to Agent…** (⌥⌘A) and **Next Agent Needing You** (⌥⌘N) do it from the
keyboard. Tab badges and notifications cover every one, and the Dock icon
counts the ones that need you. State comes from Claude Code's hooks, never
from reading the screen: Ask Claude panes report through the plugin, and a
`claude` you type yourself runs with `--settings` pointing at wtf's status
hooks (bash and zsh; that run only, nothing in `~/.claude`; `agent-status =
false` turns it off).

Closing a tab running Claude Code asks whether to keep it running in the
background (`agent-close`: ask, background or end). Background agents keep
working, unseen, while wtf runs; the Agents tab opens one again in a tab,
peeks at its screen and sends a reply you type (or Return / Esc for its
prompts), or ends it. **Start Claude Code…** starts one in a folder, with a
first prompt, in a tab or in the background. Quitting wtf ends them, after
asking.

Sub-agents an agent starts (Claude Code's Agent tool) sit folded under it:
a small "▸ 3 sub-agents · 1 working" line opens them, each with its type and
task, its latest step while it works ("Read deploy_test.go") or its report's
first line once done. They come from Claude Code's SubagentStart/Stop and
tool hooks; a sub-agent's steps don't change its agent's state.

Agents are listed in the order they started, so rows don't move; double-click
one (or Rename…) to give it a name, shown in the list, the picker and its
notifications, and given to its tab too. A short sound plays with an agent's notification, one for
needs you and one for finished (`agent-sound`), and tabs running Claude Code
join their window's **agents** tab group (`agent-group`).

Several agents on one repository each get their own **worktree**: **New
Worktree…** (palette, or the Files tab's git bar) checks out a new or
existing branch, or a detached commit, in its own folder, then opens a tab
or starts Claude Code there. **Remove Worktree…** shows what isn't committed
first, never deletes the branch, and asks again before `--force`. Agent rows
show their branch, with ↑/↓ against its upstream.

Quitting wtf ends its agents, but they can come back: a tab that ran Claude
Code returns with a **Resume** bar (`claude --resume`, in the folder it
started in), and background agents are listed under **Resumable** in the
Agents tab. Nothing resumes until you click, and a resumed agent keeps its
name.

## Resource usage

wtf can show its own CPU and memory in the address bar (`show-usage`, or
**Toggle Resource Usage in Address Bar** in the palette); click it, or
**Resource Usage…**, for the breakdown: the app, and — listed but not counted
in wtf's total — the shells and programs run in it (on macOS including ones
that left their shell, found as Activity Monitor groups them, by the process
macOS says each works for). Memory is the footprint on macOS, RSS on Linux.
Measured with direct system calls (libproc, `/proc`; no `ps`), every 2 seconds
only while shown and the window is in front: about 11 µs a sample on macOS,
plus a ~1 ms scan for detached programs every 10 seconds. Code: `internal/usage`.

## Recording and saving

The side pane's **Share & Record** tab (⌘⇧J, shield icon) has all of this —
and Hide Secrets — as buttons: a Hide Secrets switch (and "turn on whenever
wtf starts"), Start/Stop Recording for the terminal in front with its elapsed
time and file, Save Output as Text / Commands as Markdown, the other terminals
being recorded, and the files saved in this window. A terminal's right-click
menu also has Hide Secrets and Start Recording….

⌥⌘S (Shell → Start or Stop Recording…, or a tab's right-click menu) records a
terminal to an asciinema `.cast` file (asciicast v2: output with its timing and
resizes, never what you type), written by the backend as output arrives and
flushed every second; a red dot on the tab and **REC m:ss** in the address bar
show it's on. **Save Output as Text…** saves the scrollback; **Save Commands
as Markdown…** saves a postmortem-ready transcript — each command with its exit
status, start time, duration and output. Files are 0600; saved text (not
recordings) honors Hide Secrets.

## Hide Secrets

For screen sharing: ⌥⌘R (View → Hide Secrets; Ctrl+Shift+Alt+X on Linux)
covers access keys, tokens, JWTs, passwords in URLs and in `password=` /
`token:`-style settings, base64 data in YAML, and private keys wherever they
appear in a terminal — from the first frame, through selections and full-screen
programs — and copies get `[REDACTED]` in their place. The address bar's
command, tab titles, History, pins, the tab overview and notifications are
redacted too; the output itself is untouched. A "Secrets hidden" label shows
while it's on. Best effort: it can miss a secret it doesn't recognize.
`redact-secrets = true` turns it on at startup.

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
credentials file wtf reads only the profile names and `x_security_token_expires`.
Exported access keys, which override any profile, show as "env credentials" with
no account.

**Credential expiry clocks.** The AWS and Kubernetes badges, and an ssh badge
for certificates in the shell's ssh-agent, show how long the shell's
credentials have left (`42m`, `expired`) from a day before they expire; hover
for exact times. Read from the credentials themselves: `AWS_CREDENTIAL_EXPIRATION`
(aws-vault, granted, `aws configure export-credentials`), the aws CLI's SSO
cache, `x_security_token_expires` (saml2aws), the kube user's client
certificate and JWT token or OIDC id-token, and ssh-agent certificates.
Self-renewing credentials (exec plugins, assumed roles) have no clock.
In a git repository, a **git** badge shows who commits there (the tab's git
identity, else `GIT_AUTHOR_NAME` or `user.name`), the
branch, its upstream, and how many commits apart they are: `Ada · main → origin ↑2 ↓1`
(the upstream's branch is left out when it matches). Hover it for the
repository, the remote's URL (any user or token in it is dropped), the author's
email and when the repository last fetched. The counts are as of that fetch:
wtf never fetches. Outside a repository nothing runs; inside, a few read-only
git commands run at each prompt, with optional locks off.
Hover a badge for the full names. Turn badges off with `context-badges = false`.

## Session

Each window's tabs, groups, split layouts, folders and text size are saved to
`session.json` in the user config dir (`~/Library/Application Support/wtf`
on macOS, `~/.config/wtf` on Linux) and restored on launch. Shells start
fresh in their saved folders.
