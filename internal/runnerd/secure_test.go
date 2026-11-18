package runnerd

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestSecureTightensFolders(t *testing.T) {
	base := t.TempDir()
	os.Chmod(base, 0o755)
	run := filepath.Join(base, RunDir)
	os.Mkdir(run, 0o777)
	os.Chmod(run, 0o777) // past the umask
	if err := Secure(base); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, base); m != 0o700 {
		t.Errorf("base is %o, want 700", m)
	}
	if m := mode(t, run); m != 0o700 {
		t.Errorf("run is %o, want 700", m)
	}
}

func TestSecureMakesRun(t *testing.T) {
	base := t.TempDir()
	if err := Secure(base); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Join(base, RunDir)); m != 0o700 {
		t.Errorf("run is %o, want 700", m)
	}
}

func TestSecureRefusesLinks(t *testing.T) {
	base, elsewhere := t.TempDir(), t.TempDir()
	os.Chmod(elsewhere, 0o755)
	if err := os.Symlink(elsewhere, filepath.Join(base, RunDir)); err != nil {
		t.Fatal(err)
	}
	if err := Secure(base); err == nil {
		t.Fatal("run/ as a link was accepted")
	}
	if m := mode(t, elsewhere); m != 0o755 {
		t.Errorf("the link's target was changed to %o", m)
	}
	// wtf-runner's folder itself as a link.
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(elsewhere, link)
	if err := Secure(link); err == nil {
		t.Fatal("a linked base was accepted")
	}
	// run/ as a file.
	base = t.TempDir()
	os.WriteFile(filepath.Join(base, RunDir), nil, 0o600)
	if err := Secure(base); err == nil {
		t.Fatal("run/ as a file was accepted")
	}
}

func TestLoadKeyKeepsAGoodOne(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	k1, err := loadKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if m := mode(t, p); m != 0o600 {
		t.Errorf("key is %o, want 600", m)
	}
	k2, err := loadKey(p)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("key changed: %v", err)
	}
}

func TestLoadKeyReplacesUntrusted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "key")
	k1, _ := loadKey(p)

	// Readable by others: it may have been read, so it's replaced.
	os.Chmod(p, 0o644)
	k2, err := loadKey(p)
	if err != nil || bytes.Equal(k1, k2) {
		t.Fatalf("a key others could read was kept (%v)", err)
	}
	if m := mode(t, p); m != 0o600 {
		t.Errorf("new key is %o, want 600", m)
	}

	// A link to someone's key: replaced by a file, the target untouched.
	target := filepath.Join(dir, "planted")
	os.WriteFile(target, []byte("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff\n"), 0o600)
	os.Remove(p)
	os.Symlink(target, p)
	k3, err := loadKey(p)
	if err != nil || bytes.Equal(k3[:4], []byte{0, 0x11, 0x22, 0x33}) {
		t.Fatalf("a linked key was used (%v)", err)
	}
	if fi, _ := os.Lstat(p); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("the key is still a link")
	}
	if b, _ := os.ReadFile(target); !bytes.HasPrefix(b, []byte("0011")) {
		t.Error("the link's target was written to")
	}

	// A FIFO doesn't hang it.
	os.Remove(p)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(p); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(p); !fi.Mode().IsRegular() {
		t.Error("the key is still a FIFO")
	}
}
