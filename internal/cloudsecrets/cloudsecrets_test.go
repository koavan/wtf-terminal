package cloudsecrets

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dayanraj/wtf/internal/infra"
)

// fakeCLI stands in for aws and vault: answers by the first two
// arguments, and records each call with its stdin. Nothing runs.
type fakeCLI struct {
	answers map[string]string
	// Calls that fail, with this stderr.
	fail  map[string]string
	calls []call
}

type call struct {
	bin   string
	args  []string
	env   []string
	stdin string
}

func (f *fakeCLI) install(t *testing.T) string {
	t.Helper()
	old := runCLI
	t.Cleanup(func() { runCLI = old })
	runCLI = func(_ context.Context, bin string, env []string, stdin []byte, args ...string) ([]byte, error) {
		f.calls = append(f.calls, call{filepath.Base(bin), args, env, string(stdin)})
		key := args[0] + " " + args[1]
		if e, ok := f.fail[key]; ok {
			return nil, &cliError{stderr: e, err: os.ErrInvalid}
		}
		if a, ok := f.answers[key]; ok {
			return []byte(a), nil
		}
		return nil, &cliError{stderr: "unexpected " + strings.Join(args, " "), err: os.ErrInvalid}
	}
	// aws and vault the lookup finds on the "shell's" PATH.
	dir := t.TempDir()
	for _, name := range []string{"aws", "vault"} {
		os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 99\n"), 0o755)
	}
	return dir
}

func testService(t *testing.T, profile, region string) *Service {
	return &Service{home: t.TempDir(), resolve: func(map[string]string) infra.Context {
		return infra.Context{AWSProfile: profile, AWSAccount: "123456789012", AWSRegion: region}
	}}
}

func req(dir string) Request {
	return Request{Env: map[string]string{"PATH": dir, "VAULT_ADDR": "https://vault.example:8200", "AWS_PROFILE": "dev"}}
}

func has(args []string, want ...string) bool {
	return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(want, " ")+" ")
}

func TestTarget(t *testing.T) {
	r := Request{Env: map[string]string{"VAULT_ADDR": "https://v", "VAULT_NAMESPACE": "team"}}
	if got := testService(t, "dev", "eu-west-1").Target(r); got.AWSError != "" || got.Region != "eu-west-1" || got.VaultAddr != "https://v" || got.VaultNamespace != "team" {
		t.Fatalf("%+v", got)
	}
	r.Region = "us-east-2"
	if got := testService(t, "dev", "").Target(r); got.AWSError != "" || got.Region != "us-east-2" {
		t.Fatalf("picked region: %+v", got)
	}
	for _, c := range []struct{ profile, region, want string }{
		{infra.EnvCredentials, "us-east-1", "environment variables"},
		{"", "us-east-1", "no AWS profile"},
		{"dev", "", "No region"},
	} {
		if got := testService(t, c.profile, c.region).Target(Request{}); !strings.Contains(got.AWSError, c.want) {
			t.Errorf("%q/%q: %q", c.profile, c.region, got.AWSError)
		}
	}
}

func TestListsHoldNoValues(t *testing.T) {
	f := &fakeCLI{answers: map[string]string{
		"secretsmanager list-secrets": `{"SecretList":[{"Name":"prod/db","Description":"db","LastChangedDate":"2026-11-01T00:00:00+00:00","RotationEnabled":true},{"Name":"a"}]}`,
		"ssm describe-parameters":     `{"Parameters":[{"Name":"/app/key","Type":"SecureString","Version":4,"LastModifiedDate":"2026-10-01T00:00:00+00:00"}]}`,
	}}
	dir := f.install(t)
	s := testService(t, "dev", "eu-west-1")
	secrets, err := s.ListSecrets(req(dir))
	if err != nil || len(secrets) != 2 || secrets[0].Name != "a" || !secrets[1].Rotation {
		t.Fatalf("%+v %v", secrets, err)
	}
	params, err := s.ListParameters(req(dir))
	if err != nil || len(params) != 1 || params[0].Type != "SecureString" || params[0].Version != "4" {
		t.Fatalf("%+v %v", params, err)
	}
	c := f.calls[0]
	if c.bin != "aws" || !has(c.args, "--profile", "dev", "--region", "eu-west-1", "--output", "json") {
		t.Fatalf("args: %q", c.args)
	}
	// The shell's PATH; wtf's own AWS_* variables never reach the CLI.
	var path string
	for _, kv := range c.env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
		if strings.HasPrefix(kv, "AWS_ACCESS_KEY_ID=") {
			t.Fatal("wtf's own AWS keys reached the CLI")
		}
	}
	if path != dir {
		t.Fatalf("PATH %q", path)
	}
}

