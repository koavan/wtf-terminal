# Agents: every Claude Code

The **Agents** tab ({{key:sidebar.agents}}, the first icon in the side pane) lists every Claude Code running in your wtf shells, in every window, so you don't have to visit each tab to see which one is still working and which one is waiting for you.

Each row shows:

- a coloured dot and what it's doing:
  - **Needs you** (orange): it's asking you something, usually to approve a command or a file change. The row shows what it asks.
  - **Done** (green): it finished while you were looking elsewhere. It stays done until you look at its pane.
  - **Working** (accent colour).
  - **Idle** (grey): waiting for your next prompt.
- the tab's title and the folder it runs in;
- the first line of your last prompt (secrets hidden);
- how long it's been in that state.

The most urgent come first: needs you, then done, working and idle. Click a row to go to that window, tab and pane; right-click for **Copy Folder Path**.

## Without opening the side pane

- {{cmd:agents.pick}} lists the same rows in the command palette. Type to filter: `needs`, `done`, `working`, `idle`, or part of a tab title, folder or prompt.
- {{cmd:agents.next}} goes to the next agent that needs you, then to the next one that's done, round and round.
- The tab strip shows a badge on every tab running Claude Code: {{mac:an orange speech bubble|other:an orange !}} needs you, {{mac:a green check|other:a green ✓}} done, {{mac:a sparkle|other:✦}} working, {{mac:a grey sparkle|other:a grey ✦}} idle.
- When one needs you or finishes and you're looking at something else (another tab, another window, another app), wtf shows a notification.
- On macOS, the Dock icon shows how many need you.

## How wtf knows

Claude Code reports what it's doing through hooks: small commands it runs when you send a prompt, when it needs your approval, when it finishes and when it quits. wtf never reads the screen to guess.

- **Ask Claude** panes ({{key:claude.ask}}) report through wtf's plugin. See [Ask Claude](#ask-claude).
- **A `claude` you type yourself**: wtf's [shell integration](#shell-integration) (bash and zsh) runs it as `claude --settings <wtf's status hooks>`. Those settings hold only the hooks that report its state: no tools, no slash commands, and no permission rules, so your own claude asks you exactly what it asked before. They apply to that run only; nothing is written to `~/.claude`, and your own settings and hooks still apply.

wtf leaves `claude` alone when:

- the command line has its own `--settings` or `--plugin-dir`;
- your `.bashrc`, `.bash_profile` or `.zshrc` defines a `claude` alias or function (yours wins);
- `agent-status = false` is set in `config.toml`, or **Show every claude you start in the Agents tab** is off in **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General**. This applies to new panes; only Ask Claude panes are listed then.

A Claude Code that quits, or is killed, disappears from the list when its shell is back at its prompt or its pane closes.

## What it doesn't cover yet

- Other coding agents (Codex, OpenCode and others).
- Claude Code started inside tmux or over SSH from a wtf shell: its hooks run where it runs, not in wtf's shell.
- fish (no shell integration).
