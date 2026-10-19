package uicore

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/sshhosts"
)

// The SSH tab's rules: commands, host text, the host editor's fields,
// tunnels and scp. Ports of frontend/src/lib/ssh.svelte.ts and
// sshAdmin.svelte.ts (macos/Sources/WtfCore/Ssh.swift).

// SSHCommand is the command that connects; config hosts go by alias so ssh
// applies their settings.
func SSHCommand(h sshhosts.Host) string { return "ssh " + SSHPortFlag(h, "-p") + ShellQuote(h.Alias) }

// SSHPortFlag is "-p 2222 " for a known host listed with a port; config
// hosts carry their own. flag is "-p" for ssh, "-P" for scp and sftp.
func SSHPortFlag(h sshhosts.Host, flag string) string {
	if h.Known && h.Port != "" && h.Port != "22" {
		return flag + " " + ShellQuote(h.Port) + " "
	}
	return ""
}

// SSHHostID is stable across reloads: a known host and a config alias may
// share a name.
func SSHHostID(h sshhosts.Host) string {
	k := "c"
	if h.Known {
		k = "k"
	}
	return k + ":" + h.Alias + ":" + h.Port
}

// SSHDetail is "ops@bastion.example.com:2222 via jump": what adds to the alias.
func SSHDetail(h sshhosts.Host) string {
	target := ""
	if h.HostName != "" && h.HostName != h.Alias {
		target = h.HostName
	}
	or := func(s string) string {
		if s == "" {
			return h.Alias
		}
		return s
	}
	if h.User != "" {
		target = h.User + "@" + or(target)
	}
	if h.Port != "" && h.Port != "22" {
		target = or(target) + ":" + h.Port
	}
	via := ""
	if h.ProxyJump != "" {
		via = "via " + h.ProxyJump
	}
	return joinNonEmpty(" ", target, via)
}

// SSHFact is a setting shown beside a host: a short label, and a title
// that explains it.
type SSHFact struct{ Label, Title string }

// SSHFacts are the settings that change what a connection may do, shown as
// they are, never judged.
func SSHFacts(h sshhosts.Host) []SSHFact {
	var out []SSHFact
	if h.ForwardAgent != "" {
		whose := "that agent"
		if h.ForwardAgent == "yes" {
			whose = "your ssh-agent"
		}
		out = append(out, SSHFact{"agent fwd", "ForwardAgent " + h.ForwardAgent + ": the server can use " + whose + " while you're connected"})
	}
	switch check := h.StrictHostKeyChecking; check {
	case "accept-new":
		out = append(out, SSHFact{"hostkey accept-new", "StrictHostKeyChecking accept-new: a new host key is saved without asking; a changed one is refused"})
	case "no", "off":
		out = append(out, SSHFact{"hostkey " + check, "StrictHostKeyChecking " + check + ": host keys are saved and connected to without asking"})
	}
	return out
}

// SSHMatches reports whether every word of query is in the host's alias,
// address, user or jump host.
func SSHMatches(h sshhosts.Host, query string) bool {
	q := strings.ToLower(trimBlanks(query))
	if q == "" {
		return true
	}
	text := strings.ToLower(h.Alias + " " + h.HostName + " " + h.User + " " + h.ProxyJump)
	for _, w := range strings.Fields(q) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// SSHGroupName is a tab group's name for hosts: their config file's, unless
// that's plain "config".
func SSHGroupName(hosts []sshhosts.Host) string {
	sources := map[string]bool{}
	for _, h := range hosts {
		sources[h.Source] = true
	}
	if len(sources) == 1 {
		for s := range sources {
			if s != "" && BaseName(s) != "config" {
				return BaseName(s)
			}
		}
	}
	return "SSH"
}

// QuotePath is a path for a command line: ~/… stays unquoted at the front
// so the shell expands it.
func QuotePath(path, home string) string {
	p := PrettyPath(path, home)
	if p == "~" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return "~/" + ShellQuote(rest)
	}
	return ShellQuote(p)
}

func SFTPCommand(h sshhosts.Host) string { return "sftp " + SSHPortFlag(h, "-P") + ShellQuote(h.Alias) }

// SCPUpload copies local files and folders to remoteDir on the host.
func SCPUpload(h sshhosts.Host, locals []string, remoteDir, home string) string {
	quoted := make([]string, len(locals))
	for i, l := range locals {
		quoted[i] = QuotePath(l, home)
	}
	return "scp -r " + SSHPortFlag(h, "-P") + strings.Join(quoted, " ") + " " + ShellQuote(h.Alias+":"+trimBlanks(remoteDir))
}

