# Welcome to wtf

wtf is a terminal that works like a browser: tabs you can group, split and move between windows, an address bar that tells you what your shell is pointed at, and a side pane with your files, SSH hosts, AWS profiles, Kubernetes clusters, network connections, processes and command history.

## The window

- **Tab strip** (top): one tab per shell. Tabs can be grouped, renamed, reordered and dragged out into a new window. See [Tabs and tab groups](#tabs).
- **Address bar**: the folder of the focused shell, what's running, how the last command went, and badges for the Kubernetes cluster, AWS account, gcloud project and Terraform workspace it uses. See [Address bar and context badges](#context-badges).
- **Terminal**: one or more panes. See [Split panes](#panes).
- **Side pane** (left, {{key:sidebar.toggle}}): Files, SSH, AWS, Kubernetes, Network, Processes and History. See [The side pane](#side-pane).

## Four things to learn first

1. **The command palette** — {{cmd:palette}}. Every command is in it, with its shortcut. If you remember one shortcut, make it this one.
2. **Right-click.** Tabs, group chips, files, hosts, profiles, pods and processes all have a menu with what you can do to them.
3. **Snippets** — {{cmd:snippets.run}} runs your saved commands and runbooks, with blanks filled in. See [Snippets and runbooks](#snippets).
4. **The ? button** at the bottom of the side pane's icons opens this manual at the page for the tab you're on. {{cmd:help.manual}} opens it from anywhere.

## How wtf treats your setup

- Actions that touch a server, a cluster or AWS run as **real commands in a terminal tab** (`kubectl logs …`, `ssh-copy-id …`, `scp …`), so you see them, they land in your shell history, and you can run them again yourself.
- Anything that changes a file you own (`~/.ssh/config`, `~/.aws/credentials`, a kubeconfig) or something in the cloud **shows exactly what will change and waits for you to confirm**. The file is backed up first. See [Privacy and safety](#privacy).
- Side-pane tabs load things **only when you look at them**, and stop when you don't. See [The side pane](#side-pane).
