package agentctx

import (
	"os"
	"path/filepath"
	"strings"
)

// toolOf is the kind of context a program acts in ("" for none wtf knows).
func toolOf(prog string) string {
	switch prog {
	case "kubectl", "oc", "helm":
		return "kube"
	case "terraform", "tofu", "terragrunt":
		return "terraform"
	case "aws", "sam", "cdk":
		return "aws"
	case "gcloud", "gsutil", "bq":
		return "gcloud"
	case "az":
		return "azure"
	case "docker", "docker-compose":
		return "docker"
	case "ssh":
		return "ssh"
	case "git":
		return "git"
	}
	return ""
}

// program reads one program's command line. via names what ran it, when
// that set its context (aws-vault exec).
func (st *reader) program(prog string, args []word, local map[string]word, via string) {
	env := st.merged(local)
	if st.cwd != "" {
		env["PWD"] = st.cwd
	}
	// Where a variable came from: this command, earlier in the line, or
	// the shell ("").
	from := func(name string) string {
		if via != "" && (name == "AWS_PROFILE" || name == "AWS_VAULT") {
			return via
		}
		if _, ok := local[name]; ok {
			return name + "="
		}
		return ""
	}
	switch toolOf(prog) {
	case "kube":
		st.kube(prog, args, env, from)
	case "terraform":
		st.terraform(prog, args, env)
	case "aws":
		st.aws(prog, args, env, from)
	case "gcloud":
		st.gcloud(prog, args, env, from)
	case "azure":
		st.azure(prog, args, env, from)
	case "docker":
		st.docker(prog, args, env, from)
	case "ssh":
		st.ssh(args)
	case "git":
		st.git(args, env)
	}
}

// subcommand is the first argument that isn't an option.
func subcommand(args []word) string {
	for _, a := range args {
		if !strings.HasPrefix(a.text, "-") {
			return a.text
		}
	}
	return ""
}

// afterSub is what follows the subcommand.
func afterSub(args []word) []word {
	for i, a := range args {
		if !strings.HasPrefix(a.text, "-") {
			return args[i+1:]
		}
	}
	return nil
}

// unknownVar: a variable this line set from something only the shell
// knows; Fact says so.
func (st *reader) unknownVar(tool, label, name string, env map[string]string) bool {
	if dynamicVar(env[name]) {
		st.add(Fact{Tool: tool, Label: label, Unknown: true, From: name + "="})
		return true
	}
	return false
}

// ── Kubernetes: kubectl, oc, helm ────────────────────────────────────────

// Subcommands that don't talk to a cluster.
var kubeLocal = map[string]map[string]bool{
	"kubectl": {"completion": true, "kustomize": true, "plugin": true, "help": true},
	"oc":      {"completion": true, "kustomize": true, "plugin": true, "help": true},
	"helm": {"completion": true, "create": true, "dependency": true, "dep": true, "env": true, "help": true, "lint": true,
		"package": true, "plugin": true, "pull": true, "push": true, "repo": true, "search": true, "show": true, "inspect": true,
		"template": true, "verify": true, "version": true, "registry": true},
}

