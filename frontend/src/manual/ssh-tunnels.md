# SSH tunnels

**SSH → Tunnels** keeps port forwards you use often and runs them in the background.

## Adding one

**+**, or right-click a host → **New Tunnel…**. Choose the kind:

| Kind | Does | Example |
|---|---|---|
| **Local** (`-L`) | reach something near the server on a port of this computer | a database behind a bastion on `localhost:15432` |
| **Remote** (`-R`) | let the server reach something on this computer | show a local dev server to a remote box |
| **SOCKS** (`-D`) | a proxy here that sends traffic out through the server | point a browser at `localhost:1080` |

Fill in the host to go through, the ports and the destination, optionally a name, and whether it should **start when wtf opens**. The dialog shows the `ssh` command it amounts to. **Save & Start** saves it and starts it.

## Running them

- ▶ / ■ on a row starts or stops a tunnel. The dot is green when it's up, yellow while connecting or reconnecting, red when it failed — with ssh's own error underneath.
- A tunnel whose connection drops reconnects by itself, waiting longer between tries. One that never connects stops and shows why.
- Right-click: **Open localhost:PORT in Browser**, **Copy localhost:PORT**, **Copy ssh Command**, **Edit…**, **Delete**.
- The **Tunnels** label in the SSH tab shows how many are running.

Each tunnel is its own `ssh -N` with its own connection, so stopping it never affects your terminal sessions. Tunnels stop when wtf quits.

> Tunnels run without prompts, so the host must accept a key from ssh-agent or a key without a passphrase. "Permission denied (publickey)" means: load your key into the agent ([SSH keys](#ssh-keys)).
