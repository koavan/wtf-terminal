package sshhosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// editSetup writes a config and returns a service for it, plus a reader.
func editSetup(t *testing.T, config string) (*Service, string, func() string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".ssh", "config")
	if config != "" {
		write(t, path, config)
	}
	s := &Service{home: home, backupDir: filepath.Join(home, "backups")}
	read := func() string {
		data, _ := os.ReadFile(path)
		return string(data)
	}
	return s, path, read
}

func entryFor(t *testing.T, l List, alias string) (Host, string) {
	t.Helper()
	h := find(l, alias)
	if h == nil {
		t.Fatalf("no host %q in %+v", alias, l.Hosts)
	}
	return *h, l.Versions[h.Source]
}

func opts(kv ...string) []Option {
	var out []Option
	for i := 0; i < len(kv); i += 2 {
		out = append(out, Option{Key: kv[i], Value: kv[i+1]})
	}
	return out
}

const editConfig = `# My hosts
Host bastion
  HostName bastion.example.com
  # ops account
  User ops
  ForwardAgent yes

Host web1 web2
  HostName %h.internal

# Defaults
Host *
  ServerAliveInterval 30
`

func TestAddGoesBeforeHostStar(t *testing.T) {
	s, _, read := editSetup(t, editConfig)
	l := s.List()
	got, err := s.Save(Entry{Version: l.Versions[l.MainConfig], Patterns: []string{"db"},
		Options: opts("HostName", "10.0.0.7", "User", "admin", "Port", "2222", "IdentityFile", "~/.ssh/My Key")})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editConfig, "# Defaults\n", `Host db
  HostName 10.0.0.7
  User admin
  Port 2222
  IdentityFile "~/.ssh/My Key"

# Defaults
`, 1)
	if read() != want {
		t.Fatalf("config:\n%s\nwant:\n%s", read(), want)
	}
	if h := find(got, "db"); h == nil || h.Port != "2222" || !h.Editable || h.Line != 11 {
		t.Fatalf("listed: %+v", h)
	}
}

func TestAddToMissingConfig(t *testing.T) {
	s, path, read := editSetup(t, "")
	if _, err := s.Save(Entry{Patterns: []string{"box"}, Options: opts("HostName", "box.local")}); err != nil {
		t.Fatal(err)
	}
	if read() != "Host box\n    HostName box.local\n" {
		t.Fatalf("got %q", read())
	}
	fi, _ := os.Stat(path)
	dir, _ := os.Stat(filepath.Dir(path))
	if fi.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
		t.Fatalf("modes %v %v", fi.Mode(), dir.Mode())
	}
}

func TestEditInPlace(t *testing.T) {
	s, _, read := editSetup(t, editConfig)
	h, v := entryFor(t, s.List(), "bastion")
	if strings.Join(h.Patterns, " ") != "bastion" || len(h.Options) != 3 || h.Options[1] != (Option{"User", "ops"}) {
		t.Fatalf("entry: %+v", h)
	}
	// Change the user, drop ForwardAgent, add a port; rename the host.
	_, err := s.Save(Entry{File: h.Source, Line: h.Line, Version: v, Patterns: []string{"jump"},
		Options: opts("HostName", "bastion.example.com", "User", "root", "Port", "2200")})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editConfig, `Host bastion
  HostName bastion.example.com
  # ops account
  User ops
  ForwardAgent yes
`, `Host jump
  HostName bastion.example.com
  # ops account
  User root
  Port 2200
`, 1)
	if read() != want {
		t.Fatalf("config:\n%s\nwant:\n%s", read(), want)
	}
}

func TestEditRefusesStaleVersion(t *testing.T) {
	s, path, _ := editSetup(t, editConfig)
	h, v := entryFor(t, s.List(), "bastion")
	write(t, path, editConfig+"Host late\n")
	_, err := s.Save(Entry{File: h.Source, Line: h.Line, Version: v, Patterns: []string{"bastion"}})
	if err != errChanged {
		t.Fatalf("got %v", err)
	}
}

func TestDuplicateNames(t *testing.T) {
	s, _, _ := editSetup(t, editConfig)
	l := s.List()
	_, err := s.Save(Entry{Version: l.Versions[l.MainConfig], Patterns: []string{"web2"}})
	if err == nil || !strings.Contains(err.Error(), "web2 is already defined in ~/.ssh/config, line 8") {
		t.Fatalf("got %v", err)
	}
	// An entry keeps its own names.
	h, v := entryFor(t, l, "web1")
	if _, err := s.Save(Entry{File: h.Source, Line: h.Line, Version: v, Patterns: h.Patterns, Options: opts("User", "x")}); err != nil {
		t.Fatal(err)
	}
}

