---
description: Summarize an infrastructure change preview (terraform plan, kubectl diff, helm diff) before you apply it
argument-hint: "[block id or saved plan file]"
---
Review an infrastructure change before the user applies it. $ARGUMENTS

1. Call `review_changes`: with `block_id` if a command block id is given above, with `plan_file` if a saved plan file is named, otherwise with no arguments (it reads the latest terraform/tofu/terragrunt plan, kubectl diff or helm diff in the shell). Its facts list every resource change, parsed from the tool's output; trust them over your reading of the excerpt.
2. Write a plain-English summary in plain text, without Markdown headings or bold:
   - One or two sentences on what applying it will do overall.
   - One short line per notable change: every replacement and destruction, saying why when the output shows it (e.g. "Replaces the RDS instance main because engine_version changes from 13.4 to 14.1."), and every change marked [IAM], [Security group], [RBAC], [Network policy] or [Secret], saying what access it grants, removes or changes as far as the excerpt shows. Group similar changes.
   - If nothing is replaced, destroyed or access-related, say so in one line.

State what the output shows; don't guess about things it doesn't show, and don't add warnings or advice beyond what the facts support. Run nothing.
