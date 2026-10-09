package awsprofiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
)

// Asking AWS about a profile, and rotating its access key. Everything goes
// through the aws CLI, so credentials resolve exactly as in the user's own
// commands; AWS_* variables in wtf's environment are dropped so they can't
// override the profile (see infra.AWSEnv).

// Identity is who a profile's credentials are (sts get-caller-identity).
type Identity struct {
	Account string `json:"account"`
	Arn     string `json:"arn"`
	UserID  string `json:"userId"`
}

// AccessKey is one of an IAM user's access keys.
type AccessKey struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // Active, Inactive
	Created string `json:"created"`
	// When it was last used and for what; "" if never.
	LastUsed        string `json:"lastUsed"`
	LastUsedService string `json:"lastUsedService"`
	// The key this profile uses.
	Current bool `json:"current"`
}

// RotateResult reports what a rotation did, step by step.
type RotateResult struct {
	NewKeyID string   `json:"newKeyId"`
	Steps    []string `json:"steps"`
	List     List     `json:"list"`
}

const awsTimeout = 30 * time.Second

// A new key takes a few seconds before AWS accepts it. Variables for tests.
var (
	newKeyWait = 30 * time.Second
	newKeyPoll = 3 * time.Second
)

// runAWS runs the aws CLI and returns its stdout; tests replace it.
// extraEnv is added after the cleaned environment (e.g. explicit keys).
var runAWS = func(ctx context.Context, configFile, credsFile string, extraEnv []string, args ...string) ([]byte, error) {
	bin := infra.FindAWS()
	if bin == "" {
		return nil, errors.New("the aws CLI isn't installed")
	}
	cmd := exec.CommandContext(ctx, bin, append(args, "--output", "json")...)
	cmd.Env = append(infra.AWSEnv(configFile, credsFile), extraEnv...)
	cmd.Stdin = nil // an MFA prompt fails instead of waiting
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, errors.New("AWS didn't answer in time")
	}
	if err != nil {
		return nil, errors.New(lastLine(stderr.String(), "the aws CLI failed"))
	}
	return out, nil
}

