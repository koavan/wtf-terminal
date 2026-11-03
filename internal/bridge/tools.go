package bridge

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/bridge/wire"
	"github.com/dayanraj/wtf/internal/cmdhistory"
	"github.com/dayanraj/wtf/internal/explain"
	"github.com/dayanraj/wtf/internal/findings"
	"github.com/dayanraj/wtf/internal/investigate"
	"github.com/dayanraj/wtf/internal/logsum"
	"github.com/dayanraj/wtf/internal/review"
	"github.com/dayanraj/wtf/internal/snippets"
	"github.com/dayanraj/wtf/internal/uicore"
)

const (
	// Of a command's output, the model sees the start and the end.
	headLines   = 60
	tailLines   = 140
	outputBytes = 12000
	// recent_commands: how many blocks, and the end of each one's output.
	defaultRecent = 5
	maxRecent     = 20
	recentLines   = 40
	recentBytes   = 4000
	// review_changes looks this far back for a preview.
	reviewLookback = 20
	// log_patterns looks this far back, takes the latest block with at
	// least logMinLines lines without a block_id, and lists this many
	// patterns.
	logLookback = 20
	logMinLines = 10
	logPatterns = 40
	// session_commands: how many blocks by default and at most, how far
	// back a range may start, and the start and end of each output.
	sessionDefault  = 20
	sessionMax      = 50
	sessionLookback = 200
	sessionLines    = 12
	sessionBytes    = 3000
	// findings: how many by default and at most, the code around each, and
	// the biggest file read for it.
	findingsDefault = 30
	findingsMax     = 100
	findingsContext = 2
	findingsFile    = 2 << 20
	// search_history: how many by default and at most, and the context
	// values listed.
	historyDefault = 30
	historyMax     = 100
	historyFacets  = 12
)

