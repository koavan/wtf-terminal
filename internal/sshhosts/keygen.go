package sshhosts

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Generating a key pair from the Generate tab's form, without a terminal.
// The key is made and written here, in the OpenSSH format ssh-keygen
// writes (a passphrase encrypts it with bcrypt-pbkdf and AES-256, as
// ssh-keygen does). The passphrase is used once to encrypt the file and
// is never stored or logged. Files are created, never overwritten.

// KeyGenRequest is the form.
type KeyGenRequest struct {
	// "ed25519", "rsa" (4096 bits) or "ecdsa" (P-521).
	Type    string `json:"type"`
	Name    string `json:"name"`
	Comment string `json:"comment"`
	// Empty for none.
	Passphrase string `json:"passphrase"`
	// Load the new key into ssh-agent (until it restarts).
	AddToAgent bool `json:"addToAgent"`
}

// KeyGenResult is the new key.
type KeyGenResult struct {
	Path        string `json:"path"`
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
	InAgent     bool   `json:"inAgent"`
	// Why loading into the agent failed; the key files are fine regardless.
	AgentError string `json:"agentError"`
}

// minPassphrase is ssh-keygen's own minimum for a non-empty passphrase.
const minPassphrase = 5

// GenerateKey makes a key pair as ~/.ssh/<name> and <name>.pub.
func (s *Service) GenerateKey(req KeyGenRequest) (KeyGenResult, error) {
	name := strings.TrimSpace(req.Name)
	free, err := s.KeyNameFree(name)
	if err != nil {
		return KeyGenResult{}, err
	}
	if !free {
		return KeyGenResult{}, fmt.Errorf("~/.ssh/%s already exists: choose another name", name)
	}
	comment := strings.TrimSpace(req.Comment)
	if strings.ContainsFunc(comment, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return KeyGenResult{}, errors.New("the comment must be one line")
	}
	if req.Passphrase != "" && len(req.Passphrase) < minPassphrase {
		return KeyGenResult{}, fmt.Errorf("use a passphrase of at least %d characters, or none", minPassphrase)
	}

	priv, err := newPrivateKey(req.Type)
	if err != nil {
		return KeyGenResult{}, err
	}
	var block *pem.Block
	if req.Passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(req.Passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return KeyGenResult{}, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return KeyGenResult{}, err
	}
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if comment != "" {
		pub += " " + comment
	}

	if err := os.MkdirAll(s.sshDir(), 0o700); err != nil {
		return KeyGenResult{}, err
	}
	// KeyNameFree allows only plain names; checked again here, since these files hold secrets.
	path, err := s.keyPath(filepath.Join(s.sshDir(), name))
	if err != nil {
		return KeyGenResult{}, err
	}
	if err := createFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return KeyGenResult{}, err
	}
	if err := createFile(path+".pub", []byte(pub+"\n"), 0o644); err != nil {
		os.Remove(path) // half a key pair is worse than none
		return KeyGenResult{}, err
	}

	res := KeyGenResult{Path: path, PublicKey: pub, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
	if req.AddToAgent {
		if err := addToAgent(priv, comment); err != nil {
			res.AgentError = err.Error()
		} else {
			res.InAgent = true
		}
	}
	return res, nil
}

func newPrivateKey(typ string) (crypto.PrivateKey, error) {
	switch typ {
	case "ed25519", "":
		_, k, err := ed25519.GenerateKey(rand.Reader)
		return k, err
	case "rsa":
		return rsa.GenerateKey(rand.Reader, 4096)
	case "ecdsa":
		return ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	}
	return nil, fmt.Errorf("unknown key type %q", typ)
}

// createFile writes a new file, failing if one is already there.
func createFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

// addToAgent hands the unencrypted key to ssh-agent over its socket, as
// ssh-add would after asking for the passphrase.
func addToAgent(priv crypto.PrivateKey, comment string) error {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return errors.New("no ssh-agent (SSH_AUTH_SOCK isn't set)")
	}
	conn, err := net.DialTimeout("unix", sock, agentTimeout)
	if err != nil {
		return fmt.Errorf("can't reach ssh-agent: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(agentTimeout))
	return agent.NewClient(conn).Add(agent.AddedKey{PrivateKey: priv, Comment: comment})
}
