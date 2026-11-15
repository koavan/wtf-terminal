// Package agentctx says where a command an agent asks to run would act
// (#40): the Kubernetes context and namespace, AWS profile, account and
// region, Google Cloud project, Azure subscription, docker context, Terraform
// workspace, ssh host or git branch,
// as facts. They come from the shell the agent runs in (what wtf's context
// badges read, from files: nothing asks a cloud), overridden by what the
// command itself says (--context, -n, --profile, AWS_PROFILE=…, cd … &&),
// and each fact says which. A command wtf can't read that far (a variable,
// eval) says so instead of guessing. No fact is a judgement: wtf never
// labels a context as dangerous.
package agentctx

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dayanraj/wtf/internal/infra"
)

// Fact is one thing about where a command acts.
type Fact struct {
	// The program it's about ("kubectl").
	Tool string `json:"tool"`
	// What it is ("context", "namespace", "account").
	Label string `json:"label"`
	// Its value; "" with Unknown.
	Value string `json:"value"`
	// Where it comes from: "" for the shell's own context, else the part of
	// the command that sets it ("--context", "AWS_PROFILE=", "cd").
	From string `json:"from"`
	// The command sets it in a way wtf can't read ($VAR, eval).
	Unknown bool `json:"unknown"`
}

// Resolver reads a shell's context: *infra.Service.
type Resolver interface {
	Resolve(env map[string]string) infra.Context
	KubeContext(env map[string]string, name string) (cluster, namespace string, ok bool)
	DockerContext(env map[string]string, name string) (context, endpoint string)
	AzureSubscription(env map[string]string, want string) (name, id string, ok bool)
}

// Shell is where the command runs: the environment the agent inherited
// from its shell (the variables wtf's shell integration reports) and the
// folder the agent works in.
type Shell struct {
	Env map[string]string
	Cwd string
}

// maxDepth limits sh -c inside sh -c.
const maxDepth = 3

// Of lists the facts about where command would act, in the order its
// commands come. Commands that don't reach a context wtf knows of give none.
func Of(command string, sh Shell, r Resolver) []Fact {
	st := &reader{r: r, env: copyEnv(sh.Env), cwd: sh.Cwd}
	if st.cwd == "" {
		st.cwd = st.env["PWD"]
	}
	st.line(command, 0)
	return st.facts
}

type reader struct {
	r     Resolver
	env   map[string]string
	cwd   string
	facts []Fact
	// The line changed the folder (cd): how wtf follows it, or can't.
	cwdChanged, cwdUnknown bool
	// kubectl config use-context earlier in the line.
	kubeSwitch string
	// docker context use, az account set earlier in the line.
	dockerSwitch, azSwitch string
}

func (st *reader) add(f Fact) {
	for _, g := range st.facts {
		if g == f {
			return
		}
	}
	st.facts = append(st.facts, f)
}

func (st *reader) line(line string, depth int) {
	for _, cmd := range commands(words(line)) {
		st.command(cmd, depth)
	}
}

// command reads one simple command: its NAME=value prefixes, wrappers
// (sudo, env, time…), then the program.
func (st *reader) command(ws []word, depth int) {
	// The variables this command alone gets, and what it sets them from.
	local := map[string]word{}
	for len(ws) > 0 {
		name, value, ok := assignment(ws[0].text)
		if !ok {
			break
		}
		local[name] = word{text: value, dynamic: ws[0].dynamic}
		ws = ws[1:]
	}
	if len(ws) == 0 {
		// Only assignments: they stay for the rest of the line.
		for k, v := range local {
			st.set(k, v)
		}
		return
	}
	ws = unwrap(ws, local)
	if len(ws) == 0 {
		return
	}
	prog := filepath.Base(ws[0].text)
	args := ws[1:]
	switch prog {
	case "cd", "pushd":
		st.cd(args)
		return
	case "export":
		for _, a := range args {
			if n, v, ok := assignment(a.text); ok {
				st.set(n, word{text: v, dynamic: a.dynamic})
			}
		}
		return
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		if v, ok := flag(args, "-c"); ok && depth < maxDepth {
			if v.dynamic {
				st.unknownIn(v.text)
				return
			}
			sub := &reader{r: st.r, env: st.merged(local), cwd: st.cwd, cwdChanged: st.cwdChanged, cwdUnknown: st.cwdUnknown, kubeSwitch: st.kubeSwitch,
				dockerSwitch: st.dockerSwitch, azSwitch: st.azSwitch}
			sub.line(v.text, depth+1)
			for _, f := range sub.facts {
				st.add(f)
			}
		}
		return
	case "eval":
		var parts []string
		for _, a := range args {
			parts = append(parts, a.text)
		}
		st.unknownIn(strings.Join(parts, " "))
		return
	case "aws-vault":
		// aws-vault exec PROFILE [flags] -- command
		if len(args) >= 2 && args[0].text == "exec" {
			profile := args[1]
			rest := args[2:]
			for i, a := range rest {
				if a.text == "--" {
					rest = rest[i+1:]
					break
				}
			}
			local["AWS_VAULT"] = profile
			local["AWS_PROFILE"] = profile
			if len(rest) > 0 && !strings.HasPrefix(rest[0].text, "-") {
				st.program(filepath.Base(rest[0].text), rest[1:], local, "aws-vault exec")
			}
		}
		return
	}
	st.program(prog, args, local, "")
}

