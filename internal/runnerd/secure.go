package runnerd

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Secure makes sure only this user can reach the daemon's files before it
// touches any of them: wtf-runner's folder and its run/ folder must be real
// folders (not links) owned by this user, and both are set to 0700 each
// time, so nobody else can reach the sockets, the key or the agents' state.
// It creates run/ when it's missing. It refuses a folder of another user's
// rather than run from it.
func Secure(base string) error {
	uid := uint32(os.Geteuid())
	if err := ownDir(base, uid); err != nil {
		return err
	}
	run := filepath.Join(base, RunDir)
	if err := os.Mkdir(run, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return ownDir(run, uid)
}

// ownDir checks p is a folder (not a link to one) owned by uid and sets it
// to 0700, on the folder it opened, so it can't be swapped in between.
func ownDir(p string, uid uint32) error {
	fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return fmt.Errorf("%s isn't a folder (it's a link or a file); wtf-runner won't use it", p)
		}
		return &fs.PathError{Op: "open", Path: p, Err: err}
	}
	defer syscall.Close(fd)
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return &fs.PathError{Op: "stat", Path: p, Err: err}
	}
	if st.Uid != uid {
		return fmt.Errorf("%s belongs to another user (uid %d, not %d); wtf-runner won't use it", p, st.Uid, uid)
	}
	if st.Mode&0o7777 != 0o700 {
		if err := syscall.Fchmod(fd, 0o700); err != nil {
			return &fs.PathError{Op: "chmod", Path: p, Err: err}
		}
	}
	return nil
}

// loadKey reads the bridge key the agents' tokens are made from, or makes
// a new one when there's none or the one there can't be trusted: not a
// plain file, not this user's, or readable by others. Agents started with
// an old key can't report until they're started again.
func loadKey(path string) ([]byte, error) {
	k, err := readKey(path)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		log.Printf("making a new bridge key: %v", err)
	}
	k = make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(hex.EncodeToString(k) + "\n"); err != nil {
		f.Close()
		return nil, err
	}
	return k, f.Close()
}

func readKey(path string) ([]byte, error) {
	// O_NONBLOCK: a FIFO put there can't hang the daemon.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s isn't a plain file", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("%s belongs to another user", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s could be read by others (mode %o)", path, fi.Mode().Perm())
	}
	b, err := io.ReadAll(io.LimitReader(f, 256))
	if err != nil {
		return nil, err
	}
	k, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) < 32 {
		return nil, fmt.Errorf("%s doesn't hold a key", path)
	}
	return k, nil
}
