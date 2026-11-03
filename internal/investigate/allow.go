// Package investigate holds the read-only allowlist the wtf plugin's
// run_readonly tool (internal/bridge, #32) checks every command against.
package investigate

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The read-only allowlist. The model asks for a program and its arguments
// (argv), never shell text: Check accepts only the commands below, may add
// flags that keep them finite (a log tail, no pager, a timeout), and
// quotes every argument itself, so nothing the model writes can become a
// pipe, redirect, substitution or second command. Anything that changes
// state, follows forever, prompts, switches context or credentials, or
// prints secret values is refused, with the reason going back to the model.

// Command is an accepted command.
type Command struct {
	// What runs, after Check's additions.
	Argv []string `json:"argv"`
	// Argv as one shell line, every argument quoted as needed.
	Line string `json:"line"`
}

// Allowed describes the allowlist, for the model and the manual.
var Allowed = []string{
	"kubectl get, describe, logs (no -f; --tail=200 added when not given), events, top, explain, api-resources, api-versions, version, cluster-info, auth can-i, rollout status|history, config current-context|get-contexts",
	"helm history, status, list, version, get notes|metadata, show chart",
	"aws <service> describe-*, list-*, get-* (not secrets, passwords, tokens, credentials or keys), sts get-caller-identity, s3 ls, logs filter-log-events|tail (no --follow); --no-cli-pager is added",
	"gcloud … list|describe",
	"docker ps, images, inspect, logs (no -f; --tail 200 added), stats (--no-stream added), top, port, version, info",
	"dig, host, nslookup <name>",
	"curl -I/--head <http(s) URL> (no request body, uploads, output files or credentials; --max-time 10 added)",
	"ping (-c 3 added; at most -c 5)",
}

// Refused is why Check turned a command down.
type Refused struct{ Reason string }

func (r Refused) Error() string { return r.Reason }

func refuse(format string, a ...any) error { return Refused{fmt.Sprintf(format, a...)} }

// Check accepts a read-only command, or says why not.
func Check(argv []string) (Command, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Command{}, refuse("no command")
	}
	for _, a := range argv {
		if strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return Command{}, refuse("arguments can't hold control characters or newlines")
		}
	}
	prog := argv[0]
	if strings.Contains(prog, "/") {
		return Command{}, refuse("give the program by name (%s), not a path", prog[strings.LastIndex(prog, "/")+1:])
	}
	args := slices.Clone(argv[1:])
	var err error
	switch prog {
	case "kubectl":
		args, err = kubectl(args)
	case "helm":
		args, err = helm(args)
	case "aws":
		args, err = aws(args)
	case "gcloud":
		args, err = gcloud(args)
	case "docker":
		args, err = docker(args)
	case "dig", "host":
	case "nslookup":
		if len(positional(args, nil)) == 0 {
			err = refuse("nslookup needs a name (without one it waits for input)")
		}
	case "curl":
		args, err = curl(args)
	case "ping":
		args, err = ping(args)
	default:
		return Command{}, refuse("%s isn't on the read-only list", prog)
	}
	if err != nil {
		return Command{}, err
	}
	all := append([]string{prog}, args...)
	return Command{Argv: all, Line: Line(all)}, nil
}

// ── Arguments ────────────────────────────────────────────────────────────

// flagName is "--namespace" for "--namespace=x", "-n" for "-n".
func flagName(a string) string {
	if name, _, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") {
		return name
	}
	return a
}

// positional are the arguments that aren't flags or flag values; valued
// lists the flags that take a separate value.
func positional(args []string, valued map[string]bool) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(out, args[i+1:]...)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if valued[a] && !strings.Contains(a, "=") {
				i++
			}
		default:
			out = append(out, a)
		}
	}
	return out
}

// has reports whether any of the flags is given ("-o", "-o=x" or "--output=x").
func has(args []string, flags ...string) bool {
	for _, a := range args {
		if slices.Contains(flags, flagName(a)) {
			return true
		}
	}
	return false
}

