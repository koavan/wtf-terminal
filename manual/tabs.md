# Tabs and tab groups

## Tabs

- New tab: {{cmd:tab.new}}. It starts in the folder of the tab you were in. Right-click the **+** for other kinds of tab, such as an [API tab](#api).
- Close: {{cmd:pane.close}} closes the focused pane (the tab, if it's the last one); {{cmd:tab.close}} closes the whole tab; right-click a tab for **Close Other Tabs** and **Close Tabs to the Right**. If a program is still running, wtf asks first.
- Closing the last tab leaves the window open on its **home page** (so does the last shell exiting): **New Tab**, **New API Tab**, **Start Claude Code…** and **New Window**, the tabs you closed in this window to reopen, and the command palette, Workspaces, Agents, the manual and Settings. The side pane and every shortcut work there. To close the window, use {{cmd:window.close}} or its close button.
- Duplicate: {{cmd:tab.duplicate}} (or right-click a tab) opens a new tab right after it, in its group, in the folder of its focused pane and with its [git identity](#git-identities). Duplicating an API tab copies its request.
- Reopen a closed tab: {{cmd:tab.reopen}} (in its old folder).
- Switch: {{cmd:tab.next}} and {{cmd:tab.prev}}, `Ctrl+Tab`, or {{mac:⌘1–⌘8 (⌘9 for the last)|other:Alt+1–8 (Alt+9 for the last)}}.
- Move: drag a tab, or {{cmd:tab.moveRight}} / {{cmd:tab.moveLeft}}.
- Rename: double-click the tab, or {{cmd:tab.rename}}. An empty name, or right-click → **Use Automatic Name**, goes back to the automatic one (the folder, or the running program).
- See every tab at once: {{cmd:tab.overview}} shows live previews of each tab; click one to go there.

A tab's icon shows what's running in it. A dot appears when a background tab has new output, and a ✓ or ✗ when a command finished there while you were elsewhere. A red dot means the tab is being recorded (see [Recording and saving a terminal](#recording)).

Right-click a tab to record it, to save its output as text or its commands as Markdown, or to choose the [git identity](#git-identities) it works as on GitHub (**Use …**, **Stop Using Identity**).

## Tab groups

Groups work like Chrome's: a colored chip on the tab strip with its tabs next to it.

- {{cmd:tab.group}} puts the current tab in a new group and lets you name it and pick a color.
- Right-click a tab → **Add tab to group** to join an existing one, or **Remove from group**.
- Click the chip to collapse or expand the group; right-click it to rename it, recolor it, add a tab, ungroup it or close all its tabs.
- **Broadcast input to group** (in the chip's menu) sends what you type to every tab of the group. See [Broadcast input](#broadcast).

Opening several SSH hosts at once creates a group for you — see [SSH hosts](#ssh-hosts).

## Windows

- New window: {{cmd:window.new}}. Close it: {{cmd:window.close}}.
- Drag a tab down out of the tab strip and let go to open it in its own window. Its shells keep running and its scrollback comes along.
- Right-click a tab → **Move to new window** (also {{cmd:tab.toNewWindow}}), or **Move to “…”** another window.

Each window's tabs, groups, splits and folders are saved and restored the next time you open wtf. See [Sessions and windows](#sessions).
