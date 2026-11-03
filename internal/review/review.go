// Package review reads infrastructure change previews — `terraform plan`
// (and OpenTofu, Terragrunt), `kubectl diff` and `helm diff` output, or a
// saved Terraform plan — and lists what they will do as facts: each
// resource created, changed, replaced or destroyed, what forces a
// replacement, and which changes are to IAM, security groups, RBAC or
// secrets. Parsing is deterministic; the AI only writes a summary of those
// facts (Request). Nothing here applies anything.
package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/tools"
)

// Actions, as Terraform names them.
const (
	Create  = "create"
	Update  = "update"
	Replace = "replace"
	Delete  = "delete"
	Read    = "read"
	Import  = "import"
	Move    = "move"
	Forget  = "forget"
)

const (
	// Attribute names kept per change.
	maxAttributes = 12
	// The output sent along with the facts.
	excerptLines = 300
	excerptBytes = 16000
	showTimeout  = time.Minute
)

// Change is one resource the change will touch.
type Change struct {
	Action string `json:"action"`
	// Terraform's address (module.x.aws_db_instance.main), or Kind
	// namespace/name for Kubernetes.
	Address string `json:"address"`
	// The resource type or Kubernetes kind.
	Type      string `json:"type"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// Attributes or fields it changes, as far as the output shows.
	Attributes []string `json:"attributes"`
	// Attributes whose change forces the replacement.
	ForcesReplacement []string `json:"forcesReplacement"`
	// Why, when the output says ("not in configuration", "tainted").
	Reason string `json:"reason"`
	// Moved from this address.
	From string `json:"from"`
	// "IAM", "Security group", "RBAC", "Network policy" or "Secret": what
	// kind of access control it is, when it is one.
	Area string `json:"area"`
}

// Counts are the changes per action.
type Counts struct {
	Create  int `json:"create"`
	Update  int `json:"update"`
	Replace int `json:"replace"`
	Delete  int `json:"delete"`
	Read    int `json:"read"`
	Import  int `json:"import"`
	Move    int `json:"move"`
	Forget  int `json:"forget"`
}

// Review is what a preview says will happen.
type Review struct {
	// "terraform", "kubectl diff" or "helm diff"; empty when the text is
	// none of them.
	Tool    string   `json:"tool"`
	Changes []Change `json:"changes"`
	Counts  Counts   `json:"counts"`
	// Terraform's own count line ("Plan: 1 to add, 2 to change, 0 to destroy.").
	Plan string `json:"plan"`
	// The preview says there's nothing to change.
	NoChanges bool `json:"noChanges"`
	// What the source was: "the output of …" or a file name.
	Source string `json:"source"`
	// The start of the text it was read from, sent to the AI with the facts.
	Excerpt string `json:"excerpt"`
	// For display: CountLine, and ChangeLine of each change with its sign
	// ("+ ", "~ ", "-/+ ", "- ", "<= ", "→ ").
	CountText string   `json:"countText"`
	Lines     []string `json:"lines"`
}

type Service struct {
	infra *infra.Service
}

func NewService(in *infra.Service) *Service { return &Service{infra: in} }

// Reviewable says whether a command line previews infrastructure changes,
// so a block's menu can offer Review Changes without reading its output.
func Reviewable(command string) bool {
	f := strings.Fields(command)
	has := func(words ...string) bool {
		for _, w := range words {
			if slices.Contains(f, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("terraform", "tofu", "terragrunt") && has("plan", "apply", "show", "destroy", "run-all"):
		return true
	case has("kubectl", "oc") && has("diff"):
		return true
	case has("helm") && has("diff"):
		return true
	}
	return false
}

// Reviewable is the package function, for the UIs.
func (s *Service) Reviewable(command string) bool { return Reviewable(command) }

// Parse reads a command block: the command line and its output.
func (s *Service) Parse(command, output string) Review {
	r := parseText(stripANSI(output))
	r.Source = "the output of `" + strings.TrimSpace(command) + "`"
	return r
}

// ParseFile reads a saved plan: `terraform show -json` output, or a binary
// plan file, which `terraform show -json` (or `tofu`) reads for it in the
// file's folder with the shell's PATH. Showing a plan changes nothing.
func (s *Service) ParseFile(path string, env map[string]string) (Review, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Review{}, err
	}
	if !json.Valid(data) {
		data, err = showPlan(path, env)
		if err != nil {
			return Review{}, err
		}
	}
	r, err := parseJSON(data)
	if err != nil {
		return Review{}, err
	}
	r.Source = "the plan file " + filepath.Base(path)
	return r, nil
}

// ── Terraform text ───────────────────────────────────────────────────────

var (
	ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// "  # module.a.aws_db_instance.main must be replaced"
	tfHeader = regexp.MustCompile(`^\s*# (\S+) (will be created|will be updated in-place|must be replaced|will be replaced, as requested|is tainted, so must be replaced|will be destroyed|will be read during apply|will be imported|will no longer be managed by Terraform|will be forgotten|has moved to (\S+))`)
	// "    ~ engine_version = "13.4" -> "14.1" # forces replacement"
	tfAttr = regexp.MustCompile(`^\s*(~|\+|-|-/\+|\+/-)\s+"?([A-Za-z0-9_.\-]+)"?\s*(=|\{|\[)`)
	// "  (because aws_instance.old is not in configuration)"
	tfBecause = regexp.MustCompile(`^\s*# \((because .*)\)\s*$`)
	tfPlan    = regexp.MustCompile(`^\s*(Plan: .*|Changes to Outputs:)`)
	tfResHead = regexp.MustCompile(`^\s*(?:[-+~<=]|-/\+|\+/-)\s+(?:resource|data) "[^"]+" "[^"]+"`)
)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