// value is a flag's value ("-o yaml", "-o=yaml", "-oyaml" for a short one).
func value(args []string, flags ...string) (string, bool) {
	for i, a := range args {
		for _, f := range flags {
			switch {
			case a == f && i+1 < len(args):
				return args[i+1], true
			case strings.HasPrefix(a, f+"="):
				return a[len(f)+1:], true
			case len(f) == 2 && strings.HasPrefix(a, f) && len(a) > 2 && !strings.HasPrefix(a, "--"):
				return a[2:], true
			}
		}
	}
	return "", false
}

func forbid(args []string, why string, flags ...string) error {
	for _, a := range args {
		if slices.Contains(flags, flagName(a)) {
			return refuse("%s isn't allowed: %s", flagName(a), why)
		}
	}
	return nil
}

// ── kubectl ──────────────────────────────────────────────────────────────

var kubeValued = set("-n", "--namespace", "-o", "--output", "-l", "--selector", "--field-selector", "-c", "--container",
	"--sort-by", "--tail", "--since", "--since-time", "--request-timeout", "--limit-bytes", "--chunk-size", "-L",
	"--label-columns", "--template", "--max-log-requests", "--for", "--revision", "--subresource", "--types")

func kubectl(args []string) ([]string, error) {
	if err := forbid(args, "the investigation uses the shell's own cluster and credentials",
		"--context", "--kubeconfig", "--cluster", "--user", "--token", "--server", "-s", "--as", "--as-group", "--as-uid",
		"--username", "--password", "--client-key", "--client-certificate", "--certificate-authority", "--insecure-skip-tls-verify"); err != nil {
		return nil, err
	}
	if err := forbid(args, "it would never end, take input or read files", "-w", "--watch", "--watch-only", "-f", "--follow",
		"--filename", "-i", "--stdin", "-t", "--tty", "-k", "--kustomize", "--raw"); err != nil {
		return nil, err
	}
	pos := positional(args, kubeValued)
	if len(pos) == 0 {
		return nil, refuse("kubectl needs a subcommand")
	}
	sub := pos[0]
	switch sub {
	case "get", "describe", "events", "top", "explain", "api-resources", "api-versions", "version":
	case "cluster-info":
		if slices.Contains(pos, "dump") {
			return nil, refuse("kubectl cluster-info dump prints far too much; ask for specific objects")
		}
	case "logs":
		if !has(args, "--tail") {
			args = append(args, "--tail=200")
		}
	case "auth":
		if len(pos) < 2 || pos[1] != "can-i" {
			return nil, refuse("only kubectl auth can-i is allowed")
		}
	case "rollout":
		if len(pos) < 2 || (pos[1] != "status" && pos[1] != "history") {
			return nil, refuse("only kubectl rollout status and history are allowed")
		}
		if pos[1] == "status" && !has(args, "--watch") {
			args = append(args, "--watch=false")
		}
	case "config":
		if len(pos) < 2 || (pos[1] != "current-context" && pos[1] != "get-contexts") {
			return nil, refuse("only kubectl config current-context and get-contexts are allowed")
		}
	default:
		return nil, refuse("kubectl %s isn't read-only (allowed: get, describe, logs, events, top, explain, api-resources, version, cluster-info, auth can-i, rollout status|history)", sub)
	}
	// A secret's data shows only in a full output format.
	if out, ok := value(args, "-o", "--output"); ok && out != "wide" && out != "name" && sub == "get" {
		for _, p := range pos[1:] {
			if strings.Contains(strings.ToLower(p), "secret") {
				return nil, refuse("kubectl get secrets -o %s would print secret values; list them without -o, or describe them", out)
			}
		}
	}
	return args, nil
}

// ── helm ─────────────────────────────────────────────────────────────────

var helmValued = set("-n", "--namespace", "-o", "--output", "--max", "--revision", "-l", "--selector", "--filter")