func TestReadValues(t *testing.T) {
	f := &fakeCLI{answers: map[string]string{
		"secretsmanager get-secret-value": `{"Name":"prod/db","VersionId":"v-1","SecretString":"{\"user\":\"app\",\"password\":\"s3cr3t\",\"port\":5432}"}`,
		"ssm get-parameter":               `{"Parameter":{"Name":"/app/key","Type":"SecureString","Value":"abc","Version":4}}`,
	}}
	dir := f.install(t)
	s := testService(t, "dev", "eu-west-1")
	v, err := s.ReadSecret(req(dir), "prod/db")
	want := []Field{{"password", "s3cr3t"}, {"port", "5432"}, {"user", "app"}}
	if err != nil || v.Version != "v-1" || !reflect.DeepEqual(v.Fields, want) {
		t.Fatalf("%+v %v", v, err)
	}
	p, err := s.ReadParameter(req(dir), "/app/key")
	if err != nil || p.Text != "abc" || p.Version != "4" || len(p.Fields) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	if !has(f.calls[1].args, "--with-decryption") {
		t.Fatalf("args %q", f.calls[1].args)
	}
	if _, err := s.ReadSecret(req(dir), "--debug"); err == nil {
		t.Fatal("an option as a name was accepted")
	}
}

func TestVault(t *testing.T) {
	f := &fakeCLI{answers: map[string]string{
		"secrets list": `{"secret/":{"type":"kv","options":{"version":"2"}},"old/":{"type":"kv","options":null},"sys/":{"type":"system"}}`,
		"kv list":      `["b","a/","c"]`,
		"kv get":       `{"data":{"data":{"password":"pw","n":1},"metadata":{"version":3,"created_time":"2026-11-01T00:00:00Z"}}}`,
	}}
	dir := f.install(t)
	s := testService(t, "", "")
	r := req(dir)
	r.Env["VAULT_TOKEN"] = "never-used"
	mounts, err := s.VaultMounts(r)
	if err != nil || !reflect.DeepEqual(mounts, []Mount{{Path: "old/", KV: 1}, {Path: "secret/", KV: 2}}) {
		t.Fatalf("%+v %v", mounts, err)
	}
	items, err := s.VaultList(r, "secret/app")
	if err != nil || len(items) != 3 || !items[0].Folder || items[1].Name != "b" {
		t.Fatalf("%+v %v", items, err)
	}
	v, err := s.VaultRead(r, "secret/app/db", 2)
	if err != nil || v.Version != "3" || !reflect.DeepEqual(v.Fields, []Field{{"n", "1"}, {"password", "pw"}}) {
		t.Fatalf("%+v %v", v, err)
	}
	for _, c := range f.calls {
		for _, kv := range c.env {
			if strings.HasPrefix(kv, "VAULT_TOKEN=") {
				t.Fatal("VAULT_TOKEN reached the CLI")
			}
		}
		if !slicesContain(c.env, "VAULT_ADDR=https://vault.example:8200") {
			t.Fatal("VAULT_ADDR missing")
		}
	}
	f.fail = map[string]string{"kv list": "No value found at secret/metadata/none\n"}
	if items, err := s.VaultList(r, "secret/none"); err != nil || len(items) != 0 {
		t.Fatalf("%+v %v", items, err)
	}
	f.fail = map[string]string{"kv get": "Error making API request.\n\nURL: GET https://v/v1/secret/data/x\nCode: 403. Errors:\n\n* 1 error occurred:\n\t* permission denied\n\n"}
	if _, err := s.VaultRead(r, "secret/x", 2); err == nil || err.Error() != "permission denied (403)" {
		t.Fatalf("%v", err)
	}
}

