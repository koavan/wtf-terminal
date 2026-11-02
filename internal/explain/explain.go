// Package explain builds "Explain this failure" requests: what a failed
// command block knows (the command, its exit code, the tail of its output,
// the folder, the shell's Kubernetes and AWS context, and the versions of
// the tools it ran), as facts for the AI, and reads the reply back as a
// cause and a suggested fix command. The fix is only ever typed at the
// prompt, never run.
package explain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/tools"
)

const (
	// The output sent is at most this many of its last lines and bytes.
	tailLines = 150
	tailBytes = 12000
	// At most this many tools are asked for their version.
	maxTools       = 4
	versionTimeout = 4 * time.Second
	maxTokens      = 1500
)

// Failure is a finished command block, as its pane knows it.
type Failure struct {
	Command    string  `json:"command"`
	ExitCode   int     `json:"exitCode"`
	DurationMs float64 `json:"durationMs"`
	// The block's output, as shown (the tail is sent).
	Output string `json:"output"`
	// The shell's variables at its last prompt (PWD, PATH, KUBECONFIG…).
	Env map[string]string `json:"env"`
	// The folder, when Env has no PWD.
	Cwd string `json:"cwd"`
}

// Reply is a finished answer, split for the UI.
type Reply struct {
	// What went wrong, as the model put it (the fix block taken out).
	Cause string `json:"cause"`
	// A command to type at the prompt; empty when it gave none.
	Fix string `json:"fix"`
}

type Service struct {
	infra *infra.Service
	// Runs a tool for its version (tests replace it).
	version func(dir string, env []string, path string, args []string) string
}

func NewService(in *infra.Service) *Service {
	return &Service{infra: in, version: runVersion}
}

const system = `You explain why a shell command failed, for an engineer working in a terminal. Use the facts given: the command, its exit code, the end of its output, the folder, the shell's Kubernetes and AWS context, and tool versions.

Reply in plain text without Markdown headings, bold or bullet lists, in this order:
1. The likely cause, in one to three short sentences. Quote the line of output that shows it when there is one. When the facts don't settle it, say what to check.
2. If one command would fix it, or show what's needed to fix it, give exactly one, alone in a fenced block marked sh, for the same shell, folder and context. Use only names that appear in the facts; write <placeholder> for anything you'd have to guess. Leave the block out when no command helps.

The command is typed at the user's prompt for them to read, edit and run; nothing runs it for them.`

// Request is the AI request explaining f. It reads the shell's context and
// asks the tools the command ran for their versions, so call it off the
// main thread.
func (s *Service) Request(f Failure) ai.Request {
	return ai.Request{
		System:    system,
		Messages:  []ai.Message{{Role: "user", Content: s.facts(f)}},
		MaxTokens: maxTokens,
		Env:       f.Env,
	}
}

func (s *Service) facts(f Failure) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	line("Command", strings.TrimSpace(f.Command))
	line("Exit code", exitMeaning(f.ExitCode))
	if f.DurationMs > 0 {
		line("Ran for", duration(f.DurationMs))
	}
	cwd := f.Cwd
	if pwd := f.Env["PWD"]; pwd != "" {
		cwd = pwd
	}
	b.WriteString(ShellFacts(s.infra, f.Env, cwd))
	if v := s.versions(f, cwd); len(v) > 0 {
		b.WriteString("Tool versions:\n")
		for _, l := range v {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}

	out, dropped := tail(f.Output)
	switch {
	case strings.TrimSpace(out) == "":
		b.WriteString("\nOutput: none\n")
	case dropped > 0:
		fmt.Fprintf(&b, "\nOutput (the last %d lines; %d earlier lines left out):\n%s\n", strings.Count(out, "\n")+1, dropped, out)
	default:
		fmt.Fprintf(&b, "\nOutput:\n%s\n", out)
	}
	return b.String()
}

// ShellFacts are the lines that say where a shell is: its folder (home as
// ~), system and shell, and the Kubernetes, AWS, Google Cloud, Terraform
// and git context its variables point at. They're read from files only,
// never asked of a cloud.
func ShellFacts(in *infra.Service, env map[string]string, cwd string) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	line("Folder", tilde(cwd))
	line("System", systemLine(env))
	if in != nil && env != nil {
		if env["PWD"] == "" && cwd != "" {
			env = copyEnv(env)
			env["PWD"] = cwd
		}
		c := in.Resolve(env)
		line("Kubernetes", kube(c))
		line("AWS", aws(c))
		line("Google Cloud project", c.GCPProject)
		line("Terraform workspace", c.TFWorkspace)
		if g := c.Git; g != nil {
			line("Git", gitLine(g))
		}
	}
	return b.String()
}

