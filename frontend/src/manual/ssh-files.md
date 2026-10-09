# Copying files over SSH

Right-click a host in the [SSH tab](#ssh-hosts):

- **Upload Files…** — choose files or folders, then the folder on the server (empty means your home folder). Or **drag a file from the Files tab onto the host**.
- **Download…** — type the path on the server; it goes to `~/Downloads` unless you choose another folder.
- **Open sftp Session** — an interactive `sftp` in a new tab.

Uploads and downloads run `scp -r` in a new tab, so you see its progress, and a password prompt if the server asks. The dialog shows the exact command before you start.
