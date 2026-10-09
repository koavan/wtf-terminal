# Kubernetes: contexts and kubeconfigs

**Kubernetes → Contexts** lists every context you have:

- **Used by this shell** — the files kubectl reads in the focused shell (its `$KUBECONFIG`, or `~/.kube/config`), merged the way kubectl merges them.
- **Every other kubeconfig in `~/.kube`** — each file on its own (kubectl's cache folders are skipped).

Each context shows its cluster (an EKS cluster's name and region), namespace, how it logs in (aws-iam-authenticator, `aws eks get-token`, certificate, token…) and with which AWS profile. Tokens and certificates are never read out. **in use** marks the focused shell's context; **default** marks the kubeconfig's `current-context`, which new shells start with. A login helper that isn't on the shell's PATH, or a context pointing at a cluster that isn't defined, is shown in red.

## Switching — this shell only

**Double-click** a context (or ▶): wtf writes a small file holding just that context's name, cluster, user and namespace — no credentials — and types `export KUBECONFIG=<that file>:<your files>` at the prompt. kubectl in that shell uses the context; other tabs keep theirs, and your kubeconfig isn't touched. `kubectl config` commands run in that shell change the small file, not the shared one. The small files go when wtf restarts.

A context from another file in `~/.kube` points the shell at that file only, so same-named entries in different files can't mix.

Right-click → **Open New Tab with Context**, or **Show Its Resources** to switch and open [Resources](#kubernetes).

## Checking a context

**Test Connection** asks the cluster for its version and who you are (`kubectl version`, `kubectl auth whoami`). It's the only thing here that reaches a cluster, and only when you click it.

## Changing kubeconfigs

Each change shows the exact commands or edits first, and keeps a copy of the kubeconfig in wtf's cache folder (`kube-backups`, last 20):

- **Rename…** and **Set Default Namespace…** — the prompt shows the `kubectl config` command.
- **Delete…** — removes the context, and its cluster and user when no other context uses them.
- **+ → Merge a Kubeconfig File…** — adds a file someone sent you; names you already have are skipped and listed.
- **+ → Add EKS Cluster…** — pick an AWS profile and region; **List Clusters** asks AWS (only when clicked); then `aws eks update-kubeconfig` runs in a tab.
- **Log In with aws eks get-token…** — for contexts that use aws-iam-authenticator: rewrites the user to need only the aws CLI, keeping its AWS profile.
