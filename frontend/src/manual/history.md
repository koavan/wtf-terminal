# History

The History tab ({{key:sidebar.history}}) lists the commands run in the focused pane's shell — or in every pane of the window — newest first, with when each finished, how long it took and its exit code. It needs [shell integration](#shell-integration).

- **Click** a command to put it at the prompt; **double-click** to run it again.
- Right-click: **Run Again**, **Insert at Prompt**, **Show in Terminal** (scrolls back to it), **Copy Command**, **Copy Folder**.
- **🗑** clears the list. After asking, it also removes this session's commands from the shell's own history (↑ / Ctrl-R) — nothing is typed into the terminal, older sessions' history stays, and your history file isn't rewritten without them. Busy shells only have their list cleared.

The list lasts as long as the shell does.
