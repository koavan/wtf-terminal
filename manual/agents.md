# Agents: Claude Code and Codex

The **Agents** tab ({{key:sidebar.agents}}, the first icon in the side pane) lists every Claude Code and Codex running in your wtf shells, in every window, so you don't have to visit each tab to see which one is still working and which one is waiting for you.

Each row shows:

- a coloured dot and what it's doing:
  - **Needs you** (orange): it's asking you something, usually to approve a command or a file change. The row shows what it asks.
  - **Done** (green): it finished while you were looking elsewhere. It stays done until you look at its pane.
  - **Working** (accent colour). When Claude Code ends its turn with work still running in the background (a shell or monitor it started in the background, or a background sub-agent), it's still working: the row says so, as in **Working · 2 in background**, and it turns **Done** only after a turn ends with nothing left running.
  - **Idle** (grey): waiting for your next prompt.
- its name, if you gave it one (otherwise the tab's title), and the folder it runs in;
- the branch that folder is on, with ↑ and ↓ for commits ahead of and behind its upstream, so two agents in [worktrees](#worktrees) of one repository are easy to tell apart;
- the first line of your last prompt (secrets hidden);
- how long it's been in that state.

Agents are listed in the order they started, and a row stays where it is whatever its state, so you can find one at a glance. Click a row to go to that window, tab and pane; right-click for **Copy Folder Path**.

## Codex

A `codex` you start in a wtf shell is an agent like a Claude Code: it gets a row (marked **Codex** in {{cmd:agents.pick}} and in its notifications), a tab badge, a name, the **ungrouped agents** tab group, the choice to keep it running in the background when you close its tab, and a **Resume** after wtf quits (with `codex resume`). Its dialogs and menus name it **Codex** where they say **Claude Code** below. What's only for Claude Code: [Ask Claude](#ask-claude) and its plugin, {{cmd:agents.start}}, and starting an agent from [New Worktree…](#worktrees). See **How wtf knows** below for how Codex is hooked up.

## The ungrouped agents tab group

Every tab running Claude Code joins its window's **ungrouped agents** [tab group](#tabs), so your agents sit together in the tab strip. wtf makes the group, in the next free colour, when the first agent starts, and a tab leaves it when its Claude Code ends (unless another agent runs in one of its panes). The group goes when its last tab leaves. A tab that was already in another group stays in it. Agents kept running in the background aren't in a window's tabs, so they're left out until you open them in a tab again. An agent goes back into the group its tab was in when it went to the background (made again if it's gone), and only joins **ungrouped agents** if it wasn't in one.

You can collapse, rename or recolour the group like any other. Turn this off with **Group tabs running Claude Code or Codex under "ungrouped agents"** in **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General**, or `agent-group = false` in `config.toml`.

## Name an agent

Double-click an agent (or right-click → **Rename…**) and give it a name, such as `deploy fixer` or `reviewer`. The name shows in the Agents tab, in {{cmd:agents.pick}} and in its notifications, and becomes the tab's title too. Rename the tab yourself later and the Agents tab shows both: the name first, the tab's title on the second line. Leave the name empty to go back to the tab's title (and the tab to its automatic title, if it still carries the agent's name).

The name stays with the agent's shell: quit Claude Code and start it again in the same tab, and it keeps its name. It's forgotten when the shell closes or wtf quits.

## Sub-agents

When Claude Code hands work to sub-agents (its Agent tool: Explore, Plan, general-purpose or your own), its row gets a small line such as **▸ 3 sub-agents · 1 working**. They're hidden until you click it; click again (or right-click the agent → **Hide Sub-agents**) to fold them away. wtf remembers which ones you opened while it runs.

Each sub-agent shows:

- its type and what it was asked to do (the task's short description);
- while it works (accent dot): its latest step, such as `Read deploy_test.go`, `Grep TODO` or `Bash: go test ./...`;
- once it's done (grey dot): the first line of its report;
- how long ago it started, or finished.

Secrets are hidden in all of it. A sub-agent's steps don't change its agent's state, but one still running in the background when the agent's turn ends keeps the agent **Working** (counted in its **N in background**) until a later turn ends without it. When that work finishes, Claude Code wakes itself with a note of its own; the row keeps showing your last prompt, and finished sub-agents aren't cleared by it. Finished sub-agents are cleared when you send the agent its next prompt; running ones stay. Click a sub-agent to go to its agent.

## Keep agents running after you close their tab

Close a tab or pane running Claude Code (or Codex) and wtf asks:

- **Keep Running in Background**: it keeps working where you can't see it, with its shell and scrollback. The Agents tab lists it under **Background**, with its badge and notifications as before.
- **End Claude Code** (**End Codex**; **End Them** for a mix): it ends with the tab, as before.

Tick **Don't ask again** to always do the same; **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General** → **Closing a tab running Claude Code or Codex**, or `agent-close` (`ask`, `background` or `end`) in `config.toml`, changes it back. Closing a whole window asks once for all its agents.

Background agents run while wtf does: quitting wtf ends them (it asks first, listing them), and offers them for resuming next time (see **After wtf quits** below). Closing the last window you can see, with agents in it or in the background, is quitting.

For each agent, in the Agents tab (the eye button, or right-click):

- **Open in Tab** (or click the row): back into a tab of the window in front, scrollback and all.
- **Peek and Reply…**: its screen as it is now (secrets hidden), updated every second, and a box to answer it: what you type is sent with {{mac:Return|other:Enter}}; **{{mac:Return|other:Enter}}** and **Esc** send just that key, for Claude Code's own prompts (Return accepts what its prompt has selected, Esc cancels). You see the screen and choose; wtf never answers for you.
- **End Claude Code…** (or **End Codex…**): ends it and its shell, after asking.

## After wtf quits

Quitting wtf ends every Claude Code running in it, in tabs and in the background. Their conversations aren't lost: Claude Code keeps each one, and `claude --resume` picks it up where it stopped. wtf remembers which ones were running so you can bring them back with a click, and never resumes them on its own.

- **A tab that ran Claude Code** comes back with your other tabs, with a bar at the top of its pane: *Claude Code was running here when wtf quit.* (or *Codex …*). **Resume** types `claude --resume <its session>` (`codex resume <its session>` for Codex) into that shell (with a `cd` to the folder it started in, if the shell is elsewhere; a new tab if the shell is busy). **Dismiss** stops offering it. Until you choose, the bar comes back each time wtf restarts. Starting Claude Code in that shell yourself replaces the offer.
- **Agents running in the background** are listed in the Agents tab under **Resumable**: {{mac:**Resume in Tab**, **In Background**, or **Dismiss**|other:the play button resumes one in a tab; right-click for **Resume in Tab**, **Resume in Background** or **Dismiss**}}. With more than one, **Resume All…** lists them first, then resumes each in the background.

A resumed agent gets back the name you gave it. A Codex agent resumes the same way, with `codex resume <its session>`. Agents whose conversation is gone (its transcript deleted from `~/.claude`, or `~/.codex` for Codex) or whose folder is gone aren't offered. Ask Claude panes aren't offered either: open Ask Claude again instead. This needs `agent-status` (on by default), which is how wtf learns each agent's session.

## Start an agent

{{cmd:agents.start}} (also the **+** in the Agents tab): pick a folder, give a first prompt if you like, and **Start in a Tab** or **Start in Background**. It runs `claude` (with your prompt) in a new shell there, with the status hooks above.

To run several agents on one repository without them editing the same files, give each its own [worktree](#worktrees): {{cmd:worktree.new}} can start Claude Code in it straight away.

## Without opening the side pane

- {{cmd:agents.pick}} lists the same agents in the command palette, most urgent first: needs you, then done, working and idle. Type to filter: `needs`, `done`, `working`, `idle`, or part of a name, tab title, folder, branch or prompt.
- {{cmd:agents.next}} goes to the next agent that needs you, then to the next one that's done, round and round.
- The tab strip shows a badge on every tab running Claude Code or Codex: {{mac:an orange speech bubble|other:an orange !}} needs you, {{mac:a green check|other:a green ✓}} done, {{mac:a sparkle|other:✦}} working, {{mac:a grey sparkle|other:a grey ✦}} idle.
- When one needs you or finishes and you're looking at something else (another tab, another window, another app), wtf shows a notification and plays a short sound: {{mac:*Glass* when it needs you, *Pop* when it finishes|other:your desktop's "new message" sound when it needs you, its "complete" sound when it finishes}}. Several at once make one sound. Turn the sound off with **Play a sound when an agent needs you or finishes** in **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General**, or `agent-sound = false` in `config.toml`.{{mac:|other: On Linux the notification daemon plays it (so Do Not Disturb applies); if it can't, wtf uses `canberra-gtk-play` when it's installed.}}
- On macOS, the Dock icon shows how many need you.

## How wtf knows

Claude Code reports what it's doing through hooks: small commands it runs when you send a prompt, when it needs your approval, when it finishes, when it starts or finishes a sub-agent, after each tool call and when it quits. wtf never reads the screen to guess.

Codex reports through its matching lifecycle hooks. The shell integration adds wtf's command hooks to that `codex` run with temporary `--config` arguments, so nothing is written to `~/.codex`. Its session, prompt, approvals, completion, interrupts, tool calls and sub-agents use the same rows and badges; saved Codex conversations resume with `codex resume`.

- **Ask Claude** panes ({{key:claude.ask}}) report through wtf's plugin. See [Ask Claude](#ask-claude).
- **A `claude` you type yourself**: wtf's [shell integration](#shell-integration) (bash and zsh) runs it as `claude --settings <wtf's status hooks>`. Those settings hold only the hooks that report its state: no tools, no slash commands, and no permission rules, so your own claude asks you exactly what it asked before. They apply to that run only; nothing is written to `~/.claude`, and your own settings and hooks still apply.

wtf leaves `claude` alone when:

- the command line has its own `--settings` or `--plugin-dir`;
- your `.bashrc`, `.bash_profile` or `.zshrc` defines a `claude` alias or function (yours wins);
- `agent-status = false` is set in `config.toml`, or **Show every claude or codex you start in the Agents tab** is off in **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General**. This applies to new panes; only Ask Claude panes are listed then.

wtf leaves `codex` alone when your startup files define a `codex` alias or function, and when `agent-status` is off (the same setting covers both). Unlike `claude`, a `codex` with its own `--config` arguments still gets wtf's hooks added.

A Claude Code or Codex that quits, or is killed, disappears from the list when its shell is back at its prompt or its pane closes.

## What it doesn't cover yet

- Other coding agents (OpenCode and others).
- Claude Code or Codex started inside tmux or over SSH from a wtf shell: its hooks run where it runs, not in wtf's shell.
- fish (no shell integration).