func tfAction(phrase string) string {
	switch {
	case strings.HasPrefix(phrase, "will be created"):
		return Create
	case strings.HasPrefix(phrase, "will be updated"):
		return Update
	case strings.Contains(phrase, "replaced"):
		return Replace
	case strings.HasPrefix(phrase, "will be destroyed"):
		return Delete
	case strings.HasPrefix(phrase, "will be read"):
		return Read
	case strings.HasPrefix(phrase, "will be imported"):
		return Import
	case strings.HasPrefix(phrase, "has moved"):
		return Move
	}
	return Forget
}

func parseText(text string) Review {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	r := Review{Excerpt: excerpt(lines)}
	switch {
	case slices.ContainsFunc(lines, func(l string) bool {
		return tfHeader.MatchString(l) || strings.Contains(l, "will perform the following actions")
	}):
		r.Tool = "terraform"
		parseTerraform(lines, &r)
	case slices.ContainsFunc(lines, func(l string) bool { return helmHeader.MatchString(l) }):
		r.Tool = "helm diff"
		parseHelm(lines, &r)
	case slices.ContainsFunc(lines, func(l string) bool { return kubeDiff.MatchString(l) }):
		r.Tool = "kubectl diff"
		parseKubectl(lines, &r)
	default:
		for _, l := range lines {
			if strings.Contains(l, "No changes.") || strings.Contains(l, "Your infrastructure matches the configuration") {
				r.Tool, r.NoChanges = "terraform", true
			}
		}
	}
	finish(&r)
	return r
}

func parseTerraform(lines []string, r *Review) {
	var cur *Change
	// The indent of the resource's own attributes; nested ones are deeper.
	indent := -1
	for _, l := range lines {
		if m := tfHeader.FindStringSubmatch(l); m != nil {
			r.Changes = append(r.Changes, Change{Action: tfAction(m[2]), Address: m[1]})
			cur = &r.Changes[len(r.Changes)-1]
			indent = -1
			if cur.Action == Move {
				// The header names the old address; the change is the new one.
				cur.From, cur.Address = m[1], m[3]
			}
			if strings.Contains(m[2], "tainted") {
				cur.Reason = "tainted"
			}
			continue
		}
		if m := tfPlan.FindStringSubmatch(l); m != nil {
			if strings.HasPrefix(m[1], "Plan:") {
				r.Plan = strings.TrimSpace(m[1])
			}
			cur = nil
			continue
		}
		if strings.Contains(l, "No changes.") {
			r.NoChanges = true
		}
		if cur == nil {
			continue
		}
		if m := tfBecause.FindStringSubmatch(l); m != nil {
			cur.Reason = m[1]
			continue
		}
		if tfResHead.MatchString(l) {
			continue
		}
		if m := tfAttr.FindStringSubmatch(l); m != nil {
			ind := len(l) - len(strings.TrimLeft(l, " "))
			if indent < 0 {
				indent = ind
			}
			if ind != indent {
				continue
			}
			name := m[2]
			if m[1] == "~" || m[1] == "-/+" || m[1] == "+/-" || cur.Action == Update || cur.Action == Replace {
				addAttr(cur, name)
			}
			if strings.Contains(l, "# forces replacement") && !slices.Contains(cur.ForcesReplacement, name) {
				cur.ForcesReplacement = append(cur.ForcesReplacement, name)
			}
		}
	}
	for i := range r.Changes {
		c := &r.Changes[i]
		c.Type, c.Name = tfTypeName(c.Address)
		c.Area = tfArea(c.Type)
		// A create or destroy lists every attribute; those aren't changes.
		if c.Action == Create || c.Action == Delete || c.Action == Read {
			c.Attributes = nil
		}
	}
}