// SCPDownload copies a remote file or folder to localDir.
func SCPDownload(h sshhosts.Host, remote, localDir, home string) string {
	return "scp -r " + SSHPortFlag(h, "-P") + ShellQuote(h.Alias+":"+trimBlanks(remote)) + " " + QuotePath(localDir, home)
}

// SSHInstallKey adds the key's public half to the host's authorized keys.
func SSHInstallKey(keyPath string, h sshhosts.Host, home string) string {
	return "ssh-copy-id -i " + QuotePath(keyPath+".pub", home) + " " + SSHPortFlag(h, "-p") + ShellQuote(h.Alias)
}

// Keys

// SSHKeyType is a kind of key ssh-keygen can make.
type SSHKeyType struct{ ID, Label, Args, File string }

var SSHKeyTypes = []SSHKeyType{
	{"ed25519", "Ed25519 (recommended)", "-t ed25519", "id_ed25519"},
	{"rsa", "RSA 4096 (for older servers)", "-t rsa -b 4096", "id_rsa"},
	{"ecdsa", "ECDSA P-521", "-t ecdsa -b 521", "id_ecdsa"},
}

// SSHKeygen makes a key in ~/.ssh; ssh-keygen asks for the passphrase
// itself, in the tab.
func SSHKeygen(keyType, name, comment string, addToAgent, keychain bool) string {
	t := SSHKeyTypes[0]
	if i := slices.IndexFunc(SSHKeyTypes, func(k SSHKeyType) bool { return k.ID == keyType }); i >= 0 {
		t = SSHKeyTypes[i]
	}
	path := "~/.ssh/" + ShellQuote(trimBlanks(name))
	gen := "ssh-keygen " + t.Args
	if c := trimBlanks(comment); c != "" {
		gen += " -C " + ShellQuote(c)
	}
	gen += " -f " + path
	if addToAgent {
		return gen + " && " + SSHAgentAdd(path, keychain)
	}
	return gen
}

// SSHAgentAdd loads a key (its path already quoted) into ssh-agent; with
// keychain, its passphrase goes in the macOS keychain.
func SSHAgentAdd(quotedPath string, keychain bool) string {
	if keychain {
		return "ssh-add --apple-use-keychain " + quotedPath
	}
	return "ssh-add " + quotedPath
}

// FreeKeyName is a free file name like id_ed25519, id_ed25519_2, …; taken
// holds key names as "~/.ssh/<file>".
func FreeKeyName(base string, taken map[string]bool) string {
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = base + "_" + strconv.Itoa(i)
		}
		if !taken["~/.ssh/"+name] {
			return name
		}
	}
}

var keyTool = regexp.MustCompile(`^\s*(?:sudo\s+)?(?:ssh-keygen|ssh-add|ssh-copy-id|ssh-keyscan)\b`)

// IsKeyTool reports commands after which keys and hosts may have changed.
func IsKeyTool(command string) bool { return keyTool.MatchString(command) }

// SSHKeyDetails is "ED25519 · from Oct 2026 · work laptop".
func SSHKeyDetails(k sshhosts.Key) string {
	first := k.Type
	if strings.HasPrefix(k.Type, "RSA") || strings.HasPrefix(k.Type, "ECDSA") {
		first += " " + strconv.Itoa(k.Bits)
	}
	parts := []string{first}
	if d, err := time.Parse(time.RFC3339, k.Modified); err == nil {
		parts = append(parts, "from "+d.Local().Format("Jan 2006"))
	}
	if k.Comment != "" && k.Comment != k.Name {
		parts = append(parts, k.Comment)
	}
	return strings.Join(parts, " · ")
}

// HostDraft is a host entry as the editor fills it in.
type HostDraft struct {
	// Host line patterns, space-separated: "web1 web2".
	Name, HostName, User, Port, IdentityFile, ProxyJump string
	// Any other settings, one "Key value" per line.
	More string
}

// hostDraftFields are the settings with a field of their own, in order.
type hostDraftField struct {
	key   string
	field func(*HostDraft) *string
}

