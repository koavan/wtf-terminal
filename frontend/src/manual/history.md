# History

The History tab ({{key:sidebar.history}}) lists the commands run in the focused pane's shell — or in every pane of the window — newest first, with when each finished, how long it took and its exit code. **Saved** lists every wtf shell's commands, across windows and restarts, with the context each ran in (below). It needs [shell integration](#shell-integration).

- **Click** a command to put it at the prompt; **double-click** to run it again.
- Right-click: **Run Again**, **Insert at Prompt**, **Show in Terminal** (scrolls back to it), **Copy Command**, **Copy Folder**, **Save as Snippet…** ([Snippets and runbooks](#snippets)).
- **🗑** clears the list. After asking, it also removes this session's commands from the shell's own history (↑ / Ctrl-R) — nothing is typed into the terminal, older sessions' history stays, and your history file isn't rewritten without them. Busy shells only have their list cleared.

**This pane** and **All panes** last as long as the shell does.

## Saved: across windows and restarts

**Saved** lists the commands you ran in any wtf shell over the last 90 days, newest first, each with the context it ran in, as the [address bar](#context-badges) showed it: the Kubernetes context and namespace, the AWS profile, account and region (and the Google Cloud project, Terraform workspace and git branch). These are read from files when the command finishes, never asked of a cloud.

- **Search** finds commands by words in the command or its folder.
- The filters narrow it to **Today**, **Last 7 days** or **Last 30 days**, one **Kubernetes** context, and one **AWS** account, profile or region. Each lists the values your saved commands have, with how many. "What did I run against account 123456789012 today?" is **Today** + that account.
- Click, double-click and right-click work as above, in the focused shell; right-click also has **Forget This Command**.
- **🗑** here forgets every saved command (after asking); each shell's own history stays.

Secrets in commands are replaced with `[REDACTED]` (the [Hide Secrets](#secrets) patterns) before anything is saved. The commands are in `history.jsonl` in wtf's state folder, readable only by you. Clearing a pane's list also forgets that shell's saved commands. `history-days` in [Settings](#settings) sets how long they're kept; 0 saves nothing and removes what was saved.
