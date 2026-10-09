package awsprofiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every test works on files in a temp folder: the Service is built by
// hand, never with NewService, so the user's own ~/.aws is out of reach.
func testService(t *testing.T, creds, config string) *Service {
	t.Helper()
	home := t.TempDir()
	s := &Service{
		home:        home,
		credentials: filepath.Join(home, ".aws", "credentials"),
		config:      filepath.Join(home, ".aws", "config"),
		backupDir:   filepath.Join(home, "cache"),
	}
	if err := os.MkdirAll(filepath.Join(home, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	if creds != "" {
		os.WriteFile(s.credentials, []byte(creds), 0o600)
	}
	if config != "" {
		os.WriteFile(s.config, []byte(config), 0o600)
	}
	return s
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

func profile(l List, name string) *Profile {
	for i := range l.Profiles {
		if l.Profiles[i].Name == name {
			return &l.Profiles[i]
		}
	}
	return nil
}

const (
	secretA = "aSecretKeyThatIsLongEnough0001"
	secretB = "bSecretKeyThatIsLongEnough0002"
	creds   = `# work keys
[default]
aws_access_key_id = AKIADEFAULT000000001
aws_secret_access_key = ` + secretA + `

[legacy]
AWS_ACCESS_KEY_ID=AKIALEGACY0000000001
AWS_SECRET_ACCESS_KEY=` + secretB + `
AWS_REGION=eu-west-1

[temp]
aws_access_key_id = ASIATEMP000000000001
aws_secret_access_key = ` + secretA + `
aws_session_token = tokentokentoken
`
	config = `[default]
region = us-east-1

[profile admin]
role_arn = arn:aws:iam::123456789012:role/Admin
source_profile = default
s3 =
  max_concurrent_requests = 20

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start

[profile dev]
sso_session = corp
sso_account_id = 210987654321
region = eu-central-1
`
)

func TestList(t *testing.T) {
	s := testService(t, creds, config)
	l := s.List()
	var names []string
	for _, p := range l.Profiles {
		names = append(names, p.Name+":"+p.Kind)
	}
	if got := strings.Join(names, " "); got != "default:keys admin:role dev:sso legacy:keys temp:keys" {
		t.Errorf("profiles %s", got)
	}

	d := profile(l, "default")
	if d.AccessKeyID != "AKIADEFAULT000000001" || !d.HasSecret || d.Region != "us-east-1" || strings.Join(d.SourceFor, ",") != "admin" {
		t.Errorf("default %+v", d)
	}
	leg := profile(l, "legacy")
	if leg.AccessKeyID != "AKIALEGACY0000000001" || !leg.HasSecret || leg.Region != "" ||
		len(leg.CredentialsOther) != 1 || leg.CredentialsOther[0] != (Setting{"AWS_REGION", "eu-west-1"}) {
		t.Errorf("legacy (upper-case keys) %+v", leg)
	}
	if p := profile(l, "temp"); !p.HasSessionToken {
		t.Errorf("temp %+v", p)
	}
	adm := profile(l, "admin")
	if adm.Account != "123456789012" || strings.Join(adm.Locked, ",") != "s3" || len(adm.Settings) != 2 {
		t.Errorf("admin %+v", adm)
	}
	if p := profile(l, "dev"); p.Account != "210987654321" || p.InCredentials {
		t.Errorf("dev %+v", p)
	}
	if profile(l, "corp") != nil || profile(l, "sso-session corp") != nil {
		t.Error("sso-session sections aren't profiles")
	}

	// Secrets never leave the package.
	data, _ := json.Marshal(l)
	for _, secret := range []string{secretA, secretB, "tokentokentoken"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the list contains a secret: %s", secret)
		}
	}
}

func TestAddProfile(t *testing.T) {
	s := testService(t, creds, "")
	l := s.List()
	d := Draft{
		Name: "prod", Versions: l.Versions, UseKeys: true,
		AccessKeyID: "AKIAPROD000000000001", SecretAccessKey: secretB,
		Settings: []Setting{{"region", "us-west-2"}, {"output", ""}},
	}
	plan, err := s.PlanSave(d)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"~/.aws/credentials: add [prod]",
		"~/.aws/credentials: [prod] access key ID AKIAPROD000000000001",
		"~/.aws/credentials: [prod] new secret access key (not shown)",
		"~/.aws/config: add [profile prod]",
		"~/.aws/config: [profile prod] region = us-west-2",
	}
	if strings.Join(plan, "\n") != strings.Join(want, "\n") {
		t.Errorf("plan:\n%s", strings.Join(plan, "\n"))
	}
	if read(t, s.credentials) != creds {
		t.Fatal("planning must not write")
	}

	if _, err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s.credentials); got != creds+"\n[prod]\naws_access_key_id = AKIAPROD000000000001\naws_secret_access_key = "+secretB+"\n" {
		t.Errorf("credentials:\n%s", got)
	}
	if got := read(t, s.config); got != "[profile prod]\nregion = us-west-2\n" {
		t.Errorf("config:\n%s", got)
	}
	if fi, _ := os.Stat(s.config); fi.Mode().Perm() != 0o600 {
		t.Errorf("new config mode %v", fi.Mode().Perm())
	}
	if _, err := s.Save(d); err == nil {
		t.Error("saving with stale versions must fail")
	}
	d.Versions = s.List().Versions
	if _, err := s.PlanSave(d); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate name: %v", err)
	}
}

