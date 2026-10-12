# Privacy and safety

wtf sits next to your keys and credentials. This is what it does and doesn't do with them.

## It asks before changing anything

Anything that changes a file you own or something in the cloud shows what will change and waits for you:

| Change | Shown first | Backed up to (in wtf's cache folder) |
|---|---|---|
| SSH hosts (`~/.ssh/config`) | the editor, then the write | `ssh-backups` (last 30) |
| Deleting an SSH key | a confirmation | `ssh-backups/keys` (the key pair is moved, not erased) |
| known_hosts entries | the stored and scanned keys | `known_hosts.old` (ssh-keygen's own) |
| AWS profiles | every changed line | `aws-backups` (last 20) |
| AWS key rotation | the checked steps | the credentials file, as above |
| kubeconfigs | the exact commands or edits | `kube-backups` (last 20) |
| Kubernetes resources (delete, restart, scale, run) | the exact kubectl command, context, cluster and namespace | — |

Backups are readable only by you. wtf's cache folder is {{mac:`~/Library/Caches/wtf`|other:`~/.cache/wtf`}}.

## Secrets stay where they are

- **AWS**: saved secret keys and session tokens are never shown or sent to the window; the field is write-only. During rotation, the new secret goes from AWS straight into your credentials file.
- **SSH**: passphrases and passwords are typed into real ssh-add / ssh-copy-id / ssh-keygen commands in a terminal, never into wtf. The exception is a new key's passphrase in the [Generate](#generate) tab's form: wtf uses it once, in memory, to encrypt the key, and never saves, logs or sends it. **Generate in a terminal instead** keeps it out of wtf too.
- **Generated passwords** are made on this computer and never saved, logged or sent. Copying one clears the clipboard after 30 seconds.
- **Kubernetes**: tokens and certificates in kubeconfigs are never read out. Secret values are read only when you open one secret, masked until you reveal them, and dropped when the dialog closes.
- **Context badges**: the shell reports whether AWS keys are exported, never the keys. The git badge drops any user or token from a remote's URL before showing it.
- **Git identities**: kept in `git-identities.json` in wtf's config folder (names, emails and key paths, never keys). **Test GitHub Login** connects to github.com only when you ask.
- **Pinned output** stays in memory and is never written to disk.
- **Recordings and saved output** are written only where you choose, readable only by you. Recordings have what the terminal shows, never what you type.
- **Sharing your screen**: [Hide Secrets](#secrets) covers keys, tokens and passwords in the output, and redacts them when copied.

## It only talks to servers when you ask

Listing SSH hosts, keys, AWS profiles and kubeconfig contexts reads local files only. Requests to AWS or a cluster happen when you open a section, press a check or test button (such as **Test GitHub Login**), or run an action — and side-pane tabs stop when you close them.

## What it won't do

wtf doesn't guess which of your environments is "production" or flag commands as dangerous: a guess that's sometimes wrong is worse than none. It shows facts — account IDs, cluster names, the exact command — and leaves the call to you.