func (st *reader) kube(prog string, args []word, env map[string]string, from func(string) string) {
	sub := subcommand(args)
	if kubeLocal[prog][sub] || (sub == "version" && has(args, "--client")) || len(args) == 0 {
		return
	}
	kcFrom := from("KUBECONFIG")
	if v, ok := flag(args, "--kubeconfig"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "context", Unknown: true, From: "--kubeconfig"})
			return
		}
		env["KUBECONFIG"], kcFrom = st.path(v.text), "--kubeconfig"
	}
	if st.unknownVar(prog, "context", "KUBECONFIG", env) {
		return
	}
	if sub == "config" {
		// kubectl config use-context X: the line's later commands use X.
		rest := afterSub(args)
		if subcommand(rest) == "use-context" && len(rest) > 1 {
			v := rest[len(rest)-1]
			if v.dynamic {
				st.add(Fact{Tool: prog, Label: "switches context to", Unknown: true})
			} else {
				st.add(Fact{Tool: prog, Label: "switches context to", Value: v.text})
				st.kubeSwitch = v.text
			}
		}
		return
	}
	ctxFlag := "--context"
	if prog == "helm" {
		ctxFlag = "--kube-context"
	}
	var name, nameFrom, cluster, ns string
	if v, ok := flag(args, ctxFlag); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "context", Unknown: true, From: ctxFlag})
			return
		}
		name, nameFrom = v.text, ctxFlag
	} else if st.kubeSwitch != "" {
		name, nameFrom = st.kubeSwitch, "use-context"
	}
	if name == "" {
		c := st.r.Resolve(env)
		if c.KubeContext == "" {
			st.add(Fact{Tool: prog, Label: "context", Value: "none set", From: kcFrom})
			return
		}
		name, nameFrom, cluster, ns = c.KubeContext, kcFrom, c.KubeCluster, c.KubeNamespace
	} else {
		var ok bool
		if cluster, ns, ok = st.r.KubeContext(env, name); !ok {
			st.add(Fact{Tool: prog, Label: "context", Value: name + ", which isn't in the kubeconfig files", From: nameFrom})
			return
		}
	}
	st.add(Fact{Tool: prog, Label: "context", Value: name, From: nameFrom})
	if cluster != "" && cluster != name {
		st.add(Fact{Tool: prog, Label: "cluster", Value: cluster, From: nameFrom})
	}
	nsFrom := nameFrom
	if has(args, "-A", "--all-namespaces") {
		ns, nsFrom = "all namespaces", "-A"
	} else if v, ok := flag(args, "-n", "--namespace"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "namespace", Unknown: true, From: "-n"})
			return
		}
		ns, nsFrom = v.text, "-n"
	} else if ns == "" {
		ns = "default"
	}
	st.add(Fact{Tool: prog, Label: "namespace", Value: ns, From: nsFrom})
}

// ── Terraform, OpenTofu, Terragrunt ──────────────────────────────────────

// Subcommands that don't touch state.
var tfLocal = map[string]bool{"fmt": true, "validate": true, "version": true, "providers": true, "graph": true,
	"get": true, "login": true, "logout": true, "metadata": true, "test": true, "-version": true, "-help": true, "": true}

func (st *reader) terraform(prog string, args []word, env map[string]string) {
	dir, dirFrom := st.cwd, ""
	if st.cwdChanged {
		dirFrom = "cd"
	}
	if st.cwdUnknown {
		st.add(Fact{Tool: prog, Label: "folder", Unknown: true, From: "cd"})
		return
	}
	if v, ok := flag(args, "-chdir"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "folder", Unknown: true, From: "-chdir"})
			return
		}
		dir, dirFrom = st.path(v.text), "-chdir"
	}
	sub := subcommand(args)
	if tfLocal[sub] {
		return
	}
	if dir != "" {
		st.add(Fact{Tool: prog, Label: "folder", Value: tilde(dir), From: dirFrom})
	}
	if sub == "workspace" {
		rest := afterSub(args)
		if s := subcommand(rest); (s == "select" || s == "new") && len(rest) > 1 {
			if v := rest[len(rest)-1]; !v.dynamic {
				st.add(Fact{Tool: prog, Label: "switches workspace to", Value: v.text})
			}
		}
		return
	}
	if prog == "terragrunt" || sub == "init" {
		return
	}
	if dynamicVar(env["TF_WORKSPACE"]) {
		st.add(Fact{Tool: prog, Label: "workspace", Unknown: true, From: "TF_WORKSPACE="})
		return
	}
	data := env["TF_DATA_DIR"]
	if data == "" {
		data = ".terraform"
	}
	if !filepath.IsAbs(data) {
		data = filepath.Join(dir, data)
	}
	ws, wsFrom := env["TF_WORKSPACE"], ""
	if ws != "" {
		wsFrom = "TF_WORKSPACE"
	} else if fi, err := os.Stat(data); err != nil || !fi.IsDir() {
		st.add(Fact{Tool: prog, Label: "workspace", Value: "none: the folder isn't initialized"})
		return
	} else if b, err := os.ReadFile(filepath.Join(data, "environment")); err == nil {
		ws = strings.TrimSpace(string(b))
	}
	if ws == "" {
		ws = "default"
	}
	st.add(Fact{Tool: prog, Label: "workspace", Value: ws, From: wsFrom})
}