func helm(args []string) ([]string, error) {
	if err := forbid(args, "the investigation uses the shell's own cluster and credentials",
		"--kube-context", "--kubeconfig", "--kube-token", "--kube-as-user", "--kube-as-group", "--kube-apiserver", "--kube-ca-file",
		"--kube-insecure-skip-tls-verify"); err != nil {
		return nil, err
	}
	pos := positional(args, helmValued)
	if len(pos) == 0 {
		return nil, refuse("helm needs a subcommand")
	}
	switch pos[0] {
	case "history", "status", "list", "ls", "version":
	case "get":
		if len(pos) < 2 || (pos[1] != "notes" && pos[1] != "metadata") {
			return nil, refuse("only helm get notes and metadata are allowed: values and manifests can hold secrets")
		}
	case "show":
		if len(pos) < 2 || pos[1] != "chart" {
			return nil, refuse("only helm show chart is allowed")
		}
	default:
		return nil, refuse("helm %s isn't read-only (allowed: history, status, list, version, get notes|metadata, show chart)", pos[0])
	}
	return args, nil
}

// ── aws ──────────────────────────────────────────────────────────────────

var (
	awsValued = set("--region", "--output", "--query", "--endpoint-url", "--color", "--cli-read-timeout",
		"--cli-connect-timeout", "--max-items", "--page-size", "--starting-token")
	awsSecret = regexp.MustCompile(`(?i)secret|password|token|credential|login|decrypt|key-pair|private-key|signing|presign|session|authoriz`)
)

func aws(args []string) ([]string, error) {
	if err := forbid(args, "the investigation uses the shell's own AWS profile", "--profile"); err != nil {
		return nil, err
	}
	if err := forbid(args, "it would print secret values or never end", "--with-decryption", "--follow"); err != nil {
		return nil, err
	}
	pos := positional(args, awsValued)
	if len(pos) < 2 {
		return nil, refuse("aws needs a service and an operation, e.g. aws ec2 describe-instances")
	}
	svc, op := pos[0], pos[1]
	switch {
	case svc == "s3" && op == "ls":
	case svc == "logs" && op == "tail":
	case svc == "sts" && op == "get-caller-identity":
	case svc == "cloudtrail" && op == "lookup-events", svc == "logs" && op == "filter-log-events":
	case strings.HasPrefix(op, "describe-") || strings.HasPrefix(op, "list-") || strings.HasPrefix(op, "get-"):
		if awsSecret.MatchString(op) {
			return nil, refuse("aws %s %s could print secrets or credentials", svc, op)
		}
	default:
		return nil, refuse("aws %s %s isn't read-only (allowed: describe-*, list-*, get-*, s3 ls, logs tail)", svc, op)
	}
	if !has(args, "--no-cli-pager") {
		args = append(args, "--no-cli-pager")
	}
	return args, nil
}

// ── gcloud ───────────────────────────────────────────────────────────────

func gcloud(args []string) ([]string, error) {
	if err := forbid(args, "the investigation uses the shell's own account and configuration",
		"--account", "--impersonate-service-account", "--configuration", "--access-token-file"); err != nil {
		return nil, err
	}
	pos := positional(args, set("--project", "--region", "--zone", "--format", "--filter", "--limit", "--location"))
	if len(pos) < 2 {
		return nil, refuse("gcloud needs a command group and list or describe")
	}
	if verb := pos[len(pos)-1]; verb != "list" && verb != "describe" && (len(pos) < 2 || pos[len(pos)-2] != "describe") {
		return nil, refuse("only gcloud … list and … describe are allowed")
	}
	if slices.Contains(pos, "secrets") && slices.Contains(pos, "access") {
		return nil, refuse("gcloud secrets access prints secret values")
	}
	return args, nil
}

// ── docker ───────────────────────────────────────────────────────────────

