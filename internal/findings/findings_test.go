package findings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Shaped like trivy's output: severity scores on the rules, %SRCROOT% paths.
const trivySARIF = `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "Trivy", "version": "0.56.2", "rules": [
      {"id": "CVE-2024-0001", "name": "LanguageSpecificPackageVulnerability",
       "shortDescription": {"text": "openssl: buffer overflow"},
       "helpUri": "https://avd.aquasec.com/nvd/cve-2024-0001",
       "properties": {"security-severity": "9.8"}},
      {"id": "DS002", "shortDescription": {"text": "Image user should not be 'root'"},
       "properties": {"security-severity": "4.0"}}
    ]}},
    "originalUriBaseIds": {"ROOTPATH": {"uri": "file:///"}},
    "results": [
      {"ruleId": "CVE-2024-0001", "ruleIndex": 0, "level": "error",
       "message": {"text": "Package: openssl\nInstalled Version: 3.0.1"},
       "locations": [{"physicalLocation": {"artifactLocation": {"uri": "go.mod", "uriBaseId": "ROOTPATH"},
         "region": {"startLine": 12, "startColumn": 1}}}]},
      {"ruleId": "DS002", "ruleIndex": 1,
       "message": {"text": "Specify at least 1 USER command"},
       "locations": [{"physicalLocation": {"artifactLocation": {"uri": "build/Dockerfile"}, "region": {"startLine": 1}}}]}
    ]
  }]
}`

// Shaped like semgrep's: levels only, the rule by id.
const semgrepSARIF = `{
  "runs": [{
    "tool": {"driver": {"name": "Semgrep OSS", "semanticVersion": "1.90.0", "rules": [
      {"id": "go.lang.security.audit.sqli", "name": "go.lang.security.audit.sqli",
       "shortDescription": {"text": "SQL built from user input"},
       "defaultConfiguration": {"level": "error"}}
    ]}},
    "results": [
      {"ruleId": "go.lang.security.audit.sqli", "message": {"text": "Use a parameterized query"},
       "locations": [{"physicalLocation": {"artifactLocation": {"uri": "file:///abs/db.go"}, "region": {"startLine": 40, "startColumn": 9}}}]},
      {"ruleId": "unknown.rule", "message": {"text": "no rule, no level"}}
    ]
  }]
}`

func TestParse(t *testing.T) {
	tool, version, fs, err := parse([]byte(trivySARIF), "/repo")
	if err != nil || tool != "Trivy" || version != "0.56.2" || len(fs) != 2 {
		t.Fatalf("trivy: %q %q %v %+v", tool, version, err, fs)
	}
	cve := fs[0]
	if cve.Severity != "critical" || cve.Score != 9.8 || cve.Rule != "openssl: buffer overflow" ||
		cve.HelpURI == "" || cve.Line != 12 || cve.Column != 1 {
		t.Errorf("cve: %+v", cve)
	}
	// A base of file:/// makes "go.mod" absolute at /.
	if cve.Path != "/go.mod" {
		t.Errorf("cve path %q", cve.Path)
	}
	if ds := fs[1]; ds.Severity != "medium" || ds.Path != "/repo/build/Dockerfile" || ds.Line != 1 {
		t.Errorf("ds002: %+v", ds)
	}

	tool, version, fs, err = parse([]byte(semgrepSARIF), "/repo")
	if err != nil || tool != "Semgrep OSS" || version != "1.90.0" || len(fs) != 2 {
		t.Fatalf("semgrep: %q %q %v %+v", tool, version, err, fs)
	}
	// The rule's default level, by id; the path from a file: URI.
	if f := fs[0]; f.Severity != "error" || f.Path != "/abs/db.go" || f.Line != 40 || f.Rule != "SQL built from user input" {
		t.Errorf("sqli: %+v", f)
	}
	// SARIF's default level is warning.
	if f := fs[1]; f.Severity != "warning" || f.Path != "" || f.RuleID != "unknown.rule" {
		t.Errorf("no rule: %+v", f)
	}

	if _, _, _, err := parse([]byte("not json"), "/"); err == nil {
		t.Error("garbage should be an error")
	}
}

