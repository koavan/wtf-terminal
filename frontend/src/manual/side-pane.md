# The side pane

{{cmd:sidebar.toggle}} (or the button next to the window controls) shows the side pane. Its tabs are icons down its left edge, top to bottom:

| Tab | Shortcut | What it's for |
|---|---|---|
| [Files](#files) | {{key:sidebar.files}} | the focused shell's folder |
| [SSH](#ssh-hosts) | {{key:sidebar.ssh}} | hosts, keys, [git identities](#git-identities), tunnels |
| [AWS Profiles](#aws) | {{key:sidebar.aws}} | AWS CLI profiles |
| [Kubernetes](#kubernetes) | {{key:sidebar.kube}} | cluster resources and kubeconfig contexts |
| [Snippets](#snippets) | {{key:sidebar.snippets}} | saved commands and runbooks |
| [Workspaces](#workspaces) | {{key:sidebar.workspaces}} | sets of tabs to open in one go |
| [Network](#network) | {{key:sidebar.network}} | listening ports and connections |
| [Processes](#processes) | {{key:sidebar.processes}} | what's running in your terminals |
| [History](#history) | {{key:sidebar.history}} | commands run in a shell |
| [Pinned output](#pins) | {{key:sidebar.pins}} | output you pinned to keep in view |
| [Share & Record](#share) | {{key:sidebar.share}} | Hide Secrets, recording, and saving a terminal's output |

Hover an icon for the tab's name and shortcut. Pressing a tab's shortcut again closes the pane. Drag its right edge to resize it; double-click the edge for the default width. The **?** at the bottom of the icons opens this manual at the page for the tab you're on.

## Most tabs follow the focused shell

Files, Kubernetes, the AWS "in use" marker and History all follow the pane you're typing in: click into another pane or tab and they switch with it.

## It only works while you look at it

- Nothing in the side pane runs while it's closed, and only the visible tab updates.
- Network and Processes pause while the window is in the background.
- Kubernetes reads a resource only when you expand its section.
- **⏸** in a tab's header stops background updates everywhere (same as `sidebar-auto-refresh = false`); tabs then load when opened and refresh when you press **↻**. "Paused · updated …" says how old the data is.
- `sidebar-refresh-interval` sets how often visible tabs update (3 seconds by default; Kubernetes waits at least 10).
