# Address bar and context badges

The address bar shows the focused shell's folder, the program running in it, and how the last command went.

## Context badges

Next to the folder, badges show what that shell is pointed at:

| Badge | Shows |
|---|---|
| Kubernetes | the cluster and namespace of the current context |
| AWS | the account, profile and region |
| gcloud | the project |
| Terraform | the workspace (inside an initialized Terraform folder) |
| git | who commits here, the branch, its upstream and how many commits apart they are |

Hover a badge for full names. Each tab has its own: switch the AWS profile or kube context in one shell and only that tab's badge changes.

**How it works.** At each prompt the shell reports a few environment variables (`KUBECONFIG`, `AWS_PROFILE`, `AWS_REGION`, `CLOUDSDK_*`, `TF_WORKSPACE`, `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `WTF_GIT_IDENTITY`, `PATH`…) — and whether AWS keys are exported, never the keys themselves. wtf reads the rest from the tools' own files (`~/.kube/config`, `~/.aws/config`, gcloud's configurations) and, inside a git repository, from a few read-only git commands. Nothing is polled.

**The AWS account.** It comes from the profile's config when it says (the account of its `role_arn`, `sso_account_id` or `aws_account_id`). When it doesn't — static access keys, `credential_process` — wtf asks AWS once in the background (`aws sts get-caller-identity`, which needs no permissions) and remembers the answer until your AWS files change. Exported access keys show as "env credentials".

**The git badge.** Inside a git repository it reads like `Ada · main → origin ↑2 ↓1`: the [git identity](#git-identities) the tab uses, else the author git will commit as (`GIT_AUTHOR_NAME`, else `user.name`), the branch, where it pushes and pulls, ↑ commits to push and ↓ commits to pull. When the upstream's branch has the same name only the remote shows; "gone" means the upstream branch was deleted on the remote or hasn't been fetched. Hover it for the repository folder, the remote's URL (without any user or token in it), the author's email and when you last fetched. The counts are only as fresh as that fetch: wtf never fetches for you — run `git fetch` and the badge catches up at the next prompt.

wtf doesn't label anything as "production" or "dangerous": it shows the facts (account IDs, cluster names) and leaves the judgement to you.

Turn badges off with `context-badges = false` in [Settings](#settings).
