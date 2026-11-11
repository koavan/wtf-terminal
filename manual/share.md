# Share & Record

The **Share & Record** tab ({{key:sidebar.share}}, the shield icon) puts screen sharing and recording on buttons, so there's no shortcut to remember.

## Hide Secrets

- The **switch** turns [Hide Secrets](#secrets) on and off for every terminal in wtf. While it's on, the switch is green and the address bar says **Secrets hidden**.
- **Turn on whenever wtf starts** saves `redact-secrets = true` in your [settings](#settings). Ticking it also turns Hide Secrets on now. Unticking it only affects the next start, so your secrets never show in the middle of a share.

## Recording

- **Start Recording…** records the terminal you're in (the tab is named above the button) to an asciinema file. While it's recording, the tab shows the time so far and where it's saving, with a **Stop Recording** button. See [Recording and saving a terminal](#recording).
- **Save Output as Text…** and **Save Commands as Markdown…** save what's in the terminal now. Markdown needs shell integration.
- **Also recording** lists the other terminals in this window being recorded. Click one to go to it, or ■ to stop it.
- **Saved in this window** lists the recordings and files you've saved. Click one to show it in {{mac:Finder|other:the file manager}}, or copy its path.

The same actions are in the terminal's right-click menu (**Hide Secrets**, **Start Recording…**) and in a tab's right-click menu.
