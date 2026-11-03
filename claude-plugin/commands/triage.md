---
description: Triage a security finding from wtf's Findings tab: what it means, whether it's reachable, and a patch
argument-hint: "[rule id, file or finding number]"
---
Triage a security scan finding for the code in the user's wtf shell's repository. $ARGUMENTS

1. Call `findings`: with `rule` and `path` if a rule or file is given above (a finding number: call it without filters and pick that one), otherwise with no arguments and take the most severe. If several match and it's unclear which the user means, list them briefly and ask.
2. Read the code it points at, and enough around it (callers, where the input comes from, the config it reads) to judge it. Use your own read and search tools.
3. Reply in plain text:
   - What the finding means here, in one or two sentences, for this code rather than in general.
   - Whether it looks reachable or exploitable, and why: the path from input to the flagged line, or what stops it. Say how sure you are and what you couldn't check. Don't call it a false positive unless the code shows it.
   - A fix: the smallest change that addresses it, as a unified diff. For a dependency, the version to move to and where it's pinned.
4. Offer to apply the patch. Only change files when the user agrees; wtf asks before every edit. To check the fix, suggest re-running the scan (the Findings tab's Run Security Scan…), typed with `type_at_prompt` if it's a command.
