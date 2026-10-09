# Workspaces

A workspace is a saved set of tabs you open in one go: their folders, split panes, tab groups, names, and a command each one starts with. Instead of opening four tabs, SSHing into two servers and starting `kubectl get pods -w` by hand every time you go on call, you open "payments on call".

## Opening one

- {{cmd:workspace.open}}, or type its name in the command palette.
- Or the **Workspaces** side-pane tab ({{key:sidebar.workspaces}}) → ▶ on one (or double-click).

A dialog asks for any values it needs (see placeholders below) and lists every tab it will open and the command each will run. **Open Here** adds the tabs to this window; **Open in New Window** puts them in a window of their own. Tabs you already have stay as they are.

Startup commands are typed into each new shell, just as if you'd typed them.

## Making one

The quickest way: arrange your tabs the way you like them, then {{cmd:workspace.save}} (or the stack button at the top of the Workspaces tab). wtf fills in a workspace from this window: each tab's name, group and color, its split panes and their folders, and — for a pane running something like `ssh host` or `kubectl … -w` — that command as its startup command. Adjust it, name it and save.

Or press **+** in the Workspaces tab to build one from scratch. In the editor, each tab is a card:

- **Tab name**, and a **group** with its color — tabs with the same group name open in one tab group.
- One or more **panes**, each with a folder and a startup command (both optional). **Split** adds a pane; with several, choose **side by side** or **stacked**.
- Reorder tabs with ↑ ↓, remove them with ×, add more with **Add Tab**.
- **Open in a new window** makes that the default when it opens.

**Review…** shows exactly what will be written to which file; nothing is saved until **Confirm & Save**. Right-click a workspace for **Edit…**, **Duplicate…** and **Delete…** — deleting removes it from its file only; open tabs aren't affected.

## Placeholders

Names, groups, folders and commands can contain placeholders, asked for when the workspace opens — the same as [snippets](#snippets):

- `{{service}}` asks for a value; `{{ns=default}}` fills one in.
- Defaults can follow the focused shell: `$kube_namespace`, `$kube_context`, `$aws_profile`, `$aws_region`, `$cwd`.

So one "debug a service" workspace can open the right logs, pods and repo for whichever service you name.

## In the file

Workspaces live in the same files as snippets — your `~/.config/wtf/snippets.toml`, a project's `.wtf/snippets.toml`, or team files in `~/.config/wtf/snippets/` — so a repository can carry its team's workspaces too. By hand:

```
[[workspace]]
name = "Kubernetes on call"

[[workspace.tab]]
title = "{{ns}} pods"
group = "on call"
color = "red"
layout = "row"

[[workspace.tab.pane]]
command = "kubectl -n {{ns}} get pods -w"

[[workspace.tab.pane]]
command = "kubectl -n {{ns}} get events -w"

[[workspace.tab]]
title = "shell"
group = "on call"
cwd = "~"

[workspace.params.ns]
default = "$kube_namespace"
```

`window = "new"` opens it in a new window by default. A tab has either `cwd`/`command` itself, or `[[workspace.tab.pane]]`s with `layout = "row"` (side by side) or `"column"` (stacked). Group colors: blue, red, yellow, green, pink, purple, cyan, orange, grey.