// tfTypeName splits "module.a.aws_db_instance.main[0]" into its type and name.
func tfTypeName(addr string) (string, string) {
	parts := strings.Split(addr, ".")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "module" {
			i++
			continue
		}
		if parts[i] == "data" {
			continue
		}
		return parts[i], strings.Join(parts[i+1:], ".")
	}
	return "", addr
}

var (
	tfIAM = regexp.MustCompile(`^(aws_iam_|aws_.*_policy$|aws_.*_policy_attachment$|aws_kms_grant$|aws_lambda_permission$|google_.*_iam_|google_project_iam|google_service_account|azurerm_role_|azurerm_user_assigned_identity$|azuread_|kubernetes_(cluster_)?role(_binding)?(_v1)?$|kubernetes_service_account)`)
	tfNet = regexp.MustCompile(`^(aws_security_group|aws_vpc_security_group_|aws_network_acl|aws_default_security_group|aws_default_network_acl|google_compute_firewall|azurerm_network_security_|azurerm_firewall|kubernetes_network_policy)`)
	tfSec = regexp.MustCompile(`^(aws_secretsmanager_|aws_ssm_parameter$|google_secret_manager_|azurerm_key_vault_secret$|vault_|kubernetes_secret)`)
)

func tfArea(typ string) string {
	switch {
	case tfIAM.MatchString(typ):
		return "IAM"
	case tfNet.MatchString(typ):
		return "Security group"
	case tfSec.MatchString(typ):
		return "Secret"
	}
	return ""
}

func addAttr(c *Change, name string) {
	if len(c.Attributes) < maxAttributes && !slices.Contains(c.Attributes, name) {
		c.Attributes = append(c.Attributes, name)
	}
}

// ── Terraform JSON (terraform show -json) ───────────────────────────────

type tfJSONPlan struct {
	FormatVersion   string `json:"format_version"`
	ResourceChanges []struct {
		Address         string `json:"address"`
		PreviousAddress string `json:"previous_address"`
		Mode            string `json:"mode"`
		Type            string `json:"type"`
		Name            string `json:"name"`
		ActionReason    string `json:"action_reason"`
		Change          struct {
			Actions         []string         `json:"actions"`
			Before          json.RawMessage  `json:"before"`
			After           json.RawMessage  `json:"after"`
			AfterUnknown    json.RawMessage  `json:"after_unknown"`
			BeforeSensitive json.RawMessage  `json:"before_sensitive"`
			AfterSensitive  json.RawMessage  `json:"after_sensitive"`
			ReplacePaths    [][]any          `json:"replace_paths"`
			Importing       *json.RawMessage `json:"importing"`
		} `json:"change"`
	} `json:"resource_changes"`
}

