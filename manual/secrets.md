# Hide Secrets

Turn on **Hide Secrets** with the switch in the side pane's [Share & Record](#share) tab (or right-click a terminal → **Hide Secrets**, {{cmd:secrets.toggle}}, or **View → Hide Secrets**) before you share your screen, during an incident call say, and access keys, tokens and passwords that show up in your terminals are covered. While it's on, a green **Secrets hidden** label sits in the address bar. Click it, or use the shortcut again, to show them.

## What it covers

- **On screen**: each secret is drawn over with a solid bar, from the first frame it appears in, including in full-screen programs like `less` and `vim`. The covers stay on through selections and search highlights, and when you scroll back.
- **When saved**: Save Output as Text and Save Commands as Markdown. Recordings keep output as is, see [Recording](#recording).
- **When copied**: secrets become `[REDACTED]`. That goes for {{cmd:term.copy}}, copy-on-select, every copy in the right-click and command menus, {{cmd:term.copyOutput}}, and Pretty-print JSON.
- **Elsewhere in the window**: the command in the address bar, tab titles, the History tab, pinned output, the tab overview, and desktop notifications.

- **In the inspector**: JWTs and base64 values can't be opened with [Tokens and certificates](#inspect), since decoding them would show what's hidden. Certificates and TLS servers still can.

The output itself isn't changed. Turn Hide Secrets off and everything is there as it was. Programs still get what you type, so a password you type is sent as usual.

## What counts as a secret

- AWS access key IDs (`AKIA…`, `ASIA…`), and secret keys and session tokens next to their names (`aws_secret_access_key = …`, `"SecretAccessKey": "…"`).
- Tokens recognized by their prefix: GitHub (`ghp_…`, `github_pat_…`), GitLab (`glpat-…`), Slack (`xoxb-…` and webhook URLs), Stripe (`sk_live_…`), Google API keys (`AIza…`), OpenAI and Anthropic keys (`sk-…`), npm (`npm_…`), and JWTs (`eyJ….eyJ….…`).
- Passwords in URLs (`postgres://app:password@db`) and bearer tokens (`Authorization: Bearer …`).
- The values of settings named like secrets: `password`, `secret`, `token`, `api_key`, `access_key`, `private_key` or `credentials`, followed by `=` or `:`. For example `DB_PASSWORD=…`, `"client_secret": "…"`, or `export GITHUB_TOKEN=…`. Placeholders like `${TOKEN}`, `null` or `****` are left alone.
- Base64 data in YAML, such as the values under a secret's `data:` in `kubectl get secret -o yaml`.
- Private keys, from `-----BEGIN … PRIVATE KEY-----` to their end.

This is **best effort**. It errs on the side of hiding: a harmless value named like a password gets covered too. It can still miss a secret it doesn't recognize, like a password printed on its own with no name next to it. Don't rely on it alone for something you can't afford to show.

To start every session with it on, tick **Turn on whenever wtf starts** in the Share & Record tab, or set `redact-secrets = true` in [Settings](#settings). You can still turn it off and on any time.
