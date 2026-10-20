# Files

The Files tab ({{key:sidebar.files}}) shows the focused shell's folder as a tree, and follows the shell as it moves.

## Getting around

- Click a folder to expand it; double-click it to `cd` the shell there (when the shell is at its prompt). The tree follows.
- **←** (Back) returns to the folder shown before; **↑** (Up) goes to the parent. Each pane keeps its own Back history. Keys in the tree: {{mac:⌫|other:Backspace}} for Back, {{mac:⌥↑|other:Alt+↑}} for Up; your mouse's back button works too.
- **📌 Pin** keeps the tree on the current folder while the shell moves elsewhere. While pinned, Back and Up move the tree only and leave the shell alone; unpin to follow the shell again.
- Type in **Filter** to narrow the loaded folders, keeping the path to each match.
- Arrow keys move the selection, → expands a folder, ← collapses it, `Enter` acts like a click, `Esc` goes back to the terminal.
- **Collapse all** folds everything back to the top.

## Using files

- **Click a file** to put its path at the prompt (quoted, relative to the shell's folder when it's inside it).
- **Drag a file** into a terminal to paste its path, or onto an [SSH host](#ssh-files) to upload it.
- **Right-click a file**: insert its path, open it in `$EDITOR` (in a split), open it with the default app, copy its path or relative path, reveal it in {{mac:Finder|other:the file manager}}. Certificate files (`.pem`, `.crt`, `.cer`, `.der`) also get **Inspect Certificate…** (see [Tokens and certificates](#inspect)).
- **Right-click a folder**: `cd` there, open it in a new tab or split, insert its path, **Make Root** (pin the tree there), copy its path, reveal it.

Git-ignored files are dimmed. The tree re-reads after each command you run, so new files show up. While the shell is connected to another machine (ssh), the tree says so instead of showing your local files.

## Git

When the folder is in a git repository, the tree shows what's changed:

- A bar under the toolbar shows the **branch**, with ↑ and ↓ for commits ahead of and behind its upstream (hover for the upstream's name). A detached HEAD shows its commit.
- **Changed files** are colored, with a letter at the end of the row: **M** modified (yellow), **A** added, **U** untracked/new (green), **D** deleted, **!** merge conflict (red), **R** renamed. Hover the letter to see whether the change is staged.
- A **dot** on a folder means something inside it changed. Its color is the most pressing kind of change inside, so a conflict shows red.
- **N changes** in the bar lists every changed file in the repository, including deleted ones (struck through), with the folder each is in. Click a file to put its path at the prompt. Click **N changes** again to go back to the tree.
- **Right-click** a file for **Show Changes** (its `git diff`) and **Show History** (`git log --follow`), or a folder for its status, changes and history. They run as git commands in a new tab at the repository's root, so you can scroll, search and run them again.

wtf only reads git's state. Nothing here stages, commits or discards. `git status` runs when the tree refreshes (after each command, or ↻) and never takes git's index lock, so it can't get in the way of a git command you're running. In a huge repository where it takes more than 3 seconds, the bar says the status isn't available.
