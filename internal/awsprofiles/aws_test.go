package awsprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeAWS stands in for the aws CLI: an IAM user with access keys.
type fakeAWS struct {
	arn  string
	keys map[string]*fakeKey // id → key
	// Keys AWS won't accept for this many calls after creation.
	lag   int
	calls []string
	next  int
}

type fakeKey struct {
	secret, status string
	unusable       int
}

func (f *fakeAWS) install(t *testing.T) {
	old, oldWait, oldPoll := runAWS, newKeyWait, newKeyPoll
	newKeyWait, newKeyPoll = 200*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { runAWS, newKeyWait, newKeyPoll = old, oldWait, oldPoll })
	runAWS = func(_ context.Context, configFile, credsFile string, env []string, args ...string) ([]byte, error) {
		id, secret := "", ""
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "AWS_ACCESS_KEY_ID="); ok {
				id = v
			}
			if v, ok := strings.CutPrefix(e, "AWS_SECRET_ACCESS_KEY="); ok {
				secret = v
			}
		}
		f.calls = append(f.calls, strings.Join(args[:2], " "))
		if id == "" {
			return nil, errors.New("profile calls aren't used by rotation")
		}
		k := f.keys[id]
		if k == nil || k.secret != secret || k.status != "Active" {
			return nil, errors.New("An error occurred (InvalidClientTokenId): The security token included in the request is invalid.")
		}
		if k.unusable > 0 {
			k.unusable--
			return nil, errors.New("An error occurred (InvalidClientTokenId): not yet")
		}
		arg := func(name string) string { i := slices.Index(args, name); return args[i+1] }
		switch strings.Join(args[:2], " ") {
		case "sts get-caller-identity":
			return []byte(fmt.Sprintf(`{"Account":"123456789012","Arn":%q,"UserId":"AIDA1"}`, f.arn)), nil
		case "iam list-access-keys":
			var meta []map[string]string
			for id, k := range f.keys {
				meta = append(meta, map[string]string{"AccessKeyId": id, "Status": k.status, "CreateDate": "2024-01-01T00:00:00Z"})
			}
			return json.Marshal(map[string]any{"AccessKeyMetadata": meta})
		case "iam create-access-key":
			f.next++
			nid, nsecret := fmt.Sprintf("AKIANEW%013d", f.next), fmt.Sprintf("newSecretKeyThatIsLongEnough%03d", f.next)
			f.keys[nid] = &fakeKey{secret: nsecret, status: "Active", unusable: f.lag}
			return []byte(fmt.Sprintf(`{"AccessKey":{"AccessKeyId":%q,"SecretAccessKey":%q}}`, nid, nsecret)), nil
		case "iam update-access-key":
			f.keys[arg("--access-key-id")].status = arg("--status")
			return []byte("{}"), nil
		case "iam delete-access-key":
			delete(f.keys, arg("--access-key-id"))
			return []byte("{}"), nil
		}
		return nil, fmt.Errorf("unexpected call %v", args)
	}
}

const rotCreds = "[ci]\naws_access_key_id = AKIAOLD0000000000001\naws_secret_access_key = " + secretA + "\n"

func newFake() *fakeAWS {
	return &fakeAWS{arn: "arn:aws:iam::123456789012:user/ci-bot", keys: map[string]*fakeKey{
		"AKIAOLD0000000000001": {secret: secretA, status: "Active"},
	}}
}

func TestRotate(t *testing.T) {
	s := testService(t, rotCreds, "[profile ci]\nregion = eu-west-1\n")
	f := newFake()
	f.lag = 2 // AWS takes a moment to accept the new key
	f.install(t)

	plan, err := s.PlanRotate("ci", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan[0], "IAM user ci-bot (account 123456789012)") || !strings.Contains(plan[4], "Delete the old key AKIAOLD0000000000001") {
		t.Errorf("plan %v", plan)
	}

	res, err := s.Rotate("ci", true, s.List().Versions)
	if err != nil {
		t.Fatalf("%v (steps %v)", err, res.Steps)
	}
	newID := "AKIANEW0000000000001"
	if res.NewKeyID != newID {
		t.Errorf("new key %s", res.NewKeyID)
	}
	if got := read(t, s.credentials); got != "[ci]\naws_access_key_id = "+newID+"\naws_secret_access_key = newSecretKeyThatIsLongEnough001\n" {
		t.Errorf("credentials:\n%s", got)
	}
	if read(t, s.config) != "[profile ci]\nregion = eu-west-1\n" {
		t.Error("config must stay as it was")
	}
	if len(f.keys) != 1 || f.keys[newID] == nil {
		t.Errorf("AWS keys after rotation: %v", f.keys)
	}
	if p := profile(res.List, "ci"); p.AccessKeyID != newID {
		t.Errorf("returned list %+v", p)
	}
	if !strings.Contains(strings.Join(res.Steps, "\n"), "Deleted AKIAOLD0000000000001") {
		t.Errorf("steps %v", res.Steps)
	}
}

func TestRotateKeepsOldKeyInactive(t *testing.T) {
	s := testService(t, rotCreds, "")
	f := newFake()
	f.install(t)
	if _, err := s.Rotate("ci", false, s.List().Versions); err != nil {
		t.Fatal(err)
	}
	if k := f.keys["AKIAOLD0000000000001"]; k == nil || k.status != "Inactive" {
		t.Errorf("old key %+v", k)
	}
}

func TestRotateRollsBack(t *testing.T) {
	s := testService(t, rotCreds, "")
	f := newFake()
	f.lag = 1000 // AWS never accepts the new key
	f.install(t)
	res, err := s.Rotate("ci", true, s.List().Versions)
	if err == nil || !strings.Contains(err.Error(), "didn't accept the new key") {
		t.Fatalf("err %v", err)
	}
	if read(t, s.credentials) != rotCreds {
		t.Errorf("credentials should be back to the old key:\n%s", read(t, s.credentials))
	}
	if len(f.keys) != 1 || f.keys["AKIAOLD0000000000001"].status != "Active" {
		t.Errorf("AWS should be as before: %v", f.keys)
	}
	if !strings.Contains(strings.Join(res.Steps, "\n"), "nothing changed") {
		t.Errorf("steps %v", res.Steps)
	}
}

func TestRotateRefuses(t *testing.T) {
	cases := []struct {
		name, creds string
		setup       func(*fakeAWS)
		want        string
	}{
		{"two keys", rotCreds, func(f *fakeAWS) { f.keys["AKIAOTHER00000000001"] = &fakeKey{status: "Inactive"} }, "already has two access keys"},
		{"role", rotCreds, func(f *fakeAWS) { f.arn = "arn:aws:sts::123456789012:assumed-role/X/y" }, "not an IAM user"},
		{"token", rotCreds + "aws_session_token = abc\n", nil, "temporary credentials"},
		{"bad key", rotCreds, func(f *fakeAWS) { f.keys["AKIAOLD0000000000001"].secret = "other" }, "current key doesn't work"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testService(t, c.creds, "")
			f := newFake()
			if c.setup != nil {
				c.setup(f)
			}
			f.install(t)
			if _, err := s.PlanRotate("ci", true); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v, want %q", err, c.want)
			}
			if slices.Contains(f.calls, "iam create-access-key") {
				t.Error("nothing may be created")
			}
		})
	}
}