// ToolsOnPath are the well-known tools (those Explain asks for a version)
// found on a shell's PATH, sorted. Nothing is run.
func ToolsOnPath(env map[string]string) []string {
	path := env["PATH"]
	if path == "" {
		path = os.Getenv("PATH")
	}
	var found []string
	for name := range versionArgs {
		if tools.Find(name, path) != "" {
			found = append(found, name)
		}
	}
	slices.Sort(found)
	return found
}

// SplitCommand splits a reply into its text and the command in its last
// fenced block, made safe to type (see typeable); command is empty when
// there's no block.
func SplitCommand(text string) (rest, command string) {
	r := parse(text)
	return r.Cause, r.Fix
}

// fence is a fenced code block; the last one in a reply is the fix.
var fence = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*[ \t]*\n(.*?)\n?```")

// Parse splits a finished reply into the cause and the fix command.
func (s *Service) Parse(text string) Reply {
	return parse(text)
}

func parse(text string) Reply {
	all := fence.FindAllStringSubmatchIndex(text, -1)
	if len(all) == 0 {
		return Reply{Cause: strings.TrimSpace(text)}
	}
	last := all[len(all)-1]
	fix := strings.TrimSpace(text[last[2]:last[3]])
	// A prompt sign copied along isn't part of the command.
	lines := strings.Split(fix, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(strings.TrimPrefix(l, "$ "), "% ")
	}
	cause := strings.TrimSpace(text[:last[0]] + text[last[1]:])
	return Reply{Cause: cause, Fix: strings.TrimSpace(typeable(strings.Join(lines, "\n")))}
}

// typeable drops what the terminal would act on rather than show: control
// characters (an escape could end a bracketed paste early, a carriage
// return would run the line); a tab becomes a space so it doesn't complete.
func typeable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		}
		return r
	}, s)
}

// ── Facts ────────────────────────────────────────────────────────────────

var signals = map[int]string{
	1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 6: "SIGABRT", 8: "SIGFPE",
	9: "SIGKILL", 11: "SIGSEGV", 13: "SIGPIPE", 14: "SIGALRM", 15: "SIGTERM",
}

// exitMeaning is the code, with what the shell means by it when that's fixed.
func exitMeaning(code int) string {
	s := fmt.Sprint(code)
	switch {
	case code == 126:
		return s + " (the shell's code for a command found but not executable)"
	case code == 127:
		return s + " (the shell's code for command not found)"
	case code > 128 && code < 128+65:
		n := code - 128
		if name := signals[n]; name != "" {
			return fmt.Sprintf("%s (128 + %d: ended by signal %d, %s)", s, n, n, name)
		}
		return fmt.Sprintf("%s (128 + %d: ended by signal %d)", s, n, n)
	}
	return s
}

func duration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0f ms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1f s", ms/1000)
	}
	return fmt.Sprintf("%dm %ds", int(ms/60000), int(ms/1000)%60)
}

// tilde shortens the home folder to ~ (it names the user).
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + rest
	}
	return p
}

// systemLine is "macOS, zsh".
func systemLine(env map[string]string) string {
	osName := map[string]string{"darwin": "macOS", "linux": "Linux"}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	if sh := filepath.Base(env["SHELL"]); env["SHELL"] != "" {
		return osName + ", " + sh
	}
	return osName
}

func kube(c infra.Context) string {
	if c.KubeContext == "" {
		return ""
	}
	parts := []string{"context " + c.KubeContext}
	if c.KubeCluster != "" && c.KubeCluster != c.KubeContext {
		parts = append(parts, "cluster "+c.KubeCluster)
	}
	if c.KubeNamespace != "" {
		parts = append(parts, "namespace "+c.KubeNamespace)
	}
	return strings.Join(parts, ", ")
}

func aws(c infra.Context) string {
	if c.AWSProfile == "" {
		return ""
	}
	parts := []string{"profile " + c.AWSProfile}
	if c.AWSRegion != "" {
		parts = append(parts, "region "+c.AWSRegion)
	}
	if c.AWSAccount != "" {
		parts = append(parts, "account "+c.AWSAccount)
	}
	return strings.Join(parts, ", ")
}

func gitLine(g *infra.Git) string {
	s := "branch " + g.Branch
	if g.Detached {
		s = "detached at " + g.Branch
	}
	if g.Upstream != "" {
		s += ", upstream " + g.Upstream
		if g.UpstreamGone {
			s += " (gone)"
		}
	}
	return s
}

// tail is the end of the output, and how many lines were left out before it.
func tail(out string) (string, int) {
	out = strings.TrimRight(out, "\n ")
	lines := strings.Split(out, "\n")
	dropped := 0
	if len(lines) > tailLines {
		dropped = len(lines) - tailLines
		lines = lines[dropped:]
	}
	for len(lines) > 1 && len(strings.Join(lines, "\n")) > tailBytes {
		lines = lines[1:]
		dropped++
	}
	s := strings.Join(lines, "\n")
	if len(s) > tailBytes {
		s = "…" + s[len(s)-tailBytes:]
	}
	return s, dropped
}

func copyEnv(env map[string]string) map[string]string {
	c := make(map[string]string, len(env)+1)
	for k, v := range env {
		c[k] = v
	}
	return c
}

// ── Tool versions ────────────────────────────────────────────────────────

// versionArgs are the tools asked for their version, and how. Only these:
// any other program could do something else when given --version.
var versionArgs = map[string][]string{
	"kubectl": {"version", "--client"}, "helm": {"version", "--short"}, "kustomize": {"version"},
	"aws": {"--version"}, "eksctl": {"version"}, "gcloud": {"--version"},
	"terraform": {"version"}, "tofu": {"version"}, "terragrunt": {"--version"}, "packer": {"version"},
	"pulumi": {"version"}, "ansible": {"--version"}, "ansible-playbook": {"--version"}, "vault": {"version"},
	"docker": {"--version"}, "podman": {"--version"}, "kind": {"version"}, "minikube": {"version", "--short"},
	"argocd": {"version", "--client", "--short"}, "istioctl": {"version", "--remote=false"}, "skaffold": {"version"},
	"git": {"--version"}, "gh": {"--version"}, "ssh": {"-V"}, "scp": {"-V"}, "curl": {"--version"}, "jq": {"--version"},
	"node": {"--version"}, "npm": {"--version"}, "npx": {"--version"}, "yarn": {"--version"}, "pnpm": {"--version"},
	"deno": {"--version"}, "bun": {"--version"},
	"python": {"--version"}, "python3": {"--version"}, "pip": {"--version"}, "pip3": {"--version"},
	"go": {"version"}, "cargo": {"--version"}, "rustc": {"--version"},
	"java": {"-version"}, "mvn": {"-v"}, "ruby": {"--version"}, "bundle": {"--version"},
	"make": {"--version"}, "psql": {"--version"}, "mysql": {"--version"}, "redis-cli": {"--version"},
}

// wrappers run the next word as the command.
var wrappers = map[string]bool{
	"sudo": true, "time": true, "env": true, "nohup": true, "exec": true, "command": true,
	"nice": true, "xargs": true, "watch": true, "caffeinate": true, "noglob": true,
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// programs are the known tools a command line runs, in order.
func programs(command string) []string {
	var out []string
	seen := map[string]bool{}
	segments := regexp.MustCompile(`\|\||&&|[|;&\n]`).Split(command, -1)
	for _, seg := range segments {
		for _, w := range strings.Fields(seg) {
			w = strings.Trim(w, `"'()`)
			if assignment.MatchString(w) || wrappers[w] || strings.HasPrefix(w, "-") {
				continue
			}
			if _, ok := versionArgs[w]; ok && !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
			break
		}
		if len(out) == maxTools {
			break
		}
	}
	return out
}