// unwrap drops the programs that run another one unchanged (sudo, env,
// time, nohup, nice, timeout, command, exec, watch), keeping env's
// NAME=value in local.
func unwrap(ws []word, local map[string]word) []word {
	for len(ws) > 0 {
		switch filepath.Base(ws[0].text) {
		case "sudo", "nohup", "time", "command", "exec", "nice", "watch", "doas":
			ws = ws[1:]
			for len(ws) > 0 && strings.HasPrefix(ws[0].text, "-") {
				ws = ws[1:]
			}
		case "timeout":
			ws = ws[1:]
			for len(ws) > 0 && strings.HasPrefix(ws[0].text, "-") {
				ws = ws[1:]
			}
			if len(ws) > 0 {
				ws = ws[1:] // the duration
			}
		case "env":
			ws = ws[1:]
			for len(ws) > 0 {
				if n, v, ok := assignment(ws[0].text); ok {
					local[n] = word{text: v, dynamic: ws[0].dynamic}
				} else if !strings.HasPrefix(ws[0].text, "-") {
					break
				}
				ws = ws[1:]
			}
		default:
			return ws
		}
	}
	return ws
}

// set changes a variable for the rest of the line (export, a lone NAME=x).
func (st *reader) set(name string, v word) {
	if v.dynamic {
		st.env[name] = "\x00" + v.text
		return
	}
	st.env[name] = v.text
}

// merged is the environment with this command's own variables.
func (st *reader) merged(local map[string]word) map[string]string {
	env := copyEnv(st.env)
	for k, v := range local {
		if v.dynamic {
			env[k] = "\x00" + v.text
		} else {
			env[k] = v.text
		}
	}
	return env
}

func (st *reader) cd(args []word) {
	st.cwdChanged = true
	var dir word
	for _, a := range args {
		if !strings.HasPrefix(a.text, "-") || a.text == "-" {
			dir = a
			break
		}
	}
	switch {
	case dir.text == "" || dir.text == "~":
		home, _ := os.UserHomeDir()
		st.cwd = home
	case dir.dynamic || dir.text == "-":
		st.cwdUnknown = true
	default:
		st.cwd = st.path(dir.text)
	}
}

// path resolves p against the current folder (and ~).
func (st *reader) path(p string) string {
	if rest, ok := strings.CutPrefix(p, "~"); ok && (rest == "" || rest[0] == '/') {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	if filepath.IsAbs(p) || st.cwd == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(st.cwd, p)
}

// unknownIn: text runs commands wtf can't read; each program it names that
// wtf would have facts for gets an Unknown fact.
func (st *reader) unknownIn(text string) {
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ';' || r == '&' || r == '|' || r == '(' || r == ')' || r == '"' || r == '\'' || r == '\n'
	}) {
		if t := filepath.Base(w); toolOf(t) != "" {
			st.add(Fact{Tool: t, Label: "context", Unknown: true, From: "eval"})
		}
	}
}

func copyEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

// dynamicVar: a variable the command set from something only the shell
// knows.
func dynamicVar(v string) bool { return strings.HasPrefix(v, "\x00") }

// Lines are the facts as the UIs show them, one line per program:
// "kubectl: context prod-eks (from --context), namespace payments (from -n)".
func Lines(facts []Fact) []string {
	var order []string
	byTool := map[string][]string{}
	for _, f := range facts {
		v := f.Value
		if f.Unknown {
			v = "not known from this command"
		}
		part := f.Label + " " + v
		if f.From != "" && f.From != "command" {
			part += " (from " + f.From + ")"
		}
		if _, ok := byTool[f.Tool]; !ok {
			order = append(order, f.Tool)
		}
		byTool[f.Tool] = append(byTool[f.Tool], part)
	}
	out := make([]string, 0, len(order))
	for _, t := range order {
		out = append(out, t+": "+strings.Join(byTool[t], ", "))
	}
	return out
}