func TestSeverity(t *testing.T) {
	for _, c := range []struct {
		score float64
		level string
		want  string
	}{
		{9, "", "critical"}, {7.5, "note", "high"}, {4, "", "medium"}, {0.1, "", "low"}, {0, "error", "none"},
		{-1, "error", "error"}, {-1, "note", "note"}, {-1, "", "warning"}, {-1, "bogus", "warning"},
	} {
		if got := severity(c.score, c.level); got != c.want {
			t.Errorf("severity(%v, %q) = %q, want %q", c.score, c.level, got, c.want)
		}
	}
}

func TestRoot(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	sub := filepath.Join(repo, "a", "b")
	os.MkdirAll(sub, 0o755)
	if got := Root(sub); got != repo {
		t.Errorf("Root(sub) = %q, want %q", got, repo)
	}
	plain := t.TempDir()
	if got := Root(plain); got != plain {
		t.Errorf("Root(plain) = %q", got)
	}
}

func TestLoadAndScan(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	os.WriteFile(filepath.Join(repo, "ci.sarif"), []byte(semgrepSARIF), 0o644)
	os.WriteFile(filepath.Join(repo, "broken.sarif.json"), []byte("{"), 0o644)
	os.WriteFile(filepath.Join(repo, "notes.json"), []byte(trivySARIF), 0o644) // not named .sarif
	sub := filepath.Join(repo, "svc")
	os.Mkdir(sub, 0o755)

	s := &Service{cacheDir: t.TempDir()}
	scan, err := s.ScanCommand(sub, "trivy")
	if err != nil {
		t.Fatal(err)
	}
	// From a subfolder: a subshell at the root, so the shell stays put.
	if !strings.HasPrefix(scan.Command, "(cd "+shellQuote(repo)+" && trivy fs ") || !strings.HasSuffix(scan.Command, " .)") || scan.Root != repo {
		t.Errorf("command: %s", scan.Command)
	}
	if !strings.HasPrefix(scan.Output, s.cacheDir) {
		t.Errorf("output %s should be in the cache", scan.Output)
	}
	os.WriteFile(scan.Output, []byte(trivySARIF), 0o600)

	ck, _ := s.ScanCommand(repo, "checkov")
	if strings.HasPrefix(ck.Command, "(cd") || filepath.Base(ck.Output) != "results_sarif.sarif" ||
		!strings.Contains(ck.Command, "--output-file-path "+shellQuote(filepath.Dir(ck.Output))) {
		t.Errorf("checkov: %+v", ck)
	}
	if _, err := s.ScanCommand(repo, "nope"); err == nil {
		t.Error("unknown scanner should fail")
	}

	r, err := s.Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if r.Root != repo || len(r.Sources) != 3 {
		t.Fatalf("report: %+v", r)
	}
	// wtf's scan first, then the folder's files.
	if src := r.Sources[0]; !src.FromScan || src.Tool != "Trivy" || src.Count != 2 || src.Modified == "" {
		t.Errorf("scan source: %+v", src)
	}
	var broken, ci *Source
	for i := range r.Sources {
		switch filepath.Base(r.Sources[i].Path) {
		case "broken.sarif.json":
			broken = &r.Sources[i]
		case "ci.sarif":
			ci = &r.Sources[i]
		}
	}
	if broken == nil || broken.Error == "" || ci == nil || ci.FromScan || ci.Count != 2 {
		t.Errorf("folder sources: %+v", r.Sources)
	}
	if len(r.Findings) != 4 || r.Findings[0].Source != 0 || r.Findings[0].Path != "/go.mod" || r.Findings[1].Path != filepath.Join(repo, "build/Dockerfile") {
		t.Errorf("findings: %+v", r.Findings)
	}

	// Only wtf's own results can be removed.
	if err := s.Forget(filepath.Join(repo, "ci.sarif")); err == nil {
		t.Error("a file in the folder must not be removable")
	}
	if err := s.Forget(scan.Output); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Load(repo); len(r.Sources) != 2 {
		t.Errorf("after Forget: %+v", r.Sources)
	}
	if _, err := s.Load("relative"); err == nil {
		t.Error("a relative folder should be refused")
	}
}
