package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func lines(r Review) []string {
	var out []string
	for _, c := range r.Changes {
		out = append(out, ChangeLine(c))
	}
	return out
}

func expect(t *testing.T, r Review, want []string) {
	t.Helper()
	if got := lines(r); !slices.Equal(got, want) {
		t.Errorf("changes:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// testdata/review/reviewable.json is shared with the Swift port
// (WtfCore Review.reviewable), which builds the block menu.
func TestReviewable(t *testing.T) {
	b, err := os.ReadFile("../../testdata/review/reviewable.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]bool
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for cmd, want := range cases {
		if Reviewable(cmd) != want {
			t.Errorf("Reviewable(%q) = %v", cmd, !want)
		}
	}
}

func TestTerraformText(t *testing.T) {
	s := NewService(nil)
	r := s.Parse("terraform plan", "\x1b[1m"+read(t, "terraform-plan.txt")+"\x1b[0m")
	if r.Tool != "terraform" || r.Plan != "Plan: 2 to add, 1 to change, 2 to destroy." {
		t.Fatalf("tool %q plan %q", r.Tool, r.Plan)
	}
	expect(t, r, []string{
		"replace aws_db_instance.main (engine_version forces replacement; changes address, engine_version, id)",
		"change aws_iam_role_policy.app (changes policy) [IAM]",
		"destroy aws_instance.old (because aws_instance.old is not in configuration)",
		"create module.net.aws_security_group_rule.ssh [Security group]",
		"move aws_s3_bucket.audit_logs (from aws_s3_bucket.logs)",
	})
	if c := r.Counts; c.Create != 1 || c.Update != 1 || c.Replace != 1 || c.Delete != 1 || c.Move != 1 {
		t.Errorf("counts %+v", c)
	}
	if got := CountLine(r.Counts); got != "1 to create, 1 to change, 1 to replace, 1 to destroy, 1 moved" {
		t.Errorf("count line %q", got)
	}
	if !strings.Contains(r.Excerpt, `engine_version                        = "13.4" -> "14.1"`) {
		t.Error("the excerpt lacks the plan")
	}

	r = s.Parse("terraform plan", "No changes. Your infrastructure matches the configuration.\n")
	if r.Tool != "terraform" || !r.NoChanges || len(r.Changes) != 0 {
		t.Errorf("no changes: %+v", r)
	}
	if r := s.Parse("ls", "a\nb\n"); r.Tool != "" {
		t.Errorf("plain output read as %q", r.Tool)
	}
}

func TestKubectlDiff(t *testing.T) {
	r := NewService(nil).Parse("kubectl diff -f k8s/", read(t, "kubectl-diff.txt"))
	if r.Tool != "kubectl diff" {
		t.Fatalf("tool %q", r.Tool)
	}
	expect(t, r, []string{
		"change Deployment shop/checkout-api (changes replicas, image)",
		"create ClusterRoleBinding ci-admin [RBAC]",
		"destroy ConfigMap shop/app.config",
	})
	if r.Changes[0].Namespace != "shop" || r.Changes[2].Name != "app.config" {
		t.Errorf("objects: %+v", r.Changes)
	}
}

func TestHelmDiff(t *testing.T) {
	r := NewService(nil).Parse("helm diff upgrade shop ./checkout", read(t, "helm-diff.txt"))
	if r.Tool != "helm diff" {
		t.Fatalf("tool %q", r.Tool)
	}
	expect(t, r, []string{
		"change Deployment shop/checkout-api (changes replicas, image)",
		"create Secret shop/checkout-db-password [Secret]",
		"destroy Service shop/checkout-legacy",
	})
}

func TestPlanFile(t *testing.T) {
	s := NewService(nil)
	r, err := s.ParseFile(filepath.Join("testdata", "plan.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, r, []string{
		"replace aws_db_instance.main (engine_version forces replacement; changes engine_version, id, password)",
		"change aws_security_group.web (changes ingress) [Security group]",
		"replace aws_instance.tainted (tainted)",
	})
	if r.Source != "the plan file plan.json" {
		t.Errorf("source %q", r.Source)
	}
	if !strings.Contains(r.Excerpt, `aws_db_instance.main: engine_version: "13.4" -> "14.1"`) || strings.Contains(r.Excerpt, "hunter") {
		t.Errorf("excerpt: %s", r.Excerpt)
	}

	// Not JSON and no terraform on PATH: says what's needed.
	bin := filepath.Join(t.TempDir(), "tf.plan")
	os.WriteFile(bin, []byte("PK\x03\x04binary"), 0o644)
	if _, err := s.ParseFile(bin, map[string]string{"PATH": t.TempDir()}); err == nil {
		t.Error("a binary plan without terraform was read")
	}
}

func TestRequest(t *testing.T) {
	s := NewService(nil)
	if _, err := s.Request(Ask{Review: s.Parse("ls", "x")}); err == nil {
		t.Error("asked about plain output")
	}
	r, err := s.Request(Ask{Review: s.Parse("terraform plan", read(t, "terraform-plan.txt")), Cwd: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	msg := r.Messages[0].Content
	for _, want := range []string{
		"Tool: terraform\nSource: the output of `terraform plan`\n",
		"terraform's own count: Plan: 2 to add, 1 to change, 2 to destroy.\n",
		"- replace aws_db_instance.main (engine_version forces replacement",
		"\nShell:\nFolder: /w\n",
		"\nExcerpt of the output:\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("request lacks %q:\n%s", want, msg)
		}
	}
	if r.Fast {
		t.Error("a review goes to the default provider")
	}
}
