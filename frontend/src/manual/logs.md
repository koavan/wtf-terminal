# Logs in the output

wtf picks out the parts of command output you usually look for in logs, and gives them colors and actions. The output itself is never changed, so copying gives you exactly what the program printed.

## Colors

- **Log levels**: `ERROR`, `FATAL`, `PANIC` and `CRITICAL` in red, `WARN` in yellow, `INFO` in green, and `DEBUG` and `TRACE` faded. Upper-case words count anywhere; lower case counts in `level=error`, `"level":"warn"` and `[info]`.
- **Timestamps** are faded so the message stands out: `2026-10-09T12:34:56Z`, `2026-10-09 12:34:56,123`, syslog's `Oct  9 12:34:56`, web servers' `[09/Oct/2026:13:55:36 -0700]`, and plain times like `12:34:56`.
- **URLs**, **IP addresses** (with a port like `10.0.0.5:8080`, or a range like `10.0.0.0/16`), and **Kubernetes resources** as kubectl prints them (`pod/web-1`, `deployment.apps/api`, `service/web`) each get a color from your theme.

Colors come from the theme, and text a program already colored keeps its own colors. Full-screen programs like vim, less and htop are left alone. Only the screen you're looking at is checked, and only shortly after output stops changing, so even a fast stream of logs scrolls at full speed.

Turn colors off with `log-highlighting = false` in [Settings](#settings). The actions below still work.

## Actions

{{mac:⌘-click|other:Ctrl-click}} one of these, or right-click it, for its menu:

- **Timestamp**: the time in your time zone and in UTC, how long ago it was, and copy it as UTC (ISO 8601), as your time, or as Unix time. When the text has no time zone, the menu tells you it was read as your time.
- **IP address**: copy it, open `http://address:port` when it has a port, or SSH to it in a new tab.
- **Kubernetes resource**: **Logs** (for pods, and for deployments, stateful sets, daemon sets, replica sets and jobs, through their pods), **Describe** and **View YAML**, each a kubectl command run in a new tab. They use the cluster and namespace this shell reported at its last prompt (see [Address bar and context badges](#context-badges)), and the menu shows both. Output for another namespace (`kubectl -n other …`) needs that namespace switched first. A secret gets **View Decoded Values…** instead of its YAML, which would print its values. It opens the same masked viewer as the [Kubernetes](#kubernetes) tab.
- **file:line**, like `src/app.ts:12:3`, `/srv/main.go:42` or Python's `File "app.py", line 8`: **Open in Editor at Line**, which opens `$EDITOR +line` in a split and works with vi, vim, nvim, nano, emacs and micro. You can also open the file with its default app, reveal it, or copy its path. Relative names are resolved against the shell's folder. They're links only when the file exists on this machine, so a path from a server you're SSH'd into isn't one.
- **URL**: {{mac:⌘-click|other:Ctrl-click}} opens it. Right-click it to copy it.

## Pretty-print JSON

Right-click a line that holds JSON (anywhere in the line, say after a timestamp and a level) and choose **Pretty-print JSON**. It shows the JSON indented and colored, with a **Copy** button. If you've selected text, that's used instead of the line, which helps when one JSON document spans several lines.
