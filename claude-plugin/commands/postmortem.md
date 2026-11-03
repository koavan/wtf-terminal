---
description: Draft a postmortem from what happened in the wtf shell: timeline, evidence, decisions
argument-hint: "[from block N | last N commands] [what the incident was]"
---
Draft a postmortem from the user's work in their wtf shell. $ARGUMENTS

1. Call `session_commands`: with `from_block` (and `to_block`) if a range is given above, with `count` if a number of commands is, otherwise with no arguments. Call `pane_context` for the cluster, account and folder.
2. Write the draft in Markdown:
   - **Summary**: what happened and the impact, in two or three sentences, as far as the commands show it.
   - **Timeline**: one line per meaningful step, with its time, what was checked or changed, and what the output showed (quote the telling line).
   - **Root cause** and **Resolution**, as far as the evidence goes; say what's missing where it doesn't.
   - **Follow-ups**: concrete actions.
3. Mark anything you inferred rather than saw in the output. Leave placeholders like `<impact>` for what only the user knows.

Show the draft here. Write it to a file only if the user asks, and then where they say. Run nothing.
