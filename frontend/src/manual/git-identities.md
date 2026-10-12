# Git identities (more than one GitHub user)

**SSH → Git** lets different tabs work on GitHub as different users: one tab pushes to your company's repositories as your work account, another to your own as your personal account.

An identity pairs an SSH key with the author name and email to commit as. Using it in a tab types one line into that tab's shell:

```
export GIT_SSH_COMMAND='ssh -i /Users/you/.ssh/id_work -o IdentitiesOnly=yes' GIT_AUTHOR_NAME='You' GIT_AUTHOR_EMAIL=you@work.example GIT_COMMITTER_NAME='You' GIT_COMMITTER_EMAIL=you@work.example WTF_GIT_IDENTITY=work
```

From then on, git in that tab connects with only that key. `IdentitiesOnly` stops ssh from offering ssh-agent's other keys, so GitHub sees the right account. git also commits as that author. Other tabs are unchanged. Nothing in `~/.ssh/config` or `~/.gitconfig` changes, and your clone URLs (`git@github.com:…`) stay as they are.

## Setting up a second account

1. Make a key for the account in the [Generate](#generate) tab (say `id_ed25519_work`). When it's done, **Use for a Git Identity…** starts step 2 with the key filled in.
2. **SSH → Git → +**: name it (`work`), pick that key, and enter the name and email to commit as. For GitHub to link your commits to the account, use an email the account has, or its noreply address.
3. Right-click the identity → **Add Key to GitHub…**. This copies the public key and opens GitHub's "New SSH key" page. Paste it there **while signed in as that account**.
4. Right-click → **Test GitHub Login**. wtf logs in with only that key and shows the account GitHub names, for example **@you-work**. A key with a passphrase must be loaded into ssh-agent first (**SSH → Keys**, right-click → **Load into Agent**).

## Using one

- **In the current tab**: double-click the identity, or right-click a tab → **Use work**.
- **In a new tab**: right-click the identity → **Open New Tab as work**.
- **Back to your own setup**: **Stop Using** (above the list), or right-click a tab → **Stop Using Identity**. This unsets the variables.

The [git badge](#context-badges) in the address bar shows the identity a tab uses, and the author it commits as.

A tab keeps its identity when you **split** it or **Duplicate** it, and when wtf restarts: the restored shell runs the line again. If the identity was deleted meanwhile, wtf says so and the shell uses your own config.

## Good to know

- These are shell variables, so they reach git commands run in that shell and the programs it starts. GUI git clients and other terminals don't see them.
- A `GIT_SSH_COMMAND` or `core.sshCommand` you set yourself is replaced in that tab while an identity is in use.
- The identities are kept in wtf's `git-identities.json` (in `~/Library/Application Support/wtf` on macOS, `~/.config/wtf` on Linux). Deleting one leaves its key in `~/.ssh` and on GitHub.