func slicesContain(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestSecretsManagerEdit(t *testing.T) {
	f := &fakeCLI{answers: map[string]string{
		"secretsmanager get-secret-value": `{"Name":"prod/db","VersionId":"v-1","SecretString":"{\"user\":\"app\",\"password\":\"old\"}"}`,
		"secretsmanager put-secret-value": `{}`,
	}}
	dir := f.install(t)
	s := testService(t, "dev", "eu-west-1")
	c := Change{Store: SecretsManager, Name: "prod/db", Value: `{"user":"app","password":"new","host":"db"}`, Version: "v-1"}
	steps, err := s.PlanChange(req(dir), c)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(steps, "\n")
	for _, want := range []string{"new version of secret prod/db", "Account 123456789012 · profile dev · eu-west-1", "Keys: changes password · adds host (values not shown)"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan lacks %q:\n%s", want, plan)
		}
	}
	if strings.Contains(plan, "new") && strings.Contains(plan, `"new"`) {
		t.Fatal("the plan shows a value")
	}
	if err := s.ApplyChange(req(dir), c); err != nil {
		t.Fatal(err)
	}
	put := f.calls[len(f.calls)-1]
	if !has(put.args, "put-secret-value", "--cli-input-json", "file:///dev/stdin") {
		t.Fatalf("args %q", put.args)
	}
	for _, a := range put.args {
		if strings.Contains(a, "new") {
			t.Fatal("the value is on the command line")
		}
	}
	var in map[string]string
	if json.Unmarshal([]byte(put.stdin), &in) != nil || in["SecretId"] != "prod/db" || in["SecretString"] != c.Value {
		t.Fatalf("stdin %q", put.stdin)
	}

	// Someone else changed it: refuse.
	c.Version = "v-0"
	if _, err := s.PlanChange(req(dir), c); err == nil || !strings.Contains(err.Error(), "changed since you opened it") {
		t.Fatalf("%v", err)
	}
	// The same value: nothing to do.
	c.Version, c.Value = "v-1", `{"password":"old","user":"app"}`
	if _, err := s.PlanChange(req(dir), c); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("%v", err)
	}
}

func TestCreate(t *testing.T) {
	f := &fakeCLI{
		answers: map[string]string{"secretsmanager create-secret": `{}`, "ssm describe-parameters": `{"Parameters":[]}`, "ssm put-parameter": `{}`},
		fail:    map[string]string{"secretsmanager get-secret-value": "An error occurred (ResourceNotFoundException) when calling the GetSecretValue operation: no"},
	}
	dir := f.install(t)
	s := testService(t, "dev", "eu-west-1")
	c := Change{Store: SecretsManager, Name: "new/one", Create: true, Value: "token-xyz", Description: "CI token"}
	steps, err := s.PlanChange(req(dir), c)
	if err != nil || !strings.Contains(strings.Join(steps, "\n"), "Value: 9 characters (not shown)") {
		t.Fatalf("%q %v", steps, err)
	}
	if err := s.ApplyChange(req(dir), c); err != nil {
		t.Fatal(err)
	}
	p := Change{Store: ParameterStore, Name: "/app/new", Create: true, Type: "SecureString", Value: "v"}
	if err := s.ApplyChange(req(dir), p); err != nil {
		t.Fatal(err)
	}
	var in map[string]any
	json.Unmarshal([]byte(f.calls[len(f.calls)-1].stdin), &in)
	if in["Type"] != "SecureString" || in["Overwrite"] != nil {
		t.Fatalf("%v", in)
	}
	p.Type = "Bogus"
	if _, err := s.PlanChange(req(dir), p); err == nil {
		t.Fatal("bad type accepted")
	}
	// It exists already.
	f.answers["ssm describe-parameters"] = `{"Parameters":[{"Name":"/app/new","Type":"String","Version":1}]}`
	p.Type = "String"
	if _, err := s.PlanChange(req(dir), p); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("%v", err)
	}
}