var hostDraftFields = []hostDraftField{
	{"HostName", func(d *HostDraft) *string { return &d.HostName }},
	{"User", func(d *HostDraft) *string { return &d.User }},
	{"Port", func(d *HostDraft) *string { return &d.Port }},
	{"IdentityFile", func(d *HostDraft) *string { return &d.IdentityFile }},
	{"ProxyJump", func(d *HostDraft) *string { return &d.ProxyJump }},
}

// NewHostDraft splits an entry's own settings into the editor's fields; nil
// for a new entry.
func NewHostDraft(h *sshhosts.Host) HostDraft {
	var d HostDraft
	if h == nil {
		return d
	}
	patterns := h.Patterns
	if len(patterns) == 0 {
		patterns = []string{h.Alias}
	}
	d.Name = strings.Join(patterns, " ")
	unquote := func(v string) string {
		if len(v) > 1 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
			return v[1 : len(v)-1]
		}
		return v
	}
	var more []string
	for _, o := range h.Options {
		i := slices.IndexFunc(hostDraftFields, func(f hostDraftField) bool { return strings.EqualFold(f.key, o.Key) })
		if i >= 0 && *hostDraftFields[i].field(&d) == "" {
			*hostDraftFields[i].field(&d) = unquote(o.Value)
		} else {
			more = append(more, o.Key+" "+o.Value)
		}
	}
	d.More = strings.Join(more, "\n")
	return d
}

// MissingValueError is a line of More with a key but no value.
type MissingValueError struct{ Line string }

func (e MissingValueError) Error() string {
	return e.Line + " needs a value, like “ForwardAgent yes”"
}

var moreLine = regexp.MustCompile(`^\s*([^\s=]+)\s*(?:=\s*|\s+)(.*)$`)

// Options are the settings to write; empty fields are left out (and so removed).
func (d HostDraft) Options() ([]sshhosts.Option, error) {
	var out []sshhosts.Option
	for _, f := range hostDraftFields {
		if v := trimBlanks(*f.field(&d)); v != "" {
			out = append(out, sshhosts.Option{Key: f.key, Value: v})
		}
	}
	for _, line := range strings.Split(d.More, "\n") {
		if m := moreLine.FindStringSubmatch(line); m != nil {
			if v := trimBlanks(m[2]); v != "" {
				out = append(out, sshhosts.Option{Key: m[1], Value: v})
				continue
			}
		}
		if t := trimBlanks(line); t != "" {
			return nil, MissingValueError{t}
		}
	}
	return out, nil
}

// TunnelDraft is a tunnel as the editor fills it in.
type TunnelDraft struct {
	Name, Host, Port string
	// local, remote, dynamic or kubectl.
	Kind string
	// local: the port here; remote: the port on the server; dynamic: the SOCKS port.
	ListenPort         string
	DestHost, DestPort string
	AutoStart          bool
	// kubectl only.
	Context, Namespace, Resource string
	Env                          map[string]string
}

// NewTunnelDraft is a new local forward through host (and port, for hosts
// that aren't in the config).
func NewTunnelDraft(host, port string) TunnelDraft {
	return TunnelDraft{Host: host, Port: port, Kind: "local", DestHost: "localhost", Env: map[string]string{}}
}

var forwardSpec = regexp.MustCompile(`^(.*?):(\[[^\]]+\]|[^:]+):(\d+)$`)

// forwardParts splits "[bind:]port:host:hostport" into its listen part,
// host and port.
func forwardParts(spec string) ([3]string, bool) {
	m := forwardSpec.FindStringSubmatch(spec)
	if m == nil {
		return [3]string{}, false
	}
	return [3]string{m[1], m[2], m[3]}, true
}

// Spec is the forward spec the backend takes.
func (d TunnelDraft) Spec() string {
	switch d.Kind {
	case "dynamic":
		return trimBlanks(d.ListenPort)
	case "kubectl":
		local, remote := trimBlanks(d.ListenPort), trimBlanks(d.DestPort)
		if remote != "" && remote != local {
			return local + ":" + remote
		}
		return local
	}
	h := trimBlanks(d.DestHost)
	if h == "" {
		h = "localhost"
	}
	if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
		h = "[" + h + "]"
	}
	return trimBlanks(d.ListenPort) + ":" + h + ":" + trimBlanks(d.DestPort)
}

