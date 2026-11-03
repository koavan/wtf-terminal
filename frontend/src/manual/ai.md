# AI assistance

wtf's AI is your own Claude Code, run in a split beside your shell: see [Ask Claude](#ask-claude) ({{mac:⌘I|other:Ctrl+Shift+I}}). It uses your Claude Code login and settings. wtf has no AI keys or providers of its own, and never calls an AI in the background.

## Explain, commands from plain English, investigations

These go through [Ask Claude](#ask-claude): your own Claude Code in a split beside the shell, not the providers on this page.

- **Explain Failure… (Claude)**: right-click a failed command (or click its ✗ mark), or **Explain Last Failed Command…** in the palette and {{mac:the Edit menu|other:the window menu}}. The shell's Claude pane gets `/wtf:explain block N`: Claude reads that command and its output through wtf's tools, says why it likely failed, and can type a fix at your prompt for you to run.
- **Summarize Logs… (Claude)**: right-click a command with 10 or more lines of output (a `kubectl logs`, also while it runs), or **Summarize Logs of a Recent Command…** in the palette. The Claude pane gets `/wtf:logs`: wtf groups the lines into patterns (counts, levels, first and last seen, a timeline of errors), and Claude says what matters.
- **Runbook from Here… (Claude)**: right-click the first command of what you did, or **Runbook from Recent Commands…** in the palette. Claude keeps the steps that did the work, turns what changes into `{{placeholders}}`, and opens wtf's snippet editor filled in; you review it and save it, or not. **Postmortem Draft…** in the palette has Claude draft a timeline, evidence, root cause and follow-ups from the same commands.
- **Triage with Claude…**: right-click a finding in the [Findings](#findings) tab. Claude reads the finding and the code around it, says what it means here and whether it looks reachable, and proposes a patch as a diff; it edits a file only if you agree, and wtf makes it ask before each edit.
- **Ask Your History… (Claude)**: from the palette, or the {{mac:sparkles|other:star}} button in [History](#history)'s **Saved** view. The Claude pane gets `/wtf:history ` for you to say what you're after ("the command I used to rotate the RDS cert last month"); Claude searches your saved commands and their context, and can type the one you want at the prompt.
- **Command from Plain English…**: from the palette, {{mac:the Edit menu|other:the window menu}} or the terminal's right-click menu. The Claude pane gets `/wtf:cmd ` for you to finish ("delete evicted pods in all namespaces"); Claude types one command at the shell's prompt, without pressing Enter.
- **Investigate…**: from the palette, the menus or the right-click menu. The Claude pane gets `/wtf:investigate ` for you to say what's wrong ("why is checkout-api crashlooping in staging?"). Claude looks with read-only commands from wtf's list, each shown in the shell as a normal command block, and asks you before every one.

The Claude pane opens if the shell doesn't have one yet. Text is typed only once Claude Code is ready for a prompt: never while it starts (its first question in a new folder is whether you trust it) or while it works.

## Review infrastructure changes

Before you apply a change, see what it will do. Right-click the output of `terraform plan` (or `tofu`, `terragrunt`), `kubectl diff` or `helm diff` → **Review Changes…**, use **Review Last Command's Changes…** in the palette, or right-click a saved plan in the Files tab → **Review Terraform Plan…**. A binary plan is read with `terraform show -json` (or `tofu`) in its folder, which changes nothing; a `terraform show -json` file is read directly.

wtf reads the output itself, without AI, and lists every change as a fact:

- `+` create, `~` change, `-/+` replace, `-` destroy, `<=` read, `→` moved;
- which attributes change, and which ones force a replacement (`engine_version forces replacement`);
- why, when the output says (not in configuration, tainted);
- changes to access control, marked **[IAM]**, **[Security group]**, **[RBAC]**, **[Network policy]** or **[Secret]** by resource type or kind.

The counts at the top are wtf's; Terraform's own line ("Plan: 2 to add, 1 to change, 2 to destroy") is shown under them, counting a replacement as one add and one destroy.

**Summarize with Claude** closes the review and asks the shell's Claude pane (`/wtf:review`): Claude reads the same facts through wtf's tools and writes a plain-English summary of what applying it will do, why each replacement and destruction happens, and what each access change grants or removes. See [Ask Claude](#ask-claude).

## What is sent

- **Only what you ask.** Claude Code runs only in a Claude pane you opened, and you see everything it does there. wtf never starts it in the background.
- **Secrets are removed from wtf's tools.** Everything `pane_context`, `recent_commands`, `review_changes`, `run_readonly` and `type_at_prompt` return goes through the same patterns as [Hide Secrets](#secrets): access keys, tokens, JWTs, passwords in URLs and settings, private keys become `[REDACTED]`. It's best effort. Claude Code's own tools (reading files, its own Bash) aren't filtered by wtf.
- **Where it goes** is up to Claude Code and your account: it sends to Anthropic (or the provider your Claude Code is set up for) under its own terms and settings.
- **Every command and file change asks.** wtf makes Claude Code ask before each command it runs or types in your shells and each file it changes (see [Ask Claude](#ask-claude)), unless you turned on `claude-auto-mode`.

AI never runs a command that changes anything on its own. Fixes are typed at your prompt for you to read, edit and run.

## Upgrading from AI providers

wtf 1.2 and earlier sent requests to AI providers you set up (`ai-default`, `ai-fast` and `[ai.providers.<name>]` in `config.toml`). Those settings are ignored now; Settings shows a note while they're in the file, and you can delete them. API keys saved by those versions stay in the {{mac:Keychain under “wtf AI” (remove them in Keychain Access)|other:keyring under “wtf AI” (remove them in Passwords and Keys, or with `secret-tool clear service "wtf AI"`)}}.
