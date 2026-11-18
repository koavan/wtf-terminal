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

## Approvals

When Claude Code or Codex asks your permission (to run a command, change a file, or use a tool), or asks you a question, the request shows in two places, from every window and the background. You don't have to find the agent's tab to answer it.

- **A card over the terminal**, bottom right of every window. The oldest request is on top, with "1 of 3" and the others peeking out under it; answer it and the next one slides up. It never takes the keyboard from your shell: click its buttons, or press {{cmd:approval.allow}} to allow it. A request from an agent whose pane is in front of you has no card: its own dialog is right there. The arrow on the card collapses the deck to a "3 waiting for you" pill, and a new request opens it again. Turn the cards off with **Float what agents wait on you for over the terminal** in **Settings** → **General**, or `approval-cards = false` in `config.toml`.
- **The Agents tab**, under the agent asking, indented, with a colored line saying what kind it is (**wants to run**, **wants to edit**, **wants to use**, **asks you**). The tab's heading counts them: "2 waiting for you".

Each one shows:

- which agent asks (and which of its sub-agents, if one does);
- what kind of request it is, with Claude Code's description of the command;
- the command it would run, or the file it would change, with the lines an edit removes and adds;
- **where the command would act** (see below);
- **Allow ▾**, **Deny ▾** and **Go to Agent**.

**Allow** is **Yes** in Claude Code's own dialog: it runs this once. **Deny** is **No**: Claude Code doesn't run it and stops its turn, waiting for you. The ▾ next to each holds the dialog's other choices:

- **Allow ▾** lists what Claude Code offers for *Yes, and don't ask again*, in its own words, for example "Always allow Bash(npm test:\*) in this project" or "Accept all edits this session". Choosing one allows the request and saves that rule (or mode) exactly as Claude Code would, in the same settings file. wtf never makes up a rule: when Claude Code offers none, there's no ▾.
- **Deny ▾ → Deny and Tell Claude…** opens a small box where you type what Claude should do instead ("use pnpm instead"). It's refused, and Claude gets your words and carries on with them, rather than stopping its turn. This is *No, and tell Claude what to do differently*. The box takes the keyboard only after you open it: **Return** sends, **Esc** cancels. What you type reaches Claude exactly as you wrote it.

**Go to Agent** shows its pane. Claude Code's dialog stays open in its pane while the request is waiting in wtf, and whichever you answer first applies; the other goes away. Answering in the pane (**Enter**, a choice's number, or **Esc**) takes the request away from the card and the Agents tab at once, even while the command it allowed is still running.

{{cmd:agents.approval}} goes to the next agent asking, with the Agents tab open.

**The notification.** When a request comes while you're looking elsewhere, its notification shows the command and where it would act, with **Allow** and **Deny** buttons: you can answer without coming back to wtf. {{mac:Without notifications allowed for wtf, there are no buttons (click it to go to the agent).|other:The buttons need a notification daemon that shows them (GNOME's, KDE's, dunst, mako and most others do) and libnotify 0.7.9 or later; otherwise it's a plain notification.}} Answered anywhere else, even in the agent's own pane, the notification goes away.

**Codex** asks the same way, with one difference: it shows its own approval prompt only once wtf has listed the request, so wtf doesn't wait for your answer there. **Allow** and **Deny** press the keys its prompt names (**y** for *Yes, proceed*, **Esc** for *No*) in its pane, as if you did. **Allow ▾** offers *Don't ask again for this command this session* (for a file change, *for these files*), which presses **a**. **Deny and Tell Codex…** presses **Esc**, which stops Codex's turn, then types what you wrote into its prompt and sends it. A Codex file change (`apply_patch`) shows the files and the lines it changes.

**Questions.** When Claude or Codex asks you a multiple-choice question, it's listed here too, with the question and its options as buttons (on its notification too): click one to answer, as choosing it in the agent's own dialog. To answer Claude in your own words (its *Type something*), click **Other…**: a small box opens for your answer, which goes back as if you'd typed it in its dialog. Several questions at once, or a question that takes several choices, are answered with a form on the floating card: each question with its options (one to pick, or checkboxes when it takes several) and, for Claude, a field for your own words instead. **Submit** ({{cmd:approval.allow}}) sends them all once every question has an answer; Claude gets several choices as one answer ("Apple, Banana"), as its own dialog sends them. The Agents tab lists the questions and their options; answer them on the card or in the pane (**Go to Agent**). Your own answer to Codex (its **None of the above** with a note) is typed in its pane. Codex asks these in Plan mode; clicking an option presses its number in Codex's pane, as if you did.