// versions asks the tools the command ran for their versions, in parallel,
// from the shell's PATH and folder: "kubectl: v1.31.2".
func (s *Service) versions(f Failure, cwd string) []string {
	progs := programs(f.Command)
	if len(progs) == 0 || s.version == nil {
		return nil
	}
	path := f.Env["PATH"]
	if path == "" {
		path = os.Getenv("PATH")
	}
	env := os.Environ()
	for k, v := range f.Env {
		env = append(env, k+"="+v)
	}
	// No pagers, prompts or update checks while asking.
	env = append(env, "AWS_PAGER=", "AWS_CLI_AUTO_PROMPT=off", "CHECKPOINT_DISABLE=1", "PAGER=cat", "GIT_PAGER=cat")
	dir := cwd
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = os.TempDir()
	}
	out := make([]string, len(progs))
	var wg sync.WaitGroup
	for i, p := range progs {
		exe := tools.Find(p, path)
		if exe == "" {
			out[i] = p + ": not found on the shell's PATH"
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v := s.version(dir, env, exe, versionArgs[p]); v != "" {
				out[i] = p + ": " + v
			}
		}()
	}
	wg.Wait()
	var list []string
	for _, l := range out {
		if l != "" {
			list = append(list, l)
		}
	}
	return list
}

// runVersion is the first line a tool prints for its version.
func runVersion(dir string, env []string, path string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line = strings.TrimPrefix(line, "Client Version: ")
	if len(line) > 100 {
		line = line[:100] + "…"
	}
	return line
}
