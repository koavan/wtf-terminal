# AWS profiles

The AWS Profiles tab ({{key:sidebar.aws}}) lists the AWS CLI's profiles from `~/.aws/credentials` and `~/.aws/config` (or `$AWS_SHARED_CREDENTIALS_FILE` / `$AWS_CONFIG_FILE`): access keys, assume-role, SSO and credential-process profiles, with their key ID, region and account. The one the focused shell uses is marked **in use**.

## Using a profile

- **Double-click** (or ▶) points the focused shell at it: wtf types `export AWS_PROFILE=…` at its prompt. If a program is running there, it opens a new tab that does it instead. Only that shell changes.
- Right-click → **Open New Tab with Profile**, or **Log In with SSO** for SSO profiles (`aws sso login` in a tab). When the focused shell uses an SSO profile, the address bar's AWS badge counts down to the end of the login (see [credential expiry clocks](#context-badges)).

## Checking a profile

These only read, so they don't ask first. They run the AWS CLI; if it isn't installed, the message has an **Install…** link (see [Command-line tools](#tools)).

- **Check Identity (sts)** — who the credentials belong to (account and ARN), via `aws sts get-caller-identity`, which needs no permissions.
- **Check Access Keys (iam)** — for access-key profiles: each key of the IAM user, active or not, how many days old, and when and for what it was last used.

## Changing profiles

Every change shows exactly what will change and waits for you:

- **+** and **Edit…** open the editor: name, kind (Access keys, Assume role, SSO, Credential process), the kind's fields, region, output and any other settings. **Review…** lists every line that will change; **Confirm & Save** writes.
- **Delete…** and **Copy to Default…** (copies a profile into `[default]`, used when no profile is chosen) list their changes in a confirmation.

Secrets are write-only: wtf never shows a saved secret key or session token. Leave the secret empty to keep the current one; a new key ID needs its secret. Edits change only the lines involved, keep your comments and each key's spelling (`AWS_ACCESS_KEY_ID=` or `aws_access_key_id = `), and a rename updates the profiles that use it as `source_profile`. Before each write, a copy of the file goes to wtf's cache folder (`aws-backups`, last 20, readable only by you).

## Rotating an access key

Right-click an access-key profile → **Rotate Access Key…**. wtf first checks that the key works, that it belongs to an IAM user, and that the user has room for a second key (AWS allows two). It shows the steps; on **Rotate Key** it:

1. creates a new access key,
2. writes it to the profile,
3. waits until AWS accepts it (up to half a minute),
4. deactivates the old key, and — if ticked — deletes it.

If AWS never accepts the new key, the old one is put back and the new one deleted, so nothing changes. Anything else using the old key (scripts, CI, other machines) stops working.

The IAM user needs, on its own user: `iam:ListAccessKeys`, `iam:CreateAccessKey`, `iam:UpdateAccessKey`, `iam:DeleteAccessKey` (and `iam:GetAccessKeyLastUsed` for Check Access Keys). A policy that demands MFA blocks rotation with plain keys.
