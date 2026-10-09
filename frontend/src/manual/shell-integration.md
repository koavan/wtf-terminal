# Commands, prompts and output

With shell integration (on by default for **bash** and **zsh**), wtf knows where each prompt and command starts and ends. That gives you:

- **✓ / ✗ marks** beside each command, from its exit status.
- **Jump between prompts**: {{cmd:prompt.prev}} and {{cmd:prompt.next}} scroll to the previous or next command.
- **Copy a command's output**: {{cmd:term.copyOutput}} copies what the last command printed.
- **The last command in the address bar**, with its status and how long it took.
- **A notification** when a command that ran at least `notify-after` seconds (10 by default; 0 turns it off) finishes out of sight — its tab is in the background, or the window isn't in front.
- **Command history** per shell in the side pane — see [History](#history).
- **Context badges** for Kubernetes, AWS, gcloud and Terraform — see [Address bar and context badges](#context-badges).

wtf loads a small script after your usual startup files (`.bashrc`, `.zshrc`…); your prompt and settings stay as they are. It works in macOS's built-in bash 3.2 too. Turn it off with `shell-integration = false` in [Settings](#settings); it applies to new panes.

## Other terminal basics

- Copy and paste: {{cmd:term.copy}}, {{cmd:term.paste}}. Set `copy-on-select = true` to copy whatever you select.
- {{cmd:term.selectAll}} selects the whole scrollback.
- {{cmd:term.clear}} clears the screen and scrollback.
- {{mac:⌘-click|other:Ctrl-click}} a link to open it in your browser.
- Text size: {{cmd:font.bigger}}, {{cmd:font.smaller}}, {{cmd:font.reset}}.
- Drop a file from the Files tab onto a terminal to paste its path.