// ── AWS: aws, sam, cdk ───────────────────────────────────────────────────

func (st *reader) aws(prog string, args []word, env map[string]string, from func(string) string) {
	sub := subcommand(args)
	if sub == "configure" || sub == "help" || len(args) == 0 || has(args, "--version") {
		return
	}
	profFrom := from("AWS_PROFILE")
	if profFrom == "" && env["AWS_PROFILE"] == "" {
		profFrom = from("AWS_DEFAULT_PROFILE")
	}
	if v, ok := flag(args, "--profile"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "profile", Unknown: true, From: "--profile"})
			return
		}
		// An explicit profile wins over access keys in the environment.
		env["AWS_PROFILE"], profFrom = v.text, "--profile"
		delete(env, "WTF_AWS_ENV_KEYS")
		delete(env, "AWS_VAULT")
	}
	if st.unknownVar(prog, "profile", "AWS_PROFILE", env) {
		return
	}
	regFrom := from("AWS_REGION")
	if regFrom == "" && env["AWS_REGION"] == "" {
		regFrom = from("AWS_DEFAULT_REGION")
	}
	if v, ok := flag(args, "--region"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "region", Unknown: true, From: "--region"})
		} else {
			env["AWS_REGION"], regFrom = v.text, "--region"
		}
	}
	c := st.r.Resolve(env)
	if c.AWSProfile == "" {
		st.add(Fact{Tool: prog, Label: "profile", Value: "none set", From: profFrom})
		return
	}
	st.add(Fact{Tool: prog, Label: "profile", Value: c.AWSProfile, From: profFrom})
	switch {
	case c.AWSAccount != "":
		st.add(Fact{Tool: prog, Label: "account", Value: c.AWSAccount, From: profFrom})
	case c.AWSAccountNote != "":
		st.add(Fact{Tool: prog, Label: "account", Value: "not known (" + c.AWSAccountNote + ")", From: profFrom})
	}
	if c.AWSRegion != "" && !dynamicVar(c.AWSRegion) {
		st.add(Fact{Tool: prog, Label: "region", Value: c.AWSRegion, From: regFrom})
	}
}

// ── Google Cloud: gcloud, gsutil, bq ─────────────────────────────────────

func (st *reader) gcloud(prog string, args []word, env map[string]string, from func(string) string) {
	sub := subcommand(args)
	if sub == "config" || sub == "auth" || sub == "components" || sub == "help" || sub == "version" || len(args) == 0 {
		return
	}
	pFrom := from("CLOUDSDK_CORE_PROJECT")
	if v, ok := flag(args, "--configuration"); ok && !v.dynamic {
		env["CLOUDSDK_ACTIVE_CONFIG_NAME"] = v.text
		delete(env, "CLOUDSDK_CORE_PROJECT")
		pFrom = "--configuration"
	}
	if v, ok := flag(args, "--project"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "project", Unknown: true, From: "--project"})
			return
		}
		env["CLOUDSDK_CORE_PROJECT"], pFrom = v.text, "--project"
	}
	if st.unknownVar(prog, "project", "CLOUDSDK_CORE_PROJECT", env) {
		return
	}
	p := st.r.Resolve(env).GCPProject
	if p == "" {
		p = "none set"
	}
	st.add(Fact{Tool: prog, Label: "project", Value: p, From: pFrom})
}

// ── Azure: az ────────────────────────────────────────────────────────────