// TunnelDraftOf is the reverse of Spec, for editing.
func TunnelDraftOf(t sshhosts.TunnelStatus) TunnelDraft {
	d := NewTunnelDraft(t.Host, t.Port)
	d.Name = t.Name
	d.Kind = t.Kind
	d.AutoStart = t.AutoStart
	switch t.Kind {
	case "dynamic":
		d.ListenPort = t.Spec
	case "kubectl":
		parts := strings.Split(t.Spec, ":")
		d.ListenPort = parts[0]
		d.DestPort = parts[0]
		if len(parts) > 1 {
			d.DestPort = parts[1]
		}
		d.Context = t.Context
		d.Namespace = t.Namespace
		d.Resource = t.Resource
		if t.Env != nil {
			d.Env = maps.Clone(t.Env)
		}
	default:
		if p, ok := forwardParts(t.Spec); ok {
			d.ListenPort = p[0]
			d.DestHost = p[1]
			if len(p[1]) > 1 && strings.HasPrefix(p[1], "[") && strings.HasSuffix(p[1], "]") {
				d.DestHost = p[1][1 : len(p[1])-1]
			}
			d.DestPort = p[2]
		} else {
			d.ListenPort = t.Spec
		}
	}
	return d
}

// KubectlTunnelEnv is the variables a kubectl forward needs from the shell
// it's made in.
func KubectlTunnelEnv(env map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"KUBECONFIG", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "PATH"} {
		if v := env[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

// TunnelIsOn reports a tunnel that's up or trying to be.
func TunnelIsOn(t sshhosts.TunnelStatus) bool {
	return t.State == sshhosts.TunnelUp || t.State == sshhosts.TunnelConnecting || t.State == sshhosts.TunnelRetrying
}

var tunnelPortRe = regexp.MustCompile(`\d+$`)

// TunnelLocalPort is the local port a tunnel listens on (local, SOCKS and
// kubectl); false for a remote forward.
func TunnelLocalPort(t sshhosts.TunnelStatus) (string, bool) {
	switch t.Kind {
	case "remote":
		return "", false
	case "kubectl":
		for _, p := range strings.Split(t.Spec, ":") {
			if p != "" {
				return p, true
			}
		}
		return "", false
	}
	listen := t.Spec
	if t.Kind != "dynamic" {
		p, _ := forwardParts(t.Spec)
		listen = p[0]
	}
	port := tunnelPortRe.FindString(listen)
	return port, port != ""
}

// TunnelDescribe is "localhost:5432 → db.internal:5432 via bastion".
func TunnelDescribe(t sshhosts.TunnelStatus) string {
	switch t.Kind {
	case "dynamic":
		return "SOCKS proxy on localhost:" + t.Spec + " via " + t.Host
	case "kubectl":
		parts := strings.Split(t.Spec, ":")
		remote := parts[0]
		if len(parts) > 1 {
			remote = parts[1]
		}
		return "localhost:" + parts[0] + " → " + t.Resource + ":" + remote + " in " + t.Namespace
	}
	m, ok := forwardParts(t.Spec)
	if !ok {
		return t.Spec + " via " + t.Host
	}
	if t.Kind == "local" {
		listen := m[0]
		if !strings.Contains(listen, ":") {
			listen = "localhost:" + listen
		}
		return listen + " → " + m[1] + ":" + m[2] + " via " + t.Host
	}
	return t.Host + ":" + m[0] + " → " + m[1] + ":" + m[2] + " here"
}

// TunnelStateLabel is how a tunnel's state reads.
func TunnelStateLabel(state string) string {
	switch state {
	case sshhosts.TunnelConnecting:
		return "connecting…"
	case sshhosts.TunnelUp:
		return "on"
	case sshhosts.TunnelRetrying:
		return "reconnecting…"
	case sshhosts.TunnelFailed:
		return "failed"
	}
	return "off"
}

// HostKeyVerdict is what a scan says against the stored keys.
func HostKeyVerdict(scanned, stored []sshhosts.HostKey) string {
	fps := map[string]bool{}
	for _, k := range stored {
		if k.Marker == "" {
			fps[k.Fingerprint] = true
		}
	}
	if len(scanned) == 0 {
		return "The server offered no keys."
	}
	if len(fps) == 0 {
		return "None of these keys are stored yet."
	}
	n := 0
	for _, k := range scanned {
		if fps[k.Fingerprint] {
			n++
		}
	}
	switch n {
	case len(scanned):
		return "All of the server’s keys match the stored ones."
	case 0:
		return "None of the server’s keys match the stored ones."
	}
	return fmt.Sprintf("%d of the server’s %d keys match the stored ones.", n, len(scanned))
}
