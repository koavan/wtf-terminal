// Package kube reads what the Kubernetes side-pane tab shows: the
// deployments, pods and recent events of a namespace, one kind at a time
// and only when the tab asks (a section is open), so nothing reaches the
// cluster that isn't on screen.
//
// It only reads. Everything goes through kubectl, run with the focused
// shell's own variables (KUBECONFIG, AWS_PROFILE…, and PATH, so exec auth
// plugins like aws-iam-authenticator or gke-gcloud-auth-plugin are found),
// so it sees exactly what `kubectl` typed in that shell would. Actions on
// the cluster — logs, exec, restarts — are real kubectl commands the
// frontend runs in a terminal tab.
package kube

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Request says whose view of the cluster to take.
type Request struct {
	// The variables the shell reported (PWD, PATH, KUBECONFIG, AWS_*…).
	Env map[string]string `json:"env"`
	// "" means the kubeconfig's current context, and that context's namespace.
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
}

type Container struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Ready    bool   `json:"ready"`
	Restarts int    `json:"restarts"`
	// Why it isn't running, as Kubernetes says: CrashLoopBackOff,
	// ImagePullBackOff, OOMKilled, Completed…
	Reason string `json:"reason"`
	// Why its last run ended, if it restarted.
	LastReason string `json:"lastReason"`
}

type Pod struct {
	Name string `json:"name"`
	// The STATUS column of `kubectl get pods`.
	Status   string `json:"status"`
	Phase    string `json:"phase"`
	Ready    int    `json:"ready"`
	Total    int    `json:"total"`
	Restarts int    `json:"restarts"`
	Node     string `json:"node"`
	IP       string `json:"ip"`
	Created  string `json:"created"`
	// When the last restart happened, if any (RFC 3339).
	LastRestart string            `json:"lastRestart"`
	Containers  []Container       `json:"containers"`
	Labels      map[string]string `json:"labels"`
	// The workload that owns it, e.g. "ReplicaSet/web-7d9f".
	Owner string `json:"owner"`
}

type Deployment struct {
	Name      string            `json:"name"`
	Desired   int               `json:"desired"`
	Ready     int               `json:"ready"`
	UpToDate  int               `json:"upToDate"`
	Available int               `json:"available"`
	Images    []string          `json:"images"`
	Selector  map[string]string `json:"selector"`
	Created   string            `json:"created"`
	// Conditions that aren't True, as "Type: message".
	Problems []string `json:"problems"`
}

type Event struct {
	Type    string `json:"type"` // Normal, Warning
	Reason  string `json:"reason"`
	Object  string `json:"object"` // "Pod/web-7d9f-abc"
	Message string `json:"message"`
	Count   int    `json:"count"`
	Last    string `json:"last"`
}

