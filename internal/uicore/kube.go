package uicore

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/kube"
)

// The Kubernetes tab's rules: kubectl command lines, how rows read and what
// colour their dot is, and how contexts are described. Ports of
// frontend/src/lib/kube.svelte.ts and kubeconfigs.svelte.ts
// (macos/Sources/WtfCore/Kube.swift).

// KubeTarget is where kubectl commands go: a context and namespace, and
// the shell's variables for a new tab.
type KubeTarget struct {
	Context, Namespace string
	Env                map[string]string
}

// Kubectl is `kubectl --context C -n N args…`, with the shell's KUBECONFIG
// and AWS variables in front when given (for a new tab).
func (t KubeTarget) Kubectl(args ...string) string {
	var parts []string
	for _, k := range []string{"KUBECONFIG", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		if v := t.Env[k]; v != "" {
			parts = append(parts, k+"="+ShellQuote(v))
		}
	}
	parts = append(parts, "kubectl")
	if t.Context != "" {
		parts = append(parts, "--context", ShellQuote(t.Context))
	}
	if t.Namespace != "" {
		parts = append(parts, "-n", ShellQuote(t.Namespace))
	}
	return strings.Join(append(parts, args...), " ")
}

func containerFlag(c string) []string {
	if c == "" {
		return nil
	}
	return []string{"-c", ShellQuote(c)}
}

// Logs follows a pod's logs, or shows its previous run's; container "" is
// the default one.
func (t KubeTarget) Logs(pod, container string, previous bool) string {
	args := []string{"logs", "-f", "--tail=200", ShellQuote(pod)}
	if previous {
		args = []string{"logs", "--previous", ShellQuote(pod)}
	}
	return t.Kubectl(strings.Join(append(args, containerFlag(container)...), " "))
}

// Exec opens a shell in a pod: bash if it has it, else sh.
func (t KubeTarget) Exec(pod, container string) string {
	return t.Kubectl(strings.Join(append([]string{"exec", "-it", ShellQuote(pod)}, containerFlag(container)...), " "),
		"-- sh -c 'command -v bash >/dev/null && exec bash || exec sh'")
}

func (t KubeTarget) Describe(kind, name string) string {
	return t.Kubectl("describe", kind, ShellQuote(name))
}

func (t KubeTarget) YAML(kind, name string) string {
	return t.Kubectl("get", kind, ShellQuote(name), "-o yaml")
}

func (t KubeTarget) WorkloadLogs(kind, name string) string {
	return t.Kubectl("logs -f --tail=200 --all-containers --prefix", ShellQuote(kind+"/"+name))
}

func (t KubeTarget) RolloutStatus(kind, name string) string {
	return t.Kubectl("rollout status", ShellQuote(kind+"/"+name))
}

func (t KubeTarget) RolloutHistory(kind, name string) string {
	return t.Kubectl("rollout history", ShellQuote(kind+"/"+name))
}

func (t KubeTarget) Restart(kind, name string) string {
	return t.Kubectl("rollout restart", ShellQuote(kind+"/"+name))
}

func (t KubeTarget) Scale(kind, name string, n int) string {
	return t.Kubectl("scale", ShellQuote(kind+"/"+name), "--replicas="+strconv.Itoa(n))
}

// DecodeSecret prints a secret's keys and decoded values.
func (t KubeTarget) DecodeSecret(name string) string {
	return t.Kubectl("get secret", ShellQuote(name), "-o",
		ShellQuote(`go-template={{range $k, $v := .data}}{{$k}}={{$v | base64decode}}{{"\n"}}{{end}}`))
}

// TriggerCronJob runs a cron job now, as a job named after it and the time.
func (t KubeTarget) TriggerCronJob(name string, now time.Time) string {
	short := name
	if r := []rune(name); len(r) > 40 {
		short = string(r[:40])
	}
	return t.Kubectl("create job", "--from="+ShellQuote("cronjob/"+name),
		ShellQuote(short+"-manual-"+strconv.FormatInt(now.UnixMilli(), 36)))
}

func (t KubeTarget) DeletePod(name string) string {
	return t.Kubectl("delete pod", ShellQuote(name))
}

// Rows

// Tone is the colour of a row's dot.
type Tone int

const (
	ToneOK Tone = iota
	ToneWait
	ToneBad
	ToneDone
)

// KubeAge is "45s", "12m", "5h", "3d"; "" when iso isn't a time.
func KubeAge(iso string, now time.Time) string {
	d, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	s := max(0, int(now.Sub(d)/time.Second))
	switch {
	case s < 120:
		return strconv.Itoa(s) + "s"
	case s < 7200:
		return strconv.Itoa(s/60) + "m"
	case s < 172_800:
		return strconv.Itoa(s/3600) + "h"
	}
	return strconv.Itoa(s/86400) + "d"
}

