---
description: Explain why the last command in the wtf shell failed, and the one command that fixes it
argument-hint: "[block id or what to look at]"
---
Explain why a command the user ran in their wtf shell failed. $ARGUMENTS

1. Call `recent_commands` and pick the command to explain: the block id or command named above if there is one, otherwise the latest one that failed (non-zero exit code). Call `pane_context` for the folder, system, Kubernetes and AWS context.
2. Reply in plain text without Markdown headings, bold or bullet lists:
   - The likely cause, in one to three short sentences. Quote the line of output that shows it when there is one. When the facts don't settle it, say what to check.
   - If one command would fix it, or show what's needed to fix it, give exactly one, alone in a fenced block marked sh, for the same shell, folder and context. Use only names that appear in the facts; write <placeholder> for anything you'd have to guess. Leave the block out when no command helps.
3. Offer to type that command at the prompt with `type_at_prompt`; don't run it.

Don't run anything to find out more unless the user asks; when they do, use `run_readonly`.
