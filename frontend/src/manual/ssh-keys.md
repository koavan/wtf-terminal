# SSH keys and ssh-agent

**SSH → Keys** lists the key pairs in `~/.ssh` and the keys ssh-agent holds. Reading them needs no passphrase.

Each key shows its type and size, comment and fingerprint (hover), and pills for:

- **agent** — loaded in ssh-agent.
- **N hosts** — the hosts whose `IdentityFile` is this key.
- a red mode like **0644** — other users can read the private key, so ssh refuses to use it. Right-click → **Make Private (chmod 600)**.

Keys only the agent holds (from a password manager or a security key) are listed under **Agent only**. The footer says how many keys the agent has; **Unload All** empties it.

## Doing things with keys

- **Generate**: **+** → choose a type (Ed25519 unless an old server needs RSA), a file name and a comment. ssh-keygen runs in a new tab and asks for a passphrase there; it can load the new key into the agent afterwards{{mac:, remembering the passphrase in the Keychain|other:}}.
- **Copy the public key**: double-click a key.
- **Install on a server**: right-click → **Install on Host…** (or a host's **Install Public Key…**). ssh-copy-id runs in a tab and asks for your password on the server once.
- **Load / unload**: **Load into Agent**{{mac: (or **Load into Agent and Keychain**)|other:}}, **Unload from Agent**.
- **Passphrase**: **Change Passphrase…** / **Set Passphrase…** runs `ssh-keygen -p` in a tab.
- **Missing .pub**: **Create .pub File** writes it from the private key.
- **Delete**: moves the key pair out of `~/.ssh` into wtf's cache folder (`ssh-backups/keys`) — not erased, so you can get it back.

Everything that can ask for a passphrase or password (ssh-keygen, ssh-add, ssh-copy-id) runs as a real command in a terminal tab: it's in your shell history and the secret never passes through wtf. The list refreshes when those commands finish.

More than one GitHub account? Pair each key with an account in [Git identities](#git-identities) and pick one per tab.
