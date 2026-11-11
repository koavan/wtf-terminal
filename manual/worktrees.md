# Worktrees: one repository, several checkouts

A git worktree is another folder with the same repository checked out on a different branch. Every worktree shares one `.git`, so there's no second clone, and a commit made in one is in the repository straight away. But each has its own files, branch, build output and test runs.

That's what you want when two Claude Code agents (or you and an agent) work on one repository at once. In a single folder they edit the same files, run each other's half-finished changes in their tests, and sweep each other's work into their commits. In their own worktrees they can't touch each other.

## New worktree

{{cmd:worktree.new}}, or in the [Files](#files) tab the worktrees button at the end of the git bar, or right-click any row → **Worktrees** → **New Worktree…**. It's for the repository the focused shell's folder (or the Files tab's folder) is in.

- **Branch**: type a new branch's name, or pick an existing branch to check out. Branches already checked out in a worktree aren't offered (tick **Detached** to see them all): git allows a branch in only one at a time.
- **From** (a new branch only): the branch or commit it starts at. It starts as the branch you're on.
- **Detached**: check out a branch or commit on no branch, for a look. Git allows a branch in only one worktree at a time, so this is how you get a second folder on a branch that's already checked out, such as `main` while you're on it. Commits made there aren't on any branch: make one there (`git switch -c <name>`) before committing.
- **Folder**: where it goes. wtf suggests `<repository>.worktrees/<branch>` beside the repository, with `/` and spaces in the branch turned into `-`. Set `worktree-dir` in `config.toml` to put them all somewhere else, as `<worktree-dir>/<repository>/<branch>`, for example `worktree-dir = "~/worktrees"`. The folder can't be inside the repository's own checkout, and must not exist yet (or be empty).
- **Then**: **Open a Tab** there, **Start Claude Code in a Tab**, or **Start Claude Code in the Background** (see [Agents](#agents)), with an optional first prompt.

**Create…** shows the exact git command, such as `git -C ~/code/api worktree add -b feat/login ~/code/api.worktrees/feat-login main`. Nothing runs until you click **Create**. The new tab opens right after the one you're in, in its tab group.

## Open a worktree

{{cmd:worktree.open}}, or **Worktrees** → **Open Worktree** in the Files tab, lists the repository's worktrees. **Open** goes to a tab whose shell is already in that worktree, in any window, or opens one. **Start Claude Code** opens a tab there running `claude`.

## Remove a worktree

{{cmd:worktree.remove}}, or **Worktrees** → **Remove Worktree** in the Files tab. Before anything runs, wtf shows:

- the command (`git worktree remove <folder>`);
- what isn't committed there (`git status --short`), so you know what's at stake;
- how many shells are open in that folder (they stay open).

**Remove** runs it. Git removes the folder and its record of the worktree. **The branch is never deleted**: its commits stay in the repository, ready to merge, push or check out elsewhere.

If the worktree has changed or untracked files, git refuses. wtf shows git's reason and asks again: **Remove Anyway** runs `git worktree remove --force`, and those changes are lost.

wtf won't remove:

- the repository's main checkout (the folder with `.git` in it);
- a worktree locked with `git worktree lock` (unlock it first);
- a worktree a Claude Code agent is working in. End it first (Agents tab → right-click → **End Claude Code…**).

A worktree whose folder you deleted yourself is shown as **folder gone**. Removing it runs `git worktree prune`, which only drops git's record of it (and of any other worktree whose folder is gone).

## Branches in the Agents tab

Each agent in the [Agents](#agents) tab shows the branch its folder is on, with ↑ and ↓ for commits ahead of and behind its upstream (as of your last `git fetch`; wtf never fetches). Hover it to see whether the agent is in a worktree and which upstream it's compared with. wtf reads this when the agent starts and each time it finishes a turn, so it follows its commits. {{cmd:agents.pick}} searches branches too.
