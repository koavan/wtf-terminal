# Host keys (known_hosts)

When a server is rebuilt, ssh refuses to connect with **"REMOTE HOST IDENTIFICATION HAS CHANGED"**. Right-click the host → **Host Keys…** to deal with it.

The dialog shows:

- **Stored** — the keys `known_hosts` has for the server, looked up the way ssh does (hashed entries and `HostKeyAlias` included), with the file and line.
- **On the server now** — press **Scan** to ask the server which keys it presents (ssh-keyscan). Nothing is saved by scanning. Each scanned key is marked *stored*, *not stored* or *revoked*, and a line says whether they match.

Then:

- **Forget Stored Keys** removes the server's entries (`ssh-keygen -R`, which keeps the old file as `known_hosts.old`). It asks twice.
- **Trust Server's Keys** / **Replace with Server's Keys** stores what the scan found. wtf scans again first and keeps only the keys you were shown, in case the answer changed.

> Before trusting new keys, compare the fingerprints with ones from the server's admin: on the server, `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`. Click a fingerprint to copy it.

A server reached through a jump host can't be scanned directly; connect, and ssh shows the key it's offered.