func TestEditKeepsStyleAndSecret(t *testing.T) {
	s := testService(t, creds, config)
	l := s.List()
	// Change only the region of the upper-case profile; the secret field
	// is left empty, meaning "keep".
	d := Draft{
		Original: "legacy", Name: "legacy", Versions: l.Versions, UseKeys: true,
		AccessKeyID: "AKIALEGACY0000000001", Settings: []Setting{{"region", "eu-west-1"}},
	}
	if _, err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	if read(t, s.credentials) != creds {
		t.Errorf("credentials shouldn't change:\n%s", read(t, s.credentials))
	}
	if !strings.HasSuffix(read(t, s.config), "\n[profile legacy]\nregion = eu-west-1\n") {
		t.Errorf("config:\n%s", read(t, s.config))
	}

	// A new key ID needs its secret.
	d.Versions = s.List().Versions
	d.AccessKeyID = "AKIALEGACY0000000002"
	if _, err := s.PlanSave(d); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Errorf("new key without secret: %v", err)
	}
	d.SecretAccessKey = secretA
	if _, err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	got := read(t, s.credentials)
	if !strings.Contains(got, "AWS_ACCESS_KEY_ID=AKIALEGACY0000000002\nAWS_SECRET_ACCESS_KEY="+secretA+"\nAWS_REGION=eu-west-1") {
		t.Errorf("upper-case lines should keep their spelling:\n%s", got)
	}
	if !strings.HasPrefix(got, "# work keys\n[default]") {
		t.Error("comments must stay")
	}

	// Removing the session token, and a role profile's settings with a
	// nested block left alone.
	l = s.List()
	if _, err := s.Save(Draft{Original: "temp", Name: "temp", Versions: l.Versions, UseKeys: true, AccessKeyID: "ASIATEMP000000000001", RemoveSessionToken: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, s.credentials), "session_token") {
		t.Error("token should be gone")
	}
	l = s.List()
	if _, err := s.Save(Draft{Original: "admin", Name: "admin", Versions: l.Versions, Settings: []Setting{{"role_arn", "arn:aws:iam::123456789012:role/ReadOnly"}, {"source_profile", "default"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, s.config), "role_arn = arn:aws:iam::123456789012:role/ReadOnly\nsource_profile = default\ns3 =\n  max_concurrent_requests = 20") {
		t.Errorf("config:\n%s", read(t, s.config))
	}
}

func TestRenameFollowsReferences(t *testing.T) {
	s := testService(t, creds, config)
	l := s.List()
	d := Draft{Original: "default", Name: "main", Versions: l.Versions, UseKeys: true, AccessKeyID: "AKIADEFAULT000000001", Settings: []Setting{{"region", "us-east-1"}}}
	plan, err := s.PlanSave(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "[profile admin] source_profile: default → main") {
		t.Errorf("plan %v", plan)
	}
	if _, err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, s.credentials), "[main]\naws_access_key_id = AKIADEFAULT000000001\naws_secret_access_key = "+secretA) {
		t.Errorf("credentials:\n%s", read(t, s.credentials))
	}
	cfg := read(t, s.config)
	if !strings.HasPrefix(cfg, "[profile main]\nregion = us-east-1") || !strings.Contains(cfg, "source_profile = main") {
		t.Errorf("config:\n%s", cfg)
	}
}

func TestDeleteAndMakeDefault(t *testing.T) {
	s := testService(t, creds, config)
	l := s.List()
	plan, err := s.PlanDelete("default", l.Versions)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "[profile admin] uses it as source_profile and will stop working") {
		t.Errorf("plan %v", plan)
	}

	// Copy temp (with a token) over default, then legacy (without): the
	// token must not linger.
	if _, err := s.MakeDefault("temp", l.Versions); err != nil {
		t.Fatal(err)
	}
	l = s.List()
	if d := profile(l, "default"); d.AccessKeyID != "ASIATEMP000000000001" || !d.HasSessionToken || d.Region != "" {
		t.Errorf("default after copying temp: %+v", d)
	}
	if _, err := s.MakeDefault("legacy", l.Versions); err != nil {
		t.Fatal(err)
	}
	l = s.List()
	if d := profile(l, "default"); d.AccessKeyID != "AKIALEGACY0000000001" || d.HasSessionToken {
		t.Errorf("default after copying legacy: %+v", d)
	}

	if _, err := s.Delete("legacy", l.Versions); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, s.credentials), "[legacy]") || profile(s.List(), "legacy") != nil {
		t.Error("legacy should be gone")
	}
	backups, _ := os.ReadDir(s.backupDir)
	if len(backups) < 3 {
		t.Errorf("backups %d", len(backups))
	}
	if fi, _ := os.Stat(s.backupDir); fi.Mode().Perm() != 0o700 {
		t.Errorf("backup folder mode %v", fi.Mode().Perm())
	}
}

func TestValidation(t *testing.T) {
	s := testService(t, creds, config)
	v := s.List().Versions
	bad := []Draft{
		{Name: "", Versions: v},
		{Name: "a]b", Versions: v},
		{Name: "profile x", Versions: v},
		{Name: "x", Versions: v, UseKeys: true, AccessKeyID: "nope", SecretAccessKey: secretA},
		{Name: "x", Versions: v, UseKeys: true, AccessKeyID: "AKIAXXXXXXXXXXXXXXXX", SecretAccessKey: "short"},
		{Name: "x", Versions: v, Settings: []Setting{{"region", "Mars"}}},
		{Name: "x", Versions: v, Settings: []Setting{{"output", "xml"}}},
		{Name: "x", Versions: v, Settings: []Setting{{"bad key", "1"}}},
		{Name: "x", Versions: v, Settings: []Setting{{"aws_secret_access_key", "x"}}},
		{Name: "x", Versions: v, Settings: []Setting{{"region", "us-east-1"}, {"Region", "us-east-2"}}},
	}
	for _, d := range bad {
		if _, err := s.PlanSave(d); err == nil {
			t.Errorf("%+v should be refused", d)
		}
	}
}
