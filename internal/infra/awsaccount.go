package infra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Profiles with static keys or a credential_process don't say which account
// they belong to; only AWS can tell. The lookup runs the aws CLI in the
// background, so it resolves credentials exactly as the user's own `aws`
// commands do, and the answer is kept until the AWS config or credentials
// file changes. Resolve never waits for it: it reports the lookup as
// pending and calls Service.changed once the answer is in.

const (
	lookupTimeout = 15 * time.Second
	// A failed lookup (offline, expired keys) is tried again after this.
	retryAfter = 2 * time.Minute

	// AccountPending is the account note while AWS is being asked.
	AccountPending = "asking AWS…"
)

var accountID = regexp.MustCompile(`^\d{12}$`)

// accountLookup asks AWS which account a profile's credentials belong to.
type accountLookup func(ctx context.Context, profile, region, configFile, credsFile string) (string, error)

type accountEntry struct {
	// The config and credentials files the answer was for.
	fingerprint string
	done        bool
	account     string
	err         string
	at          time.Time
}

// account returns the cached answer for a profile and starts a lookup when
// there is none. note is AccountPending, or why the lookup failed.
func (s *Service) account(profile, region, configFile, credsFile string) (account, note string) {
	key := strings.Join([]string{profile, region, configFile, credsFile}, "\x00")
	fp := fingerprint(configFile) + "|" + fingerprint(credsFile)

	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.accounts[key]
	if e != nil && e.fingerprint == fp {
		switch {
		case !e.done:
			return "", AccountPending
		case e.err == "":
			return e.account, ""
		case time.Since(e.at) < retryAfter:
			return "", e.err
		}
	}

	e = &accountEntry{fingerprint: fp}
	s.accounts[key] = e
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		defer cancel()
		acct, err := s.lookup(ctx, profile, region, configFile, credsFile)
		s.mu.Lock()
		e.done, e.at, e.account = true, time.Now(), acct
		if err != nil {
			e.account, e.err = "", err.Error()
		}
		s.mu.Unlock()
		s.changed()
	}()
	return "", AccountPending
}

// fingerprint changes whenever a file is edited, created or deleted.
func fingerprint(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d:%d", fi.ModTime().UnixNano(), fi.Size())
}

// stsLookup runs `aws sts get-caller-identity`, which needs no permissions.
func stsLookup(ctx context.Context, profile, region, configFile, credsFile string) (string, error) {
	bin := findAWS()
	if bin == "" {
		return "", errors.New("the aws CLI isn't installed")
	}
	if region == "" {
		region = "us-east-1" // any region answers; the CLI needs one
	}
	cmd := exec.CommandContext(ctx, bin, "sts", "get-caller-identity",
		"--profile", profile, "--region", region, "--query", "Account", "--output", "text")
	cmd.Env = awsEnv(configFile, credsFile)
	cmd.Stdin = nil // an MFA prompt fails instead of waiting forever
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", errors.New("AWS didn't answer in time")
	}
	if err != nil {
		return "", errors.New(lastLine(stderr.String(), "the aws CLI failed"))
	}
	acct := strings.TrimSpace(string(out))
	if !accountID.MatchString(acct) {
		return "", fmt.Errorf("unexpected answer from AWS: %.40q", acct)
	}
	return acct, nil
}

// awsEnv is wtf's environment without AWS settings that could override the
// profile, pointing the CLI at the files the shell uses.
func awsEnv(configFile, credsFile string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "AWS_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"AWS_CONFIG_FILE="+configFile,
		"AWS_SHARED_CREDENTIALS_FILE="+credsFile,
		"AWS_PAGER=",
		"AWS_CLI_AUTO_PROMPT=off",
	)
}

// findAWS looks on PATH and then where installers put the CLI: apps
// started from the Finder get a minimal PATH.
func findAWS() string {
	if p, err := exec.LookPath("aws"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		"/opt/homebrew/bin/aws",
		"/usr/local/bin/aws",
		filepath.Join(home, ".local", "bin", "aws"),
		"/usr/bin/aws",
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// lastLine is the last non-blank line of s, shortened, or fallback.
func lastLine(s, fallback string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if line == "" {
		return fallback
	}
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	return line
}
