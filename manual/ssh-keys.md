# SSH keys and ssh-agent

**SSH → Keys** lists the key pairs in `~/.ssh` and the keys ssh-agent holds. Reading them needs no passphrase.

Each key shows its type (with its size for RSA and ECDSA; ED25519 is always 256 bits), when its file was written (**from Mar 2023** — when the key was made, unless its passphrase was changed since; the exact date on hover), its comment and fingerprint (hover), and pills for:

- **agent** — loaded in ssh-agent.
- **N hosts** — the hosts whose `IdentityFile` is this key.
- a red mode like **0644** — other users can read the private key, so ssh refuses to use it. Right-click → **Make Private (chmod 600)**.

Keys only the agent holds (from a password manager or a security key) are listed under **Agent only**. When the agent holds SSH certificates, the address bar shows an **ssh** badge with how long they have left (see [credential expiry clocks](#context-badges)). The footer says how many keys the agent has; **Unload All** empties it.

## Doing things with keys

- **Generate**: **+** opens the [Generate](#generate) tab's key form: type, file name, comment and passphrase, and the key is made right there. To have ssh-keygen ask for the passphrase in a terminal instead, use the form's **Generate in a terminal instead**.
- **Copy the public key**: double-click a key.
- **Install on a server**: right-click → **Install on Host…** (or a host's **Install Public Key…**). ssh-copy-id runs in the focused shell (a new tab if a program is running there) and asks for your password on the server once.
- **Load / unload**: **Load into Agent**{{mac: (or **Load into Agent and Keychain**)|other:}}, **Unload from Agent**.
- **Passphrase**: **Change Passphrase…** / **Set Passphrase…** runs `ssh-keygen -p` in the focused shell (a new tab if a program is running there). Loading into the agent runs `ssh-add` there too.
- **Missing .pub**: **Create .pub File** writes it from the private key.
- **Delete**: moves the key pair out of `~/.ssh` into wtf's cache folder (`ssh-backups/keys`) — not erased, so you can get it back.

Everything else that can ask for a passphrase or password (ssh-add, ssh-copy-id, changing a passphrase) runs as a real command in a terminal tab: it's in your shell history and the secret never passes through wtf. The one exception is the passphrase you type into the Generate tab's form, which wtf uses once to encrypt the new key and doesn't keep. The list refreshes when those commands finish.

More than one GitHub account? Pair each key with an account in [Git identities](#git-identities) and pick one per tab.
