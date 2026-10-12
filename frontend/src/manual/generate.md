# Generate: SSH keys and passwords

The **Generate** tab ({{key:sidebar.generate}}, the key icon at the bottom of the side pane) makes SSH key pairs and passwords from a form, with no terminal needed. Its two parts are **SSH Key** and **Password**. The command palette also has **Generate SSH Key…** and **Generate Password…**.

## SSH Key

Fill in the form and press **Generate Key**:

- **Type**: Ed25519 unless a server is too old for it; then RSA 4096. ECDSA P-521 is there if something requires it.
- **File name**: the key is saved as `~/.ssh/<name>` (private, readable only by you) and `<name>.pub`. A free name is filled in for you. wtf never replaces a file that's already there.
- **Comment**: a label stored with the key, such as which account or machine it's for. Servers show it in `authorized_keys`.
- **Passphrase**: encrypts the key file, so a stolen copy is useless without it. Type it twice, or leave both empty for none. **Suggest one** fills in a random 24-character passphrase. Save it in your password manager with **Copy** (the clipboard clears after 30 seconds). While [Hide Secrets](#secrets) is on, a suggested passphrase isn't shown on screen.
- **Load it into ssh-agent**: the new key is ready to use straight away, until the agent restarts (usually when you log out).

Then the tab shows the new key's fingerprint and public key, with buttons to:

- **Copy Public Key**: this is the part servers and GitHub need. The private key never leaves `~/.ssh`.
- **Add to GitHub…**: copies the public key and opens GitHub's "New SSH key" page.
- **Install on Host…**: adds it to a server's `authorized_keys` (ssh-copy-id runs in a tab and asks for your password on the server once).
- **Use for a Git Identity…**: starts a [git identity](#git-identities) with this key, for working on GitHub as another user.
{{mac:- **Load into Agent and Keychain**: runs `ssh-add --apple-use-keychain` in a tab, so the key loads again after a restart without asking.|other:}}

The key file is the same OpenSSH format `ssh-keygen` writes, so ssh, git and every other tool read it as usual. It also appears in **SSH → Keys**.

**About the passphrase.** wtf uses it once, in memory, to encrypt the new file. It isn't saved, logged or sent anywhere. If you'd rather it never pass through wtf at all, choose **Generate in a terminal instead**: `ssh-keygen` runs in a new tab and asks for the passphrase itself.

## Password

A random password appears as soon as you open the tab, and a new one each time you change an option:

- **Length**: drag the slider (4–64) or type up to 128.
- **Characters**: lowercase, uppercase, digits and symbols. Every kind you tick appears at least once.
- **Leave out look-alikes**: skips `I l 1 O 0 o |`, for passwords someone has to read or type.
- **About N bits of randomness**: how hard it is to guess. Each extra bit doubles the work.

**Copy** puts it on the clipboard, which wtf clears after 30 seconds unless you've copied something else since. **New** makes another. While [Hide Secrets](#secrets) is on, the password shows as dots until you click **Show**.

Passwords are made on this computer with the system's secure random generator. They aren't saved, aren't kept in any history, and are never sent anywhere. The symbols leave out quotes, spaces, backslashes and backticks, so a password pastes into a shell or a config file without escaping.
