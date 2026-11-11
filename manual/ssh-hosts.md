# SSH hosts

The SSH tab ({{key:sidebar.ssh}}) has four parts: **Hosts**, [Keys](#ssh-keys), [Git](#git-identities) (which GitHub user each tab works as) and [Tunnels](#ssh-tunnels).

## What's listed

- The `Host` entries of `~/.ssh/config` and every file it `Include`s, grouped by file, each with its user, address, port and jump host worked out the way ssh does (first value wins, `Host *` defaults apply).
- Pills show settings that change what a connection may do, as your config sets them for that host (including `Host *`); hover one for the exact setting:
  - **agent fwd** — `ForwardAgent` is on (`yes`, or an agent socket): the server can use your keys while you're connected.
  - **hostkey no** / **hostkey off** / **hostkey accept-new** — `StrictHostKeyChecking` lets ssh connect without asking about the server's key (`accept-new` still refuses a changed key).
  - A jump host shows as **via …** after the address.
- **Known hosts**: servers in `~/.ssh/known_hosts` your config doesn't name, newest first.
- Git hosts such as github.com are left out. Type in the filter to narrow the list.

## Connecting

- **Double-click** a host (or press ▶ on its row, or `Enter`) to connect in the focused shell (a new tab if a program is running there). It's typed at the prompt, so it's in that shell's history and its `SSH_AUTH_SOCK` and settings apply.
- Right-click → **Open in New Tab**, **Open in Split Right / Down**, or **Open sftp Session**.
- **Several at once**: {{mac:⌘-click|other:Ctrl-click}} or shift-click to select hosts, then **Open as Tab Group** (or `Enter`). Each opens in its own tab, grouped and named after their config file. Right-click the selection for **Open … in a Tab Group with Broadcast Input** to type into all of them at once ([Broadcast input](#broadcast)).
- **Copy ssh Command** and **Copy Host Name** in the menu.

## Adding and editing hosts

- **+** adds a host: name, address, user, port, key file, jump host, and any other settings, one `Key value` per line under **More settings**. **Save & Connect** adds it and connects.
- Right-click a host → **Edit Host…** or **Delete Host…**; right-click a known host → **Save as Host…**.
- **Key file**: pick a key from the list (your `~/.ssh` keys) or type the path of the *private* key, e.g. `~/.ssh/id_ed25519`. If your agent holds several keys and the server says "Too many authentication failures", add `IdentitiesOnly yes` under More settings.

New hosts go into `~/.ssh/config` before any `Host *` block, so their own settings win. Edits change only the lines involved and keep your comments. Before writing, wtf checks that the file hasn't changed since it was listed and that `ssh -G` accepts the result — a typo can't break ssh. The old file is kept in wtf's cache folder (`ssh-backups`, last 30). Hosts from files outside `~/.ssh` (OrbStack's, for example) belong to the program that writes them and are read-only.

## More from a host's menu

- **Install Public Key…** — log in with a key instead of a password. See [SSH keys](#ssh-keys).
- **Host Keys…** — see and fix the server's fingerprint. See [Host keys](#ssh-host-keys).
- **Upload Files… / Download…** — see [Copying files](#ssh-files).
- **New Tunnel…** — see [Tunnels](#ssh-tunnels).
