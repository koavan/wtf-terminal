package infra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// testService never calls AWS: lookups go to `lookup` (nil fails them),
// and finished lookups are signalled on the returned service's channel.
type testSvc struct {
	*Service
	done chan struct{}
}

func testService(home string, lookup accountLookup) testSvc {
	if lookup == nil {
		lookup = func(context.Context, string, string, string, string) (string, error) {
			return "", errors.New("no lookups in this test")
		}
	}
	done := make(chan struct{}, 16)
	return testSvc{newService(home, lookup, func() { done <- struct{}{} }), done}
}

func (s testSvc) waitLookup(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("lookup never finished")
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

const kubeA = `
apiVersion: v1
kind: Config
current-context: prod-eks
contexts:
- name: prod-eks
  context:
    cluster: arn:aws:eks:us-east-1:123456789012:cluster/payments
    namespace: payments
- name: dev
  context:
    cluster: dev
`

func TestKubeDefaultConfig(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".kube", "config"), kubeA)
	got := testService(home, nil).Resolve(map[string]string{"PWD": home})
	if got.KubeContext != "prod-eks" || got.KubeNamespace != "payments" ||
		got.KubeCluster != "arn:aws:eks:us-east-1:123456789012:cluster/payments" {
		t.Fatalf("got %+v", got)
	}
}

func TestKubeconfigListMerges(t *testing.T) {
	home := t.TempDir()
	// The first file has no current-context, the second defines the context
	// again with another namespace: kubectl takes the first definition.
	write(t, filepath.Join(home, "a.yaml"), "contexts:\n- name: dev\n  context:\n    namespace: team-a\n")
	write(t, filepath.Join(home, "b.yaml"), "current-context: dev\ncontexts:\n- name: dev\n  context:\n    namespace: other\n")
	env := map[string]string{
		"PWD":        home,
		"KUBECONFIG": "a.yaml" + string(filepath.ListSeparator) + filepath.Join(home, "b.yaml"),
	}
	got := testService(home, nil).Resolve(env)
	if got.KubeContext != "dev" || got.KubeNamespace != "team-a" {
		t.Fatalf("got %+v", got)
	}
}

func TestKubeNoConfig(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".kube", "config"), "not: [valid")
	if got := testService(home, nil).Resolve(map[string]string{}); !reflect.DeepEqual(got, Context{}) {
		t.Fatalf("got %+v", got)
	}
}

func TestKubeCacheSeesChanges(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".kube", "config")
	write(t, path, kubeA)
	s := testService(home, nil)
	s.Resolve(nil)
	// What `kubectl config use-context dev` does.
	write(t, path, "current-context: dev\n")
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(path, later, later)
	if got := s.Resolve(nil); got.KubeContext != "dev" {
		t.Fatalf("got %+v", got)
	}
}

const awsConfig = `
[default]
region = eu-west-1

[profile prod-admin]
region = us-east-1
source_profile = sso
role_arn = arn:aws:iam::210987654321:role/admin

[profile sso]
sso_session = corp
sso_account_id = 111122223333
sso_role_name = Dev

[profile static]
aws_access_key_id = AKIA
`

func TestAWS(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".aws", "config"), awsConfig)
	s := testService(home, nil)

	keys := "WTF_AWS_ENV_KEYS"
	cases := []struct {
		env                      map[string]string
		profile, account, region string
	}{
		{map[string]string{}, "default", "", "eu-west-1"}, // the CLI's fallback
		// The assumed role's account, not the source profile's.
		{map[string]string{"AWS_PROFILE": "prod-admin"}, "prod-admin", "210987654321", "us-east-1"},
		{map[string]string{"AWS_PROFILE": "sso"}, "sso", "111122223333", ""},
		{map[string]string{"AWS_PROFILE": "static"}, "static", "", ""},
		{map[string]string{"AWS_PROFILE": "default"}, "default", "", "eu-west-1"},
		{map[string]string{"AWS_PROFILE": "prod-admin", "AWS_REGION": "ap-south-1"}, "prod-admin", "210987654321", "ap-south-1"},
		{map[string]string{"AWS_PROFILE": "unknown"}, "unknown", "", ""},
		// Exported keys beat the profile…
		{map[string]string{"AWS_PROFILE": "prod-admin", keys: "1"}, EnvCredentials, "", ""},
		{map[string]string{keys: "1", "AWS_REGION": "us-west-2"}, EnvCredentials, "", "us-west-2"},
		// …except aws-vault's, which are for its profile.
		{map[string]string{"AWS_VAULT": "sso", keys: "1"}, "sso", "111122223333", ""},
	}
	for _, c := range cases {
		got := s.Resolve(c.env)
		if got.AWSProfile != c.profile || got.AWSAccount != c.account || got.AWSRegion != c.region {
			t.Errorf("%v: got %q %q %q, want %q %q %q", c.env,
				got.AWSProfile, got.AWSAccount, got.AWSRegion, c.profile, c.account, c.region)
		}
	}
}

func TestGCP(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "gcloud")
	write(t, filepath.Join(dir, "active_config"), "work\n")
	write(t, filepath.Join(dir, "configurations", "config_work"), "[core]\naccount = me@example.com\nproject = shop-prod\n")
	write(t, filepath.Join(dir, "configurations", "config_play"), "[core]\nproject = sandbox\n")
	s := testService(home, nil)

	if got := s.Resolve(nil).GCPProject; got != "shop-prod" {
		t.Errorf("active config: got %q", got)
	}
	if got := s.Resolve(map[string]string{"CLOUDSDK_ACTIVE_CONFIG_NAME": "play"}).GCPProject; got != "sandbox" {
		t.Errorf("config from env: got %q", got)
	}
	if got := s.Resolve(map[string]string{"CLOUDSDK_CORE_PROJECT": "x"}).GCPProject; got != "x" {
		t.Errorf("project from env: got %q", got)
	}
}

