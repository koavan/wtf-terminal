package sshhosts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hostKeyLine makes a fresh public key, as "type base64".
func hostKeyLine(t *testing.T, typ string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "k")
	keygen(t, path, "-t", typ, "-N", "")
	pub, _ := os.ReadFile(path + ".pub")
	f := strings.Fields(string(pub))
	return f[0] + " " + f[1], sshFingerprint(t, path+".pub")
}

func hostKeysService(t *testing.T, config, known string) (*Service, string) {
	t.Helper()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not installed")
	}
	home := t.TempDir()
	knownFile := filepath.Join(home, ".ssh", "known_hosts")
	// ssh expands ~ to the real home folder, whatever $HOME says, so the
	// file must be named outright or these tests would edit the user's own.
	write(t, filepath.Join(home, ".ssh", "config"), config+"Host *\n  UserKnownHostsFile "+knownFile+"\n")
	if known != "" {
		write(t, knownFile, known)
	}
	s := &Service{home: home}
	r, err := s.resolveHost("guard.invalid", "")
	if err != nil || len(r.knownFiles) != 1 || r.knownFiles[0] != knownFile {
		t.Fatalf("known_hosts files resolve to %v (%v), not the test's own", r.knownFiles, err)
	}
	return s, knownFile
}

func stubScan(t *testing.T, out string) *[]string {
	t.Helper()
	var calls []string
	old := keyscan
	keyscan = func(host, port string) (string, string, error) {
		calls = append(calls, host+":"+port)
		return out, "", nil
	}
	t.Cleanup(func() { keyscan = old })
	return &calls
}

func TestHostKeys(t *testing.T) {
	ed, edFP := hostKeyLine(t, "ed25519")
	rsa, _ := hostKeyLine(t, "rsa")
	other, _ := hostKeyLine(t, "ed25519")
	s, known := hostKeysService(t,
		"Host web\n  HostName web.example.com\n  Port 2222\nHost db\n  HostKeyAlias db-cluster\nHost inner\n  ProxyJump web\n",
		"[web.example.com]:2222 "+ed+"\nweb.example.com "+other+"\n"+hashName("db-cluster")+" "+rsa+"\n@revoked [web.example.com]:2222 "+rsa+"\n")

	hk, err := s.HostKeys("web", "")
	if err != nil {
		t.Fatal(err)
	}
	if hk.Target != "[web.example.com]:2222" || hk.HostName != "web.example.com" || hk.Port != "2222" {
		t.Errorf("target %+v", hk)
	}
	if len(hk.Files) != 1 || hk.Files[0] != known {
		t.Errorf("files %v, want %s", hk.Files, known)
	}
	if len(hk.Stored) != 2 || hk.Stored[0].Fingerprint != edFP || hk.Stored[0].Line != 1 || hk.Stored[1].Marker != "revoked" {
		t.Errorf("stored %+v", hk.Stored)
	}

	// HostKeyAlias names the entry; a hashed name is still found.
	db, err := s.HostKeys("db", "")
	if err != nil || db.Target != "db-cluster" || len(db.Stored) != 1 || db.Stored[0].Type != "RSA" {
		t.Errorf("db: %+v %v", db, err)
	}

	// A known host listed by address and port.
	byPort, err := s.HostKeys("web.example.com", "2222")
	if err != nil || len(byPort.Stored) != 2 {
		t.Errorf("by port: %+v %v", byPort, err)
	}

	if in, _ := s.HostKeys("inner", ""); in.Via != "web" {
		t.Errorf("via %q", in.Via)
	}
	if _, err := s.ScanHostKeys("inner", ""); err == nil || !strings.Contains(err.Error(), "through web") {
		t.Errorf("scanning through a jump host: %v", err)
	}
	if _, err := s.HostKeys("-oProxyCommand=x", ""); err == nil {
		t.Error("option-like names must be refused")
	}

	if err := s.ForgetHostKeys("web", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(known)
	// ssh-keygen -R keeps @revoked lines: a revocation must outlive a re-key.
	if strings.Contains(string(data), "[web.example.com]:2222 "+ed) || !strings.Contains(string(data), "web.example.com "+other) ||
		!strings.Contains(string(data), "@revoked [web.example.com]:2222") {
		t.Errorf("after forget:\n%s", data)
	}
}

func TestTrustHostKeys(t *testing.T) {
	old, _ := hostKeyLine(t, "ed25519")
	ed, edFP := hostKeyLine(t, "ed25519")
	rsa, rsaFP := hostKeyLine(t, "rsa")
	s, known := hostKeysService(t, "Host web\n  HostName 10.0.0.9\n  Port 2200\n", "[10.0.0.9]:2200 "+old)
	calls := stubScan(t, "# 10.0.0.9:2200 SSH-2.0-OpenSSH_9.6\n[10.0.0.9]:2200 "+ed+"\n[10.0.0.9]:2200 "+rsa+"\n")

	scanned, err := s.ScanHostKeys("web", "")
	if err != nil || len(scanned) != 2 || scanned[0].Fingerprint != edFP {
		t.Fatalf("scan: %+v %v", scanned, err)
	}
	if (*calls)[0] != "10.0.0.9:2200" {
		t.Errorf("scanned %v", *calls)
	}

	// Only the keys the user saw are stored, replacing the old one.
	if err := s.TrustHostKeys("web", "", []string{edFP}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(known)
	if got := string(data); got != "[10.0.0.9]:2200 "+ed+"\n" {
		t.Errorf("known_hosts:\n%q", got)
	}

	if err := s.TrustHostKeys("web", "", []string{"SHA256:somethingelse"}); err == nil {
		t.Error("a fingerprint the server no longer has must be refused")
	}
	if err := s.TrustHostKeys("web", "", []string{edFP, rsaFP}); err != nil {
		t.Fatal(err)
	}
	if hk, _ := s.HostKeys("web", ""); len(hk.Stored) != 2 {
		t.Errorf("stored after trusting both: %+v", hk.Stored)
	}
}

func TestTrustHashesWhenAsked(t *testing.T) {
	ed, edFP := hostKeyLine(t, "ed25519")
	s, known := hostKeysService(t, "Host web\n  HostName web.example.com\n  HashKnownHosts yes\n", "")
	stubScan(t, "web.example.com "+ed+"\n")
	if err := s.TrustHostKeys("web", "", []string{edFP}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(known)
	if !strings.HasPrefix(string(data), "|1|") || strings.Contains(string(data), "web.example.com") {
		t.Errorf("want a hashed entry, got %q", data)
	}
	if hk, _ := s.HostKeys("web", ""); len(hk.Stored) != 1 || hk.Stored[0].Fingerprint != edFP {
		t.Errorf("ssh-keygen must find the hashed entry: %+v", hk.Stored)
	}
}
