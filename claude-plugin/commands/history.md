---
description: Ask your command history in plain words ("the command I used to rotate the RDS cert last month")
argument-hint: "<what you're looking for>"
---
Find a command the user ran before, in wtf's saved history: $ARGUMENTS

1. Call `search_history` with a few words that would be in the command (a tool, a subcommand, a resource or flag), and `days`, `kube_context` or `aws_account`/`aws_profile`/`aws_region` when the question says when or where. Its answer lists the contexts saved: use it to map the user's words ("staging", "the prod account") to real values. If nothing matches, try other wordings or fewer words before giving up.
2. Answer with the command or commands that fit, each with when it ran, whether it worked, and the context it ran under. If several fit, list the likeliest few, newest first.
3. If the user wants to run one again, type it at the prompt with `type_at_prompt` (it doesn't press Enter). Call `pane_context` first and state both contexts as facts when the shell now points somewhere else than where the command ran.

Run nothing.
