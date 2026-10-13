package findings

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// The parts of SARIF 2.1.0 the tab shows. Scanners fill in different
// subsets; everything here is optional.
type sarifLog struct {
	Runs []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Name            string      `json:"name"`
			Version         string      `json:"version"`
			SemanticVersion string      `json:"semanticVersion"`
			Rules           []sarifRule `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
	OriginalURIBaseIDs map[string]struct {
		URI string `json:"uri"`
	} `json:"originalUriBaseIds"`
	Results []sarifResult `json:"results"`
}

type sarifRule struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	ShortDescription *text   `json:"shortDescription"`
	FullDescription  *text   `json:"fullDescription"`
	HelpURI          string  `json:"helpUri"`
	Properties       props   `json:"properties"`
	Default          *config `json:"defaultConfiguration"`
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	RuleIndex *int   `json:"ruleIndex"`
	Rule      *struct {
		ID    string `json:"id"`
		Index *int   `json:"index"`
	} `json:"rule"`
	Level      string          `json:"level"`
	Message    text            `json:"message"`
	Locations  []sarifLocation `json:"locations"`
	Properties props           `json:"properties"`
}

type sarifLocation struct {
	Physical *struct {
		Artifact struct {
			URI       string `json:"uri"`
			URIBaseID string `json:"uriBaseId"`
		} `json:"artifactLocation"`
		Region *struct {
			StartLine   int `json:"startLine"`
			StartColumn int `json:"startColumn"`
		} `json:"region"`
	} `json:"physicalLocation"`
}

type text struct {
	Text string `json:"text"`
}

type config struct {
	Level string `json:"level"`
}

type props map[string]any

// securitySeverity is the CVSS-like score GitHub code scanning reads
// (trivy, grype, checkov and others set it), or -1.
func (p props) securitySeverity() float64 {
	switch v := p["security-severity"].(type) {
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	case float64:
		return v
	}
	return -1
}

// severity is what the scanner said: its security-severity score as
// GitHub buckets it, else the SARIF level (whose default is warning).
func severity(score float64, level string) string {
	switch {
	case score >= 9:
		return "critical"
	case score >= 7:
		return "high"
	case score >= 4:
		return "medium"
	case score > 0:
		return "low"
	case score == 0:
		return "none"
	}
	switch level {
	case "error", "warning", "note", "none":
		return level
	}
	return "warning"
}

// parse turns one SARIF file into findings; root resolves relative paths.
func parse(data []byte, root string) (tool, version string, out []Finding, err error) {
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		return "", "", nil, err
	}
	for _, run := range log.Runs {
		d := run.Tool.Driver
		if tool == "" {
			tool, version = d.Name, d.Version
			if version == "" {
				version = d.SemanticVersion
			}
		}
		byID := map[string]*sarifRule{}
		for i := range d.Rules {
			byID[d.Rules[i].ID] = &d.Rules[i]
		}
		for _, r := range run.Results {
			id, idx := r.RuleID, r.RuleIndex
			if r.Rule != nil {
				if id == "" {
					id = r.Rule.ID
				}
				if idx == nil {
					idx = r.Rule.Index
				}
			}
			var rule *sarifRule
			if idx != nil && *idx >= 0 && *idx < len(d.Rules) {
				rule = &d.Rules[*idx]
			} else {
				rule = byID[id]
			}
			if id == "" && rule != nil {
				id = rule.ID
			}

			f := Finding{Tool: d.Name, RuleID: id, Message: strings.TrimSpace(r.Message.Text)}
			score, level := r.Properties.securitySeverity(), r.Level
			if rule != nil {
				f.Rule = firstText(rule.ShortDescription, rule.Name)
				f.Description = firstText(rule.FullDescription, "")
				f.HelpURI = rule.HelpURI
				if score < 0 {
					score = rule.Properties.securitySeverity()
				}
				if level == "" && rule.Default != nil {
					level = rule.Default.Level
				}
			}
			f.Severity = severity(score, level)
			if score >= 0 {
				f.Score = score
			}
			if f.Message == "" {
				f.Message = f.Rule
			}
			for _, loc := range r.Locations {
				if loc.Physical == nil || loc.Physical.Artifact.URI == "" {
					continue
				}
				base := ""
				if b, ok := run.OriginalURIBaseIDs[loc.Physical.Artifact.URIBaseID]; ok {
					base = b.URI
				}
				f.Path = resolve(loc.Physical.Artifact.URI, base, root)
				if rg := loc.Physical.Region; rg != nil {
					f.Line, f.Column = rg.StartLine, rg.StartColumn
				}
				break
			}
			out = append(out, f)
		}
	}
	return tool, version, out, nil
}

func firstText(t *text, fallback string) string {
	if t != nil && strings.TrimSpace(t.Text) != "" {
		return strings.TrimSpace(t.Text)
	}
	return fallback
}

// resolve makes an artifact URI a path: file: URIs as they are, relative
// ones against their base (originalUriBaseIds) or the scanned folder.
func resolve(uri, base, root string) string {
	p, ok := filePath(uri)
	if !ok {
		return uri // http:, a package URL…: shown as it is
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if b, ok := filePath(base); ok && filepath.IsAbs(b) {
		return filepath.Join(b, p)
	}
	return filepath.Join(root, p)
}

// filePath is the path in a file: or relative URI; false for other schemes.
func filePath(uri string) (string, bool) {
	if uri == "" {
		return "", false
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri, true
	}
	switch u.Scheme {
	case "file":
		return u.Path, true
	case "":
		return u.Path, true
	}
	return "", false
}
