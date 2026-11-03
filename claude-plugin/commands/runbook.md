---
description: Turn what you just did in the wtf shell into a runbook (or snippet) in wtf's snippet editor
argument-hint: "[from block N | last N commands] [what it's for]"
---
Make a reusable runbook from commands the user ran in their wtf shell. $ARGUMENTS

1. Call `session_commands`: with `from_block` (and `to_block`) if a range is given above, with `count` if a number of commands is, otherwise with no arguments.
2. Keep the commands that did the work, in order; leave out typos, failed attempts that were retried, and commands that only looked around (unless checking is a step worth keeping, like waiting for a rollout). Ask the user if it's unclear which ones matter.
3. Turn what changes from one run to the next into `{{placeholders}}` (a namespace, a deployment, a host, a version), with defaults that follow the shell where they fit (`{{ns=$kube_namespace}}`). Give each step a short title.
4. Call `open_snippet_editor` with a clear name, a one-line description, the steps (or one `command` if it's a single command), and what each placeholder is. The user reviews and saves it there; say so, and list the steps in a few lines.

Run nothing.
