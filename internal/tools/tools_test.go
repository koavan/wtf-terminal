package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho v1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindPrefersTheShellsPATH(t *testing.T) {
	dir := t.TempDir()
	shellBin := filepath.Join(dir, "shell")
	writeExec(t, filepath.Join(shellBin, "wtf-test-tool"))
	// Relative entries mean nothing to wtf, which has its own working directory.
	if got := Find("wtf-test-tool", "relative:"+shellBin); got != filepath.Join(shellBin, "wtf-test-tool") {
		t.Fatalf("got %q", got)
	}
	if got := Find("wtf-test-tool", ""); got != "" {
		t.Fatalf("found %q off the shell's PATH", got)
	}
	// A folder or a file that isn't executable doesn't count.
	if err := os.MkdirAll(filepath.Join(dir, "d", "wtf-test-tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Find("wtf-test-tool", filepath.Join(dir, "d")); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFindLooksInLocalBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeExec(t, filepath.Join(home, ".local", "bin", "wtf-test-tool"))
	if got := Find("wtf-test-tool", ""); got != filepath.Join(home, ".local", "bin", "wtf-test-tool") {
		t.Fatalf("got %q", got)
	}
}

func ids(ms []Method) string {
	var s []string
	for _, m := range ms {
		s = append(s, m.ID)
	}
	return strings.Join(s, ",")
}

func TestMethodsOnMacOS(t *testing.T) {
	kubectl, _ := lookup("kubectl")
	aws, _ := lookup("aws")
	git, _ := lookup("git")
	ssh, _ := lookup("openssh")

	p := platform{goos: "darwin", goarch: "arm64", home: "/Users/me", brew: "/opt/homebrew/bin/brew", shellPATH: "/opt/homebrew/bin:/usr/bin", shell: "/bin/zsh"}
	if got := ids(p.methods(kubectl)); got != "brew,official" {
		t.Fatalf("kubectl: %s", got)
	}
	if got := ids(p.methods(git)); got != "brew,xcode" {
		t.Fatalf("git: %s", got)
	}
	if got := ids(p.methods(ssh)); got != "" {
		t.Fatalf("ssh is part of macOS, got %s", got)
	}
	if m := p.methods(aws)[0]; m.Command != "brew install awscli" || m.Script != "" {
		t.Fatalf("aws brew: %+v", m)
	}
	m := p.methods(aws)[1]
	if !m.Sudo || !strings.Contains(m.Script, "pkgutil --check-signature AWSCLIV2.pkg") {
		t.Fatalf("aws pkg: %+v", m)
	}

	// Homebrew installed but not on the shell's PATH: its full path.
	p.shellPATH = "/usr/bin"
	if m := p.methods(kubectl)[0]; m.Command != "/opt/homebrew/bin/brew install kubectl" {
		t.Fatalf("got %q", m.Command)
	}

	// No Homebrew: only the official downloads.
	p.brew = ""
	if got := ids(p.methods(kubectl)); got != "official" {
		t.Fatalf("kubectl without brew: %s", got)
	}
	off := p.methods(kubectl)[0]
	for _, want := range []string{"bin/darwin/arm64/kubectl", "shasum -a 256 --check", `"$HOME/.local/bin/kubectl"`} {
		if !strings.Contains(off.Script, want) {
			t.Errorf("kubectl script lacks %q:\n%s", want, off.Script)
		}
	}
	if !strings.Contains(off.Note, "~/.zshrc") || off.Sudo {
		t.Errorf("note %q, sudo %v", off.Note, off.Sudo)
	}
}

func TestMethodsOnLinux(t *testing.T) {
	kubectl, _ := lookup("kubectl")
	aws, _ := lookup("aws")
	notify, _ := lookup("notify-send")
	ssh, _ := lookup("openssh")

	apt := &packageManagers[0]
	p := platform{goos: "linux", goarch: "amd64", home: "/home/me", pm: apt, unzip: true, shellPATH: "/home/me/.local/bin:/usr/bin", shell: "/usr/bin/fish"}
	if got := ids(p.methods(kubectl)); got != "official" {
		t.Fatalf("kubectl: %s", got)
	}
	k := p.methods(kubectl)[0]
	if !strings.Contains(k.Script, "bin/linux/amd64/kubectl") || !strings.Contains(k.Script, "sha256sum --check") || k.Note != "" {
		t.Fatalf("kubectl: %+v", k)
	}
	a := p.methods(aws)[0]
	if !strings.Contains(a.Script, "awscli-exe-linux-x86_64.zip") || a.Sudo || strings.Contains(a.Script, "apt-get") {
		t.Fatalf("aws: %+v", a)
	}
	if m := p.methods(notify); len(m) != 1 || m[0].Command != "sudo apt-get update && sudo apt-get install libnotify-bin" || !m[0].Sudo {
		t.Fatalf("notify-send: %+v", m)
	}
	p.pm = &packageManagers[3] // pacman
	if m := p.methods(ssh); m[0].Command != "sudo pacman -S --needed openssh" {
		t.Fatalf("ssh: %+v", m)
	}

	// No unzip: the script installs it first, with sudo.
	p.pm, p.unzip = apt, false
	a = p.methods(aws)[0]
	if !a.Sudo || !strings.Contains(a.Script, "sudo apt-get update && sudo apt-get install unzip") {
		t.Fatalf("aws without unzip: %+v", a)
	}
	if !strings.Contains(a.Script, "aws/install --install-dir") {
		t.Fatal(a.Script)
	}

	// arm64 Linux and ~/.local/bin off PATH, with fish.
	p.goarch, p.shellPATH = "arm64", "/usr/bin"
	a = p.methods(aws)[0]
	if !strings.Contains(a.Script, "linux-aarch64.zip") || !strings.Contains(a.Note, "fish_add_path ~/.local/bin") {
		t.Fatalf("aws arm64: %+v", a)
	}
	// An architecture AWS doesn't build for: no official method.
	p.goarch = "riscv64"
	if got := ids(p.methods(aws)); got != "" {
		t.Fatalf("riscv64: %s", got)
	}

	// No package manager: nothing for the distribution's packages.
	p.pm = nil
	if got := ids(p.methods(notify)); got != "" {
		t.Fatalf("no package manager: %s", got)
	}
}

// The scripts are valid sh.
func TestScriptsParse(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for _, p := range []platform{
		{goos: "darwin", goarch: "arm64", home: "/Users/me"},
		{goos: "linux", goarch: "amd64", home: "/home/me", pm: &packageManagers[1]},
		{goos: "linux", goarch: "arm64", home: "/home/me"},
	} {
		for _, tl := range catalog {
			for _, m := range p.methods(tl) {
				if m.Script == "" {
					continue
				}
				f := filepath.Join(t.TempDir(), "s.sh")
				if err := os.WriteFile(f, []byte(m.Script), 0o600); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command(sh, "-n", f).CombinedOutput(); err != nil {
					t.Errorf("%s/%s %s: %v\n%s", p.goos, p.goarch, tl.id, err, out)
				}
			}
		}
	}
}

func TestStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", "")
	defer func(f func() []string) { installDirs = f }(installDirs)
	installDirs = func() []string { return nil }
	writeExec(t, filepath.Join(dir, "bin", "ssh"))
	writeExec(t, filepath.Join(dir, "bin", "ssh-keygen"))
	p := platform{goos: "linux", goarch: "amd64", home: dir, pm: &packageManagers[0], shellPATH: filepath.Join(dir, "bin")}
	s := &Service{}

	ssh, _ := lookup("openssh")
	st := s.status(ssh, p)
	if !st.Found || st.Path != filepath.Join(dir, "bin", "ssh") || strings.Join(st.Missing, ",") != "ssh-add,ssh-keyscan" {
		t.Fatalf("%+v", st)
	}
	if len(st.Methods) != 1 {
		t.Fatalf("missing programs: offer the package, got %+v", st.Methods)
	}

	// ss or lsof: either will do.
	writeExec(t, filepath.Join(dir, "bin", "lsof"))
	net, _ := lookup("ss")
	if st := s.status(net, p); !st.Found || len(st.Missing) != 0 || st.Path != filepath.Join(dir, "bin", "lsof") || st.Methods != nil {
		t.Fatalf("%+v", st)
	}

	kubectl, _ := lookup("kubectl")
	if st := s.status(kubectl, p); st.Found || len(st.Methods) == 0 {
		t.Fatalf("%+v", st)
	}
}

func TestVersion(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\necho 'Client Version: v1.31.2'\necho 'Kustomize Version: v5'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := version(prog, []string{"version", "--client"}); got != "v1.31.2" {
		t.Fatalf("got %q", got)
	}
	// ssh -V prints on stderr.
	if err := os.WriteFile(prog, []byte("#!/bin/sh\necho 'OpenSSH_9.8p1, LibreSSL 3.3.6' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := version(prog, nil); got != "OpenSSH_9.8p1, LibreSSL 3.3.6" {
		t.Fatalf("got %q", got)
	}
}

func TestInstallWritesTheScript(t *testing.T) {
	s := &Service{cacheDir: t.TempDir()}
	cmd, err := s.writeScript("kubectl-official.sh", "echo hi\n")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.cacheDir, "install", "kubectl-official.sh")
	if cmd != "sh "+path {
		t.Fatalf("got %q", cmd)
	}
	// Written afresh, readable only by the user.
	if _, err := s.writeScript("kubectl-official.sh", "echo again\n"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if string(b) != "echo again\n" || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%q %v", b, fi.Mode())
	}

	if _, err := s.Install("nope", "brew", ""); err == nil {
		t.Fatal("unknown tool")
	}
	if _, err := s.Install("kubectl", "nope", ""); err == nil {
		t.Fatal("unknown method")
	}
	cmd, err = s.Install("kubectl", "official", "")
	if err != nil || cmd != "sh "+filepath.Join(s.cacheDir, "install", "kubectl-official.sh") {
		t.Fatalf("%q %v", cmd, err)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/homebrew/bin/brew":         "/opt/homebrew/bin/brew",
		"/Users/a b/Library/Caches/x.sh": "'/Users/a b/Library/Caches/x.sh'",
		"/home/o'neil/.cache/wtf/x.sh":   `'/home/o'"'"'neil/.cache/wtf/x.sh'`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestFirstRunOnce(t *testing.T) {
	s := &Service{stateDir: t.TempDir()}
	first := s.FirstRun("/nonexistent")
	// What's found depends on the machine; scanners are never listed.
	for _, name := range first {
		for _, tl := range catalog {
			if tl.scanner && tl.name == name {
				t.Errorf("first run lists the scanner %s", name)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(s.stateDir, "tools-checked")); err != nil {
		t.Fatal(err)
	}
	if again := s.FirstRun("/nonexistent"); again != nil {
		t.Fatalf("second run: %v", again)
	}
}
