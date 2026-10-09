# Broadcast input

Broadcast sends what you type in one pane to other panes too — to run the same commands on several servers at once.

## Turning it on

- **For a tab group**: right-click a tab in the group (or open the group chip's menu) → **Broadcast Input to Group**. Every pane of every tab in the group receives your typing.
- **For a tab's split panes**: right-click the tab → **Broadcast Input to Panes**.
- {{cmd:broadcast.toggle}} turns it on for the current tab's group (or its splits), and off again.
- Fastest for SSH: select several hosts in the [SSH tab](#ssh-hosts), right-click → **Open … in a Tab Group with Broadcast Input**.

## While it's on

- An orange bar above the terminal says where your typing goes, with a **Stop** button.
- Panes that receive input are outlined in orange, and their tabs show a broadcast icon.
- Only what you type and paste is sent. Answers the terminal gives a program on its own (cursor position, mouse clicks) stay in their pane.

Broadcast never survives a restart. Closing or ungrouping the tabs ends it.

> Check the bar before pressing Enter on anything destructive: everything you type goes everywhere it lists.