// Tools are what the plugin's MCP server offers. Every result is passed
// through Hide Secrets' redaction.
var Tools = []wire.Tool{
	{
		Name: "pane_context",
		Description: "What the user's wtf shell is set up for: its folder, system, Kubernetes context and namespace, AWS profile and region, " +
			"Google Cloud project, Terraform workspace and git branch (read from files, never asked of a cloud), whether a program is running in it, " +
			"and which well-known tools are on its PATH. Call it first to know where commands would run.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
	{
		Name: "recent_commands",
		Description: "The last commands the user ran in their wtf shell, oldest first, each with its exit code and the end of its output. " +
			"Use it to see what failed or what the user is working on.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":20,"description":"How many commands (default 5)."}},"additionalProperties":false}`),
	},
	{
		Name: "review_changes",
		Description: "Reads an infrastructure change preview the user ran in their wtf shell — terraform/tofu/terragrunt plan, kubectl diff or helm diff — " +
			"and lists what it will do as parsed facts: each resource created, changed, replaced or destroyed, what forces a replacement, and which " +
			"changes touch IAM, security groups, RBAC, network policies or secrets. Without block_id it reads the latest such command. " +
			"With plan_file it reads a saved Terraform plan instead (terraform show -json, which changes nothing).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"block_id":{"type":"integer","description":"A command block id from recent_commands."},` +
			`"plan_file":{"type":"string","description":"A saved plan file, relative to the shell's folder or absolute."}},"additionalProperties":false}`),
	},
	{
		Name: "log_patterns",
		Description: "Groups a command's log output in the user's wtf shell (kubectl logs, docker logs, journalctl, a log file…) into patterns: " +
			"lines that differ only in numbers, ids, addresses, durations, times and quoted values count as one. Each pattern has its count, " +
			"log level, first and last time seen and an example line; errors and warnings come first, and a timeline counts them over time. " +
			"Without block_id it reads the latest command with at least 10 lines of output, also one still running. Use it to summarize logs " +
			"instead of reading every line.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"block_id":{"type":"integer","description":"A command block id from recent_commands."}},"additionalProperties":false}`),
	},
	{
		Name: "session_commands",
		Description: "A stretch of the user's work in their wtf shell, oldest first: each command with when it started, how long it took, " +
			"its exit code, and the start and end of its output. Give from_block and to_block (ids from recent_commands) for a range, " +
			"or count for the last commands (default 20, at most 50). Use it to write a runbook or a postmortem from what the user did.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"count":{"type":"integer","minimum":1,"maximum":50,"description":"How many of the last commands (default 20)."},` +
			`"from_block":{"type":"integer","description":"The first block of a range."},` +
			`"to_block":{"type":"integer","description":"The last block of a range (default: the latest)."}},"additionalProperties":false}`),
	},
	{
		Name: "open_snippet_editor",
		Description: "Opens wtf's snippet editor filled in with a snippet (one command) or a runbook (steps run one at a time), for the user " +
			"to review, change and save; nothing is saved until they confirm there. Write the parts that change as {{placeholders}} " +
			"(e.g. kubectl -n {{ns}} rollout restart deploy/{{name}}); a default can follow the shell: {{ns=$kube_namespace}}, " +
			"$kube_context, $aws_profile, $aws_region, $cwd. Give command, or steps, not both.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"name":{"type":"string"},"description":{"type":"string"},` +
			`"command":{"type":"string","description":"A snippet's one command."},` +
			`"steps":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"command":{"type":"string"}},"required":["command"],"additionalProperties":false},"description":"A runbook's steps, in order."},` +
			`"params":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"default":{"type":"string"},"choices":{"type":"array","items":{"type":"string"}}},"required":["name"],"additionalProperties":false},"description":"What each placeholder is."}},` +
			`"required":["name"],"additionalProperties":false}`),
	},
	{
		Name: "findings",
		Description: "The security scan results wtf's Findings tab shows for the user's wtf shell's folder (its git repository): every SARIF report there " +
			"and from scans run in wtf (Trivy, Semgrep, Gitleaks, Checkov, CodeQL…). Each finding has a number, severity, tool, rule, file:line, message " +
			"and the lines of code around it; most severe first. Filter with rule (id or name), path (part of the file's path) and severity " +
			"(the least severe to include). Read more of the code with your own tools.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"rule":{"type":"string","description":"Part of a rule id or name."},` +
			`"path":{"type":"string","description":"Part of a file's path, relative to the repository."},` +
			`"severity":{"type":"string","enum":["critical","high","medium","low"],"description":"Include this severity and worse (SARIF error counts as high, warning as medium, note as low)."},` +
			`"limit":{"type":"integer","minimum":1,"maximum":100,"description":"How many (default 30)."}},"additionalProperties":false}`),
	},
	{
		Name: "search_history",
		Description: "Searches the commands the user ran in any wtf shell, saved across windows and restarts (the History tab's Saved view): " +
			"each with when it ran, exit code, duration, folder, and the Kubernetes context and namespace, AWS profile, account and region, " +
			"Google Cloud project, Terraform workspace and git branch it ran under. text matches commands and folders that contain every word " +
			"(try a few wordings: the tool's name, a resource, a flag). The answer also lists the contexts saved, so you can map \"staging\" or " +
			"an account to the right value. Newest first.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"text":{"type":"string","description":"Words that must all appear in the command or its folder."},` +
			`"days":{"type":"integer","minimum":1,"description":"Only the last this many days."},` +
			`"kube_context":{"type":"string"},"kube_namespace":{"type":"string"},` +
			`"aws_account":{"type":"string"},"aws_profile":{"type":"string"},"aws_region":{"type":"string"},` +
			`"failed":{"type":"boolean","description":"Only commands that failed."},` +
			`"limit":{"type":"integer","minimum":1,"maximum":100,"description":"How many (default 30)."}},"additionalProperties":false}`),
	},
	{
		Name: "run_readonly",
		Description: "Runs one read-only command in the user's wtf shell, where they can see it, as a normal command block, and returns its exit code and output. " +
			"Give the program and its arguments as argv (no shell syntax: no pipes, redirects or variables). Only this allowlist runs; anything else is refused with the reason:\n- " +
			strings.Join(investigate.Allowed, "\n- ") +
			"\nThe user approves every command. To suggest a command that changes something, use type_at_prompt instead.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"argv":{"type":"array","items":{"type":"string"},"minItems":1,"description":"Program and arguments, e.g. [\"kubectl\",\"get\",\"pods\",\"-n\",\"shop\"]."},` +
			`"purpose":{"type":"string","description":"Why, in a few words, shown to the user."}},"required":["argv"],"additionalProperties":false}`),
	},
	{
		Name: "type_at_prompt",
		Description: "Types a command at the user's wtf shell prompt without pressing Enter, replacing what's typed there, for the user to read, edit and run themselves. " +
			"Use it for fixes and anything that changes state. Fails when a program is running in the shell.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The command line."}},"required":["command"],"additionalProperties":false}`),
	},
}

