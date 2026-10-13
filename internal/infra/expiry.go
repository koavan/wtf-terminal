package infra

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// When the shell's credentials stop working, as the credentials themselves
// say: nothing is estimated, and credentials that renew on their own
// (exec plugins such as aws-iam-authenticator, assumed roles while their
// source works) report nothing.
//
// - AWS: the expiry exported with temporary keys (aws-vault, granted,
//   `aws configure export-credentials`), the SSO token of an SSO profile,
//   and the x_security_token_expires stamp tools like saml2aws write to
//   the credentials file.
// - Kubernetes: the current context user's client certificate, and its
//   token or OIDC id-token when that is a JWT.
// - SSH: certificates loaded in the shell's ssh-agent.
//
// Credentials are read only to find the expiry and never kept.

// Expiry is when one credential stops working.
type Expiry struct {
	// "aws", "kube" or "ssh": the badge it belongs to.
	Kind string `json:"kind"`
	// What expires, for people: "AWS SSO session acme".
	What string `json:"what"`
	// Unix milliseconds.
	ExpiresAt int64 `json:"expiresAt"`
}

func expiry(kind, what string, t time.Time) []Expiry {
	if t.IsZero() {
		return nil
	}
	return []Expiry{{Kind: kind, What: what, ExpiresAt: t.UnixMilli()}}
}

// parseTime reads the timestamps the tools write: RFC 3339, and the
// "…UTC" form of the aws CLI v1's SSO cache.
func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05UTC", "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ── AWS ──────────────────────────────────────────────────────────────────

// credsExpiryKey is the one value read from the credentials file.
const credsExpiryKey = "x_security_token_expires"

// awsEnvExpiry is the expiry exported with temporary keys.
func awsEnvExpiry(env map[string]string) []Expiry {
	t := parseTime(firstOf(env["AWS_CREDENTIAL_EXPIRATION"], env["AWS_SESSION_EXPIRATION"]))
	what := "AWS credentials in the environment"
	if v := env["AWS_VAULT"]; v != "" {
		what = "aws-vault session " + v
	}
	return expiry("aws", what, t)
}

// awsProfileExpiries follows source_profile to the profile that holds the
// credentials, and reports its SSO token or credentials-file stamp.
func (s *Service) awsProfileExpiries(config, creds iniFile, section, profile string) []Expiry {
	for range 5 {
		if t := parseTime(creds.get(profile, credsExpiryKey)); !t.IsZero() {
			return expiry("aws", "AWS session of profile "+profile, t)
		}
		if out := s.ssoExpiry(config, section); out != nil {
			return out
		}
		src := config.get(section, "source_profile")
		if src == "" || src == profile {
			return nil
		}
		profile, section = src, "profile "+src
		if src == "default" {
			section = "default"
		}
	}
	return nil
}

