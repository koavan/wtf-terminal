# Ask Claude (Claude Code)

**Ask Claude** opens a split beside your shell that runs your own [Claude Code](https://claude.com/claude-code), with wtf's plugin loaded. Claude works through tools that see what wtf knows about that shell: its folder and cloud context, the commands you just ran and their output, and the change previews you ran. It runs read-only commands where you see them, and types fixes at your prompt for you to run. It uses your own Claude Code login and settings; wtf has no AI keys of its own for this.

You need Claude Code installed, so that `claude` runs in your shell, and logged in (run `claude` once in any shell to log in).

## Opening it

Press {{mac:⌘I|other:Ctrl+Shift+I}} in a shell, or choose **Ask Claude** from the palette, {{mac:the Edit menu|other:the window menu}} or the terminal's right-click menu. wtf splits the shell to the right and types this in the new pane:

```
claude --plugin-dir <wtf's plugin folder>
```

That pane is the **Claude pane**. The shell you opened it from is its **target**: the tools work on that shell, not on the Claude pane. Nothing is installed into your `~/.claude` folder; the plugin is only loaded in Claude panes. The first time you run Claude Code in a folder, it asks whether you trust it.

Ask Claude again from the same shell and you go back to its Claude pane. If you quit Claude Code there, wtf starts it again. Asking from a Claude pane itself does nothing.

## What Claude can do with your shell

| Tool | What it does |
|---|---|
| `pane_context` | Reads the shell's folder, system, Kubernetes context and namespace, AWS profile and region, Google Cloud project, Terraform workspace, git branch, and which well-known tools are installed. These come from files; nothing asks a cloud. |
| `recent_commands` | Reads the last commands you ran in the shell, with exit codes and the end of their output. |
| `review_changes` | Reads a `terraform plan`, `kubectl diff` or `helm diff` you ran, or a saved plan file, and lists each change as facts. |
| `log_patterns` | Groups a command's log output (`kubectl logs`, `docker logs`, `journalctl`…, also while it runs) into patterns: lines that differ only in numbers, ids, addresses, durations, times and quoted values count as one, each with its count, level, first and last time seen; errors and warnings first, with a timeline of them. |
| `session_commands` | Reads a stretch of your work in the shell: each command with when it started, how long it took, its exit code and the start and end of its output (a range of blocks, or the last ones). |
| `open_snippet_editor` | Opens wtf's own [snippet editor](#snippets) filled in with a snippet or runbook Claude wrote. Nothing is saved until you review it there and click **Confirm & Save**. |
| `run_readonly` | Runs one command from a fixed read-only list (`kubectl get`, `aws … describe`, `dig` and the like) in the shell, as a normal command block you can see, and returns its output. Anything else is refused. |
| `type_at_prompt` | Types a command at the shell's prompt without pressing Enter, so you read, edit and run it yourself. |

Secrets are hidden (as **Hide Secrets** does) in everything these tools return. Claude Code's own tools, such as reading files or its own Bash, aren't filtered by wtf.

Slash commands do the common jobs. wtf's own menus type them for you: **Explain Failure…**, **Summarize Logs…** (a command's right-click menu, when it has 10 lines or more, or the palette), **Runbook from Here…** (a command's right-click menu, or the palette), **Postmortem Draft…** (the palette), **Command from Plain English…**, **Investigate…** and a review's **Summarize with Claude** open the shell's Claude pane if needed and type the command once Claude Code is ready for a prompt (never while it starts or works).

- `/wtf:explain`: why the last command failed, and one command that fixes it.
- `/wtf:cmd <what you want>`: one command for this shell, typed at the prompt.
- `/wtf:review`: a plain-English summary of the latest change preview.
- `/wtf:logs`: a summary of a command's logs: the patterns that matter, how often and when.
- `/wtf:runbook`: a runbook from what you just did, with `{{placeholders}}` for what changes, opened in wtf's snippet editor for you to save.
- `/wtf:postmortem`: a postmortem draft: timeline, evidence, root cause and follow-ups, from your commands and their output.
- `/wtf:investigate <what's wrong>`: an investigation with read-only commands, each approved by you.

## Approving commands

wtf makes Claude Code ask you before every command it runs or types (`run_readonly`, `type_at_prompt`, and Claude Code's own Bash and Monitor) and before every file it changes (Edit, Write and the like). It asks even in Claude Code's auto mode or accept-edits mode, and even after you chose "don't ask again". Claude Code shows the command or the change, and you choose to allow or deny it.

To let Claude Code's own permission mode decide instead, turn on **Let Claude Code's own permission mode decide instead** in **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General**, or set `claude-auto-mode = true` in `config.toml`. It applies to Claude panes opened after that. It's off unless you turn it on.

## Knowing what it's doing

The tab with a Claude pane shows what Claude Code is doing:

- {{mac:a sparkle|other:✦}} in the accent colour: working;
- {{mac:an orange speech bubble|other:an orange !}}: it needs you, usually to approve a command;
- {{mac:a grey sparkle|other:a grey ✦}}: waiting for your next prompt.

When it needs you or finishes and you're looking at something else (another tab, another app), wtf shows a notification.

Closing the Claude pane ends Claude Code and forgets the target.
