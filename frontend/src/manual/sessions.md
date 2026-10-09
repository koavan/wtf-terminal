# Sessions and windows

## Sessions

Each window's tabs, groups, splits, folders and text size are saved as you work and restored the next time you open wtf. Shells start fresh in their saved folders (programs that were running aren't restarted). The session lives in `session.json` in wtf's config folder ({{mac:`~/Library/Application Support/wtf`|other:`~/.config/wtf`}}).

## Windows

- {{cmd:window.new}}; {{cmd:window.close}}.
- Drag a tab out of the tab strip to give it its own window — its shells keep running and its scrollback comes along. Right-click a tab to move it to another window.
- Closing a pane, tab, group or window, or quitting, asks first when a program is still running in it.
