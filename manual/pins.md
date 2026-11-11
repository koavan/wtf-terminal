# Pinned output

The **Pinned** tab ({{key:sidebar.pins}}) keeps output in view while you keep working: a pod list next to the deploy you're watching, an error message while you look for its cause, the IP addresses you'll need in a minute.

## Pinning

- **A command's output**: click its ✓/✗ mark, or right-click its output, and choose **Pin Output**. {{cmd:term.pinOutput}} in the command palette pins the last finished command's output.
- **Part of the output**: select it, right-click, **Pin Selection**.

The side pane opens on the Pinned tab, and the keyboard stays in your terminal. Pinning the same command again brings its pin up to date instead of adding another one.

A command **still running** when you pin it (`kubectl get pods -w`, a build, `tail -f`) keeps its pin up to date. Its pin refreshes every second or two while the Pinned tab is on screen, and gets its final output, exit status and duration when the command finishes. When the tab isn't showing, nothing is done in the background.

## A pin

Each pin shows the command (or "Selection"), how it went (✓ or ✗ with its exit code, and how long it took) and the tab it came from. The output keeps its own scrollbar; drag its bottom edge to make it taller.

- **Copy**, **Collapse** and **Unpin** are the buttons on its header. Double-clicking the header also collapses it.
- Right-click the header for **Show in Terminal** (scrolls the terminal to that command, while its tab is open), **Copy Command**, **Save as Snippet…**, and the rest.
- The trash button at the top unpins everything, after asking.

A pin is a copy of the text. It stays when the command leaves the scrollback, when you clear the terminal, and when you close the tab it came from. It keeps the last 200,000 characters of very long output, where a command's result usually is. A window keeps 30 pins, and pinning another drops the oldest.

Pins live only in that window, in memory. They're never written to disk, because output can hold secrets, so they're gone when the window closes or wtf quits.
