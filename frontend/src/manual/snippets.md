# Snippets and runbooks

Snippets are commands you run often, with blanks to fill in: `kubectl -n {{ns}} rollout restart deploy/{{name}}`. Runbooks are snippets with several steps you run one at a time. Both live in plain text files, so a team can share them through git.

## The Snippets tab

{{cmd:sidebar.snippets}} shows every snippet in the side pane, grouped by file: **This project**, **Yours**, and team files. Filter by name or command. The **Workspaces** tab next to it lists your [workspaces](#workspaces), from the same files.

- **▶** (or double-click) runs one — see below.
- **+** makes a new one in a form; right-click → **Edit…**, **Duplicate…**, **Copy Command** or **Delete…**.
- The file button opens your snippets file in a text editor, if you'd rather write TOML.

## Making and editing snippets

The editor asks for a name and description, then either a **Command** or a **Runbook** of steps (add, remove and reorder them). Each `{{placeholder}}` you type gets a row for what to enter, a default, choices (comma-separated) and **raw** (insert as typed rather than quoted). For a new snippet, choose where to save it: your file, the project's — created at the repository's root if it doesn't exist yet — or a team file.

**Review…** shows the exact block that will be written and to which file; nothing is written until **Confirm & Save**. Editing replaces only that snippet's block: other snippets and your comments in the file stay as they are. Deleting asks first.

## Running one

{{cmd:snippets.run}} lists your snippets (they also show up when you type in the command palette), as does the Snippets tab. Pick one:

- With nothing to fill in, it goes straight to the prompt.
- Otherwise a form asks for each value, with a live preview of the exact command. **Insert** (`Enter`) puts it at the prompt so you can check it and press Enter yourself; **Run** ({{mac:⌘↩|other:Ctrl+Enter}}) runs it; **Run in New Tab** runs it in a fresh tab.
- **Runbooks** list their steps, each filled in. Press **Run** (or **Insert**) on one step at a time and watch it finish; done steps get a ✓.

Values are shell-quoted for you, so a value with spaces stays one argument.

## Saving one

Right-click a command in the terminal (or its ✓/✗ mark) → **Save as Snippet…**, or right-click it in [History](#history). The editor opens with the command filled in: name it, turn the parts that change into placeholders — `{{ns}}`, `{{name}}` — and choose where to save it.

## Writing them by hand

{{cmd:snippets.open}} opens your file (`~/.config/wtf/snippets.toml`), created with examples the first time:

```
[[snippet]]
name = "Restart a deployment"
description = "Rolls every pod, one at a time"
command = "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}"

[snippet.params.name]
description = "The deployment"
choices = ["web", "worker", "api"]
```

- `{{name}}` asks for a value. `{{name=default}}` fills one in, which you can change.
- A default can follow the focused shell: `$kube_namespace`, `$kube_context`, `$aws_profile`, `$aws_region`, `$cwd`.
- Under `[snippet.params.NAME]`: `description` (shown in the form), `default`, `choices` (a list to pick from), and `raw = true` to insert the value as typed instead of quoting it — for several flags at once.
- Go templates in a command (`{{.metadata.name}}`, `{{end}}`) are left alone.

A runbook has steps instead of a command:

```
[[snippet]]
name = "Drain a node for maintenance"

[[snippet.steps]]
title = "Stop new pods landing on it"
command = "kubectl cordon {{node}}"

[[snippet.steps]]
title = "Move its pods elsewhere"
command = "kubectl drain {{node}} --ignore-daemonsets"
```

Each command is one line. Mistakes in a file are listed in the palette instead of the snippets they affect.

## Sharing with a team

wtf reads, in this order:

1. `.wtf/snippets.toml` in the shell's folder and every folder above it, up to your home folder — commit one to a repo and everyone working in it gets its runbooks (they're marked *project*);
2. your own `~/.config/wtf/snippets.toml`;
3. every `*.toml` in `~/.config/wtf/snippets/` — clone or link a team's runbook repo there.

Files are read fresh each time you open the list, so a `git pull` is all it takes to get the latest.