func lastLine(s, fallback string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if line == "" {
		return fallback
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}

func (s *Service) aws(ctx context.Context, profile string, args ...string) ([]byte, error) {
	return runAWS(ctx, s.config, s.credentials, nil, append(args, "--profile", profile)...)
}

// withKeys runs the CLI with explicit keys, ignoring every profile.
func (s *Service) withKeys(ctx context.Context, id, secret string, args ...string) ([]byte, error) {
	env := []string{"AWS_ACCESS_KEY_ID=" + id, "AWS_SECRET_ACCESS_KEY=" + secret, "AWS_REGION=us-east-1"}
	return runAWS(ctx, "/dev/null", "/dev/null", env, args...)
}

func decodeIdentity(out []byte) (Identity, error) {
	var r struct{ Account, Arn, UserId string }
	if err := json.Unmarshal(out, &r); err != nil || r.Arn == "" {
		return Identity{}, fmt.Errorf("unexpected answer from AWS: %.60q", out)
	}
	return Identity{Account: r.Account, Arn: r.Arn, UserID: r.UserId}, nil
}

// Verify asks AWS who a profile's credentials belong to. It needs no
// permissions, so it only fails for bad or expired credentials.
func (s *Service) Verify(profile string) (Identity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
	defer cancel()
	out, err := s.aws(ctx, profile, "sts", "get-caller-identity", "--region", s.regionFor(profile))
	if err != nil {
		return Identity{}, err
	}
	return decodeIdentity(out)
}

// regionFor is the profile's region, or any region: STS and IAM answer
// everywhere, but the CLI wants one.
func (s *Service) regionFor(profile string) string {
	for _, p := range s.List().Profiles {
		if p.Name == profile && p.Region != "" {
			return p.Region
		}
	}
	return "us-east-1"
}

// AccessKeys lists the access keys of the IAM user a profile's keys
// belong to, with their age and last use. It needs iam:ListAccessKeys
// and iam:GetAccessKeyLastUsed on that user.
func (s *Service) AccessKeys(profile string) ([]AccessKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
	defer cancel()
	keys, err := s.listKeys(ctx, profile)
	if err != nil {
		return nil, err
	}
	current := s.currentKey(profile)
	for i := range keys {
		keys[i].Current = keys[i].ID == current
		out, err := s.aws(ctx, profile, "iam", "get-access-key-last-used", "--access-key-id", keys[i].ID)
		if err != nil {
			continue // the list is still useful without it
		}
		var r struct {
			AccessKeyLastUsed struct{ LastUsedDate, ServiceName string }
		}
		if json.Unmarshal(out, &r) == nil {
			keys[i].LastUsed = r.AccessKeyLastUsed.LastUsedDate
			if r.AccessKeyLastUsed.ServiceName != "N/A" {
				keys[i].LastUsedService = r.AccessKeyLastUsed.ServiceName
			}
		}
	}
	return keys, nil
}

func (s *Service) listKeys(ctx context.Context, profile string) ([]AccessKey, error) {
	out, err := s.aws(ctx, profile, "iam", "list-access-keys")
	if err != nil {
		return nil, err
	}
	var r struct {
		AccessKeyMetadata []struct{ AccessKeyId, Status, CreateDate string }
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("unexpected answer from AWS: %.60q", out)
	}
	keys := []AccessKey{}
	for _, k := range r.AccessKeyMetadata {
		keys = append(keys, AccessKey{ID: k.AccessKeyId, Status: k.Status, Created: k.CreateDate})
	}
	return keys, nil
}

// currentKey is the access key ID in a profile's credentials section.
func (s *Service) currentKey(profile string) string {
	f, _ := s.read()
	if sec := f.creds.find(profile); sec != nil {
		if k := sec.get(keyID); k != nil {
			return k.value
		}
	}
	return ""
}

// ── Rotation ─────────────────────────────────────────────────────────────

type rotation struct {
	oldID, oldSecret string
	identity         Identity
	keys             []AccessKey
}

// prepareRotation checks a profile can be rotated: its own long-term keys,
// belonging to an IAM user who has room for a second key.
func (s *Service) prepareRotation(ctx context.Context, profile string) (rotation, error) {
	var r rotation
	f, _ := s.read()
	sec := f.creds.find(profile)
	if sec == nil || sec.get(keyID) == nil || sec.get(keySecret) == nil {
		return r, errors.New("rotation is for profiles with an access key and secret in the credentials file")
	}
	if sec.get(keyToken) != nil || sec.get(keyTokenOld) != nil {
		return r, errors.New("this profile has temporary credentials (a session token); they can't be rotated, only replaced")
	}
	r.oldID, r.oldSecret = sec.get(keyID).value, sec.get(keySecret).value
	if !strings.HasPrefix(r.oldID, "AKIA") {
		return r, fmt.Errorf("%s isn't a long-term IAM user key (AKIA…)", r.oldID)
	}
	out, err := s.withKeys(ctx, r.oldID, r.oldSecret, "sts", "get-caller-identity")
	if err != nil {
		return r, fmt.Errorf("the current key doesn't work: %v", err)
	}
	if r.identity, err = decodeIdentity(out); err != nil {
		return r, err
	}
	if !strings.Contains(r.identity.Arn, ":user/") {
		return r, fmt.Errorf("these keys belong to %s, not an IAM user", r.identity.Arn)
	}
	out, err = s.withKeys(ctx, r.oldID, r.oldSecret, "iam", "list-access-keys")
	if err != nil {
		return r, fmt.Errorf("can't list the user's keys (iam:ListAccessKeys): %v", err)
	}
	var lk struct {
		AccessKeyMetadata []struct{ AccessKeyId, Status, CreateDate string }
	}
	if json.Unmarshal(out, &lk) != nil {
		return r, fmt.Errorf("unexpected answer from AWS: %.60q", out)
	}
	for _, k := range lk.AccessKeyMetadata {
		r.keys = append(r.keys, AccessKey{ID: k.AccessKeyId, Status: k.Status, Created: k.CreateDate, Current: k.AccessKeyId == r.oldID})
	}
	if len(r.keys) >= 2 {
		var other string
		for _, k := range r.keys {
			if k.ID != r.oldID {
				other = k.ID + " (" + strings.ToLower(k.Status) + ")"
			}
		}
		return r, fmt.Errorf("the user already has two access keys, AWS's limit; delete the other one, %s, first", other)
	}
	return r, nil
}

// PlanRotate checks a profile can be rotated and lists the steps.
func (s *Service) PlanRotate(profile string, deleteOld bool) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
	defer cancel()
	r, err := s.prepareRotation(ctx, profile)
	if err != nil {
		return nil, err
	}
	user := r.identity.Arn[strings.LastIndex(r.identity.Arn, "/")+1:]
	steps := []string{
		fmt.Sprintf("Create a new access key for IAM user %s (account %s)", user, r.identity.Account),
		fmt.Sprintf("Write it to [%s] in %s, replacing %s", profile, prettyHome(s.home, s.credentials), r.oldID),
		"Check that AWS accepts the new key",
		fmt.Sprintf("Deactivate the old key %s", r.oldID),
	}
	if deleteOld {
		steps = append(steps, fmt.Sprintf("Delete the old key %s", r.oldID))
	}
	steps = append(steps, "Anything else using the old key (scripts, CI, other machines) stops working")
	return steps, nil
}

