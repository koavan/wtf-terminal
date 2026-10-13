// Package findings reads security scanners' SARIF reports for the side
// pane's Findings tab, and builds the commands that produce them.
//
// Nothing runs on its own: a report is read when the tab asks (opened,
// refreshed, or a scan finished), and a scan is a visible command the user
// runs in a terminal. Scans run from wtf write their SARIF to wtf's cache
// folder, so the repository stays clean; *.sarif files at the folder's root
// (from CI, say) are read too.
package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/tools"
)

// Finding is one result: what a rule found, and where.
type Finding struct {
	// Index into Report.Sources.
	Source int    `json:"source"`
	Tool   string `json:"tool"`
	RuleID string `json:"ruleId"`
	// The rule's short description or name; Description is the long one.
	Rule        string `json:"rule"`
	Description string `json:"description"`
	HelpURI     string `json:"helpUri"`
	// critical, high, medium, low (from a security-severity score), or the
	// SARIF level: error, warning, note, none.
	Severity string  `json:"severity"`
	Score    float64 `json:"score"`
	Message  string  `json:"message"`
	// An absolute path when the location is a file, else the URI as given.
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Source is one SARIF file.
type Source struct {
	Path    string `json:"path"`
	Tool    string `json:"tool"`
	Version string `json:"version"`
	// When the file was written (RFC 3339).
	Modified string `json:"modified"`
	Count    int    `json:"count"`
	// Written by a scan run from wtf (in its cache), not found in the folder.
	FromScan bool   `json:"fromScan"`
	Error    string `json:"error"`
}

type Report struct {
	// The folder the findings are for: the git repository's root, or the folder itself.
	Root     string    `json:"root"`
	Sources  []Source  `json:"sources"`
	Findings []Finding `json:"findings"`
	// Findings left out past MaxFindings.
	Truncated int `json:"truncated"`
}

const (
	// MaxFindings keeps a huge report from flooding the side pane.
	MaxFindings = 5000
	maxFileSize = 64 << 20
)

type Service struct {
	cacheDir string
}

func NewService() *Service {
	s := &Service{}
	if dir, err := os.UserCacheDir(); err == nil {
		s.cacheDir = filepath.Join(dir, "wtf", "findings")
	}
	return s
}

// Root is the folder findings are kept for: the enclosing git repository's
// root, or the folder itself outside one.
func Root(folder string) string {
	folder = filepath.Clean(folder)
	for dir := folder; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return folder
		}
		dir = parent
	}
}

// scanDir is where wtf's scans of root write their SARIF.
func (s *Service) scanDir(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(s.cacheDir, hex.EncodeToString(sum[:8]))
}

// Load reads every SARIF report for folder's root.
func (s *Service) Load(folder string) (Report, error) {
	if folder == "" || !filepath.IsAbs(folder) {
		return Report{}, errors.New("no folder: the focused pane hasn't reported where it is")
	}
	root := Root(folder)
	r := Report{Root: root, Sources: []Source{}, Findings: []Finding{}}
	for _, f := range s.files(root) {
		src, found := read(f.path, root)
		src.FromScan = f.fromScan
		for i := range found {
			found[i].Source = len(r.Sources)
		}
		room := MaxFindings - len(r.Findings)
		if len(found) > room {
			r.Truncated += len(found) - room
			found = found[:room]
		}
		r.Findings = append(r.Findings, found...)
		r.Sources = append(r.Sources, src)
	}
	return r, nil
}

type sarifFile struct {
	path     string
	fromScan bool
}

// files lists wtf's own scan results for root, then *.sarif at its top.
func (s *Service) files(root string) []sarifFile {
	var out []sarifFile
	if s.cacheDir != "" {
		_ = filepath.WalkDir(s.scanDir(root), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && isSARIF(d.Name()) {
				out = append(out, sarifFile{path, true})
			}
			return nil
		})
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() && isSARIF(e.Name()) {
			out = append(out, sarifFile{filepath.Join(root, e.Name()), false})
		}
	}
	return out
}

func isSARIF(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".sarif") || strings.HasSuffix(n, ".sarif.json")
}

