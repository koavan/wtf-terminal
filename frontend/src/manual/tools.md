# Command-line tools

Some side-pane features run programs you'd run yourself: the same programs, your config, your logins. wtf doesn't need any of them to start; a feature whose program is missing says so when you open it.

| Program | Used by |
| --- | --- |
| `kubectl` | [Kubernetes](#kubernetes) tab, Test Connection in [Contexts](#kube-contexts), kubectl [port forwards](#ssh-tunnels) |
| AWS CLI (`aws`, version 2) | [AWS profiles](#aws): Check Identity and Access Keys; the AWS account in the [address bar](#context-badges); Add EKS Cluster |
| `git` | Branch and sync in the address bar, git status in [Files](#files) |
| OpenSSH (`ssh`, `ssh-keygen`, `ssh-add`, `ssh-keyscan`) | [SSH](#ssh-hosts) tab: connect, test, keys, host keys, tunnels, file copies |
| `lsof` (macOS), `ss` or `lsof` (Linux) | [Network](#network) tab |
| `notify-send` (Linux) | Notifications when a long command finishes |

Everything else (profiles, contexts, SSH config, expiry clocks, the [inspector](#inspect)) reads files and needs nothing installed.

## Seeing what's installed

The wrench button at the bottom of the side pane's icon rail (above **?**), the command palette's **Command-Line Tools**, **wtf** menu → **Command-Line Tools…** (macOS), or **About wtf** → **Tools**. For each program: where it was found, the version it reports, and what uses it. **Check Again** looks again, e.g. after installing.

wtf looks the way the features do: on the focused shell's PATH first, then its own, then where installers put programs (`/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin`, `/usr/bin`…). So a program installed in `~/.local/bin` is found even when your shell's PATH doesn't include it.

On first start, if something is missing, wtf opens this list once. It only looks for the files; nothing runs and nothing is installed.

## Installing a tool

Click **Install…** next to a missing program, or next to a "kubectl isn't installed" or "the aws CLI isn't installed" message in the side pane. wtf shows the ways it can be installed on this system, the recommended one first:

- **Homebrew** (macOS, when Homebrew is installed): `brew install kubectl`, `brew install awscli`, `brew install git`.
- **Official release**: kubectl from `dl.k8s.io` for your system and processor (Apple silicon or Intel, amd64 or arm64), checked against its published SHA-256 checksum and put in `~/.local/bin` — no password needed. The AWS CLI from `awscli.amazonaws.com`: on macOS the official package, whose Apple signature is checked before `sudo installer` runs it; on Linux the official zip, installed into `~/.local/aws-cli` and `~/.local/bin` without sudo.
- **Apple Command Line Tools** (git on macOS): `xcode-select --install`; macOS asks before downloading.
- **Your distribution's package** (Linux): `apt-get`, `dnf`, `yum`, `pacman`, `zypper` or `apk`, whichever is installed, e.g. `sudo apt-get update && sudo apt-get install openssh-client`.

Before anything runs you see the exact command, or the whole script for an official download, where it installs and how the download is checked. **Run in New Tab** runs it in a new terminal tab, so you watch every step and type your password there if sudo asks. wtf never installs anything on its own. Scripts are written fresh to wtf's cache folder (`install/`) each time and are safe to delete.

When it finishes, **Check Again** in Command-Line Tools, then retry the feature.

### ~/.local/bin and your PATH

wtf always finds programs in `~/.local/bin`. To type them in the terminal too, your shell's PATH needs it; the install dialog says so when it's missing, with the line to add:

- zsh: `export PATH="$HOME/.local/bin:$PATH"` in `~/.zshrc`
- bash: the same line in `~/.bashrc` (`~/.bash_profile` on macOS)
- fish: run `fish_add_path ~/.local/bin` once

Open a new tab afterwards.

## On Linux packages

The `.deb` recommends `git`, `openssh-client`, `lsof` and `libnotify-bin`, so `apt` installs them along with wtf unless recommends are turned off. kubectl and the AWS CLI aren't in most distributions' default repositories; install them from Command-Line Tools.