func (s *Service) call(ctx context.Context, session, tool string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	var text string
	var err error
	switch tool {
	case "pane_context":
		text, err = s.paneContext(ctx, session)
	case "recent_commands":
		var a struct {
			Count int `json:"count"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.recentCommands(ctx, session, a.Count)
		}
	case "review_changes":
		var a struct {
			BlockID  *int   `json:"block_id"`
			PlanFile string `json:"plan_file"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.reviewChanges(ctx, session, a.BlockID, a.PlanFile)
		}
	case "log_patterns":
		var a struct {
			BlockID *int `json:"block_id"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.logPatterns(ctx, session, a.BlockID)
		}
	case "session_commands":
		var a struct {
			Count int  `json:"count"`
			From  *int `json:"from_block"`
			To    *int `json:"to_block"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.sessionCommands(ctx, session, a.Count, a.From, a.To)
		}
	case "open_snippet_editor":
		var a snippets.Snippet
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.openSnippetEditor(ctx, session, a)
		}
	case "findings":
		var a struct {
			Rule     string `json:"rule"`
			Path     string `json:"path"`
			Severity string `json:"severity"`
			Limit    int    `json:"limit"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.findings(ctx, session, a.Rule, a.Path, a.Severity, a.Limit)
		}
	case "search_history":
		var a struct {
			Text          string `json:"text"`
			Days          int    `json:"days"`
			KubeContext   string `json:"kube_context"`
			KubeNamespace string `json:"kube_namespace"`
			AWSAccount    string `json:"aws_account"`
			AWSProfile    string `json:"aws_profile"`
			AWSRegion     string `json:"aws_region"`
			Failed        bool   `json:"failed"`
			Limit         int    `json:"limit"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			q := cmdhistory.Query{Text: a.Text, KubeContext: a.KubeContext, KubeNamespace: a.KubeNamespace, AWSAccount: a.AWSAccount,
				AWSProfile: a.AWSProfile, AWSRegion: a.AWSRegion, Failed: a.Failed, Limit: a.Limit}
			if a.Days > 0 {
				q.Since = time.Now().AddDate(0, 0, -a.Days).Format(time.RFC3339)
			}
			text, err = s.searchHistory(q)
		}
	case "run_readonly":
		var a struct {
			Argv    []string `json:"argv"`
			Purpose string   `json:"purpose"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.runReadonly(ctx, session, a.Argv)
		}
	case "type_at_prompt":
		var a struct {
			Command string `json:"command"`
		}
		if err = json.Unmarshal(raw, &a); err == nil {
			text, err = s.typeAtPrompt(ctx, session, a.Command)
		}
	default:
		return "", fmt.Errorf("no tool %q", tool)
	}
	if err != nil {
		return "", fmt.Errorf("%s", uicore.Redact(err.Error()))
	}
	return uicore.Redact(text), nil
}

func (s *Service) pane(ctx context.Context, session string) (Pane, error) {
	r, err := s.ask(ctx, UIRequest{Kind: KindPane, Session: session})
	if err != nil {
		return Pane{}, err
	}
	if r.Pane == nil {
		return Pane{}, fmt.Errorf("that shell is gone")
	}
	return *r.Pane, nil
}

func (s *Service) paneContext(ctx context.Context, session string) (string, error) {
	p, err := s.pane(ctx, session)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if p.Title != "" {
		fmt.Fprintf(&b, "Tab: %s\n", p.Title)
	}
	b.WriteString(explain.ShellFacts(s.infra, p.Env, p.Cwd))
	switch {
	case p.Running && p.Process != "":
		fmt.Fprintf(&b, "State: running %s (not at its prompt)\n", p.Process)
	case p.Running:
		b.WriteString("State: a program is running (not at its prompt)\n")
	default:
		b.WriteString("State: at its prompt\n")
	}
	if !p.Integrated {
		b.WriteString("Shell integration: off, so wtf can't tell commands apart: recent_commands, review_changes and run_readonly don't work here\n")
	}
	if p.Env != nil {
		if t := explain.ToolsOnPath(p.Env); len(t) > 0 {
			fmt.Fprintf(&b, "Tools on PATH: %s\n", strings.Join(t, ", "))
		}
	}
	return b.String(), nil
}

func (s *Service) blocks(ctx context.Context, session string, n int) ([]Block, error) {
	r, err := s.ask(ctx, UIRequest{Kind: KindBlocks, Session: session, Count: n})
	return r.Blocks, err
}

func (s *Service) recentCommands(ctx context.Context, session string, n int) (string, error) {
	if n <= 0 {
		n = defaultRecent
	}
	n = min(n, maxRecent)
	list, err := s.blocks(ctx, session, n)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No commands yet in this shell (or it has no shell integration).", nil
	}
	var b strings.Builder
	for i, bl := range list {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[block %d] $ %s\n%s\n", bl.ID, strings.TrimSpace(bl.Command), exitLine(bl))
		out, dropped := tail(bl.Output, recentLines, recentBytes)
		switch {
		case strings.TrimSpace(out) == "":
			b.WriteString("Output: none (or no longer in the scrollback)\n")
		case dropped > 0:
			fmt.Fprintf(&b, "Output (last lines; %d earlier left out):\n%s\n", dropped, out)
		default:
			fmt.Fprintf(&b, "Output:\n%s\n", out)
		}
	}
	return b.String(), nil
}

func (s *Service) reviewChanges(ctx context.Context, session string, id *int, planFile string) (string, error) {
	var r review.Review
	if planFile != "" {
		p, err := s.pane(ctx, session)
		if err != nil {
			return "", err
		}
		path := planFile
		if !strings.HasPrefix(path, "/") {
			dir := p.Cwd
			if pwd := p.Env["PWD"]; pwd != "" {
				dir = pwd
			}
			path = dir + "/" + path
		}
		if r, err = review.NewService(s.infra).ParseFile(path, p.Env); err != nil {
			return "", err
		}
	} else {
		list, err := s.blocks(ctx, session, reviewLookback)
		if err != nil {
			return "", err
		}
		var found *Block
		for i := len(list) - 1; i >= 0; i-- {
			bl := &list[i]
			if (id != nil && bl.ID == *id) || (id == nil && review.Reviewable(bl.Command)) {
				found = bl
				break
			}
		}
		switch {
		case found == nil && id != nil:
			return "", fmt.Errorf("no block %d among the last %d commands", *id, reviewLookback)
		case found == nil:
			return "", fmt.Errorf("none of the last %d commands is a terraform plan, kubectl diff or helm diff", reviewLookback)
		case found.Output == "":
			return "", fmt.Errorf("the output of `%s` has left the scrollback", strings.TrimSpace(found.Command))
		}
		r = review.NewService(s.infra).Parse(found.Command, found.Output)
	}
	if r.Tool == "" {
		return "", fmt.Errorf("that isn't the output of a terraform plan, kubectl diff or helm diff")
	}
	return review.Facts(r) + "\nExcerpt of the output:\n" + r.Excerpt + "\n", nil
}

func (s *Service) logPatterns(ctx context.Context, session string, id *int) (string, error) {
	list, err := s.blocks(ctx, session, logLookback)
	if err != nil {
		return "", err
	}
	lines := func(b *Block) int { return strings.Count(strings.TrimRight(b.Output, "\n"), "\n") + 1 }
	var found *Block
	for i := len(list) - 1; i >= 0; i-- {
		bl := &list[i]
		if id != nil && bl.ID == *id || id == nil && strings.TrimSpace(bl.Output) != "" && lines(bl) >= logMinLines {
			found = bl
			break
		}
	}
	switch {
	case found == nil && id != nil:
		return "", fmt.Errorf("no block %d among the last %d commands", *id, logLookback)
	case found == nil:
		return "", fmt.Errorf("none of the last %d commands has %d or more lines of output: give block_id", logLookback, logMinLines)
	case strings.TrimSpace(found.Output) == "":
		return "", fmt.Errorf("the output of `%s` has left the scrollback", strings.TrimSpace(found.Command))
	}
	sum := logsum.Summarize(found.Output, time.Now(), time.Local)
	return fmt.Sprintf("[block %d] $ %s\n%s\n\n%s", found.ID, strings.TrimSpace(found.Command), exitLine(*found), sum.Text(logPatterns)), nil
}

func (s *Service) sessionCommands(ctx context.Context, session string, count int, from, to *int) (string, error) {
	n := count
	if from != nil {
		n = sessionLookback
	} else if n <= 0 {
		n = sessionDefault
	}
	if from == nil {
		n = min(n, sessionMax)
	}
	list, err := s.blocks(ctx, session, n)
	if err != nil {
		return "", err
	}
	if from != nil {
		var picked []Block
		for _, bl := range list {
			if bl.ID >= *from && (to == nil || bl.ID <= *to) {
				picked = append(picked, bl)
			}
		}
		if len(picked) == 0 {
			return "", fmt.Errorf("no commands from block %d among the last %d", *from, sessionLookback)
		}
		if len(picked) > sessionMax {
			picked = picked[:sessionMax]
		}
		list = picked
	}
	if len(list) == 0 {
		return "No commands yet in this shell (or it has no shell integration).", nil
	}
	var b strings.Builder
	for i, bl := range list {
		if i > 0 {
			b.WriteString("\n")
		}
		when := ""
		if t, err := time.Parse(time.RFC3339, bl.StartedAt); err == nil {
			when = " " + t.Local().Format("2006-01-02 15:04:05")
		}
		took := ""
		if bl.DurationMs > 0 {
			took = " (took " + (time.Duration(bl.DurationMs) * time.Millisecond).Round(100*time.Millisecond).String() + ")"
		}
		fmt.Fprintf(&b, "[block %d]%s%s $ %s\n%s\n", bl.ID, when, took, strings.TrimSpace(bl.Command), exitLine(bl))
		out := strings.TrimRight(bl.Output, "\n")
		if strings.TrimSpace(out) == "" {
			b.WriteString("Output: none (or no longer in the scrollback)\n")
			continue
		}
		lines := strings.Split(out, "\n")
		if len(lines) > 2*sessionLines {
			fmt.Fprintf(&b, "Output (first and last %d lines; %d left out):\n%s\n…\n%s\n", sessionLines, len(lines)-2*sessionLines,
				strings.Join(lines[:sessionLines], "\n"), strings.Join(lines[len(lines)-sessionLines:], "\n"))
		} else {
			if len(out) > sessionBytes {
				out = out[:sessionBytes] + "…"
			}
			fmt.Fprintf(&b, "Output:\n%s\n", out)
		}
	}
	return b.String(), nil
}

func (s *Service) openSnippetEditor(ctx context.Context, session string, sn snippets.Snippet) (string, error) {
	sn.Name = strings.TrimSpace(sn.Name)
	sn.ID, sn.Source, sn.Index, sn.Project, sn.Tags = "", "", 0, false, []string{}
	if sn.Steps == nil {
		sn.Steps = []snippets.Step{}
	}
	if sn.Params == nil {
		sn.Params = []snippets.Param{}
	}
	switch {
	case sn.Name == "":
		return "", fmt.Errorf("give the snippet a name")
	case strings.TrimSpace(sn.Command) == "" && len(sn.Steps) == 0:
		return "", fmt.Errorf("give command, or steps for a runbook")
	case strings.TrimSpace(sn.Command) != "" && len(sn.Steps) > 0:
		return "", fmt.Errorf("give command or steps, not both")
	}
	for _, st := range sn.Steps {
		if strings.TrimSpace(st.Command) == "" {
			return "", fmt.Errorf("every step needs a command")
		}
	}
	if _, err := s.ask(ctx, UIRequest{Kind: KindSnippet, Session: session, Snippet: &sn}); err != nil {
		return "", err
	}
	return "Opened wtf's snippet editor with it. Nothing is saved until the user reviews it there and clicks Confirm & Save.", nil
}

// severityRank orders findings, most severe first; SARIF levels sit with
// the security severities they're closest to.
var severityRank = map[string]int{"critical": 0, "high": 1, "error": 1, "medium": 2, "warning": 2, "low": 3, "note": 3, "none": 4}

func rankOf(sev string) int {
	if r, ok := severityRank[sev]; ok {
		return r
	}
	return 4
}

func (s *Service) findings(ctx context.Context, session, rule, path, severity string, limit int) (string, error) {
	p, err := s.pane(ctx, session)
	if err != nil {
		return "", err
	}
	folder := p.Cwd
	if pwd := p.Env["PWD"]; pwd != "" {
		folder = pwd
	}
	r, err := findings.NewService().Load(folder)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = findingsDefault
	}
	limit = min(limit, findingsMax)
	worst := 4
	if severity != "" {
		rk, ok := severityRank[severity]
		if !ok {
			return "", fmt.Errorf("severity is critical, high, medium or low")
		}
		worst = rk
	}
	rel := func(f string) string {
		if r2, err := filepath.Rel(r.Root, f); err == nil && !strings.HasPrefix(r2, "..") {
			return r2
		}
		return f
	}
	type numbered struct {
		n int
		f findings.Finding
	}
	var all []numbered
	for i, f := range r.Findings {
		all = append(all, numbered{i + 1, f})
	}
	slices.SortStableFunc(all, func(a, b numbered) int {
		if d := rankOf(a.f.Severity) - rankOf(b.f.Severity); d != 0 {
			return d
		}
		if a.f.Score != b.f.Score {
			return cmp.Compare(b.f.Score, a.f.Score)
		}
		return cmp.Compare(a.n, b.n)
	})
	counts := map[string]int{}
	var picked []numbered
	for _, x := range all {
		counts[x.f.Severity]++
		f := x.f
		if rankOf(f.Severity) > worst ||
			rule != "" && !strings.Contains(strings.ToLower(f.RuleID+" "+f.Rule), strings.ToLower(rule)) ||
			path != "" && !strings.Contains(strings.ToLower(rel(f.Path)), strings.ToLower(path)) {
			continue
		}
		picked = append(picked, x)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Findings for %s\n", tildePath(r.Root))
	if len(r.Sources) == 0 {
		b.WriteString("No SARIF reports: none at the repository's top and no scans run from wtf. The user can run one from the Findings tab (Run Security Scan…).\n")
		return b.String(), nil
	}
	b.WriteString("Reports:\n")
	for _, src := range r.Sources {
		what := src.Tool
		if src.Version != "" {
			what += " " + src.Version
		}
		where := "in the folder"
		if src.FromScan {
			where = "scan run from wtf"
		}
		if src.Error != "" {
			fmt.Fprintf(&b, "- %s (%s): couldn't be read: %s\n", filepath.Base(src.Path), where, src.Error)
			continue
		}
		fmt.Fprintf(&b, "- %s: %d findings, %s, written %s\n", what, src.Count, where, src.Modified)
	}
	var sev []string
	for _, k := range []string{"critical", "high", "error", "medium", "warning", "low", "note", "none"} {
		if counts[k] > 0 {
			sev = append(sev, fmt.Sprintf("%d %s", counts[k], k))
		}
	}
	fmt.Fprintf(&b, "All: %d (%s)", len(r.Findings), strings.Join(sev, ", "))
	if r.Truncated > 0 {
		fmt.Fprintf(&b, "; %d more past wtf's limit", r.Truncated)
	}
	fmt.Fprintf(&b, ". Matching: %d", len(picked))
	if len(picked) > limit {
		fmt.Fprintf(&b, ", the first %d shown", limit)
		picked = picked[:limit]
	}
	b.WriteString(".\n")
	for _, x := range picked {
		f := x.f
		name := f.RuleID
		if f.Rule != "" && f.Rule != f.RuleID {
			name += " — " + f.Rule
		}
		where := rel(f.Path)
		if f.Line > 0 {
			where += fmt.Sprintf(":%d", f.Line)
			if f.Column > 0 {
				where += fmt.Sprintf(":%d", f.Column)
			}
		}
		fmt.Fprintf(&b, "\n#%d [%s] %s %s\n   at %s\n", x.n, f.Severity, f.Tool, name, where)
		if f.Message != "" {
			fmt.Fprintf(&b, "   %s\n", strings.ReplaceAll(strings.TrimSpace(f.Message), "\n", "\n   "))
		}
		if code := codeAround(f.Path, f.Line); code != "" {
			b.WriteString(code)
		}
		if f.HelpURI != "" {
			fmt.Fprintf(&b, "   help: %s\n", f.HelpURI)
		}
	}
	return b.String(), nil
}

// codeAround is a finding's line and the ones around it, numbered.
func codeAround(path string, line int) string {
	if line <= 0 || !filepath.IsAbs(path) {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > findingsFile {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		return ""
	}
	var b strings.Builder
	for i := max(1, line-findingsContext); i <= min(len(lines), line+findingsContext); i++ {
		mark := " "
		if i == line {
			mark = ">"
		}
		t := lines[i-1]
		if len(t) > 200 {
			t = t[:200] + "…"
		}
		fmt.Fprintf(&b, "   %s%5d | %s\n", mark, i, t)
	}
	return b.String()
}

// tildePath shortens the home folder to ~.
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(p, home+"/"); ok {
			return "~/" + rest
		}
	}
	return p
}

func (s *Service) searchHistory(q cmdhistory.Query) (string, error) {
	if s.History == nil {
		return "", fmt.Errorf("this wtf keeps no saved history")
	}
	if q.Limit <= 0 {
		q.Limit = historyDefault
	}
	q.Limit = min(q.Limit, historyMax)
	r := s.History.Search(q)
	var b strings.Builder
	if !r.Saving {
		b.WriteString("Saving history is off (history-days = 0 in the user's wtf settings), so there are no saved commands.\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "Saved history keeps %d days. %d matching", r.Days, r.Total)
	if r.Total > len(r.Entries) {
		fmt.Fprintf(&b, ", the newest %d shown", len(r.Entries))
	}
	b.WriteString(".\n")
	facet := func(label string, list []cmdhistory.Facet) {
		if len(list) == 0 {
			return
		}
		var parts []string
		for i, f := range list {
			if i == historyFacets {
				parts = append(parts, fmt.Sprintf("… %d more", len(list)-historyFacets))
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%d)", f.Value, f.Count))
		}
		fmt.Fprintf(&b, "%s in all saved commands: %s\n", label, strings.Join(parts, ", "))
	}
	facet("Kubernetes contexts", r.KubeContexts)
	facet("Kubernetes namespaces", r.Namespaces)
	facet("AWS accounts", r.AWSAccounts)
	facet("AWS profiles", r.AWSProfiles)
	facet("AWS regions", r.AWSRegions)
	for _, e := range r.Entries {
		at := e.StartedAt
		if t, err := time.Parse(time.RFC3339, e.StartedAt); err == nil {
			at = t.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&b, "\n%s · exit %d · %s · in %s\n$ %s\n", at, e.ExitCode,
			(time.Duration(e.DurationMs) * time.Millisecond).Round(100*time.Millisecond), tildePath(e.Cwd), e.Command)
		var ctx []string
		if e.KubeContext != "" {
			k := "Kubernetes " + e.KubeContext
			if e.KubeNamespace != "" {
				k += " (namespace " + e.KubeNamespace + ")"
			}
			ctx = append(ctx, k)
		}
		if e.AWSProfile != "" || e.AWSAccount != "" {
			a := "AWS"
			for _, v := range [][2]string{{"profile", e.AWSProfile}, {"account", e.AWSAccount}, {"region", e.AWSRegion}} {
				if v[1] != "" {
					a += " " + v[0] + " " + v[1]
				}
			}
			ctx = append(ctx, a)
		}
		for _, v := range [][2]string{{"GCP project", e.GCPProject}, {"Terraform workspace", e.TFWorkspace}, {"git branch", e.GitBranch}} {
			if v[1] != "" {
				ctx = append(ctx, v[0]+" "+v[1])
			}
		}
		if len(ctx) > 0 {
			fmt.Fprintf(&b, "  ran under: %s\n", strings.Join(ctx, "; "))
		}
	}
	return b.String(), nil
}

func (s *Service) runReadonly(ctx context.Context, session string, argv []string) (string, error) {
	cmd, err := investigate.Check(argv)
	if err != nil {
		return "", fmt.Errorf("refused: %w", err)
	}
	r, err := s.ask(ctx, UIRequest{Kind: KindRun, Session: session, Text: cmd.Line})
	if err != nil {
		return "", err
	}
	if r.Block == nil {
		return "", fmt.Errorf("the command's block wasn't found")
	}
	bl := *r.Block
	var b strings.Builder
	fmt.Fprintf(&b, "[block %d] $ %s\n%s\n", bl.ID, cmd.Line, exitLine(bl))
	if bl.TimedOut {
		b.WriteString("It didn't finish in time and was interrupted (Ctrl-C).\n")
	}
	if out := clip(bl.Output); strings.TrimSpace(out) == "" {
		b.WriteString("Output: none\n")
	} else {
		fmt.Fprintf(&b, "Output:\n%s\n", out)
	}
	return b.String(), nil
}

func (s *Service) typeAtPrompt(ctx context.Context, session, command string) (string, error) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("no command")
	}
	if strings.ContainsFunc(command, func(r rune) bool { return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f }) {
		return "", fmt.Errorf("the command can't hold control characters")
	}
	if _, err := s.ask(ctx, UIRequest{Kind: KindType, Session: session, Text: command}); err != nil {
		return "", err
	}
	return "Typed at the prompt. Nothing ran: the user reads it and presses Enter if they want it.", nil
}

func exitLine(b Block) string {
	if b.ExitCode < 0 {
		return "Exit code: unknown (still running?)"
	}
	return fmt.Sprintf("Exit code: %d", b.ExitCode)
}

// clip keeps the start and the end of a command's output.
func clip(out string) string {
	out = strings.TrimRight(out, "\n")
	lines := strings.Split(out, "\n")
	if len(lines) > headLines+tailLines {
		cut := len(lines) - headLines - tailLines
		lines = append(append(lines[:headLines:headLines], fmt.Sprintf("… (%d lines left out) …", cut)), lines[len(lines)-tailLines:]...)
	}
	s := strings.Join(lines, "\n")
	if len(s) > outputBytes {
		half := outputBytes / 2
		s = s[:half] + "\n… (cut) …\n" + s[len(s)-half:]
	}
	return strings.ToValidUTF8(s, "")
}

// tail is the last lines of out, and how many lines were left out.
func tail(out string, maxLines, maxBytes int) (string, int) {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return "", 0
	}
	lines := strings.Split(out, "\n")
	dropped := 0
	if len(lines) > maxLines {
		dropped = len(lines) - maxLines
		lines = lines[dropped:]
	}
	for len(lines) > 1 && len(strings.Join(lines, "\n")) > maxBytes {
		lines = lines[1:]
		dropped++
	}
	s := strings.Join(lines, "\n")
	if len(s) > maxBytes {
		s = s[len(s)-maxBytes:]
	}
	return strings.ToValidUTF8(s, ""), dropped
}
