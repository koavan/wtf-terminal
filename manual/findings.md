# Findings: security scans

The **Findings** tab ({{key:sidebar.findings}}, the bug icon) shows what security scanners found in the focused shell's repository, grouped by severity and then by rule. Click a location to open the file at that line.

It's for the repository the focused shell is in: the top of its git repository, or the folder itself outside one. Move the shell to another repository and the tab follows. Nothing is polled; the results are read when the tab opens, when you press refresh, and when a scan finishes in a terminal.

## Running a scan

Press ▶ at the top of the tab, **Run Scan…** when there are no results yet, or **Run Security Scan…** in the command palette. Pick a scanner; the dialog shows the exact command. **Run Scan** types it into the focused shell (a new tab if a program is running there), so you see its progress and its own messages. When it finishes, the tab reads the new results.

| Scanner | Looks for | Command |
| --- | --- | --- |
| Trivy | Vulnerable dependencies, secrets, misconfigured Dockerfiles, Terraform and Kubernetes files | `trivy fs --scanners vuln,secret,misconfig --format sarif --output … .` |
| Semgrep | Bugs and insecure patterns in source code | `semgrep scan --config auto --sarif --output … .` |
| Checkov | Misconfigured infrastructure as code: Terraform, CloudFormation, Kubernetes, Helm, Dockerfiles | `checkov -d . --output sarif --output-file-path …` |
| Gitleaks | Secrets committed to git history | `gitleaks detect --source . --redact --report-format sarif --report-path …` |
| Grype | Vulnerable dependencies | `grype dir:. --output sarif --file …` |

- The scan covers the whole repository. From a subfolder, the command runs in a subshell at the repository's top — `(cd /path/to/repo && trivy …)` — so your shell stays where it is.
- The results (SARIF) go to wtf's cache folder, not into the repository, so there's nothing to commit or ignore.
- Trivy and Grype download a vulnerability database on their first run and when it's out of date. Semgrep's `--config auto` downloads rules from semgrep.dev for the languages it finds. Gitleaks runs with `--redact`, so the secrets themselves stay out of its report.
- A scanner that isn't installed says so; **Install …** opens [Command-line tools](#tools) at it (Homebrew on macOS; on Linux, its website lists the ways).

## Results from elsewhere

Any `.sarif` or `.sarif.json` file at the top of the repository is read too — one downloaded from CI, or from a scanner you ran yourself. Other SARIF producers (tfsec, CodeQL, Bandit…) work the same way.

The top of the tab lists every results file: the scanner and its version, how many findings, and when it was written. Right-click one to reveal or copy its path; a scan run from wtf also has **Remove These Results…** (files in the repository are yours and stay).

## Severity

Severity is the scanner's own, never worked out by wtf:

- When the scanner gives a security-severity score (Trivy, Grype, Checkov and others do), it's shown the way GitHub code scanning shows it: **Critical** (9.0 and up), **High** (7.0), **Medium** (4.0), **Low** (above 0).
- Otherwise it's the SARIF level: **Error**, **Warning** or **Note**. A result with no level is a warning, as SARIF says.

## Acting on a finding

- **Click a rule** to show where it was found; click again to hide.
- **Click a location** to open the file in `$EDITOR` at that line, in a split.
- **Right-click a location**: **Triage with Claude…** (see [Ask Claude](#ask-claude): what it means here, whether it looks reachable, and a patch you approve), Open in Editor, {{mac:Reveal in Finder|other:Open Containing Folder}}, Copy Path, Copy `path:line`, Copy Message, Copy Rule ID, and **Open Rule Documentation** when the scanner links to it.
- **Filter** by rule, file, message or scanner; every word must match.

A results file with more than 5,000 findings shows the first 5,000 and says how many were left out.