func read(path, root string) (Source, []Finding) {
	src := Source{Path: path}
	fi, err := os.Stat(path)
	if err != nil {
		src.Error = err.Error()
		return src, nil
	}
	src.Modified = fi.ModTime().UTC().Format(time.RFC3339)
	if fi.Size() > maxFileSize {
		src.Error = fmt.Sprintf("too big to read (%d MB)", fi.Size()>>20)
		return src, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		src.Error = err.Error()
		return src, nil
	}
	tool, version, found, err := parse(data, root)
	if err != nil {
		src.Error = "not SARIF wtf can read: " + err.Error()
		return src, nil
	}
	src.Tool, src.Version, src.Count = tool, version, len(found)
	if src.Tool == "" {
		src.Tool = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return src, found
}

// Forget deletes one of wtf's own scan results. Files in the folder are
// the user's and stay.
func (s *Service) Forget(path string) error {
	if s.cacheDir == "" || !isSARIF(path) {
		return errors.New("only results of scans run from wtf can be removed here")
	}
	rel, err := filepath.Rel(s.cacheDir, filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return errors.New("only results of scans run from wtf can be removed here")
	}
	return os.Remove(path)
}

// Scanner is one scanner the tab can run.
type Scanner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// What it looks for.
	About string `json:"about"`
	// The Tools dialog's id, for Install….
	Tool string `json:"tool"`
	// Something to know before running it (network use, say).
	Note  string `json:"note"`
	Found bool   `json:"found"`
}

type scanner struct {
	Scanner
	// The command for an output path; it scans ".".
	command func(out string) string
	// The file the scanner writes, when it picks the name itself (checkov).
	dirOutput string
}

var scanners = []scanner{
	{
		Scanner: Scanner{ID: "trivy", Name: "Trivy", Tool: "trivy",
			About: "Vulnerable dependencies, secrets and misconfigurations (Dockerfiles, Terraform, Kubernetes)",
			Note:  "Downloads its vulnerability database on the first run and when it's a day old."},
		command: func(out string) string {
			return "trivy fs --scanners vuln,secret,misconfig --format sarif --output " + shellQuote(out) + " ."
		},
	},
	{
		Scanner: Scanner{ID: "semgrep", Name: "Semgrep", Tool: "semgrep",
			About: "Bugs and insecure patterns in source code",
			Note:  "--config auto downloads rules from semgrep.dev for the languages it finds."},
		command: func(out string) string {
			return "semgrep scan --config auto --sarif --output " + shellQuote(out) + " ."
		},
	},
	{
		Scanner: Scanner{ID: "checkov", Name: "Checkov", Tool: "checkov",
			About: "Misconfigured infrastructure as code: Terraform, CloudFormation, Kubernetes, Helm, Dockerfiles"},
		command: func(out string) string {
			return "checkov -d . --output sarif --output-file-path " + shellQuote(filepath.Dir(out))
		},
		dirOutput: "results_sarif.sarif",
	},
	{
		Scanner: Scanner{ID: "gitleaks", Name: "Gitleaks", Tool: "gitleaks",
			About: "Secrets committed to git history",
			Note:  "--redact keeps the secrets themselves out of the report."},
		command: func(out string) string {
			return "gitleaks detect --source . --redact --report-format sarif --report-path " + shellQuote(out)
		},
	},
	{
		Scanner: Scanner{ID: "grype", Name: "Grype", Tool: "grype",
			About: "Vulnerable dependencies",
			Note:  "Downloads its vulnerability database on the first run and when it's out of date."},
		command: func(out string) string {
			return "grype dir:. --output sarif --file " + shellQuote(out)
		},
	},
}

// Scanners lists the scanners, and whether each is installed (shell PATH first).
func (s *Service) Scanners(shellPATH string) []Scanner {
	out := make([]Scanner, len(scanners))
	for i, sc := range scanners {
		out[i] = sc.Scanner
		out[i].Found = tools.Find(sc.ID, shellPATH) != ""
	}
	return out
}

// Scan is a scanner's command, as the dialog shows it and the shell runs it.
type Scan struct {
	Root    string `json:"root"`
	Command string `json:"command"`
	// Where its SARIF goes.
	Output string `json:"output"`
}

// ScanCommand builds the command that scans folder's root with a scanner,
// writing SARIF to wtf's cache. From another folder it runs in a subshell
// that cd's to the root, leaving the shell where it is.
func (s *Service) ScanCommand(folder, id string) (Scan, error) {
	var sc *scanner
	for i := range scanners {
		if scanners[i].ID == id {
			sc = &scanners[i]
		}
	}
	if sc == nil {
		return Scan{}, errors.New("unknown scanner " + id)
	}
	if folder == "" || !filepath.IsAbs(folder) {
		return Scan{}, errors.New("no folder: the focused pane hasn't reported where it is")
	}
	if s.cacheDir == "" {
		return Scan{}, errors.New("no cache folder to write the results to")
	}
	root := Root(folder)
	dir := s.scanDir(root)
	out := filepath.Join(dir, sc.ID+".sarif")
	if sc.dirOutput != "" {
		dir = filepath.Join(dir, sc.ID)
		out = filepath.Join(dir, sc.dirOutput)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Scan{}, err
	}
	cmd := sc.command(out)
	if filepath.Clean(folder) != root {
		cmd = "(cd " + shellQuote(root) + " && " + cmd + ")"
	}
	return Scan{Root: root, Command: cmd, Output: out}, nil
}

// shellQuote quotes a path for sh when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:@%+=") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
