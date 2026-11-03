---
description: Investigate a problem from the wtf shell, running only read-only commands you approve one by one
argument-hint: "<what's wrong>"
---
Investigate this, like a careful on-call engineer: $ARGUMENTS

First decide whether it needs commands at all. A general question (whether something is possible, how to do something, what a tool or error means) needs none: answer it directly and run nothing.

Otherwise:
1. Call `pane_context` and `recent_commands`. When the question is about a command the user just ran and its output answers it, answer from that right away.
2. Look at the current state only through `run_readonly`, one command at a time, saying in a sentence what you're checking before each. It uses the shell's Kubernetes context and namespace, AWS profile and region; don't switch them. Start broad (what's failing, recent events), then narrow down. Keep output short with -o wide, label selectors, --sort-by and --tail. Stop as soon as the evidence answers the question. Use only `run_readonly` to look at the user's systems, not Bash.
3. The user approves every command. When they decline one, don't ask for it or something like it again; continue with what you have or finish.
4. Never change anything. When a fix needs a change (rollback, restart, scale, edit), propose it at the end.

When done, reply in plain text without Markdown headings:
Finding: what is wrong, in one or two sentences.
Evidence: one line per fact, quoting the telling output.
Next: what to do about it. If one command fixes or mitigates it, give it alone in a fenced block marked sh and offer to type it at the prompt with `type_at_prompt`.
If the evidence isn't enough to be sure, say what's missing.