func (st *reader) azure(prog string, args []word, env map[string]string, from func(string) string) {
	sub := subcommand(args)
	switch sub {
	case "", "login", "logout", "configure", "version", "help", "extension", "upgrade", "find", "interactive", "init":
		return
	case "account":
		// az account set --subscription X: later commands use X.
		if subcommand(afterSub(args)) == "set" {
			if v, ok := flag(args, "--subscription", "-s", "--name", "-n"); ok && !v.dynamic {
				st.add(Fact{Tool: prog, Label: "switches subscription to", Value: v.text})
				st.azSwitch = v.text
			}
		}
		return
	}
	if dynamicVar(env["AZURE_CONFIG_DIR"]) {
		st.add(Fact{Tool: prog, Label: "subscription", Unknown: true, From: "AZURE_CONFIG_DIR="})
		return
	}
	want, wantFrom := st.azSwitch, ""
	if want != "" {
		wantFrom = "account set"
	} else if from("AZURE_CONFIG_DIR") != "" {
		wantFrom = "AZURE_CONFIG_DIR="
	}
	if v, ok := flag(args, "--subscription"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "subscription", Unknown: true, From: "--subscription"})
			return
		}
		want, wantFrom = v.text, "--subscription"
	}
	name, id, ok := st.r.AzureSubscription(env, want)
	switch {
	case ok:
		st.add(Fact{Tool: prog, Label: "subscription", Value: name + " (" + id + ")", From: wantFrom})
	case want != "":
		st.add(Fact{Tool: prog, Label: "subscription", Value: want + ", which isn't in the CLI's profile", From: wantFrom})
	default:
		st.add(Fact{Tool: prog, Label: "subscription", Value: "none set", From: wantFrom})
	}
}

// ── Docker: docker, docker-compose ───────────────────────────────────────

func (st *reader) docker(prog string, args []word, env map[string]string, from func(string) string) {
	// docker's own options come before the subcommand (docker run -c is
	// CPU shares).
	global := args[:len(args)-len(globalArgsSkipped(args))]
	switch sub := subcommand(globalArgsSkipped(args)); sub {
	case "", "context", "help", "completion", "login", "logout", "search", "manifest", "trust", "init":
		if sub == "context" {
			rest := afterSub(globalArgsSkipped(args))
			if subcommand(rest) == "use" && len(rest) > 1 && !rest[len(rest)-1].dynamic {
				st.add(Fact{Tool: prog, Label: "switches context to", Value: rest[len(rest)-1].text})
				st.dockerSwitch = rest[len(rest)-1].text
			}
		}
		return
	}
	if v, ok := flag(global, "-H", "--host"); ok {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "daemon", Unknown: true, From: "-H"})
		} else {
			st.add(Fact{Tool: prog, Label: "daemon", Value: v.text, From: "-H"})
		}
		return
	}
	name, nameFrom := st.dockerSwitch, ""
	if name != "" {
		nameFrom = "context use"
	}
	if v, ok := flag(global, "--context", "-c"); ok && prog == "docker" {
		if v.dynamic {
			st.add(Fact{Tool: prog, Label: "context", Unknown: true, From: "--context"})
			return
		}
		name, nameFrom = v.text, "--context"
	}
	for _, n := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG"} {
		if st.unknownVar(prog, "context", n, env) {
			return
		}
		if name == "" && from(n) != "" && env[n] != "" {
			nameFrom = n + "="
		}
	}
	ctx, endpoint := st.r.DockerContext(env, name)
	if ctx != "" {
		st.add(Fact{Tool: prog, Label: "context", Value: ctx, From: nameFrom})
	}
	switch {
	case endpoint != "":
		st.add(Fact{Tool: prog, Label: "daemon", Value: endpoint, From: nameFrom})
	case ctx != "":
		st.add(Fact{Tool: prog, Label: "daemon", Value: "not known: no such context", From: nameFrom})
	}
}

// globalArgsSkipped drops docker's options that come before the subcommand
// and take a value, so the subcommand is found.
func globalArgsSkipped(args []word) []word {
	for i := 0; i < len(args); i++ {
		switch a := args[i].text; {
		case a == "--context" || a == "-c" || a == "-H" || a == "--host" || a == "--config" || a == "-l" || a == "--log-level" ||
			a == "--tlscacert" || a == "--tlscert" || a == "--tlskey" || a == "-f" || a == "--file" || a == "-p" || a == "--project-name":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return args[i:]
		}
	}
	return nil
}

