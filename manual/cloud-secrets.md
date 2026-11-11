# Cloud secrets

The Cloud Secrets tab ({{key:sidebar.secrets}}) browses secrets in three stores, picked at the top:

- **Secrets Manager**: AWS Secrets Manager's secrets.
- **Parameters**: AWS Systems Manager Parameter Store (String, StringList and SecureString).
- **Vault**: HashiCorp Vault's key/value secrets engines (KV version 1 and 2).

Nothing is read until the tab shows a store, and nothing refreshes on its own. Press ↻ to read again.

## Whose secrets

Like the Kubernetes tab, it follows the focused shell:

- **AWS**: the shell's profile (`AWS_PROFILE`, else `default`) and its region (`AWS_REGION`, else the profile's). The header shows the profile and account. Pick another region in the region box; that choice lasts until wtf quits. To use another profile, switch the shell to it (AWS Profiles → Use in This Shell). Access keys in the shell's environment (`AWS_ACCESS_KEY_ID`) are never reported to wtf, so a shell that uses them shows a note instead.
- **Vault**: the shell's `VAULT_ADDR`, `VAULT_NAMESPACE` and `VAULT_CACERT`. The token is the vault CLI's own (from `vault login`, kept in `~/.vault-token` or your token helper). wtf never sees a `VAULT_TOKEN` set in the shell, so log in with `vault login` if you rely on one.

wtf runs the `aws` and `vault` CLIs to do this. If one isn't installed, the message has an **Install…** link (see [Command-line tools](#tools)).

## Browsing

- **Secrets Manager** and **Parameters** list names with their description, when they last changed, the parameter type and version, and whether rotation is on. Up to 2,000 are listed; use the filter box to narrow them down.
- **Vault** starts with the server's key/value mounts. Double-click a folder to open it, and use **↑** to go back up. If you aren't allowed to list mounts, type a path (like `secret/app`) in the path box and press Return.

Lists never contain values.

## Seeing a value

Double-click a secret (or right-click → **Show Value…**). wtf reads it only then. A JSON value (most Secrets Manager secrets, and every Vault secret) is shown key by key. Every value starts hidden: reveal one with the eye or **Reveal All**, and copy it with the copy button (the clipboard is emptied after [clipboard-clear](#settings) seconds). Revealed values hide again when wtf loses focus, and are gone once the dialog closes.

While [Hide Secrets](#secrets) is on, the dialog shows key names only: revealing, copying and editing wait until you turn Hide Secrets off.

## Changing a value, or adding a secret

Every change shows its plan and waits for you:

- **Edit…** in the value dialog (or right-click → **Edit Value…**) opens the value as text. For Vault it's a JSON object of keys and values.
- **+** adds a secret in the store shown: a name (for Vault, a path, starting in the folder you're in), the parameter type for Parameter Store (SecureString is encrypted with the account's default key, `aws/ssm`), an optional description, and the value.
- **Review…** lists what will happen: which secret and version, the account and region (or Vault server and namespace), and which keys change, are added or removed, or how long the value becomes. Values are never shown in the plan. **Save** makes the change.

wtf checks again before writing that nobody else has changed the secret since you opened it. If someone has, nothing is written and it asks you to open the secret again. What a save does:

- **Secrets Manager** stores a new version. It becomes `AWSCURRENT`, and the one before stays as `AWSPREVIOUS`.
- **Parameter Store** overwrites the parameter (its version goes up by one), keeping its type, KMS key and tier.
- **Vault KV 2** writes a new version with check-and-set, so a concurrent write fails instead of being lost. Creating uses `-cas=0`, so it never replaces an existing secret. **KV 1** keeps no history: the old value is gone.

Values reach the CLIs on their standard input, never on the command line, so other programs can't see them in the process list. wtf doesn't delete secrets. Use the console or the CLI for that.

## Permissions needed

- Secrets Manager: `secretsmanager:ListSecrets`, `GetSecretValue` (plus `kms:Decrypt` for a customer-managed key), `PutSecretValue` and `CreateSecret`.
- Parameter Store: `ssm:DescribeParameters`, `GetParameter` (plus `kms:Decrypt` for SecureString), and `PutParameter`.
- Vault: `list` and `read` on the paths you browse, `create`/`update` to write, and `read` on `sys/mounts` to list the mounts (optional).