func DeploymentTone(d kube.Deployment) Tone {
	switch {
	case d.Ready == d.Desired:
		return ToneOK
	case d.Ready == 0 && d.Desired > 0:
		return ToneBad
	}
	return ToneWait
}

// DeploymentDetail is "2/3 ready · 1 up to date · 5h".
func DeploymentDetail(d kube.Deployment, now time.Time) string {
	s := strconv.Itoa(d.Ready) + "/" + strconv.Itoa(d.Desired) + " ready"
	if d.UpToDate != d.Desired {
		s += " · " + strconv.Itoa(d.UpToDate) + " up to date"
	}
	return s + " · " + KubeAge(d.Created, now)
}

var badPodStatus = regexp.MustCompile(`Err|BackOff|Error|OOMKilled|Failed|Evicted|ExitCode|Unknown|NotReady`)

func PodTone(p kube.Pod) Tone {
	switch {
	case p.Status == "Running":
		if p.Ready == p.Total {
			return ToneOK
		}
		return ToneWait
	case p.Status == "Completed" || p.Status == "Succeeded":
		return ToneDone
	case badPodStatus.MatchString(p.Status):
		return ToneBad
	}
	return ToneWait
}

// PodDetail is "Running · 1/2 ready · 2 restarts (OOMKilled 5m ago) · 3h".
func PodDetail(p kube.Pod, now time.Time) string {
	parts := []string{p.Status}
	if p.Total > 0 {
		parts = append(parts, strconv.Itoa(p.Ready)+"/"+strconv.Itoa(p.Total)+" ready")
	}
	if p.Restarts > 0 {
		r := strconv.Itoa(p.Restarts) + " restarts"
		if p.Restarts == 1 {
			r = "1 restart"
		}
		if i := slices.IndexFunc(p.Containers, func(c kube.Container) bool { return c.LastReason != "" }); i >= 0 {
			r += " (" + p.Containers[i].LastReason
			if ago := KubeAge(p.LastRestart, now); ago != "" {
				r += " " + ago + " ago"
			}
			r += ")"
		}
		parts = append(parts, r)
	}
	parts = append(parts, KubeAge(p.Created, now))
	return joinDot(parts...)
}