// Rotate replaces a profile's access key with a new one. If anything
// fails before the old key is deactivated, everything is put back: the
// file gets the old key again and the new key is deleted.
func (s *Service) Rotate(profile string, deleteOld bool, versions map[string]string) (RotateResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*awsTimeout+newKeyWait)
	defer cancel()
	var res RotateResult
	step := func(format string, args ...any) { res.Steps = append(res.Steps, fmt.Sprintf(format, args...)) }

	// The file must be what the user saw when they confirmed.
	if _, err := s.begin(versions); err != nil {
		return res, err
	}
	r, err := s.prepareRotation(ctx, profile)
	if err != nil {
		return res, err
	}

	out, err := s.withKeys(ctx, r.oldID, r.oldSecret, "iam", "create-access-key")
	if err != nil {
		return res, fmt.Errorf("couldn't create a new key (iam:CreateAccessKey): %v", err)
	}
	var created struct {
		AccessKey struct{ AccessKeyId, SecretAccessKey string }
	}
	if json.Unmarshal(out, &created) != nil || created.AccessKey.AccessKeyId == "" || created.AccessKey.SecretAccessKey == "" {
		return res, errors.New("AWS's answer to create-access-key couldn't be read; check the user's keys in the console")
	}
	newID, newSecret := created.AccessKey.AccessKeyId, created.AccessKey.SecretAccessKey
	res.NewKeyID = newID
	step("Created %s", newID)

	undoNewKey := func(cause error) (RotateResult, error) {
		if _, err := s.withKeys(context.Background(), r.oldID, r.oldSecret, "iam", "delete-access-key", "--access-key-id", newID); err != nil {
			return res, fmt.Errorf("%v — and deleting the new key %s failed too (%v); delete it in the console", cause, newID, err)
		}
		step("Deleted %s again; nothing changed", newID)
		return res, cause
	}

	c, err := s.begin(versions)
	if err == nil {
		d := Draft{Original: profile, Name: profile, Versions: versions, UseKeys: true, AccessKeyID: newID, SecretAccessKey: newSecret}
		d.Settings = s.settingsOf(c, profile)
		if err = c.applySave(d); err == nil {
			_, err = s.commit(c)
		}
	}
	if err != nil {
		return undoNewKey(fmt.Errorf("couldn't write the new key: %v", err))
	}
	step("Wrote %s to [%s]", newID, profile)

	deadline := time.Now().Add(newKeyWait)
	for {
		_, err = s.withKeys(ctx, newID, newSecret, "sts", "get-caller-identity")
		if err == nil || time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(newKeyPoll)
	}
	if err != nil {
		// Put the old key back, then remove the new one.
		if rerr := s.restoreKey(profile, r.oldID, r.oldSecret); rerr != nil {
			return res, fmt.Errorf("AWS didn't accept the new key (%v), and restoring the old one failed (%v); the old key %s is still active — restore it from wtf's aws-backups", err, rerr, r.oldID)
		}
		step("Put %s back in [%s]", r.oldID, profile)
		return undoNewKey(fmt.Errorf("AWS didn't accept the new key: %v", err))
	}
	step("AWS accepts %s", newID)

	if _, err := s.withKeys(ctx, newID, newSecret, "iam", "update-access-key", "--access-key-id", r.oldID, "--status", "Inactive"); err != nil {
		res.List = s.List()
		return res, fmt.Errorf("the profile now uses %s, but deactivating the old key %s failed: %v", newID, r.oldID, err)
	}
	step("Deactivated %s", r.oldID)
	if deleteOld {
		if _, err := s.withKeys(ctx, newID, newSecret, "iam", "delete-access-key", "--access-key-id", r.oldID); err != nil {
			res.List = s.List()
			return res, fmt.Errorf("the old key %s is deactivated, but deleting it failed: %v", r.oldID, err)
		}
		step("Deleted %s", r.oldID)
	}
	res.List = s.List()
	return res, nil
}

// settingsOf is a profile's current config settings, to keep them as they are.
func (s *Service) settingsOf(c *change, profile string) []Setting {
	var out []Setting
	if sec := c.f.config.find(configSection(profile)); sec != nil {
		for _, k := range sec.keys {
			if !k.nested {
				out = append(out, Setting{Key: keyName(k), Value: k.value})
			}
		}
	}
	return out
}

// restoreKey writes a key back into a profile, whatever the file holds now.
func (s *Service) restoreKey(profile, id, secret string) error {
	f, problems := s.read()
	if len(problems) > 0 {
		return errors.New(problems[0])
	}
	c := &change{s: s, f: f, touched: map[string]bool{}}
	d := Draft{Original: profile, Name: profile, UseKeys: true, AccessKeyID: id, SecretAccessKey: secret, Settings: s.settingsOf(c, profile)}
	if err := c.applySave(d); err != nil {
		return err
	}
	_, err := s.commit(c)
	return err
}