func docker(args []string) ([]string, error) {
	if err := forbid(args, "it would never end or reach another host", "-f", "--follow", "-H", "--host"); err != nil {
		return nil, err
	}
	pos := positional(args, set("--format", "-n", "--tail", "--since", "--until", "--filter"))
	if len(pos) == 0 {
		return nil, refuse("docker needs a subcommand")
	}
	switch pos[0] {
	case "ps", "images", "inspect", "top", "port", "version", "info":
	case "logs":
		if !has(args, "--tail", "-n") {
			args = append(args, "--tail", "200")
		}
	case "stats":
		if !has(args, "--no-stream") {
			args = append(args, "--no-stream")
		}
	default:
		return nil, refuse("docker %s isn't read-only (allowed: ps, images, inspect, logs, stats, top, port, version, info)", pos[0])
	}
	return args, nil
}

// ── curl ─────────────────────────────────────────────────────────────────

var (
	curlLong = set("--request", "--data", "--data-raw", "--data-binary", "--data-urlencode", "--data-ascii", "--json", "--form",
		"--form-string", "--upload-file", "--output", "--remote-name", "--remote-name-all", "--config", "--user", "--oauth2-bearer",
		"--cookie-jar", "--dump-header", "--unix-socket", "--abstract-unix-socket", "--output-dir", "--netrc", "--netrc-file", "--proxy-user")
	curlShort  = "XdFTOoKucDn" // request, data, form, upload, output, config, user, cookie jar, dump header, netrc
	curlValued = set("-H", "--header", "-m", "--max-time", "--connect-timeout", "-A", "--user-agent", "-e", "--referer",
		"--resolve", "--connect-to", "-x", "--proxy", "-w", "--write-out", "--retry", "-b", "--cookie")
)

func curl(args []string) ([]string, error) {
	head := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--"):
			name := flagName(a)
			if curlLong[name] {
				return nil, refuse("curl %s isn't allowed: only HEAD requests that send no data and write no files", name)
			}
			if name == "--head" {
				head = true
			}
			if curlValued[name] && !strings.Contains(a, "=") {
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j, ch := range a[1:] {
				if strings.ContainsRune(curlShort, ch) {
					return nil, refuse("curl -%c isn't allowed: only HEAD requests that send no data and write no files", ch)
				}
				if ch == 'I' {
					head = true
				}
				// A short flag with a value ends the group: -H 'x', -m10.
				if curlValued["-"+string(ch)] {
					if j == len(a)-2 {
						i++
					}
					break
				}
			}
		}
	}
	if !head {
		return nil, refuse("curl must be a HEAD request: add -I")
	}
	urls := 0
	for _, p := range positional(args, curlValued) {
		if !strings.HasPrefix(p, "http://") && !strings.HasPrefix(p, "https://") {
			return nil, refuse("curl only fetches http:// and https:// URLs, not %q", p)
		}
		urls++
	}
	if urls == 0 {
		return nil, refuse("curl needs a URL")
	}
	if !has(args, "-m", "--max-time") {
		args = append(args, "--max-time", "10")
	}
	return args, nil
}

// ── ping ─────────────────────────────────────────────────────────────────

func ping(args []string) ([]string, error) {
	if err := forbid(args, "it would flood the network or never end", "-f", "-i", "-l"); err != nil {
		return nil, err
	}
	if v, ok := value(args, "-c"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 5 {
			return nil, refuse("ping -c must be 1 to 5")
		}
	} else {
		args = append([]string{"-c", "3"}, args...)
	}
	if len(positional(args, set("-c", "-W", "-w", "-t", "-s"))) == 0 {
		return nil, refuse("ping needs a host")
	}
	return args, nil
}

// ── Quoting ──────────────────────────────────────────────────────────────

var plain = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// Line is argv as one shell line: every argument the shell would read
// differently is single-quoted.
func Line(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if plain.MatchString(a) {
			parts[i] = a
		} else {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(parts, " ")
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}