// PodTip is a pod's tooltip: its node, IP, owner and containers.
func PodTip(p kube.Pod) string {
	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}
	lines := []string{p.Name, "Node: " + dash(p.Node) + "  IP: " + dash(p.IP)}
	if p.Owner != "" {
		lines = append(lines, "Owner: "+p.Owner)
	}
	for _, c := range p.Containers {
		line := c.Name + ": " + c.Image
		if !c.Ready {
			reason := c.Reason
			if reason == "" {
				reason = "not ready"
			}
			line += " — " + reason
		}
		if c.Restarts > 0 {
			line += ", " + strconv.Itoa(c.Restarts) + " restarts"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// KubeTableKind is one of the table kinds: id, label, kubectl kind, summary
// columns, note.
type KubeTableKind struct {
	ID, Label, Kind string
	Columns         []string
	Note            string
}

var KubeTableKinds = []KubeTableKind{
	{"statefulsets", "StatefulSets", "statefulset", []string{"READY"}, ""},
	{"daemonsets", "DaemonSets", "daemonset", []string{"DESIRED", "READY", "NODE SELECTOR"}, ""},
	{"jobs", "Jobs", "job", []string{"STATUS", "COMPLETIONS", "DURATION"}, ""},
	{"cronjobs", "CronJobs", "cronjob", []string{"SCHEDULE", "SUSPEND", "LAST SCHEDULE"}, ""},
	{"services", "Services", "service", []string{"TYPE", "CLUSTER-IP", "EXTERNAL-IP", "PORT(S)"}, ""},
	{"ingresses", "Ingresses", "ingress", []string{"HOSTS", "ADDRESS"}, ""},
	{"configmaps", "ConfigMaps", "configmap", []string{"DATA"}, ""},
	{"secrets", "Secrets", "secret", []string{"TYPE", "DATA"},
		"Listed by name, type and key count; a secret’s values are read only when you open that secret"},
	{"pvcs", "PersistentVolumeClaims", "pvc", []string{"STATUS", "CAPACITY", "STORAGECLASS"}, ""},
	{"hpas", "HorizontalPodAutoscalers", "hpa", []string{"REFERENCE", "TARGETS", "REPLICAS"}, ""},
	{"nodes", "Nodes", "node", []string{"STATUS", "ROLES", "VERSION"}, "Cluster-wide, not in a namespace"},
}

// kubeRatio is the tone of "ready/wanted"; false when s isn't one.
func kubeRatio(s string) (Tone, bool) {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		return 0, false
	}
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	switch {
	case err1 != nil || err2 != nil:
		return 0, false
	case x == y:
		return ToneOK, true
	case x == 0 && y > 0:
		return ToneBad, true
	}
	return ToneWait, true
}

// RowTone is a table row's dot; false for kinds (or rows) without one.
func RowTone(kind string, cells map[string]string) (Tone, bool) {
	switch kind {
	case "statefulsets":
		return kubeRatio(cells["READY"])
	case "daemonsets":
		d, ok1 := cells["DESIRED"]
		r, ok2 := cells["READY"]
		if !ok1 || !ok2 {
			return 0, false
		}
		return kubeRatio(r + "/" + d)
	case "jobs":
		if s := cells["STATUS"]; s != "" {
			switch s {
			case "Complete":
				return ToneOK, true
			case "Failed":
				return ToneBad, true
			}
			return ToneWait, true
		}
		return kubeRatio(cells["COMPLETIONS"])
	case "pvcs":
		s, ok := cells["STATUS"]
		if !ok {
			return 0, false
		}
		switch s {
		case "Bound":
			return ToneOK, true
		case "Lost":
			return ToneBad, true
		}
		return ToneWait, true
	case "nodes":
		s, ok := cells["STATUS"]
		if !ok {
			return 0, false
		}
		switch {
		case s == "Ready":
			return ToneOK, true
		case strings.HasPrefix(s, "NotReady") || strings.HasPrefix(s, "Unknown"):
			return ToneBad, true
		}
		return ToneWait, true
	}
	return 0, false
}

// RowSummary is a table row's summary columns and age, as "a · b · 3d".
func RowSummary(kind string, cells map[string]string) string {
	var parts []string
	if i := slices.IndexFunc(KubeTableKinds, func(k KubeTableKind) bool { return k.ID == kind }); i >= 0 {
		for _, c := range KubeTableKinds[i].Columns {
			if v := cells[c]; v != "" && v != "<none>" {
				parts = append(parts, v)
			}
		}
	}
	return joinDot(append(parts, cells["AGE"])...)
}

var firstPortRe = regexp.MustCompile(`^\d+`)

// FirstPort is the first port of PORT(S): "80/TCP,443/TCP" or
// "80:30080/TCP" → "80".
func FirstPort(ports string) string { return firstPortRe.FindString(ports) }

// Contexts

func ClusterLabel(c kube.KubeContext) string {
	if c.EKSCluster != "" {
		return c.EKSCluster + " (EKS " + c.EKSRegion + ")"
	}
	if u, err := url.Parse(c.Server); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return c.Cluster
}

func AuthLabel(c kube.KubeContext) string {
	switch c.Auth {
	case "exec":
		base := BaseName(c.ExecCommand)
		if base == "aws" && slices.Contains(c.ExecArgs, "get-token") {
			return "aws eks get-token"
		}
		return base
	case "client-cert":
		return "client certificate"
	case "token":
		return "token"
	case "auth-provider":
		return "auth provider"
	case "basic":
		return "username and password"
	}
	return "no credentials"
}

// ContextDetail is "sales (EKS us-west-2) · ns api · aws eks get-token · profile devops".
func ContextDetail(c kube.KubeContext) string {
	ns, profile := "", ""
	if c.Namespace != "" {
		ns = "ns " + c.Namespace
	}
	if c.AWSProfile != "" {
		profile = "profile " + c.AWSProfile
	}
	return joinDot(ClusterLabel(c), ns, AuthLabel(c), profile)
}

var kubeconfigChange = regexp.MustCompile(`\b(aws\s+eks\s+update-kubeconfig|kubectl\s+config\s|eksctl\s+utils\s+write-kubeconfig)`)

// ChangesKubeconfig reports commands after which the kubeconfig may have changed.
func ChangesKubeconfig(command string) bool { return kubeconfigChange.MatchString(command) }

// EKSAdd adds an EKS cluster to a kubeconfig (file "" is kubectl's default).
func EKSAdd(cluster, region, profile, alias, file string) string {
	s := "aws eks update-kubeconfig --name " + ShellQuote(cluster) + " --region " + ShellQuote(region)
	if profile != "" {
		s += " --profile " + ShellQuote(profile)
	}
	if alias != "" {
		s += " --alias " + ShellQuote(alias)
	}
	if file != "" {
		s += " --kubeconfig " + ShellQuote(file)
	}
	return s
}

// EKSRegions are the regions offered when adding an EKS cluster.
var EKSRegions = []string{"us-east-1", "us-east-2", "us-west-1", "us-west-2", "ca-central-1", "sa-east-1", "eu-west-1", "eu-west-2",
	"eu-west-3", "eu-central-1", "eu-central-2", "eu-north-1", "eu-south-1", "ap-south-1", "ap-southeast-1",
	"ap-southeast-2", "ap-northeast-1", "ap-northeast-2", "ap-east-1", "me-central-1", "af-south-1"}