// ssoExpiry reads the aws CLI's SSO token cache, named after the SHA-1 of
// the sso-session name (or, for older profiles, of the start URL).
func (s *Service) ssoExpiry(config iniFile, section string) []Expiry {
	key, what := "", ""
	if name := config.get(section, "sso_session"); name != "" {
		key, what = name, "AWS SSO session "+name
	} else if url := config.get(section, "sso_start_url"); url != "" {
		key, what = url, "AWS SSO login "+url
	}
	if key == "" || s.home == "" {
		return nil
	}
	sum := sha1.Sum([]byte(key))
	f, _ := s.load(filepath.Join(s.home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json"), ssoTokenFormat)
	return expiry("aws", what, f.expires)
}

func ssoTokenExpiry(data []byte) time.Time {
	var token struct {
		ExpiresAt string `json:"expiresAt"`
	}
	if json.Unmarshal(data, &token) != nil {
		return time.Time{}
	}
	return parseTime(token.ExpiresAt)
}

// ── Kubernetes ───────────────────────────────────────────────────────────

type kubeUser struct {
	Name string `yaml:"name"`
	User struct {
		ClientCertificateData string `yaml:"client-certificate-data"`
		ClientCertificate     string `yaml:"client-certificate"`
		Token                 string `yaml:"token"`
		AuthProvider          struct {
			Config struct {
				IDToken string `yaml:"id-token"`
			} `yaml:"config"`
		} `yaml:"auth-provider"`
	} `yaml:"user"`

	CertExpires  time.Time `yaml:"-"`
	TokenExpires time.Time `yaml:"-"`
}

// forgetCredentials turns the users' credentials into their expiries, so
// no token stays in memory, and makes certificate paths absolute.
func (k *kubeconfig) forgetCredentials(dir string) {
	for i := range k.Users {
		u := &k.Users[i].User
		if u.ClientCertificateData != "" {
			if pemData, err := base64.StdEncoding.DecodeString(u.ClientCertificateData); err == nil {
				k.Users[i].CertExpires = certExpiry(pemData)
			}
		}
		if p := u.ClientCertificate; p != "" && !filepath.IsAbs(p) {
			u.ClientCertificate = filepath.Join(dir, p)
		}
		k.Users[i].TokenExpires = jwtExpiry(firstOf(u.Token, u.AuthProvider.Config.IDToken))
		u.ClientCertificateData, u.Token, u.AuthProvider.Config.IDToken = "", "", ""
	}
}

func (s *Service) kubeUserExpiries(configs []kubeconfig, name string) []Expiry {
	if name == "" {
		return nil
	}
	for _, c := range configs {
		for _, u := range c.Users {
			if u.Name != name {
				continue
			}
			cert := u.CertExpires
			if cert.IsZero() && u.User.ClientCertificate != "" {
				f, _ := s.load(u.User.ClientCertificate, certFormat)
				cert = f.expires
			}
			out := expiry("kube", "Kubernetes client certificate of user "+name, cert)
			return append(out, expiry("kube", "Kubernetes token of user "+name, u.TokenExpires)...)
		}
	}
	return nil
}

// certExpiry is when the first certificate in PEM data expires.
func certExpiry(data []byte) time.Time {
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return time.Time{}
		}
		if block.Type == "CERTIFICATE" {
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
				return cert.NotAfter
			}
		}
	}
}

// jwtExpiry is a JWT's exp claim, unverified: it is only shown.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(claims.Exp), 0)
}

// ── SSH ──────────────────────────────────────────────────────────────────

const (
	agentTimeout = 500 * time.Millisecond
	// Panes prompting together share one answer.
	agentReuse = 5 * time.Second
)

type agentEntry struct {
	at    time.Time
	certs []Expiry
}

// sshCertificates lists the certificates in the shell's ssh-agent.
func (s *Service) sshCertificates(env map[string]string) []Expiry {
	sock := env["SSH_AUTH_SOCK"]
	if !filepath.IsAbs(sock) {
		return nil
	}
	s.mu.Lock()
	e, ok := s.agents[sock]
	s.mu.Unlock()
	if ok && time.Since(e.at) < agentReuse {
		return e.certs
	}
	e = agentEntry{at: time.Now(), certs: agentCertificates(sock)}
	s.mu.Lock()
	s.agents[sock] = e
	s.mu.Unlock()
	return e.certs
}

func agentCertificates(sock string) []Expiry {
	conn, err := net.DialTimeout("unix", sock, agentTimeout)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(agentTimeout))
	keys, err := agent.NewClient(conn).List()
	if err != nil {
		return nil
	}
	var out []Expiry
	for _, k := range keys {
		if !strings.HasSuffix(k.Format, "-cert-v01@openssh.com") {
			continue
		}
		pub, err := ssh.ParsePublicKey(k.Blob)
		if err != nil {
			continue
		}
		cert, ok := pub.(*ssh.Certificate)
		if !ok || cert.ValidBefore == ssh.CertTimeInfinity {
			continue
		}
		what := "SSH certificate"
		if cert.KeyId != "" {
			what += " " + cert.KeyId
		}
		if len(cert.ValidPrincipals) > 0 {
			what += " (" + strings.Join(cert.ValidPrincipals, ", ") + ")"
		}
		out = append(out, expiry("ssh", what, time.Unix(int64(cert.ValidBefore), 0))...)
	}
	return out
}