// ── ssh ──────────────────────────────────────────────────────────────────

// ssh options that take a value.
const sshValued = "BbcDEeFIiJLlmOoPpQRSWw"

func (st *reader) ssh(args []word) {
	var jump string
	for i := 0; i < len(args); i++ {
		a := args[i].text
		if a == "--" {
			i++
			if i < len(args) {
				st.sshHost(args[i], jump)
			}
			return
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			st.sshHost(args[i], jump)
			return
		}
		// -J host, -Jhost; flags may be grouped (-vJ host).
		for j := 1; j < len(a); j++ {
			if strings.IndexByte(sshValued, a[j]) < 0 {
				continue
			}
			v := a[j+1:]
			if v == "" && i+1 < len(args) {
				i++
				v = args[i].text
			}
			if a[j] == 'J' {
				jump = v
			}
			break
		}
	}
}

func (st *reader) sshHost(host word, jump string) {
	if host.dynamic {
		st.add(Fact{Tool: "ssh", Label: "host", Unknown: true})
		return
	}
	st.add(Fact{Tool: "ssh", Label: "host", Value: host.text, From: "command"})
	if jump != "" {
		st.add(Fact{Tool: "ssh", Label: "through", Value: jump, From: "-J"})
	}
}

// ── git push ─────────────────────────────────────────────────────────────

func (st *reader) git(args []word, env map[string]string) {
	// git -C dir push …: global options come first.
	for len(args) > 0 && strings.HasPrefix(args[0].text, "-") {
		switch a := args[0].text; {
		case a == "-C" && len(args) > 1:
			if args[1].dynamic {
				return
			}
			env["PWD"] = st.path(args[1].text)
			args = args[2:]
		case a == "-c" && len(args) > 1:
			args = args[2:]
		default:
			args = args[1:]
		}
	}
	if len(args) == 0 || args[0].text != "push" {
		return
	}
	args = args[1:]
	var pos []word
	force := ""
	for i := 0; i < len(args); i++ {
		a := args[i].text
		switch {
		case a == "-f" || a == "--force" || strings.HasPrefix(a, "--force-with-lease") || a == "--force-if-includes":
			if force == "" {
				force = a
			}
		case a == "--mirror" || a == "--all" || a == "--tags" || a == "--delete" || a == "-d":
			pos = append(pos, word{text: a})
		case a == "-o" || a == "--push-option" || a == "--repo" || a == "--receive-pack" || a == "--exec":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			pos = append(pos, args[i])
		}
	}
	g := st.r.Resolve(env).Git
	if g == nil {
		return
	}
	var remote, refs []string
	for _, p := range pos {
		if p.dynamic {
			st.add(Fact{Tool: "git", Label: "pushes to", Unknown: true})
			return
		}
		if strings.HasPrefix(p.text, "--") || p.text == "-d" {
			refs = append(refs, p.text)
		} else if remote == nil {
			remote = []string{p.text}
		} else {
			refs = append(refs, p.text)
			if strings.HasPrefix(p.text, "+") && force == "" {
				force = "+" + strings.TrimPrefix(p.text, "+")
			}
		}
	}
	st.add(Fact{Tool: "git", Label: "branch", Value: g.Branch})
	switch {
	case remote != nil && len(refs) > 0:
		st.add(Fact{Tool: "git", Label: "pushes to", Value: remote[0] + " " + strings.Join(refs, " "), From: "command"})
	case remote != nil:
		st.add(Fact{Tool: "git", Label: "pushes to", Value: remote[0], From: "command"})
	case g.Upstream != "":
		st.add(Fact{Tool: "git", Label: "pushes to", Value: g.Upstream})
	default:
		st.add(Fact{Tool: "git", Label: "pushes to", Value: "no upstream set"})
	}
	if force != "" {
		st.add(Fact{Tool: "git", Label: "force", Value: "yes", From: force})
	}
}

// tilde shortens the home folder to ~.
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
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