func TestParameterOverwriteKeepsKeyAndTier(t *testing.T) {
	f := &fakeCLI{answers: map[string]string{
		"ssm describe-parameters": `{"Parameters":[{"Name":"/app/key","Type":"SecureString","KeyId":"alias/app","Tier":"Advanced","Version":4}]}`,
		"ssm get-parameter":       `{"Parameter":{"Name":"/app/key","Type":"SecureString","Value":"abc","Version":4}}`,
		"ssm put-parameter":       `{"Version":5}`,
	}}
	dir := f.install(t)
	s := testService(t, "dev", "eu-west-1")
	c := Change{Store: ParameterStore, Name: "/app/key", Value: "abcdef", Version: "4"}
	steps, err := s.PlanChange(req(dir), c)
	if err != nil || !strings.Contains(steps[0], "version 4 → 5 (SecureString, same KMS key alias/app)") || !strings.Contains(strings.Join(steps, "\n"), "3 → 6 characters") {
		t.Fatalf("%q %v", steps, err)
	}
	if err := s.ApplyChange(req(dir), c); err != nil {
		t.Fatal(err)
	}
	var in map[string]any
	json.Unmarshal([]byte(f.calls[len(f.calls)-1].stdin), &in)
	if in["KeyId"] != "alias/app" || in["Tier"] != "Advanced" || in["Overwrite"] != true || in["Type"] != "SecureString" || in["Value"] != "abcdef" {
		t.Fatalf("%v", in)
	}
}

func TestVaultWrite(t *testing.T) {
	f := &fakeCLI{answers: map[string]string{
		"kv get": `{"data":{"data":{"password":"pw"},"metadata":{"version":3}}}`,
		"kv put": `{}`,
	}}
	dir := f.install(t)
	s := testService(t, "", "")
	c := Change{Store: Vault, Name: "secret/app/db", KV: 2, Value: `{"password":"pw2"}`, Version: "3"}
	steps, err := s.PlanChange(req(dir), c)
	if err != nil || !strings.Contains(steps[0], "Write version 4 of secret/app/db") || !strings.Contains(steps[1], "vault.example") {
		t.Fatalf("%q %v", steps, err)
	}
	if err := s.ApplyChange(req(dir), c); err != nil {
		t.Fatal(err)
	}
	put := f.calls[len(f.calls)-1]
	if !has(put.args, "kv", "put", "-format=json", "-cas=3", "secret/app/db", "-") || put.stdin != `{"password":"pw2"}` {
		t.Fatalf("%q %q", put.args, put.stdin)
	}
	c.Value = `["not an object"]`
	if _, err := s.PlanChange(req(dir), c); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("%v", err)
	}
	// Creating: -cas=0, so an existing secret is never replaced.
	f.fail = map[string]string{"kv get": "No value found at secret/data/app/new"}
	c = Change{Store: Vault, Name: "secret/app/new", KV: 2, Create: true, Value: `{"k":"v"}`}
	if err := s.ApplyChange(req(dir), c); err != nil {
		t.Fatal(err)
	}
	if !has(f.calls[len(f.calls)-1].args, "-cas=0") {
		t.Fatalf("%q", f.calls[len(f.calls)-1].args)
	}
}