func TestValidation(t *testing.T) {
	s, _, read := editSetup(t, editConfig)
	l := s.List()
	v := l.Versions[l.MainConfig]
	cases := []struct {
		e    Entry
		want string
	}{
		{Entry{Patterns: []string{"*"}}, "give the host a name"},
		{Entry{Patterns: []string{"a b"}}, "can't contain spaces"},
		{Entry{Patterns: []string{"a"}, Options: opts("Port", "99999")}, "must be a number"},
		{Entry{Patterns: []string{"a"}, Options: opts("Include", "x")}, "can't be used inside"},
		{Entry{Patterns: []string{"a"}, Options: opts("User", "a\nHost evil")}, "can't span lines"},
		{Entry{Patterns: []string{"a"}, Options: opts("Not-A-Key", "x")}, "isn't a setting name"},
		// ssh itself rejects unknown settings.
		{Entry{Patterns: []string{"a"}, Options: opts("Bogus", "yes")}, "Bad configuration option: bogus"},
	}
	for _, c := range cases {
		c.e.Version = v
		if _, err := s.Save(c.e); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.e, err, c.want)
		}
	}
	if read() != editConfig {
		t.Fatal("a refused edit changed the file")
	}
}

func TestDelete(t *testing.T) {
	s, _, read := editSetup(t, editConfig)
	h, v := entryFor(t, s.List(), "bastion")
	if _, err := s.Delete(h.Source, h.Line, v); err != nil {
		t.Fatal(err)
	}
	// The blank line before the next entry stays, after the header comment.
	want := "# My hosts\n\nHost web1 web2\n  HostName %h.internal\n\n# Defaults\nHost *\n  ServerAliveInterval 30\n"
	if read() != want {
		t.Fatalf("got:\n%s", read())
	}
	// A copy of the old file was kept.
	backups, _ := os.ReadDir(s.backupDir)
	if len(backups) != 1 || !strings.HasSuffix(backups[0].Name(), "-.ssh_config") {
		t.Fatalf("backups: %v", backups)
	}
}

func TestDeleteLastEntry(t *testing.T) {
	s, _, read := editSetup(t, "Host a\n  User x\n\nHost b\n  User y\n")
	h, v := entryFor(t, s.List(), "b")
	if _, err := s.Delete(h.Source, h.Line, v); err != nil {
		t.Fatal(err)
	}
	if read() != "Host a\n  User x\n" {
		t.Fatalf("got %q", read())
	}
}

func TestOnlyFilesUnderSSHAreEditable(t *testing.T) {
	s, _, _ := editSetup(t, "Include ~/.orbstack/ssh/config\nHost mine\n")
	write(t, filepath.Join(s.home, ".orbstack", "ssh", "config"), "Host orb\n  Port 32222\n")
	l := s.List()
	orb, v := entryFor(t, l, "orb")
	if orb.Editable || !find(l, "mine").Editable {
		t.Fatalf("editable: orb %v mine %v", orb.Editable, find(l, "mine").Editable)
	}
	if _, err := s.Save(Entry{File: orb.Source, Line: orb.Line, Version: v, Patterns: []string{"orb"}}); err == nil {
		t.Fatal("edited a file outside ~/.ssh")
	}
}

func TestWriteFollowsSymlink(t *testing.T) {
	s, path, _ := editSetup(t, "")
	real := filepath.Join(s.home, "dotfiles", "ssh_config")
	write(t, real, "Host a\n")
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	l := s.List()
	if _, err := s.Save(Entry{Version: l.Versions[l.MainConfig], Patterns: []string{"b"}}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(path); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
	if data, _ := os.ReadFile(real); string(data) != "Host a\n\nHost b\n" {
		t.Fatalf("got %q", data)
	}
}

func TestKeys(t *testing.T) {
	s, _, _ := editSetup(t, "")
	for _, f := range []string{"id_ed25519", "id_ed25519.pub", "work", "work.pub", "known_hosts", "config"} {
		write(t, filepath.Join(s.sshDir(), f), "")
	}
	if got := strings.Join(s.Keys(), " "); got != "~/.ssh/id_ed25519 ~/.ssh/work" {
		t.Fatalf("got %q", got)
	}
}
