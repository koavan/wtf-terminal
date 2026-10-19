package uicore

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dayanraj/wtf/internal/sshhosts"
)

func sshHost(f func(*sshhosts.Host)) sshhosts.Host {
	h := sshhosts.Host{Alias: "web1"}
	f(&h)
	return h
}

func plainHost(*sshhosts.Host) {}

func expectText(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestSSHCommandsAndText(t *testing.T) {
	expectText(t, SSHCommand(sshHost(plainHost)), "ssh web1")
	known := sshHost(func(h *sshhosts.Host) { h.Known = true; h.Port = "2222"; h.Alias = "10.0.0.5" })
	expectText(t, SSHCommand(known), "ssh -p 2222 10.0.0.5")
	expectText(t, SFTPCommand(known), "sftp -P 2222 10.0.0.5")
	// Config hosts carry their own port.
	expectText(t, SSHCommand(sshHost(func(h *sshhosts.Host) { h.Port = "2222" })), "ssh web1")
	expectText(t, SSHDetail(sshHost(func(h *sshhosts.Host) {
		h.Alias, h.HostName, h.User, h.Port, h.ProxyJump = "b", "bastion.example.com", "ops", "2222", "jump"
	})), "ops@bastion.example.com:2222 via jump")
	expectText(t, SSHDetail(sshHost(func(h *sshhosts.Host) { h.HostName = "web1" })), "")
	var labels []string
	for _, f := range SSHFacts(sshHost(func(h *sshhosts.Host) { h.ForwardAgent = "yes"; h.StrictHostKeyChecking = "no" })) {
		labels = append(labels, f.Label)
	}
	if !reflect.DeepEqual(labels, []string{"agent fwd", "hostkey no"}) {
		t.Errorf("facts %v", labels)
	}
	if !SSHMatches(sshHost(func(h *sshhosts.Host) { h.User = "ops" }), "WEB ops") {
		t.Error("should match")
	}
	if SSHMatches(sshHost(plainHost), "web db") {
		t.Error("shouldn't match")
	}
	prod := sshHost(func(h *sshhosts.Host) { h.Source = "/h/.ssh/config.d/prod" })
	expectText(t, SSHGroupName([]sshhosts.Host{prod, prod}), "prod")
	expectText(t, SSHGroupName([]sshhosts.Host{sshHost(func(h *sshhosts.Host) { h.Source = "/h/.ssh/config" })}), "SSH")
}

func TestSCPAndKeyCommands(t *testing.T) {
	h := sshHost(func(h *sshhosts.Host) { h.Known = true; h.Port = "2222" })
	expectText(t, SCPUpload(h, []string{"/h/my file", "/tmp/x"}, " /srv ", "/h"), "scp -r -P 2222 ~/'my file' /tmp/x web1:/srv")
	expectText(t, SCPDownload(sshHost(plainHost), "/var/log/app.log", "/h/Downloads", "/h"), "scp -r web1:/var/log/app.log ~/Downloads")
	expectText(t, SSHInstallKey("/h/.ssh/id_ed25519", sshHost(plainHost), "/h"), "ssh-copy-id -i ~/.ssh/id_ed25519.pub web1")
	expectText(t, SSHKeygen("rsa", "work", "me at work", true, true),
		"ssh-keygen -t rsa -b 4096 -C 'me at work' -f ~/.ssh/work && ssh-add --apple-use-keychain ~/.ssh/work")
	expectText(t, FreeKeyName("id_ed25519", map[string]bool{"~/.ssh/id_ed25519": true}), "id_ed25519_2")
	if !IsKeyTool("sudo ssh-add -D") || IsKeyTool("ssh web1") {
		t.Error("IsKeyTool")
	}
}

func TestHostEditorDraft(t *testing.T) {
	h := sshHost(func(h *sshhosts.Host) {
		h.Patterns = []string{"web1", "w1"}
		h.Options = []sshhosts.Option{{Key: "hostname", Value: "10.0.0.5"}, {Key: "IdentityFile", Value: `"~/my key"`},
			{Key: "IdentityFile", Value: "~/.ssh/other"}, {Key: "ForwardAgent", Value: "yes"}}
	})
	d := NewHostDraft(&h)
	if d.Name != "web1 w1" || d.HostName != "10.0.0.5" || d.IdentityFile != "~/my key" {
		t.Errorf("draft %+v", d)
	}
	expectText(t, d.More, "IdentityFile ~/.ssh/other\nForwardAgent yes")
	e := d
	e.More = "ForwardAgent=yes\n  \nLocalForward 8080 localhost:80"
	opts, err := e.Options()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range opts {
		got = append(got, o.Key+" "+o.Value)
	}
	if want := []string{"HostName 10.0.0.5", "IdentityFile ~/my key", "ForwardAgent yes", "LocalForward 8080 localhost:80"}; !reflect.DeepEqual(got, want) {
		t.Errorf("options %q", got)
	}
	e.More = "Compression"
	var missing MissingValueError
	if _, err := e.Options(); !errors.As(err, &missing) {
		t.Errorf("want MissingValueError, got %v", err)
	}
	if d := NewHostDraft(nil); d != (HostDraft{}) {
		t.Errorf("new draft %+v", d)
	}
}

func tunnelOf(host, kind, spec string) sshhosts.TunnelStatus {
	return sshhosts.TunnelStatus{Tunnel: sshhosts.Tunnel{Host: host, Kind: kind, Spec: spec}}
}

func TestTunnels(t *testing.T) {
	d := NewTunnelDraft("bastion", "")
	d.ListenPort, d.DestHost, d.DestPort = "5432", "db.internal", "5432"
	expectText(t, d.Spec(), "5432:db.internal:5432")
	d.DestHost = "::1"
	expectText(t, d.Spec(), "5432:[::1]:5432")
	tl := tunnelOf("bastion", "local", "127.0.0.1:5432:db:5433")
	expectText(t, TunnelDescribe(tl), "127.0.0.1:5432 → db:5433 via bastion")
	if p, ok := TunnelLocalPort(tl); !ok || p != "5432" {
		t.Errorf("local port %q", p)
	}
	back := TunnelDraftOf(tl)
	if back.ListenPort != "127.0.0.1:5432" || back.DestHost != "db" || back.DestPort != "5433" {
		t.Errorf("back %+v", back)
	}
	k := tunnelOf("", "kubectl", "8080:80")
	k.Namespace, k.Resource = "shop", "svc/web"
	expectText(t, TunnelDescribe(k), "localhost:8080 → svc/web:80 in shop")
	if p, ok := TunnelLocalPort(k); !ok || p != "8080" {
		t.Errorf("kubectl port %q", p)
	}
	expectText(t, TunnelDescribe(tunnelOf("b", "dynamic", "1080")), "SOCKS proxy on localhost:1080 via b")
	if p, ok := TunnelLocalPort(tunnelOf("", "remote", "8080:localhost:80")); ok {
		t.Errorf("remote port %q", p)
	}
	expectText(t, TunnelDescribe(tunnelOf("b", "remote", "8080:localhost:80")), "b:8080 → localhost:80 here")
}

func TestHostKeyVerdicts(t *testing.T) {
	a, b := sshhosts.HostKey{Fingerprint: "A"}, sshhosts.HostKey{Fingerprint: "B"}
	keys := func(k ...sshhosts.HostKey) []sshhosts.HostKey { return k }
	expectText(t, HostKeyVerdict(nil, keys(a)), "The server offered no keys.")
	expectText(t, HostKeyVerdict(keys(a), nil), "None of these keys are stored yet.")
	expectText(t, HostKeyVerdict(keys(a), keys(a)), "All of the server’s keys match the stored ones.")
	expectText(t, HostKeyVerdict(keys(a, b), keys(a)), "1 of the server’s 2 keys match the stored ones.")
	expectText(t, HostKeyVerdict(keys(b), keys(a)), "None of the server’s keys match the stored ones.")
}
