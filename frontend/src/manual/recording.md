# Recording and saving a terminal

For a postmortem, a ticket or a how-to: record a terminal as it happens, or save what's already in it.

## Recording

Press **Start Recording…** in the side pane's [Share & Record](#share) tab, or right-click the terminal or its tab → **Start Recording…** (also {{cmd:record.toggle}} and **Shell → Start or Stop Recording…**). Pick where to save the `.cast` file, and everything the terminal shows from then on is recorded with its timing, until you stop it the same way or the shell exits. While recording, the tab has a red dot and the address bar shows **REC** and how long it's been going. Click it to stop.

The file is an [asciinema](https://asciinema.org) recording (asciicast v2). Play it with `asciinema play file.cast`, convert it to a GIF with `agg`, or upload it to asciinema.org. A recording:

- is written by wtf as the output arrives, and doesn't slow the terminal down. It's saved to disk every second, so a crash loses at most the last second.
- has the terminal's output and size changes, **not what you type**. A password you type at a prompt isn't in it, but anything the terminal shows is.
- keeps output **as is**: [Hide Secrets](#secrets) doesn't apply to it. The file is readable only by you, but check it before you share it.
- carries on if you move the tab to another window.

## Saving what's there

- **Save Output as Text…** (the Share & Record tab, a tab's right-click menu, {{cmd:export.text}} or the Shell menu): the whole scrollback as plain text, with soft-wrapped lines joined.
- **Save Commands as Markdown…** ({{cmd:export.markdown}}): a transcript with one section per command, giving the command, whether it succeeded or its exit code, when it started, how long it took, and its output in a code block. It's ready to paste into a postmortem or a ticket. It needs [shell integration](#shell-integration) (bash or zsh). Output that has left the scrollback is marked as such.

With [Hide Secrets](#secrets) on, saved text and transcripts have secrets replaced with `[REDACTED]`. Saved files are readable only by you.
