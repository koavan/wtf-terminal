# Commands, prompts and output

With shell integration (on by default for **bash** and **zsh**), wtf knows where each prompt and command starts and ends, so each command is a **block**: its prompt, the command, and what it printed. That gives you:

- **✓ / ✗ marks** in the left margin beside each command, from its exit status. Hover one for the exit code, how long it took and when it finished.
- **Durations**: a command that took a second or more shows how long at the end of its prompt line (when the line has room).
- **Actions for any command**: click its mark, or right-click anywhere in its output — see below.
- **Jump between prompts**: {{cmd:prompt.prev}} and {{cmd:prompt.next}} scroll to the previous or next command.
- **Copy a command's output**: {{cmd:term.copyOutput}} copies what the last command printed.
- **The last command in the address bar**, with its status and how long it took.
- **A notification** when a command that ran at least `notify-after` seconds (10 by default; 0 turns it off) finishes out of sight — its tab is in the background, or the window isn't in front.
- **Command history** per shell in the side pane — see [History](#history).
- **Context badges** for Kubernetes, AWS, gcloud and Terraform — see [Address bar and context badges](#context-badges).

wtf loads a small script after your usual startup files (`.bashrc`, `.zshrc`…); your prompt and settings stay as they are. It works in macOS's built-in bash 3.2 too. Turn it off with `shell-integration = false` in [Settings](#settings); it applies to new panes.

## Acting on a command

Click a command's ✓/✗ mark, or right-click in its output. The command's block is highlighted while the menu is open, so you can see which one it's about:

- **Copy Command** — the command line as you typed it.
- **Copy Output** — everything it printed (soft-wrapped lines joined back together). For a command still running, what it has printed so far.
- **Copy Command and Output** — both, as they looked: `$ command` followed by the output. Handy for pasting into a ticket or chat.
- **Select Output** — selects it in the terminal.
- **Pin Output** — keeps a copy in the side pane's Pinned tab while you work on; a running command's pin follows it. See [Pinned output](#pins).
- **Run Again** — types it at the prompt and runs it (when the shell is at its prompt), in this pane only.
- **Save as Snippet…** — keep it, with blanks to fill in next time. See [Snippets and runbooks](#snippets).

Right-clicking a timestamp, address, resource or file:line puts its actions first, and a line holding JSON offers **Pretty-print JSON** — see [Logs in the output](#logs). Right-clicking also offers **Copy** (with a selection), **Paste**, **Select All**, **Find…** and **Clear**. {{cmd:term.copyOutput}} still copies the last command's output from the keyboard.

Output that has scrolled out of the scrollback (`scrollback` in [Settings](#settings)) can't be copied any more; the command itself still can.

## Other terminal basics

- Copy and paste: {{cmd:term.copy}}, {{cmd:term.paste}}. Set `copy-on-select = true` to copy whatever you select.
- {{cmd:term.selectAll}} selects the whole scrollback.
- {{cmd:term.clear}} clears the screen and scrollback.
- {{mac:⌘-click|other:Ctrl-click}} a link to open it in your browser. Timestamps, IP addresses, Kubernetes resources and file:line references have actions too — see [Logs in the output](#logs).
- Text size: {{cmd:font.bigger}}, {{cmd:font.smaller}}, {{cmd:font.reset}}.
- Drop a file from the Files tab onto a terminal to paste its path.