Only the agents' own permission prompts show here: wtf doesn't add any. A claude or codex you start yourself asks exactly what it asked before, under its own permission mode and approval policy; wtf only shows the same request in a second place.

### Where a command would act

For commands that act on infrastructure, the request lists where they would run, as facts from what wtf already knows about the agent's shell (the same as its [context badges](#context-badges)):

| Command | Shows |
|---|---|
| `kubectl`, `oc`, `helm` | the context, its cluster and the namespace |
| `aws`, `sam`, `cdk` | the profile, account and region |
| `gcloud`, `gsutil`, `bq` | the project |
| `az` | the subscription, its name and id |
| `docker`, `docker-compose` | the context and the daemon it talks to |
| `terraform`, `tofu`, `terragrunt` | the folder and the workspace |
| `ssh` | the host, and the jump host |
| `git push` | the branch, where it pushes, and whether it forces |

What the command says wins over the shell, and each fact says where it came from: `kubectl: context prod-eks (from --context), namespace payments (from -n)`. wtf follows `--context`, `--kube-context`, `--kubeconfig`, `-n`, `-A`, `--profile`, `--region`, `--project`, `--configuration`, `--subscription`, docker's `-c` and `-H`, `-chdir=`, `NAME=value` before a command, `export`, `cd … &&`, `env`, `sudo`, `aws-vault exec`, `kubectl config use-context`, `docker context use`, `az account set` and `sh -c '…'`. When the command sets one in a way wtf can't read (a variable, `$(…)`, `eval`), it says *not known from this command* rather than showing the shell's.

These are facts, not warnings: wtf doesn't label a context as production or dangerous. Nothing is asked of a cloud to work them out (Docker's contexts and Azure's subscriptions come from `~/.docker` and `~/.azure`), except the AWS account, which wtf looks up the way the AWS badge does when a profile's files don't say; the request shows *asking AWS…* until the answer comes, then the account. Secrets are hidden in all of it.

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

## Runners: agents on another machine

A **runner** is a Linux machine you reach over SSH where Claude Code agents can run, signed in to **your own Claude account** on that machine. wtf never uses or asks for an API key for them. Runners are listed under **Runners** at the bottom of the Agents tab.

