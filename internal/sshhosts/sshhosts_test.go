package sshhosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, config, known string, extra map[string]string) (List, string) {
	t.Helper()
	home := t.TempDir()
	if config != "" {
		write(t, filepath.Join(home, ".ssh", "config"), config)
	}
	if known != "" {
		write(t, filepath.Join(home, ".ssh", "known_hosts"), known)
	}
	for name, body := range extra {
		write(t, filepath.Join(home, name), body)
	}
	return (&Service{home: home}).List(), home
}

func find(l List, alias string) *Host {
	for i := range l.Hosts {
		if l.Hosts[i].Alias == alias {
			return &l.Hosts[i]
		}
	}
	return nil
}

func aliases(l List) string {
	var out []string
	for _, h := range l.Hosts {
		out = append(out, h.Alias)
	}
	return strings.Join(out, " ")
}

func TestConfigHosts(t *testing.T) {
	l, home := list(t, `
# Global defaults
User fallback

Host bastion
    HostName bastion.example.com
    User ops

Host web1 web2
  HostName=%h.internal.example.com
  ProxyJump bastion

Host db "quoted name"
  Port = 2222

Host *.internal.example.com !skip.internal.example.com
  User deploy

Host *
  User ignored-because-first-wins
  Port 22
`, "", nil)

	if got := aliases(l); got != "bastion web1 web2 db quoted name" {
		t.Fatalf("aliases: %q", got)
	}
	b := find(l, "bastion")
	// Lines before the first Host apply to every host, and come first.
	if b.HostName != "bastion.example.com" || b.User != "fallback" || b.Port != "22" || b.ProxyJump != "" {
		t.Errorf("bastion: %+v", b)
	}
	w := find(l, "web2")
	// %h is the alias.
	if w.HostName != "web2.internal.example.com" || w.User != "fallback" || w.ProxyJump != "bastion" {
		t.Errorf("web2: %+v", w)
	}
	if d := find(l, "db"); d.Port != "2222" || d.HostName != "db" {
		t.Errorf("db: %+v", d)
	}
	if b.Source != filepath.Join(home, ".ssh", "config") {
		t.Errorf("source: %q", b.Source)
	}
}

func TestWildcardsApplyInOrder(t *testing.T) {
	l, _ := list(t, `
Host *.prod
  User prod-user
Host api.prod
  User api-user
  HostName 10.0.0.1
Host !legacy.prod *.prod
  Port 2200
Host legacy.prod
`, "", nil)
	// The earlier wildcard block wins over the specific one, as in ssh.
	if a := find(l, "api.prod"); a.User != "prod-user" || a.Port != "2200" || a.HostName != "10.0.0.1" {
		t.Errorf("api.prod: %+v", a)
	}
	if lg := find(l, "legacy.prod"); lg.Port != "" {
		t.Errorf("negated pattern still applied: %+v", lg)
	}
}

func TestIncludes(t *testing.T) {
	l, home := list(t, `
Include config.d/*
Include ~/other/ssh.conf
Include missing/*
Host main
`, "", map[string]string{
		".ssh/config.d/b-staging": "Host stage1\n  HostName 10.1.0.1\n",
		".ssh/config.d/a-prod":    "Host prod1\n  User root\nInclude ../config\n", // a loop
		"other/ssh.conf":          "Match host foo\n  User nope\nHost other\n",
	})
	if got := aliases(l); got != "prod1 stage1 other main" {
		t.Fatalf("aliases: %q", got)
	}
	if p := find(l, "prod1"); p.User != "root" || p.Source != filepath.Join(home, ".ssh", "config.d", "a-prod") {
		t.Errorf("prod1: %+v", p)
	}
	if len(l.Problems) != 0 {
		t.Errorf("problems: %v", l.Problems)
	}
}

func TestMatchBlocksDontLeak(t *testing.T) {
	l, _ := list(t, "Host a\nMatch host a\n  User from-match\n  Include x\n", "", nil)
	if a := find(l, "a"); a.User != "" {
		t.Errorf("a: %+v", a)
	}
}

func TestKnownHosts(t *testing.T) {
	known := `github.com ssh-ed25519 AAAA
10.0.0.5 ssh-ed25519 AAAA
|1|hashed=|salt= ssh-rsa AAAA
@revoked bad.example.com ssh-rsa AAAA
[127.0.0.1]:32222 ssh-ed25519 AAAA
web1.internal.example.com,10.0.0.9 ssh-ed25519 AAAA
10.0.0.5 ecdsa-sha2-nistp256 AAAA
# a comment
new.example.com ssh-ed25519 AAAA
`
	config := "Host orb\n  HostName 127.0.0.1\n  Port 32222\nHost web1\n  HostName web1.internal.example.com\n"
	l, _ := list(t, config, known, nil)
	// Configured hosts first; then known ones, newest first, without git
	// hosts, hashed or revoked entries, or servers the config names.
	// 10.0.0.5 was added again after 10.0.0.9.
	if got := aliases(l); got != "orb web1 new.example.com 10.0.0.5 10.0.0.9" {
		t.Fatalf("aliases: %q", got)
	}
	if h := find(l, "10.0.0.5"); !h.Known || h.Source != "" {
		t.Errorf("known host: %+v", h)
	}
}

func TestKnownHostPort(t *testing.T) {
	l, _ := list(t, "", "[gw.example.com]:2222 ssh-ed25519 AAAA\n", nil)
	if h := find(l, "gw.example.com"); h == nil || h.Port != "2222" {
		t.Fatalf("got %+v", l.Hosts)
	}
}

func TestGitHostsHidden(t *testing.T) {
	l, _ := list(t, "Host github.com\n  IdentityFile ~/.ssh/github\nHost gh\n  HostName github.com\n", "", nil)
	if len(l.Hosts) != 0 {
		t.Fatalf("got %+v", l.Hosts)
	}
}

func TestNoSSHFolder(t *testing.T) {
	l, _ := list(t, "", "", nil)
	if l.Hosts == nil || len(l.Hosts) != 0 || len(l.Problems) != 0 {
		t.Fatalf("got %+v", l)
	}
}

func TestHostFacts(t *testing.T) {
	l, _ := list(t, `
Host lab
  ForwardAgent yes
  StrictHostKeyChecking no

Host build
  ForwardAgent ~/.1password/agent.sock
  ForwardAgent no

Host plain
  ForwardAgent no

Host *
  StrictHostKeyChecking Accept-New
`, "", nil)
	if h := find(l, "lab"); h.ForwardAgent != "yes" || h.StrictHostKeyChecking != "no" {
		t.Errorf("lab: %+v", h)
	}
	// The first value wins; Host * fills in what an entry leaves out.
	if h := find(l, "build"); h.ForwardAgent != "~/.1password/agent.sock" || h.StrictHostKeyChecking != "accept-new" {
		t.Errorf("build: %+v", h)
	}
	if h := find(l, "plain"); h.ForwardAgent != "" {
		t.Errorf("plain: %+v", h)
	}
}
