---
description: Summarize a command's logs in the wtf shell into patterns, errors first, with a timeline
argument-hint: "[block id or what to look for]"
---
Summarize the logs of a command the user ran in their wtf shell. $ARGUMENTS

1. Call `log_patterns`: with `block_id` if a block id is given above, otherwise with no arguments (it takes the latest command with enough output). If the user named a command, call `recent_commands` first to find its block id.
2. Write a short summary in plain text, without Markdown headings:
   - One or two sentences on what the logs show overall: how many lines, the time span, whether things look healthy.
   - Then the patterns that matter, errors and warnings first: one line each with how often it happened and when (first and last seen), quoting the example only when it adds something. Group patterns that are the same problem.
   - If the timeline shows a burst or a start time for the errors, say when.
3. If the cause isn't clear from the logs, say what to look at next. To look, use `run_readonly`; to suggest a command that changes something, `type_at_prompt`.

State what the logs show; don't guess beyond them.
