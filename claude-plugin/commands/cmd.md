---
description: Turn a plain-English request into one shell command, typed at the wtf prompt for you to run
argument-hint: "<what you want to do>"
---
The user wants a shell command for: $ARGUMENTS

1. Call `pane_context` to know the shell, folder, system, Kubernetes and AWS context, and which tools are installed. Call `recent_commands` only if the request refers to something they just did.
2. Write one command line that does it, for that shell and context, using tools that are installed. Use only names that appear in the facts or the request; write <placeholder> for anything you'd have to guess.
3. Type it at the prompt with `type_at_prompt` (it doesn't press Enter), then say in one or two sentences what it does and anything it changes. Don't run it.

If the request is a question about how to do something rather than a task, answer it and run nothing.