func parseJSON(data []byte) (Review, error) {
	var p tfJSONPlan
	if err := json.Unmarshal(data, &p); err != nil {
		return Review{}, fmt.Errorf("not a Terraform plan: %w", err)
	}
	if p.FormatVersion == "" {
		return Review{}, errors.New("not a Terraform plan (no format_version): use terraform show -json")
	}
	r := Review{Tool: "terraform"}
	var values []string
	for _, rc := range p.ResourceChanges {
		ch := rc.Change
		c := Change{Address: rc.Address, Type: rc.Type, Name: rc.Name, Area: tfArea(rc.Type)}
		switch a := strings.Join(ch.Actions, ","); a {
		case "create":
			c.Action = Create
		case "update":
			c.Action = Update
		case "delete":
			c.Action = Delete
		case "delete,create", "create,delete":
			c.Action = Replace
		case "read":
			c.Action = Read
		case "forget":
			c.Action = Forget
		case "no-op":
			switch {
			case rc.PreviousAddress != "" && rc.PreviousAddress != rc.Address:
				c.Action = Move
			case ch.Importing != nil:
				c.Action = Import
			default:
				continue
			}
		default:
			continue
		}
		if rc.PreviousAddress != "" && rc.PreviousAddress != rc.Address {
			c.From = rc.PreviousAddress
		}
		c.Reason = jsonReason(rc.ActionReason)
		if c.Action == Update || c.Action == Replace {
			c.Attributes = changedKeys(ch.Before, ch.After, ch.AfterUnknown)
			values = append(values, valueChanges(rc.Address, c.Attributes, ch.Before, ch.After, ch.BeforeSensitive, ch.AfterSensitive)...)
		}
		for _, path := range ch.ReplacePaths {
			if len(path) > 0 {
				if s, ok := path[0].(string); ok && !slices.Contains(c.ForcesReplacement, s) {
					c.ForcesReplacement = append(c.ForcesReplacement, s)
				}
			}
		}
		r.Changes = append(r.Changes, c)
	}
	r.NoChanges = len(r.Changes) == 0
	if len(values) > 0 {
		r.Excerpt = excerpt(append([]string{"Changed values (sensitive ones left out):"}, values...))
	}
	finish(&r)
	return r, nil
}

