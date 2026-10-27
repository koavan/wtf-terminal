# API requests

An API tab sends HTTP requests and shows their responses, next to your terminals. It's a tab like any other: split it beside a shell, put it in a group, move it to another window.

## Opening one

- {{mac:⌥⌘T|other:Ctrl+Alt+Shift+A}}, or **New API Tab** in the {{mac:Shell menu|other:window menu}}.
- Right-click the **+** on the tab strip → **New API Tab**.
- Right-click a tab → **New API Tab to the Right**.
- The command palette: **New API Tab**.

Nothing runs in an API tab until you press **Send**.

## Writing a request

Pick the method, type the URL and press **Send**, `Return` in the URL field, or {{mac:⌘↩|other:Ctrl+Return}} anywhere in the tab. A URL without `http://` or `https://` goes over `http://`, as with curl.

- **Params** lists the URL's query string. Edit it in either place and the other follows. Untick a parameter to leave it out without deleting it.
- **Headers** works the same way. The values of credential headers (`Authorization`, `Cookie`, and names containing `token`, `secret`, `password`, `api-key` or `session`) are masked; click the eye to see one.
- **Body** is sent as written. **Content-Type** sets that header for you, and **Format JSON** indents the body ({{mac:⌘Z|other:Ctrl+Z}} undoes it).

## The response

The status, how long it took, its size and the protocol, then the body or the headers. JSON is indented (**Pretty**, keeping the key order and numbers exactly as sent) or shown as it came (**Raw**). {{mac:⌘F|other:Ctrl+Shift+F}} finds in the response. Bodies over 20 MB are cut off, and a body that isn't text is described rather than shown. A request waits up to 5 minutes; **Cancel** stops it sooner.

Redirects are followed; the response says where it finally came from.

## curl

The menu next to **Send** ({{mac:⋯|other:⋮}}):

- **Copy as curl**: the request as a curl command. If it carries credentials, the clipboard empties after 30 seconds (see [Privacy](#privacy)).
- **Run in Shell**: types the curl command into an idle shell in the same tab, or a new tab. The command, credentials included, goes into that shell's history.
- **Paste curl Command**: fills in the request from a curl command on the clipboard. Pasting one into the URL field does the same.
- **Skip TLS Certificate Checks (-k)**: for servers with self-signed certificates. The response says so while it's on.

Pasted commands can be multi-line, use `-H`, `-d`/`--data-raw`/`--data-binary`/`--json`, `-u user:password`, `-X`, `-G`, `-b`, `-A`, `-k` and the usual output options. wtf refuses a command rather than send something different from what curl would: forms (`-F`), files (`-d @file`), proxies and other auth schemes are reported, not dropped. Shell variables like `$TOKEN` aren't expanded: replace them before sending.

## What is saved

API tabs come back with your session: method, URL, parameters, headers and body. **Credential header values are not saved**: after a restart, those headers are there with empty values to fill in again. Responses aren't saved. [Hide Secrets](#secrets) hides tokens in responses on screen and when copied.
