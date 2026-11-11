# Resource usage

wtf can show its own CPU and memory in the address bar, for example `2.1% · 412 MB`. Turn it on with **Toggle Resource Usage in Address Bar** in the command palette, the checkbox in the Resource Usage dialog, or `show-usage = true` in [Settings](#settings).

Click the readout, or use **Resource Usage…** in the palette{{mac: (or **View → Resource Usage…**)|other:}}, for the breakdown, process by process:

- **wtf**: the app itself, and what it runs on its own: background tunnels, and the quick `git` and `kubectl` checks behind the address bar and side pane.
- **Windows**: the WebKit processes that draw wtf's windows. {{mac:On macOS they're separate system processes (`com.apple.WebKit.WebContent`, `…GPU`, `…Networking`); wtf finds its own the way Activity Monitor groups them, by asking macOS which app each one works for.|other:With WebKitGTK they're wtf's child processes (`WebKitWebProcess`, `WebKitNetworkProcess`…).}}
- **Your programs**: the shells and everything running in them, including programs that detached from their shell{{mac:|other: (on Linux, a program that detached is no longer counted)}}. They're listed but not added to wtf's total, so a busy build doesn't look like wtf.

The readout's numbers are **wtf** plus **Windows**.

## What the numbers mean

- **CPU** is a percentage of one core, over the last 2 seconds, like `top` shows it: a computer with 10 cores has 1000% in all. Right after you open the readout it shows `…` until there are two measurements to compare.
- **Memory** is {{mac:the footprint, the same number Activity Monitor's Memory column shows|other:resident memory (RSS), as `top` shows it. Pages shared between processes count in each, so the total can be higher than what's really used}}.

## What it costs

Nothing while it's hidden: no timer runs and nothing is measured. While the readout or the dialog is on screen, wtf measures every 2 seconds, only while its window is in front. Each measurement asks the system directly ({{mac:libproc|other:`/proc`}}) without starting `ps` or `top`, and takes well under a millisecond. {{mac:Every 10 seconds it also looks through all processes for the WebKit ones, which takes about a millisecond.|other:}} The measuring is included in the numbers it shows.

{{mac:If a future macOS stops saying which app a process works for, the dialog says so and counts only wtf's own processes.|other:}}