// Target is where the tab looks: a context, its cluster, and a namespace.
type Target struct {
	Context   string `json:"context"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
}

const (
	timeout   = 20 * time.Second
	maxEvents = 40
)

// runKubectl runs kubectl and returns stdout; tests replace it.
var runKubectl = func(ctx context.Context, bin string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Stdin = nil // an auth prompt fails instead of waiting
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, errors.New("the cluster didn't answer in time")
	}
	if err != nil {
		return nil, errors.New(lastLine(stderr.String(), err.Error()))
	}
	return out, nil
}

type Service struct {
	home string
	// Where per-shell kubeconfig files go (see configs.go).
	stateDir string
	// Overrides the cache folder for backups; tests set it.
	cacheDir string
}

// NewService keeps per-shell kubeconfig files in stateDir.
func NewService(stateDir string) *Service {
	home, _ := os.UserHomeDir()
	return &Service{home: home, stateDir: stateDir}
}

// ServiceStartup clears the per-shell files of an earlier run.
func (s *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	s.ClearShellConfigs()
	return nil
}

// Variables taken from the shell rather than wtf's own environment.
func shellVar(name string) bool {
	return name == "PATH" || name == "KUBECONFIG" ||
		strings.HasPrefix(name, "AWS_") || strings.HasPrefix(name, "CLOUDSDK_") ||
		strings.HasPrefix(name, "AZURE_")
}

// env is wtf's environment with the shell's cluster and cloud variables in
// place of its own. A relative KUBECONFIG is relative to the shell's folder.
func (s *Service) env(r Request) []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, fromShell := r.Env[name]; fromShell && shellVar(name) {
			continue
		}
		if name == "KUBECONFIG" || strings.HasPrefix(name, "AWS_") {
			continue // the shell didn't set them: neither does wtf
		}
		out = append(out, kv)
	}
	for name, value := range r.Env {
		if !shellVar(name) || strings.ContainsAny(value, "\x00\n") {
			continue
		}
		if name == "KUBECONFIG" {
			value = s.absList(value, r.Env["PWD"])
		}
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out
}

func (s *Service) absList(list, pwd string) string {
	parts := filepath.SplitList(list)
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "~/"):
			parts[i] = filepath.Join(s.home, p[2:])
		case p != "" && !filepath.IsAbs(p) && pwd != "":
			parts[i] = filepath.Join(pwd, p)
		}
	}
	return strings.Join(parts, string(filepath.ListSeparator))
}

// kubectl finds kubectl on the shell's PATH, then wtf's, then where
// installers put it.
func (s *Service) kubectl(r Request) (string, error) {
	for _, dir := range filepath.SplitList(r.Env["PATH"]) {
		if isExec(filepath.Join(dir, "kubectl")) {
			return filepath.Join(dir, "kubectl"), nil
		}
	}
	if p, err := exec.LookPath("kubectl"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/kubectl", "/usr/local/bin/kubectl", "/usr/bin/kubectl", filepath.Join(s.home, ".local", "bin", "kubectl")} {
		if isExec(p) {
			return p, nil
		}
	}
	return "", errors.New("kubectl isn't installed (or isn't on the shell's PATH)")
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func validName(kind, v string) error {
	if strings.HasPrefix(v, "-") || strings.ContainsAny(v, " \t\r\n\x00") {
		return fmt.Errorf("%q isn't a %s name", v, kind)
	}
	return nil
}

// target resolves the context and namespace to use, as kubectl would.
func (s *Service) target(ctx context.Context, bin string, env []string, r Request) (kctx, cluster, ns string, err error) {
	if err := validName("context", r.Context); err != nil {
		return "", "", "", err
	}
	if err := validName("namespace", r.Namespace); err != nil {
		return "", "", "", err
	}
	args := []string{"config", "view", "--minify", "-o", "json"}
	if r.Context != "" {
		args = append(args, "--context", r.Context)
	}
	out, err := runKubectl(ctx, bin, env, args...)
	if err != nil {
		return "", "", "", err
	}
	var cfg struct {
		CurrentContext string `json:"current-context"`
		Contexts       []struct {
			Name    string
			Context struct{ Cluster, Namespace string }
		}
	}
	if json.Unmarshal(out, &cfg) != nil || len(cfg.Contexts) == 0 {
		return "", "", "", errors.New("no Kubernetes context is set (kubectl config current-context)")
	}
	c := cfg.Contexts[0]
	kctx, cluster, ns = c.Name, c.Context.Cluster, c.Context.Namespace
	if r.Namespace != "" {
		ns = r.Namespace
	}
	if ns == "" {
		ns = "default"
	}
	return kctx, cluster, ns, nil
}

// Target works out the context and namespace a request means, as kubectl
// would. It reads the kubeconfig only: nothing reaches the cluster.
func (s *Service) Target(r Request) (Target, error) {
	bin, err := s.kubectl(r)
	if err != nil {
		return Target{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	kctx, cluster, ns, err := s.target(ctx, bin, s.env(r), r)
	if err != nil {
		return Target{}, err
	}
	return Target{Context: kctx, Cluster: cluster, Namespace: ns}, nil
}

// get runs `kubectl get` for one kind of resource in a resolved context
// and namespace (from Target).
func (s *Service) get(r Request, resource string, extra ...string) ([]byte, error) {
	if r.Context == "" || r.Namespace == "" {
		return nil, errors.New("no context and namespace to read from")
	}
	if err := validName("context", r.Context); err != nil {
		return nil, err
	}
	if err := validName("namespace", r.Namespace); err != nil {
		return nil, err
	}
	bin, err := s.kubectl(r)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := append([]string{"get", resource, "--context", r.Context, "--namespace", r.Namespace, "--request-timeout", "15s", "-o", "json"}, extra...)
	return runKubectl(ctx, bin, s.env(r), args...)
}

// Deployments lists a namespace's deployments.
func (s *Service) Deployments(r Request) ([]Deployment, error) {
	out, err := s.get(r, "deployments.apps")
	if err != nil {
		return nil, err
	}
	items, err := parseItems(out)
	if err != nil {
		return nil, err
	}
	list := []Deployment{}
	for _, it := range items {
		if it.Kind == "Deployment" {
			list = append(list, parseDeployment(it))
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

var (
	labelKey   = regexp.MustCompile(`^([a-z0-9]([a-z0-9.-]*[a-z0-9])?/)?[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	labelValue = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?)?$`)
)

// Pods lists a namespace's pods; with a selector (a deployment's labels),
// only those the cluster says match.
func (s *Service) Pods(r Request, selector map[string]string) ([]Pod, error) {
	var extra []string
	if len(selector) > 0 {
		keys := make([]string, 0, len(selector))
		for k := range selector {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			if !labelKey.MatchString(k) || !labelValue.MatchString(selector[k]) {
				return nil, fmt.Errorf("%s=%s isn't a label", k, selector[k])
			}
			parts[i] = k + "=" + selector[k]
		}
		extra = []string{"--selector", strings.Join(parts, ",")}
	}
	out, err := s.get(r, "pods", extra...)
	if err != nil {
		return nil, err
	}
	items, err := parseItems(out)
	if err != nil {
		return nil, err
	}
	list := []Pod{}
	for _, it := range items {
		if it.Kind == "Pod" {
			list = append(list, parsePod(it))
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// Events lists a namespace's latest events, newest first.
func (s *Service) Events(r Request) ([]Event, error) {
	out, err := s.get(r, "events")
	if err != nil {
		return nil, err
	}
	return parseEvents(out)
}

// Table is a resource list as kubectl prints it: its columns (NAME, TYPE,
// AGE…) and one row of cells per resource.
type Table struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Kinds the tab can list as tables: id → kubectl resource, and whether it
// lives in a namespace. Nothing else can be asked for.
var tableKinds = map[string]struct {
	resource   string
	namespaced bool
}{
	"statefulsets": {"statefulsets.apps", true},
	"daemonsets":   {"daemonsets.apps", true},
	"jobs":         {"jobs.batch", true},
	"cronjobs":     {"cronjobs.batch", true},
	"services":     {"services", true},
	"ingresses":    {"ingresses.networking.k8s.io", true},
	"configmaps":   {"configmaps", true},
	"secrets":      {"secrets", true},
	"pvcs":         {"persistentvolumeclaims", true},
	"hpas":         {"horizontalpodautoscalers.autoscaling", true},
	"nodes":        {"nodes", false},
}

// Table lists one kind of resource the way `kubectl get <kind>` prints it.
// The cluster works out the columns (server-side printing), so the answer
// is small — and for secrets it holds names, types and key counts, never
// values: their data never reaches wtf.
func (s *Service) Table(r Request, kind string) (Table, error) {
	k, ok := tableKinds[kind]
	if !ok {
		return Table{}, fmt.Errorf("unknown resource kind %q", kind)
	}
	if r.Context == "" || (k.namespaced && r.Namespace == "") {
		return Table{}, errors.New("no context and namespace to read from")
	}
	if err := validName("context", r.Context); err != nil {
		return Table{}, err
	}
	if err := validName("namespace", r.Namespace); err != nil {
		return Table{}, err
	}
	bin, err := s.kubectl(r)
	if err != nil {
		return Table{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"get", k.resource, "--context", r.Context, "--request-timeout", "15s"}
	if k.namespaced {
		args = append(args, "--namespace", r.Namespace)
	}
	out, err := runKubectl(ctx, bin, s.env(r), args...)
	if err != nil {
		return Table{}, err
	}
	return parseTable(string(out)), nil
}

// parseTable reads kubectl's aligned table. Columns are separated by two
// or more spaces in the header ("LAST SCHEDULE" is one column), and every
// cell starts where its header does, so rows are cut at those positions —
// cells may contain single spaces, like a cron schedule.
func parseTable(text string) Table {
	t := Table{Columns: []string{}, Rows: [][]string{}}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return t
	}
	header := []rune(lines[0])
	var starts []int
	for i := 0; i < len(header); i++ {
		if header[i] != ' ' && (i == 0 || (i >= 2 && header[i-1] == ' ' && header[i-2] == ' ')) {
			starts = append(starts, i)
		}
	}
	cut := func(line []rune, i int) string {
		from := starts[i]
		if from >= len(line) {
			return ""
		}
		to := len(line)
		if i+1 < len(starts) && starts[i+1] < to {
			to = starts[i+1]
		}
		return strings.TrimSpace(string(line[from:to]))
	}
	for i := range starts {
		t.Columns = append(t.Columns, cut(header, i))
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		line := []rune(l)
		row := make([]string, len(starts))
		for i := range starts {
			row[i] = cut(line, i)
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// Namespaces lists the cluster's namespaces, for the picker (when it's
// opened). Users who may not list them get an error; the picker then takes
// a typed name.
func (s *Service) Namespaces(r Request) ([]string, error) {
	bin, err := s.kubectl(r)
	if err != nil {
		return nil, err
	}
	if r.Context == "" {
		return nil, errors.New("no context to read from")
	}
	if err := validName("context", r.Context); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := runKubectl(ctx, bin, s.env(r), "get", "namespaces", "--context", r.Context, "--request-timeout", "15s", "-o", "name")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if n, ok := strings.CutPrefix(strings.TrimSpace(line), "namespace/"); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ── Parsing ──────────────────────────────────────────────────────────────

type meta struct {
	Name              string            `json:"name"`
	CreationTimestamp string            `json:"creationTimestamp"`
	DeletionTimestamp string            `json:"deletionTimestamp"`
	Labels            map[string]string `json:"labels"`
	OwnerReferences   []struct {
		Kind, Name string
		Controller bool
	} `json:"ownerReferences"`
}

type containerState struct {
	Waiting    *struct{ Reason string }    `json:"waiting"`
	Running    *struct{ StartedAt string } `json:"running"`
	Terminated *struct {
		Reason     string
		ExitCode   int
		FinishedAt string
	} `json:"terminated"`
}

type containerStatus struct {
	Name         string
	Ready        bool
	RestartCount int
	State        containerState
	LastState    containerState
}

type item struct {
	Kind     string          `json:"kind"`
	Metadata meta            `json:"metadata"`
	Spec     json.RawMessage `json:"spec"`
	Status   json.RawMessage `json:"status"`
}

func parseItems(data []byte) ([]item, error) {
	var list struct{ Items []item }
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("couldn't read kubectl's answer: %v", err)
	}
	return list.Items, nil
}

func parsePod(it item) Pod {
	var spec struct {
		NodeName       string
		Containers     []struct{ Name, Image string }
		InitContainers []struct{ Name string }
	}
	var status struct {
		Phase                 string
		Reason                string
		PodIP                 string
		ContainerStatuses     []containerStatus
		InitContainerStatuses []containerStatus
	}
	_ = json.Unmarshal(it.Spec, &spec)
	_ = json.Unmarshal(it.Status, &status)

	p := Pod{
		Name: it.Metadata.Name, Phase: status.Phase, Node: spec.NodeName, IP: status.PodIP,
		Created: it.Metadata.CreationTimestamp, Labels: it.Metadata.Labels, Containers: []Container{},
		Total: len(spec.Containers),
	}
	if p.Labels == nil {
		p.Labels = map[string]string{}
	}
	for _, o := range it.Metadata.OwnerReferences {
		if o.Controller {
			p.Owner = o.Kind + "/" + o.Name
		}
	}
	byName := map[string]containerStatus{}
	for _, cs := range status.ContainerStatuses {
		byName[cs.Name] = cs
	}
	for _, c := range spec.Containers {
		cs := byName[c.Name]
		ct := Container{Name: c.Name, Image: c.Image, Ready: cs.Ready, Restarts: cs.RestartCount}
		switch {
		case cs.State.Waiting != nil:
			ct.Reason = cs.State.Waiting.Reason
		case cs.State.Terminated != nil:
			ct.Reason = cs.State.Terminated.Reason
		}
		if t := cs.LastState.Terminated; t != nil {
			ct.LastReason = t.Reason
			if t.FinishedAt > p.LastRestart {
				p.LastRestart = t.FinishedAt
			}
		}
		if ct.Ready {
			p.Ready++
		}
		p.Restarts += cs.RestartCount
		p.Containers = append(p.Containers, ct)
	}
	p.Status = podStatus(it.Metadata, status.Phase, status.Reason, status.InitContainerStatuses, len(spec.InitContainers), status.ContainerStatuses)
	return p
}

// podStatus works out the STATUS column the way `kubectl get pods` does:
// the pod's reason, else an init container still working, else the reason
// a container isn't running, else the phase.
func podStatus(m meta, phase, reason string, inits []containerStatus, initCount int, containers []containerStatus) string {
	status := phase
	if reason != "" {
		status = reason
	}
	for i, cs := range inits {
		switch {
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0:
			continue
		case cs.State.Terminated != nil:
			if cs.State.Terminated.Reason != "" {
				return "Init:" + cs.State.Terminated.Reason
			}
			return fmt.Sprintf("Init:ExitCode:%d", cs.State.Terminated.ExitCode)
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "PodInitializing":
			return "Init:" + cs.State.Waiting.Reason
		default:
			return fmt.Sprintf("Init:%d/%d", i, initCount)
		}
	}
	running := false
	for i := len(containers) - 1; i >= 0; i-- {
		cs := containers[i]
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			status = cs.State.Waiting.Reason
		case cs.State.Terminated != nil && cs.State.Terminated.Reason != "":
			status = cs.State.Terminated.Reason
		case cs.State.Terminated != nil:
			status = fmt.Sprintf("ExitCode:%d", cs.State.Terminated.ExitCode)
		case cs.Ready && cs.State.Running != nil:
			running = true
		}
	}
	if status == "Completed" && running {
		status = "Running"
	}
	if m.DeletionTimestamp != "" {
		status = "Terminating"
	}
	return status
}

func parseDeployment(it item) Deployment {
	var spec struct {
		Replicas *int
		Selector struct{ MatchLabels map[string]string }
		Template struct {
			Spec struct{ Containers []struct{ Image string } }
		}
	}
	var status struct {
		ReadyReplicas, UpdatedReplicas, AvailableReplicas int
		Conditions                                        []struct{ Type, Status, Message string }
	}
	_ = json.Unmarshal(it.Spec, &spec)
	_ = json.Unmarshal(it.Status, &status)
	d := Deployment{
		Name: it.Metadata.Name, Desired: 1, Ready: status.ReadyReplicas, UpToDate: status.UpdatedReplicas,
		Available: status.AvailableReplicas, Selector: spec.Selector.MatchLabels, Created: it.Metadata.CreationTimestamp,
		Images: []string{}, Problems: []string{},
	}
	if spec.Replicas != nil {
		d.Desired = *spec.Replicas
	}
	if d.Selector == nil {
		d.Selector = map[string]string{}
	}
	for _, c := range spec.Template.Spec.Containers {
		d.Images = append(d.Images, c.Image)
	}
	for _, c := range status.Conditions {
		if c.Status != "True" {
			d.Problems = append(d.Problems, c.Type+": "+c.Message)
		}
	}
	return d
}

func parseEvents(data []byte) ([]Event, error) {
	var list struct {
		Items []struct {
			Type, Reason, Message string
			Count                 int
			LastTimestamp         string
			EventTime             string
			FirstTimestamp        string
			Metadata              meta
			InvolvedObject        struct{ Kind, Name string }
			Series                *struct {
				Count            int
				LastObservedTime string
			}
		}
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("couldn't read events: %v", err)
	}
	events := []Event{}
	for _, e := range list.Items {
		ev := Event{Type: e.Type, Reason: e.Reason, Message: strings.TrimSpace(e.Message), Count: e.Count,
			Object: e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name}
		ev.Last = firstOf(e.LastTimestamp, e.EventTime, e.FirstTimestamp, e.Metadata.CreationTimestamp)
		if e.Series != nil {
			ev.Count = max(ev.Count, e.Series.Count)
			ev.Last = firstOf(e.Series.LastObservedTime, ev.Last)
		}
		if ev.Count == 0 {
			ev.Count = 1
		}
		events = append(events, ev)
	}
	// Newest first; the times are RFC 3339, which sorts as text (eventTime
	// has microseconds, so compare by parsed time).
	sort.SliceStable(events, func(i, j int) bool { return when(events[i].Last).After(when(events[j].Last)) })
	if len(events) > maxEvents {
		events = events[:maxEvents]
	}
	return events, nil
}

func when(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func lastLine(s, fallback string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if line == "" {
		return fallback
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}

// ── Secret values ────────────────────────────────────────────────────────

// SecretEntry is one key of a secret, decoded.
type SecretEntry struct {
	Key string `json:"key"`
	// The decoded value, when it's text; for binary data, its base64.
	Value  string `json:"value"`
	Binary bool   `json:"binary"`
	Size   int    `json:"size"`
}

// SecretValues reads one secret and decodes its values — only when the
// user asks to see that secret. Nothing is kept: the values go to the
// dialog that asked, and nowhere else.
func (s *Service) SecretValues(r Request, name string) ([]SecretEntry, error) {
	if name == "" || validName("secret", name) != nil {
		return nil, fmt.Errorf("%q isn't a secret name", name)
	}
	out, err := s.get(r, "secret/"+name)
	if err != nil {
		return nil, err
	}
	var sec struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out, &sec); err != nil {
		return nil, fmt.Errorf("couldn't read the secret: %v", err)
	}
	entries := []SecretEntry{}
	for k, v := range sec.Data {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			entries = append(entries, SecretEntry{Key: k, Value: v, Binary: true, Size: len(v)})
			continue
		}
		e := SecretEntry{Key: k, Size: len(raw)}
		if utf8.Valid(raw) && !bytes.ContainsRune(raw, 0) {
			e.Value = string(raw)
		} else {
			e.Value, e.Binary = v, true
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}