// valueChanges are "addr: key: before -> after" for short values that
// aren't marked sensitive; a plan's JSON holds sensitive values in clear.
func valueChanges(addr string, keys []string, before, after, beforeSens, afterSens json.RawMessage) []string {
	var b, a, bs, as map[string]json.RawMessage
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	_ = json.Unmarshal(beforeSens, &bs)
	_ = json.Unmarshal(afterSens, &as)
	var out []string
	for _, k := range keys {
		if string(bs[k]) != "" && string(bs[k]) != "false" || string(as[k]) != "" && string(as[k]) != "false" {
			continue
		}
		bv, av := string(b[k]), string(a[k])
		if bv == "" {
			bv = "(none)"
		}
		if av == "" {
			av = "(known after apply)"
		}
		if len(bv) > 120 || len(av) > 120 {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s: %s -> %s", addr, k, bv, av))
	}
	return out
}

func jsonReason(r string) string {
	switch r {
	case "replace_because_tainted":
		return "tainted"
	case "replace_by_request":
		return "replacement requested (-replace)"
	case "replace_by_triggers":
		return "replace_triggered_by"
	case "delete_because_no_resource_config":
		return "because it is not in configuration"
	case "delete_because_no_module":
		return "because its module is not in configuration"
	case "delete_because_count_index", "delete_because_each_key":
		return "because count or for_each no longer includes it"
	}
	return ""
}

// changedKeys are the top-level attributes that differ, sorted.
func changedKeys(before, after, unknown json.RawMessage) []string {
	var b, a, u map[string]json.RawMessage
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	_ = json.Unmarshal(unknown, &u)
	var keys []string
	for k, av := range a {
		if bv, ok := b[k]; !ok || string(bv) != string(av) {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	for k, v := range u {
		if string(v) == "true" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	if len(keys) > maxAttributes {
		keys = keys[:maxAttributes]
	}
	return keys
}

func showPlan(path string, env map[string]string) ([]byte, error) {
	shellPATH := env["PATH"]
	if shellPATH == "" {
		shellPATH = os.Getenv("PATH")
	}
	exe := tools.Find("terraform", shellPATH)
	if exe == "" {
		exe = tools.Find("tofu", shellPATH)
	}
	if exe == "" {
		return nil, errors.New("this is a binary plan: reading it needs terraform or tofu on the shell's PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "show", "-json", "-no-color", filepath.Base(path))
	cmd.Dir = filepath.Dir(path)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "CHECKPOINT_DISABLE=1", "TF_IN_AUTOMATION=1", "TF_INPUT=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stripANSI(stderr.String()))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s show -json: %s", filepath.Base(exe), firstLines(msg, 4))
	}
	return out, nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ")
}

// ── kubectl diff ─────────────────────────────────────────────────────────

var (
	// "diff -u -N /tmp/LIVE-1/apps.v1.Deployment.default.web /tmp/MERGED-2/apps.v1.Deployment.default.web"
	kubeDiff = regexp.MustCompile(`^diff -u -N \S*/LIVE-[^/\s]+/(\S+) \S*/MERGED-`)
	kubeHunk = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	yamlKey  = regexp.MustCompile(`^[-+](\s*)(?:- )?([A-Za-z0-9_./-]+):`)
	version  = regexp.MustCompile(`^v\d+((alpha|beta)\d*)?$`)
)

// noise are fields every apply changes; listing them says nothing.
var noise = map[string]bool{
	"generation": true, "resourceVersion": true, "managedFields": true, "time": true,
	"uid": true, "creationTimestamp": true, "manager": true, "operation": true,
	"fieldsType": true, "fieldsV1": true, "apiVersion": true,
}

// kubeObject reads "apps.v1.Deployment.default.web" (group, version, kind,
// namespace, name; core objects have no group, cluster-scoped ones no
// namespace, and names may hold dots).
func kubeObject(file string) (kind, ns, name string) {
	parts := strings.Split(file, ".")
	for i, p := range parts {
		if version.MatchString(p) && i+1 < len(parts) {
			rest := parts[i+2:]
			kind = parts[i+1]
			switch len(rest) {
			case 0:
			case 1:
				name = rest[0]
			default:
				ns, name = rest[0], strings.Join(rest[1:], ".")
			}
			return
		}
	}
	return "", "", file
}

func parseKubectl(lines []string, r *Review) {
	var cur *Change
	for _, l := range lines {
		if m := kubeDiff.FindStringSubmatch(l); m != nil {
			kind, ns, name := kubeObject(m[1])
			r.Changes = append(r.Changes, Change{Action: Update, Type: kind, Namespace: ns, Name: name, Area: kubeArea(kind)})
			cur = &r.Changes[len(r.Changes)-1]
			cur.Address = kind + " " + strings.TrimPrefix(ns+"/"+name, "/")
			continue
		}
		if cur == nil {
			continue
		}
		if m := kubeHunk.FindStringSubmatch(l); m != nil {
			if m[1] == "0" && m[2] == "0" {
				cur.Action = Create
			} else if m[3] == "0" && m[4] == "0" {
				cur.Action = Delete
			}
			continue
		}
		if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") || cur.Action != Update {
			continue
		}
		if m := yamlKey.FindStringSubmatch(l); m != nil && !noise[m[2]] {
			addAttr(cur, m[2])
		}
	}
	for i := range r.Changes {
		if r.Changes[i].Action != Update {
			r.Changes[i].Attributes = nil
		}
	}
}

func kubeArea(kind string) string {
	switch kind {
	case "Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding", "ServiceAccount":
		return "RBAC"
	case "NetworkPolicy", "CiliumNetworkPolicy", "AuthorizationPolicy":
		return "Network policy"
	case "Secret", "SealedSecret", "ExternalSecret":
		return "Secret"
	}
	return ""
}

// ── helm diff ────────────────────────────────────────────────────────────

// "default, web, Deployment (apps) has changed:"
var helmHeader = regexp.MustCompile(`^(\S*), (\S+), (\S+) \(([^)]*)\) (has changed|has been added|has been removed)`)

func parseHelm(lines []string, r *Review) {
	var cur *Change
	for _, l := range lines {
		if m := helmHeader.FindStringSubmatch(l); m != nil {
			action := Update
			switch m[5] {
			case "has been added":
				action = Create
			case "has been removed":
				action = Delete
			}
			r.Changes = append(r.Changes, Change{Action: action, Type: m[3], Namespace: m[1], Name: m[2], Area: kubeArea(m[3])})
			cur = &r.Changes[len(r.Changes)-1]
			cur.Address = m[3] + " " + strings.TrimPrefix(m[1]+"/"+m[2], "/")
			continue
		}
		if cur == nil || cur.Action != Update {
			continue
		}
		// helm diff indents the manifest under the header.
		t := strings.TrimLeft(l, " ")
		if (strings.HasPrefix(t, "+ ") || strings.HasPrefix(t, "- ")) && len(t) > 2 {
			if m := yamlKey.FindStringSubmatch(t[:1] + t[2:]); m != nil && !noise[m[2]] {
				addAttr(cur, m[2])
			}
		}
	}
}

// ── Facts and the AI request ─────────────────────────────────────────────

func finish(r *Review) {
	r.Counts = Counts{}
	for _, c := range r.Changes {
		switch c.Action {
		case Create:
			r.Counts.Create++
		case Update:
			r.Counts.Update++
		case Replace:
			r.Counts.Replace++
		case Delete:
			r.Counts.Delete++
		case Read:
			r.Counts.Read++
		case Import:
			r.Counts.Import++
		case Move:
			r.Counts.Move++
		case Forget:
			r.Counts.Forget++
		}
	}
	if r.Changes == nil {
		r.Changes = []Change{}
	}
	r.CountText = CountLine(r.Counts)
	r.Lines = make([]string, 0, len(r.Changes))
	for _, c := range r.Changes {
		r.Lines = append(r.Lines, sign[c.Action]+ChangeLine(c))
	}
}

var sign = map[string]string{Create: "+ ", Update: "~ ", Replace: "-/+ ", Delete: "- ", Read: "<= ", Import: "← ", Move: "→ ", Forget: "· "}

func excerpt(lines []string) string {
	if len(lines) > excerptLines {
		lines = append(lines[:excerptLines:excerptLines], fmt.Sprintf("… (%d more lines)", len(lines)-excerptLines))
	}
	s := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(s) > excerptBytes {
		s = s[:excerptBytes] + "\n… (cut)"
	}
	return s
}

// Facts are the changes as text: the counts, then one line per change.
func Facts(r Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tool: %s\nSource: %s\n", r.Tool, r.Source)
	if r.Plan != "" {
		fmt.Fprintf(&b, "%s's own count: %s\n", r.Tool, r.Plan)
	}
	fmt.Fprintf(&b, "Parsed: %s\n", CountLine(r.Counts))
	if r.NoChanges && len(r.Changes) == 0 {
		b.WriteString("It reports no changes.\n")
	}
	for _, c := range r.Changes {
		b.WriteString("- " + ChangeLine(c) + "\n")
	}
	return b.String()
}

// CountLine is "1 to create, 2 to change, 1 to replace, 0 to destroy".
func CountLine(c Counts) string {
	parts := []string{
		fmt.Sprintf("%d to create", c.Create), fmt.Sprintf("%d to change", c.Update),
		fmt.Sprintf("%d to replace", c.Replace), fmt.Sprintf("%d to destroy", c.Delete),
	}
	for _, x := range []struct {
		n    int
		what string
	}{{c.Read, "to read"}, {c.Import, "to import"}, {c.Move, "moved"}, {c.Forget, "to forget"}} {
		if x.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.what))
		}
	}
	return strings.Join(parts, ", ")
}

// ChangeLine is one change as a fact: "replace aws_db_instance.main
// (engine_version forces replacement) [IAM]".
func ChangeLine(c Change) string {
	verb := map[string]string{Create: "create", Update: "change", Replace: "replace", Delete: "destroy",
		Read: "read", Import: "import", Move: "move", Forget: "forget"}[c.Action]
	s := verb + " " + c.Address
	var notes []string
	if c.From != "" {
		notes = append(notes, "from "+c.From)
	}
	if len(c.ForcesReplacement) > 0 {
		notes = append(notes, strings.Join(c.ForcesReplacement, ", ")+" forces replacement")
	}
	if len(c.Attributes) > 0 {
		notes = append(notes, "changes "+strings.Join(c.Attributes, ", "))
	}
	if c.Reason != "" {
		notes = append(notes, c.Reason)
	}
	if len(notes) > 0 {
		s += " (" + strings.Join(notes, "; ") + ")"
	}
	if c.Area != "" {
		s += " [" + c.Area + "]"
	}
	return s
}
