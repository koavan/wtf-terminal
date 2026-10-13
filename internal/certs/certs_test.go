package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type issued struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

// issue makes a certificate for names, signed by parent (self-signed when nil).
func issue(t *testing.T, cn string, names []string, notAfter time.Time, ca bool, parent *issued) issued {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     notAfter,
		IsCA:         ca, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return issued{c, der, key}
}

func pemOf(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// serve answers TLS on localhost with the chain, until the test ends.
func serve(t *testing.T, chain ...issued) string {
	t.Helper()
	cert := tls.Certificate{PrivateKey: chain[0].key}
	for _, c := range chain {
		cert.Certificate = append(cert.Certificate, c.der)
	}
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	return l.Addr().String()
}

func TestTarget(t *testing.T) {
	for in, want := range map[string][2]string{
		"example.com":                  {"example.com", "example.com:443"},
		"example.com:8443":             {"example.com", "example.com:8443"},
		"https://example.com/a?b=c":    {"example.com", "example.com:443"},
		"https://example.com:9443/":    {"example.com", "example.com:9443"},
		"ldaps://dc.corp":              {"dc.corp", "dc.corp:636"},
		"10.0.0.1":                     {"10.0.0.1", "10.0.0.1:443"},
		"[::1]:8443":                   {"::1", "[::1]:8443"},
		"  api.internal.example.com/ ": {"api.internal.example.com", "api.internal.example.com:443"},
	} {
		host, addr, err := Target(in)
		if err != nil || host != want[0] || addr != want[1] {
			t.Errorf("%q: %q %q %v", in, host, addr, err)
		}
	}
	for _, bad := range []string{"", "a b", "host:99999", "host:x", "https://", "user@host"} {
		if _, _, err := Target(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestInspectHost(t *testing.T) {
	root := issue(t, "Test Root", nil, time.Now().Add(240*time.Hour), true, nil)
	leaf := issue(t, "localhost", []string{"localhost", "127.0.0.1"}, time.Now().Add(72*time.Hour), false, &root)
	addr := serve(t, leaf, root)

	pool := x509.NewCertPool()
	pool.AddCert(root.cert)
	s := &Service{roots: pool}

	r, err := s.InspectHost("https://" + addr + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verified || r.VerifyError != "" || r.Protocol != "TLS 1.3" || r.ALPN != "h2" || r.Cipher == "" {
		t.Fatalf("report: %+v", r)
	}
	if len(r.Certificates) != 2 || r.Certificates[0].Subject != "CN=localhost" || !r.Certificates[1].SelfSigned || !r.Certificates[1].IsCA {
		t.Fatalf("chain: %+v", r.Certificates)
	}
	if strings.Join(r.Certificates[0].Names, ",") != "localhost,127.0.0.1" || r.Certificates[0].Key != "ECDSA P-256" {
		t.Fatalf("leaf: %+v", r.Certificates[0])
	}

	// Untrusted: shown anyway, with the reason.
	r, err = (&Service{roots: x509.NewCertPool()}).InspectHost(addr)
	if err != nil || r.Verified || !strings.Contains(r.VerifyError, "unknown authority") || len(r.Certificates) != 2 {
		t.Fatalf("untrusted: %+v %v", r, err)
	}

	// Expired.
	old := issue(t, "localhost", []string{"127.0.0.1"}, time.Now().Add(-time.Hour), false, &root)
	r, err = s.InspectHost(serve(t, old, root))
	if err != nil || r.Verified || !strings.Contains(r.VerifyError, "expired") {
		t.Fatalf("expired: %+v %v", r, err)
	}

	// Nobody listening.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	gone := l.Addr().String()
	l.Close()
	if _, err := s.InspectHost(gone); err == nil {
		t.Fatal("no error for a closed port")
	}
}

func TestInspectFileAndText(t *testing.T) {
	a := issue(t, "a.example", []string{"a.example"}, time.Now().Add(time.Hour), false, nil)
	b := issue(t, "b.example", nil, time.Now().Add(time.Hour), false, nil)
	keyDER, _ := x509.MarshalECPrivateKey(a.key)
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	dir := t.TempDir()

	bundle := filepath.Join(dir, "bundle.pem")
	os.WriteFile(bundle, []byte(keyPEM+pemOf(a.der)+"junk\n"+pemOf(b.der)), 0o600)
	r, err := (&Service{}).InspectFile(bundle)
	if err != nil || len(r.Certificates) != 2 || r.Certificates[1].Subject != "CN=b.example" || r.Source != bundle {
		t.Fatalf("bundle: %+v %v", r, err)
	}
	if len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "private key") {
		t.Fatalf("notes: %v", r.Notes)
	}

	der := filepath.Join(dir, "a.der")
	os.WriteFile(der, a.der, 0o600)
	if r, err := (&Service{}).InspectFile(der); err != nil || len(r.Certificates) != 1 {
		t.Fatalf("der: %+v %v", r, err)
	}

	keyOnly := filepath.Join(dir, "key.pem")
	os.WriteFile(keyOnly, []byte(keyPEM), 0o600)
	if _, err := (&Service{}).InspectFile(keyOnly); err == nil || !strings.Contains(err.Error(), "no certificates") {
		t.Fatalf("key only: %v", err)
	}

	// From a terminal selection, indented as in YAML.
	sel := strings.ReplaceAll(pemOf(a.der), "\n", "\n    ")
	if r, err := (&Service{}).InspectText("    " + sel); err != nil || r.Certificates[0].Subject != "CN=a.example" {
		t.Fatalf("text: %+v %v", r, err)
	}
}
