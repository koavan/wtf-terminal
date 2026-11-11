package kube

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ── Effective permissions (#15) ──────────────────────────────────────────

// Rule is one line of `kubectl auth can-i --list`: what may be done to a
// kind of resource (or a non-resource URL like /healthz).
type Rule struct {
	// A resource ("pods", "deployments.apps", "*.*") or a non-resource URL.
	Resource    string `json:"resource"`
	NonResource bool   `json:"nonResource"`
	// Only these objects, when the rule names them.
	Names []string `json:"names"`
	Verbs []string `json:"verbs"`
}

// Permissions is who the context's credentials are and what they may do
// in a namespace, as the cluster answers (SelfSubjectRulesReview).
type Permissions struct {
	// From `kubectl auth whoami` (Kubernetes 1.28+); "" when the cluster
	// doesn't say.
	User   string   `json:"user"`
	Groups []string `json:"groups"`
	Rules  []Rule   `json:"rules"`
	// Every verb on every resource ("*.*" with "*").
	Admin bool `json:"admin"`
}

// Permissions asks the cluster what the context's user may do in the
// namespace. Only when asked (the tab's Permissions section is open); it
// reads, and changes nothing.
func (s *Service) Permissions(r Request) (Permissions, error) {
	p := Permissions{Groups: []string{}, Rules: []Rule{}}
	if r.Context == "" || r.Namespace == "" {
		return p, errors.New("no context and namespace to read from")
	}
	if err := validName("context", r.Context); err != nil {
		return p, err
	}
	if err := validName("namespace", r.Namespace); err != nil {
		return p, err
	}
	bin, err := s.kubectl(r)
	if err != nil {
		return p, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	env := s.env(r)
	out, err := runKubectl(ctx, bin, env, "auth", "can-i", "--list", "--context", r.Context, "--namespace", r.Namespace, "--request-timeout", "15s")
	if err != nil {
		return p, err
	}
	p.Rules = parseRules(string(out))
	for _, rule := range p.Rules {
		if rule.Resource == "*.*" && len(rule.Names) == 0 && contains(rule.Verbs, "*") {
			p.Admin = true
		}
	}
	// Older clusters (and some proxies) don't have whoami: that's fine.
	if out, err := runKubectl(ctx, bin, env, "auth", "whoami", "--context", r.Context, "--request-timeout", "10s", "-o", "json"); err == nil {
		p.User, p.Groups = parseWhoami(out)
	}
	return p, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// parseRules reads can-i --list's table:
//
//	Resources          Non-Resource URLs   Resource Names   Verbs
//	pods               []                  []               [get list watch]
//	                   [/healthz]          []               [get]
func parseRules(text string) []Rule {
	t := parseTable(text)
	col := func(name string) int {
		for i, c := range t.Columns {
			if strings.EqualFold(c, name) {
				return i
			}
		}
		return -1
	}
	res, urls, names, verbs := col("Resources"), col("Non-Resource URLs"), col("Resource Names"), col("Verbs")
	rules := []Rule{}
	if verbs < 0 {
		return rules
	}
	cell := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return row[i]
	}
	for _, row := range t.Rows {
		rule := Rule{Names: bracketList(cell(row, names)), Verbs: bracketList(cell(row, verbs))}
		if r := cell(row, res); r != "" {
			rule.Resource = r
		} else if u := bracketList(cell(row, urls)); len(u) > 0 {
			rule.Resource, rule.NonResource = strings.Join(u, " "), true
		} else {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

// bracketList reads kubectl's "[get list watch]".
func bracketList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	out := strings.Fields(s)
	if out == nil {
		out = []string{}
	}
	return out
}

func parseWhoami(out []byte) (string, []string) {
	var r struct {
		Status struct {
			UserInfo struct {
				Username string   `json:"username"`
				Groups   []string `json:"groups"`
			} `json:"userInfo"`
		} `json:"status"`
	}
	if json.Unmarshal(out, &r) != nil {
		return "", []string{}
	}
	g := r.Status.UserInfo.Groups
	if g == nil {
		g = []string{}
	}
	return r.Status.UserInfo.Username, g
}