func TestTerraform(t *testing.T) {
	home := t.TempDir()
	stack := filepath.Join(home, "stack")
	write(t, filepath.Join(stack, ".terraform", "environment"), "prod")
	write(t, filepath.Join(home, "plain", "x"), "")
	write(t, filepath.Join(home, "fresh", ".terraform", "providers"), "")
	s := testService(home, nil)

	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"PWD": stack}, "prod"},
		{map[string]string{"PWD": stack, "TF_WORKSPACE": "staging"}, "staging"},
		{map[string]string{"PWD": filepath.Join(home, "plain"), "TF_WORKSPACE": "staging"}, ""},
		{map[string]string{"PWD": filepath.Join(home, "fresh")}, ""}, // default workspace
		{map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := s.Resolve(c.env).TFWorkspace; got != c.want {
			t.Errorf("%v: got %q, want %q", c.env, got, c.want)
		}
	}
}

func TestAWSNothingConfigured(t *testing.T) {
	s := testService(t.TempDir(), nil)
	if got := s.Resolve(map[string]string{"AWS_REGION": "us-east-1"}); got.AWSProfile != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestAWSAccountLookup(t *testing.T) {
	home := t.TempDir()
	creds := filepath.Join(home, ".aws", "credentials")
	write(t, creds, "[default]\naws_access_key_id = AKIA1\n\n[ppp]\naws_access_key_id = AKIA2\n")

	var mu sync.Mutex
	calls := map[string]int{}
	s := testService(home, func(_ context.Context, profile, region, config, credsFile string) (string, error) {
		mu.Lock()
		calls[profile]++
		mu.Unlock()
		if credsFile != creds || region != "" {
			t.Errorf("lookup got creds %q region %q", credsFile, region)
		}
		if profile == "ppp" {
			return "123456789012", nil
		}
		return "", errors.New("InvalidClientTokenId")
	})
	ppp := map[string]string{"AWS_PROFILE": "ppp"}

	// Static keys don't name the account: AWS is asked, without waiting.
	if got := s.Resolve(ppp); got.AWSAccount != "" || got.AWSAccountNote != AccountPending {
		t.Fatalf("first resolve: %+v", got)
	}
	s.waitLookup(t)
	for range 3 {
		if got := s.Resolve(ppp); got.AWSAccount != "123456789012" || got.AWSAccountNote != "" {
			t.Fatalf("after lookup: %+v", got)
		}
	}

	// The default profile, used when none is chosen; failures are shown.
	if got := s.Resolve(nil); got.AWSProfile != "default" || got.AWSAccountNote != AccountPending {
		t.Fatalf("default: %+v", got)
	}
	s.waitLookup(t)
	if got := s.Resolve(nil); got.AWSAccountNote != "InvalidClientTokenId" {
		t.Fatalf("failed lookup: %+v", got)
	}

	// Unknown profiles aren't looked up: the CLI would fail anyway.
	if got := s.Resolve(map[string]string{"AWS_PROFILE": "nope"}); got.AWSAccountNote != "" {
		t.Fatalf("unknown profile: %+v", got)
	}

	// New keys may be for another account: ask again.
	write(t, creds, "[default]\n[ppp]\naws_access_key_id = AKIA3\n")
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(creds, later, later)
	if got := s.Resolve(ppp); got.AWSAccountNote != AccountPending {
		t.Fatalf("after credentials changed: %+v", got)
	}
	s.waitLookup(t)

	mu.Lock()
	defer mu.Unlock()
	if calls["ppp"] != 2 || calls["default"] != 1 || calls["nope"] != 0 {
		t.Fatalf("lookups: %v", calls)
	}
}

func TestCredentialsKeepOnlySectionNamesAndExpiry(t *testing.T) {
	home := t.TempDir()
	creds := filepath.Join(home, ".aws", "credentials")
	write(t, creds, "[ppp]\naws_secret_access_key = SECRET\n")
	s := testService(home, nil)
	c, ok := s.load(creds, credsFormat)
	if !ok || !c.ini.has("ppp") || len(c.ini["ppp"]) != 0 {
		t.Fatalf("got %v", c.ini)
	}
}

func TestSTSLookup(t *testing.T) {
	// A stand-in for the aws CLI that checks how it is called.
	bin := t.TempDir()
	script := `#!/bin/sh
[ -n "$AWS_ACCESS_KEY_ID" ] && { echo "env keys leaked" >&2; exit 1; }
[ "$AWS_SHARED_CREDENTIALS_FILE" = /c ] || { echo "creds file $AWS_SHARED_CREDENTIALS_FILE" >&2; exit 1; }
case "$*" in
  "sts get-caller-identity --profile good --region us-east-1 --query Account --output text") echo 123456789012 ;;
  *) echo "noise"; echo "An error occurred (ExpiredToken): expired" >&2; exit 254 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "aws"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAWTF")

	ctx := context.Background()
	if acct, err := stsLookup(ctx, "good", "", "/a", "/c"); acct != "123456789012" || err != nil {
		t.Fatalf("good: %q %v", acct, err)
	}
	_, err := stsLookup(ctx, "bad", "eu-west-1", "/a", "/c")
	if err == nil || !strings.Contains(err.Error(), "ExpiredToken") {
		t.Fatalf("bad: %v", err)
	}
}
