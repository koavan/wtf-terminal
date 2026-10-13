package infra

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var when = time.Date(2026, 10, 12, 18, 30, 0, 0, time.UTC)

func wantExpiries(t *testing.T, got []Expiry, want ...Expiry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

func TestAWSEnvExpiry(t *testing.T) {
	s := testService(t.TempDir(), nil)
	got := s.Resolve(map[string]string{"WTF_AWS_ENV_KEYS": "1", "AWS_CREDENTIAL_EXPIRATION": "2026-10-12T18:30:00Z"})
	wantExpiries(t, got.Expiries, Expiry{"aws", "AWS credentials in the environment", when.UnixMilli()})

	// aws-vault: its profile, and the older variable name.
	got = s.Resolve(map[string]string{"WTF_AWS_ENV_KEYS": "1", "AWS_VAULT": "ops", "AWS_SESSION_EXPIRATION": "2026-10-12T20:30:00+02:00"})
	if got.AWSProfile != "ops" {
		t.Fatalf("profile %q", got.AWSProfile)
	}
	wantExpiries(t, got.Expiries, Expiry{"aws", "aws-vault session ops", when.UnixMilli()})

	// Keys without an expiry say nothing.
	got = s.Resolve(map[string]string{"WTF_AWS_ENV_KEYS": "1"})
	wantExpiries(t, got.Expiries)
}

func ssoCache(t *testing.T, home, key, body string) {
	sum := sha1.Sum([]byte(key))
	write(t, filepath.Join(home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json"), body)
}

func TestAWSSSOExpiry(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".aws", "config"), `
[profile new]
sso_session = acme
sso_account_id = 111111111111
[sso-session acme]
sso_start_url = https://acme.awsapps.com/start
[profile old]
sso_start_url = https://old.awsapps.com/start
sso_account_id = 222222222222
[profile admin]
role_arn = arn:aws:iam::333333333333:role/admin
source_profile = new
[profile nologin]
sso_session = never
sso_account_id = 444444444444
`)
	ssoCache(t, home, "acme", `{"accessToken": "SECRET", "expiresAt": "2026-10-12T18:30:00Z"}`)
	ssoCache(t, home, "https://old.awsapps.com/start", `{"accessToken": "SECRET", "expiresAt": "2026-10-12T18:30:00UTC"}`)
	s := testService(home, nil)

	for profile, what := range map[string]string{
		"new":   "AWS SSO session acme",
		"admin": "AWS SSO session acme",
		"old":   "AWS SSO login https://old.awsapps.com/start",
	} {
		got := s.Resolve(map[string]string{"AWS_PROFILE": profile})
		wantExpiries(t, got.Expiries, Expiry{"aws", what, when.UnixMilli()})
	}
	wantExpiries(t, s.Resolve(map[string]string{"AWS_PROFILE": "nologin"}).Expiries)

}

func TestAWSCredentialsFileExpiry(t *testing.T) {
	home := t.TempDir()
	creds := filepath.Join(home, ".aws", "credentials")
	write(t, creds, "[saml]\naws_secret_access_key = SECRET\naws_session_token = TOKEN\nx_security_token_expires = 2026-10-12T18:30:00Z\n")
	s := testService(home, func(_ context.Context, _, _, _, _ string) (string, error) { return "555555555555", nil })
	got := s.Resolve(map[string]string{"AWS_PROFILE": "saml"})
	wantExpiries(t, got.Expiries, Expiry{"aws", "AWS session of profile saml", when.UnixMilli()})

	c, _ := s.load(creds, credsFormat)
	if len(c.ini["saml"]) != 1 || strings.Contains(c.ini.get("saml", credsExpiryKey), "SECRET") {
		t.Fatalf("credentials kept: %v", c.ini)
	}
}

// selfSigned is a PEM certificate valid until notAfter.
func selfSigned(t *testing.T, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ada"},
		NotBefore:    notAfter.Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func jwt(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(claims)) + ".sig"
}

func TestKubeExpiries(t *testing.T) {
	home := t.TempDir()
	certData := base64.StdEncoding.EncodeToString(selfSigned(t, when))
	write(t, filepath.Join(home, "kube", "certs", "dev.crt"), string(selfSigned(t, when.Add(time.Hour))))
	write(t, filepath.Join(home, "kube", "config"), `
current-context: admin
contexts:
- name: admin
  context: {cluster: c1, user: admin}
- name: dev
  context: {cluster: c1, user: dev}
- name: oidc
  context: {cluster: c1, user: oidc}
- name: eks
  context: {cluster: c1, user: eks}
users:
- name: admin
  user:
    client-certificate-data: `+certData+`
    client-key-data: SECRETKEY
- name: dev
  user:
    client-certificate: certs/dev.crt
    token: `+jwt(`{"exp": 1791829800}`)+`
- name: oidc
  user:
    auth-provider:
      name: oidc
      config: {id-token: `+jwt(`{"exp": 1791829800.0}`)+`}
- name: eks
  user:
    exec: {command: aws-iam-authenticator}
`)
	s := testService(home, nil)
	cfg := filepath.Join(home, "kube", "config")
	env := func(ctx string) map[string]string {
		// $KUBECONFIG names the file; the first one sets the context.
		first := filepath.Join(home, "ctx-"+ctx)
		write(t, first, "current-context: "+ctx+"\n")
		return map[string]string{"KUBECONFIG": first + string(filepath.ListSeparator) + cfg}
	}

	wantExpiries(t, s.Resolve(env("admin")).Expiries,
		Expiry{"kube", "Kubernetes client certificate of user admin", when.UnixMilli()})
	wantExpiries(t, s.Resolve(env("dev")).Expiries,
		Expiry{"kube", "Kubernetes client certificate of user dev", when.Add(time.Hour).UnixMilli()},
		Expiry{"kube", "Kubernetes token of user dev", time.Unix(1791829800, 0).UnixMilli()})
	wantExpiries(t, s.Resolve(env("oidc")).Expiries,
		Expiry{"kube", "Kubernetes token of user oidc", time.Unix(1791829800, 0).UnixMilli()})
	wantExpiries(t, s.Resolve(env("eks")).Expiries)

	// The parsed kubeconfig keeps no credentials.
	c, _ := s.load(cfg, kubeFormat)
	for _, u := range c.kube.Users {
		if u.User.ClientCertificateData != "" || u.User.Token != "" || u.User.AuthProvider.Config.IDToken != "" {
			t.Fatalf("credentials kept for %s", u.Name)
		}
	}
}

func TestJWTExpiry(t *testing.T) {
	for _, tok := range []string{"", "opaque-token", "a.b", jwt(`{}`), jwt(`not json`), "a.!!!.c"} {
		if got := jwtExpiry(tok); !got.IsZero() {
			t.Fatalf("%q: %v", tok, got)
		}
	}
}

func TestSSHAgentCertificates(t *testing.T) {
	dir, err := os.MkdirTemp("", "wtfag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	keyring := agent.NewKeyring()
	_, caKey, _ := ed25519.GenerateKey(rand.Reader)
	ca, _ := ssh.NewSignerFromKey(caKey)
	add := func(validBefore uint64, id string) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		signer, _ := ssh.NewSignerFromKey(priv)
		cert := &ssh.Certificate{
			Key: signer.PublicKey(), CertType: ssh.UserCert, KeyId: id,
			ValidPrincipals: []string{"ada", "ops"}, ValidBefore: validBefore,
		}
		if err := cert.SignCert(rand.Reader, ca); err != nil {
			t.Fatal(err)
		}
		if err := keyring.Add(agent.AddedKey{PrivateKey: priv, Certificate: cert}); err != nil {
			t.Fatal(err)
		}
	}
	add(uint64(when.Unix()), "ada@corp")
	add(ssh.CertTimeInfinity, "forever")
	_, plain, _ := ed25519.GenerateKey(rand.Reader)
	_ = keyring.Add(agent.AddedKey{PrivateKey: plain})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, c); c.Close() }()
		}
	}()

	s := testService(t.TempDir(), nil)
	wantExpiries(t, s.Resolve(map[string]string{"SSH_AUTH_SOCK": sock}).Expiries,
		Expiry{"ssh", "SSH certificate ada@corp (ada, ops)", when.UnixMilli()})
	// A dead socket is no certificates, quickly.
	wantExpiries(t, s.Resolve(map[string]string{"SSH_AUTH_SOCK": filepath.Join(dir, "gone")}).Expiries)
}
