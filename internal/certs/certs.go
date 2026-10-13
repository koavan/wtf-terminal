// Package certs reads X.509 certificates for the certificate inspector: the
// chain a TLS server presents, or the certificates in a file or in text
// copied from the terminal. Only facts are reported — dates, names, keys,
// and whether the chain verifies against the system's roots, with the
// error the TLS library gives when it doesn't.
package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Bigger files aren't certificates.
const maxFileSize = 1 << 20

const dialTimeout = 10 * time.Second

type Service struct {
	// Overridden by tests, which serve TLS with their own roots.
	roots *x509.CertPool
}

func NewService() *Service { return &Service{} }

// Cert is one certificate, as people read it.
type Cert struct {
	Subject string   `json:"subject"`
	Issuer  string   `json:"issuer"`
	Names   []string `json:"names"`
	// Unix milliseconds.
	NotBefore          int64  `json:"notBefore"`
	NotAfter           int64  `json:"notAfter"`
	Serial             string `json:"serial"`
	SHA256             string `json:"sha256"`
	Key                string `json:"key"`
	SignatureAlgorithm string `json:"signatureAlgorithm"`
	IsCA               bool   `json:"isCA"`
	SelfSigned         bool   `json:"selfSigned"`
}

// Report is what was found; the TLS fields are set for a server only.
type Report struct {
	// What was inspected: "example.com:443", a path, or "selected text".
	Source       string `json:"source"`
	Certificates []Cert `json:"certificates"`
	// Things left out, e.g. a private key in the same file.
	Notes []string `json:"notes"`

	Address  string `json:"address"`
	Protocol string `json:"protocol"`
	Cipher   string `json:"cipher"`
	ALPN     string `json:"alpn"`
	// Whether the chain verifies for the host name against the system's
	// roots, and why not.
	Verified    bool   `json:"verified"`
	VerifyError string `json:"verifyError"`
}

// Target splits what was clicked or typed — a URL, host or host:port — into
// the host and the address to dial (port 443 unless given).
func Target(s string) (host, addr string, err error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return "", "", fmt.Errorf("%q isn't a URL", s)
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"ldaps": "636", "smtps": "465", "imaps": "993"}[u.Scheme]
			if port == "" {
				port = "443"
			}
		}
		return u.Hostname(), net.JoinHostPort(u.Hostname(), port), nil
	}
	s = strings.TrimSuffix(s, "/")
	host, port, splitErr := net.SplitHostPort(s)
	if splitErr != nil {
		host, port = strings.Trim(s, "[]"), "443"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("%q isn't a port", port)
	}
	if host == "" || strings.ContainsAny(host, " /@?#") {
		return "", "", fmt.Errorf("%q isn't a host name", s)
	}
	return host, net.JoinHostPort(host, port), nil
}

// InspectHost connects to a TLS server and reports the chain it presents.
// It accepts any certificate so that broken ones can be seen, and checks
// separately whether the chain would be trusted.
func (s *Service) InspectHost(target string) (Report, error) {
	host, addr, err := Target(target)
	if err != nil {
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	cfg := &tls.Config{
		InsecureSkipVerify: true, // verified below, and reported
		NextProtos:         []string{"h2", "http/1.1"},
	}
	if net.ParseIP(host) == nil {
		cfg.ServerName = host
	}
	d := tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			return Report{}, fmt.Errorf("%s didn't answer within %s", addr, dialTimeout)
		}
		return Report{}, err
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()

	r := Report{
		Source:   addr,
		Address:  conn.RemoteAddr().String(),
		Protocol: tls.VersionName(state.Version),
		Cipher:   tls.CipherSuiteName(state.CipherSuite),
		ALPN:     state.NegotiatedProtocol,
	}
	for _, c := range state.PeerCertificates {
		r.Certificates = append(r.Certificates, describe(c))
	}
	if len(state.PeerCertificates) == 0 {
		r.VerifyError = "the server sent no certificate"
		return r, nil
	}
	r.Verified, r.VerifyError = s.verify(host, state.PeerCertificates)
	return r, nil
}

func (s *Service) verify(host string, chain []*x509.Certificate) (bool, string) {
	inter := x509.NewCertPool()
	for _, c := range chain[1:] {
		inter.AddCert(c)
	}
	opts := x509.VerifyOptions{Roots: s.roots, Intermediates: inter}
	if _, err := chain[0].Verify(opts); err != nil {
		return false, err.Error()
	}
	if err := chain[0].VerifyHostname(host); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// InspectFile reports the certificates in a PEM or DER file.
func (s *Service) InspectFile(path string) (Report, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Report{}, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
		return Report{}, errors.New("not a certificate file (too big, or not a regular file)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	r, err := parse(data)
	r.Source = path
	return r, err
}

// InspectText reports the PEM certificates in text, e.g. a selection,
// which may be indented (YAML) or padded with spaces (the terminal).
func (s *Service) InspectText(text string) (Report, error) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	r, err := parse([]byte(strings.Join(lines, "\n")))
	r.Source = "selected text"
	return r, err
}

func parse(data []byte) (Report, error) {
	var r Report
	keys, other := 0, map[string]int{}
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch {
		case block.Type == "CERTIFICATE" || block.Type == "TRUSTED CERTIFICATE":
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				r.Notes = append(r.Notes, "A certificate couldn't be read: "+err.Error())
				continue
			}
			r.Certificates = append(r.Certificates, describe(c))
		case strings.Contains(block.Type, "PRIVATE KEY"):
			keys++
		default:
			other[block.Type]++
		}
		clear(block.Bytes)
	}
	if keys > 0 {
		r.Notes = append(r.Notes, "It also holds a private key, which isn't shown.")
	}
	for t := range other {
		r.Notes = append(r.Notes, fmt.Sprintf("Skipped: %s.", strings.ToLower(t)))
	}
	if len(r.Certificates) == 0 && keys == 0 && len(other) == 0 {
		// No PEM at all: perhaps DER.
		if certs, err := x509.ParseCertificates(data); err == nil {
			for _, c := range certs {
				r.Certificates = append(r.Certificates, describe(c))
			}
		}
	}
	if len(r.Certificates) == 0 {
		return r, errors.New("no certificates found")
	}
	return r, nil
}

func describe(c *x509.Certificate) Cert {
	sum := sha256.Sum256(c.Raw)
	out := Cert{
		Subject:            c.Subject.String(),
		Issuer:             c.Issuer.String(),
		NotBefore:          c.NotBefore.UnixMilli(),
		NotAfter:           c.NotAfter.UnixMilli(),
		Serial:             colonHex(c.SerialNumber.Bytes()),
		SHA256:             colonHex(sum[:]),
		Key:                keyName(c.PublicKey),
		SignatureAlgorithm: c.SignatureAlgorithm.String(),
		IsCA:               c.IsCA,
		SelfSigned:         c.Subject.String() == c.Issuer.String() && c.CheckSignatureFrom(c) == nil,
	}
	out.Names = append(out.Names, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		out.Names = append(out.Names, ip.String())
	}
	out.Names = append(out.Names, c.EmailAddresses...)
	for _, u := range c.URIs {
		out.Names = append(out.Names, u.String())
	}
	return out
}

func keyName(k any) string {
	switch k := k.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return fmt.Sprintf("%T", k)
}

func colonHex(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, ":")
}
