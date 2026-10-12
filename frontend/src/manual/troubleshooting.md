# Troubleshooting

## No ✓/✗ marks, history, badges or "in use" markers

These need [shell integration](#shell-integration), which works with **bash** and **zsh**. Check `shell-integration = true` in [Settings](#settings), and open a new tab (it applies to new shells). Other shells (fish, nu) aren't supported yet.

## Kubernetes: "Can't read the cluster" or a login error

The message under the section is kubectl's own. Common ones:

- **`executable aws-iam-authenticator not found`** — the context logs in with a helper that isn't installed or isn't on your shell's PATH. Either install it, or in [Contexts](#kube-contexts) right-click the context → **Log In with aws eks get-token…** to need only the aws CLI.
- **`You must be logged in to the server (Unauthorized)`** — the AWS profile the context uses can't access the cluster, or its SSO session expired (`aws sso login`).
- **Nothing listed for a context in another file** — kubectl only reads `$KUBECONFIG` or `~/.kube/config`. Double-click the context in Contexts to point the shell at its file.

The tab uses the focused shell's PATH: tabs opened before an update of wtf may need to be reopened.

## SSH tunnel shows "Permission denied (publickey)"

Tunnels run without prompts. Load the key into ssh-agent: [SSH keys](#ssh-keys) → right-click → **Load into Agent**.

## git pushes or commits as the wrong GitHub user

- Check the address bar's git badge: it names the [git identity](#git-identities) the tab uses. No identity means the tab uses your own `~/.ssh/config` and `~/.gitconfig`.
- Right-click the identity → **Test GitHub Login**. The account it shows is the one git in that tab pushes as. If it's the wrong account, the key was added to that account on GitHub: remove it there and add it to the right one.
- Commits already made keep their author. Fix the last one with `git commit --amend --reset-author` (after switching identity).
- Tabs opened outside wtf, and GUI git clients, don't see a tab's identity.

## Git identity: "Permission denied (publickey)"

GitHub doesn't know the key. Right-click the identity → **Add Key to GitHub…** and paste it while signed in as that account. If the key has a passphrase, load it into ssh-agent first ([SSH keys](#ssh-keys) → right-click → **Load into Agent**).

## "REMOTE HOST IDENTIFICATION HAS CHANGED"

The server's key changed (often after a rebuild). See [Host keys](#ssh-host-keys).

## AWS: "the user already has two access keys"

AWS allows two per user. Delete the unused one (Check Access Keys shows which), then rotate.

## A setting doesn't apply

wtf reports mistakes in the window when you save `config.toml` and keeps the last good settings. Some settings (shell, shell integration) apply to new tabs only.

## "The shell is busy"

Actions that type into your shell (cd from the Files tab, switching a profile or context) wait for the prompt. Finish or stop the running program, or use the action's "new tab" variant.

## Reporting a problem

**About wtf** ({{mac:in the **wtf** menu, or|other:type "About" in}} the command palette) shows the version, the commit it was built from, your system, your shell and whether shell integration is on. **Copy Diagnostics** copies those facts (no file paths, profiles or hosts) to paste into a bug report. The same window shows where your settings, snippets, saved session and backups live: click a path to show it in {{mac:Finder|other:the file manager}}.