**Add one**: **Add Runner…** at the bottom of the Agents tab, or right-click a host in the [SSH tab](#ssh-hosts) → **Use as Agent Runner…**. The machine must already be one of your SSH hosts, and wtf must be able to connect without asking anything: with a key (loaded in your agent, or without a passphrase) and its host key already accepted. If it isn't accepted yet, the runner says so; **Open a Shell There…** connects once in a tab so you can check and accept it. Adding a runner changes nothing on it.

wtf then checks it and shows what it found, as facts:

- the system (Linux, arm64 or amd64), tmux, and the Claude Code and wtf-runner versions there;
- who Claude Code is signed in as there, the plan, and until when the sign-in lasts;
- that `ANTHROPIC_API_KEY` is set in your login shell there, if it is. Agents wtf starts there don't get it, so they always use your Claude account.

A green dot means it's ready; orange means something is missing, shown in the row with a button for the next step, in this order:

1. **Install tmux…** and **Install Claude Code…** open a tab that runs the install on the runner (tmux through its package manager, which may ask for your password; Claude Code with its official installer), so you see every step.
2. **Sign In…** opens a tab running `claude auth login` on the runner. It shows a link: open it in your browser here, sign in to your Claude account, and paste the code it gives you back into the tab. Your sign-in stays on the runner.
3. **Finish Claude Code Setup…** opens Claude Code there once for its first-run screens (theme and notes; it asks you to trust wtf-runner's own folder). Type `/exit` when it's done.
4. **Install wtf-runner…** copies wtf-runner, Claude Code's hook helper and wtf's status hooks to `~/.local/share/wtf-runner` on the runner, after you confirm. It changes nothing else there. When you update wtf, the runner shows **Update wtf-runner…**.

After a step in a tab, right-click the runner → **Check Again**. wtf checks each runner once when the Agents tab first shows, and when you ask; it never polls.

Right-click a runner for **Check Again**, **Open a Shell There**, **Sign In Again…**, **Rename…** and **Remove Runner…**. Removing it only forgets it in wtf, unless you also tick **Also delete ~/.local/share/wtf-runner there**. Claude Code, its sign-in and your files stay as they are.

### Agents on a runner

**Start one**: **Start Claude Code…** on a ready runner's row (or right-click it), or **On a Runner…** in {{cmd:agents.start}}. Give a folder on the runner (`~/…` works), a first prompt if you like, the permission mode, and a name. **Start in Background** leaves it running there; **Start and Open** also shows it in a tab. It runs in a tmux session on the runner, with your login shell's tools, signed in to your Claude account there: `ANTHROPIC_API_KEY` is removed from its environment. It keeps running when you close the tab, quit wtf or this computer sleeps.

The first time Claude Code runs in a folder it asks whether you trust it, before it reports anything: the agent shows **Starting** until you answer that in its tab.

Each runner's agents are listed under it, like the agents here: **Needs you**, **Working**, **Done**, **Idle**, or **Ended** once its tmux session is gone, with its folder and last prompt.

- **Click** an agent to open its terminal in a tab (`ssh -t … tmux attach`). Type there as in any tab. Closing the tab, or detaching with Ctrl-b d, leaves it running; another click brings the same tab back.
- **Right-click** for **Peek and Reply…** (the end of its screen, and a line to type into it, sent with Enter), **Rename…**, **Mark as Seen**, **Copy Folder Path**, **End Claude Code…** (closes its tmux session, after you confirm), and **Remove from List** once it has ended.
- **What it asks you** shows exactly like a local agent's: a card over the terminal, a row under the agent in the Agents tab, and a notification, with **Allow**, **Deny**, the always-allow choices, **Deny and tell…** and question forms. Your answer goes to the runner, where Claude Code is waiting on it. A request can wait there for as long as Claude Code lets it (up to a day), even while wtf isn't connected.
- When it needs you or finishes, you get a notification and a sound, as for agents here, saying which runner it's on.

wtf connects to each runner you've added when it starts (one SSH connection each, kept open; nothing is polled), so a runner's agent that needs you reaches you without opening the Agents tab. After a drop it reconnects by itself, at once when your computer wakes up{{mac:|other: or within half a minute}}; meanwhile the runner's row says **Not connected** and why, and right-click → **Reconnect Now** tries straight away.

When wtf-runner on the runner restarts (you update it, say) while an agent waits for you, the request comes back within a minute and can be answered from wtf as before. An agent that ended stays listed for 3 days, with its last screen in its tab, then goes.

## Without opening the side pane

- {{cmd:agents.pick}} lists the same agents in the command palette, most urgent first: needs you, then done, working and idle. Type to filter: `needs`, `done`, `working`, `idle`, or part of a name, tab title, folder, branch or prompt.
- {{cmd:agents.next}} goes to the next agent that needs you, then to the next one that's done, round and round.
- The tab strip shows a badge on every tab running Claude Code or Codex: {{mac:an orange speech bubble|other:an orange !}} needs you, {{mac:a green check|other:a green ✓}} done, {{mac:a sparkle|other:✦}} working, {{mac:a grey sparkle|other:a grey ✦}} idle.
- When one needs you or finishes and you're looking at something else (another tab, another window, another app), wtf shows a notification and plays a short sound{{mac: (the first notification asks you to allow wtf's notifications; if you said no, turn them on in **System Settings → Notifications → wtf**)|other:}}: {{mac:*Glass* when it needs you, *Pop* when it finishes|other:your desktop's "new message" sound when it needs you, its "complete" sound when it finishes}}. Several at once make one sound. Notifications don't pile up: each agent has one at most, its latest, which takes its last one's place, and it goes away once it's out of date (the agent is working again, or quit) or when you look at the agent's pane. {{mac:A long command's notification likewise replaces its shell's last one.|other:With libnotify older than 0.7.9 wtf can't replace or take away a notification, so they stay until you dismiss them.}} Turn the sound off with **Play a sound when an agent needs you or finishes** in **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **General**, or `agent-sound = false` in `config.toml`.{{mac:|other: On Linux the notification daemon plays it (so Do Not Disturb applies); if it can't, wtf uses `canberra-gtk-play` when it's installed.}}
- On macOS, the Dock icon shows how many need you.

## How wtf knows

Claude Code reports what it's doing through hooks: small commands it runs when you send a prompt, when it needs your approval (its `PermissionRequest` hook, which also carries the request to its card and the Agents tab, and your answer back), when it finishes, when it starts or finishes a sub-agent, after each tool call and when it quits. wtf never reads the screen to guess.

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
- Claude Code or Codex you start yourself inside tmux or over SSH from a wtf shell: its hooks run where it runs, not in wtf's shell. Start it on a [runner](#agents) instead to see it here.
- fish (no shell integration).
