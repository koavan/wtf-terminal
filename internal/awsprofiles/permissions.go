package awsprofiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ── Effective permissions (#15) ──────────────────────────────────────────
//
// Who a profile's credentials are, and the IAM policies that apply to that
// user or role: attached (managed) and inline, the user's groups' too, and
// the permissions boundary. Read only when asked; nothing is changed. Each
// call that IAM refuses is reported as a problem rather than failing the
// whole answer, since a user may read some of these and not others.

// Policy is one IAM policy that applies to the identity.
type Policy struct {
	Name string `json:"name"`
	// Managed policies only; inline ones have none.
	Arn    string `json:"arn"`
	Inline bool   `json:"inline"`
	// Where it's attached: "user", "group", "role" or "boundary".
	Via string `json:"via"`
	// The user, group or role it's attached to.
	Owner string `json:"owner"`
	// AWS managed (arn:aws:iam::aws:policy/…) rather than the account's own.
	AWSManaged bool `json:"awsManaged"`
}

// Permissions is the identity and what's attached to it.
type Permissions struct {
	Identity Identity `json:"identity"`
	// "user", "role", "root" or "other" (federated users and the like).
	Kind string `json:"kind"`
	// The IAM user or role name.
	Name string `json:"name"`
	// For an assumed role, its session name.
	Session  string   `json:"session"`
	Groups   []string `json:"groups"`
	Policies []Policy `json:"policies"`
	// What IAM refused to say (e.g. "iam:ListAttachedRolePolicies: AccessDenied").
	Problems []string `json:"problems"`
}

var (
	userArn = regexp.MustCompile(`^arn:aws[\w-]*:iam::\d{12}:user/(?:.*/)?([^/]+)$`)
	roleArn = regexp.MustCompile(`^arn:aws[\w-]*:sts::\d{12}:assumed-role/([^/]+)/(.+)$`)
	rootArn = regexp.MustCompile(`^arn:aws[\w-]*:iam::\d{12}:root$`)
	iamName = regexp.MustCompile(`^[\w+=,.@-]{1,128}$`)
)

// Permissions asks AWS who a profile is and which policies apply to it.
func (s *Service) Permissions(profile string) (Permissions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*awsTimeout)
	defer cancel()
	p := Permissions{Groups: []string{}, Policies: []Policy{}, Problems: []string{}}
	out, err := s.aws(ctx, profile, "sts", "get-caller-identity", "--region", s.regionFor(profile))
	if err != nil {
		return p, err
	}
	if p.Identity, err = decodeIdentity(out); err != nil {
		return p, err
	}
	iam := func(action string, into any, args ...string) bool {
		out, err := s.aws(ctx, profile, append([]string{"iam"}, args...)...)
		if err != nil {
			p.Problems = append(p.Problems, "iam:"+action+": "+shortErr(err))
			return false
		}
		if json.Unmarshal(out, into) != nil {
			p.Problems = append(p.Problems, "iam:"+action+": unexpected answer")
			return false
		}
		return true
	}
	type attached struct {
		AttachedPolicies []struct{ PolicyName, PolicyArn string }
	}
	type inline struct{ PolicyNames []string }
	addAttached := func(a attached, via, owner string) {
		for _, x := range a.AttachedPolicies {
			p.Policies = append(p.Policies, Policy{Name: x.PolicyName, Arn: x.PolicyArn, Via: via, Owner: owner, AWSManaged: awsManaged(x.PolicyArn)})
		}
	}
	addInline := func(in inline, via, owner string) {
		for _, n := range in.PolicyNames {
			p.Policies = append(p.Policies, Policy{Name: n, Inline: true, Via: via, Owner: owner})
		}
	}
	boundary := func(arn string) {
		if arn != "" {
			p.Policies = append(p.Policies, Policy{Name: arn[strings.LastIndex(arn, "/")+1:], Arn: arn, Via: "boundary", Owner: p.Name, AWSManaged: awsManaged(arn)})
		}
	}
	arn := p.Identity.Arn
	switch {
	case rootArn.MatchString(arn):
		p.Kind = "root"
	case userArn.MatchString(arn):
		p.Kind, p.Name = "user", userArn.FindStringSubmatch(arn)[1]
		var u struct {
			User struct {
				PermissionsBoundary struct{ PermissionsBoundaryArn string }
			}
		}
		if iam("GetUser", &u, "get-user", "--user-name", p.Name) {
			boundary(u.User.PermissionsBoundary.PermissionsBoundaryArn)
		}
		var a attached
		if iam("ListAttachedUserPolicies", &a, "list-attached-user-policies", "--user-name", p.Name) {
			addAttached(a, "user", p.Name)
		}
		var in inline
		if iam("ListUserPolicies", &in, "list-user-policies", "--user-name", p.Name) {
			addInline(in, "user", p.Name)
		}
		var g struct{ Groups []struct{ GroupName string } }
		if iam("ListGroupsForUser", &g, "list-groups-for-user", "--user-name", p.Name) {
			for _, grp := range g.Groups {
				p.Groups = append(p.Groups, grp.GroupName)
				var a attached
				if iam("ListAttachedGroupPolicies", &a, "list-attached-group-policies", "--group-name", grp.GroupName) {
					addAttached(a, "group", grp.GroupName)
				}
				var in inline
				if iam("ListGroupPolicies", &in, "list-group-policies", "--group-name", grp.GroupName) {
					addInline(in, "group", grp.GroupName)
				}
			}
		}
	case roleArn.MatchString(arn):
		m := roleArn.FindStringSubmatch(arn)
		p.Kind, p.Name, p.Session = "role", m[1], m[2]
		var r struct {
			Role struct {
				PermissionsBoundary struct{ PermissionsBoundaryArn string }
			}
		}
		if iam("GetRole", &r, "get-role", "--role-name", p.Name) {
			boundary(r.Role.PermissionsBoundary.PermissionsBoundaryArn)
		}
		var a attached
		if iam("ListAttachedRolePolicies", &a, "list-attached-role-policies", "--role-name", p.Name) {
			addAttached(a, "role", p.Name)
		}
		var in inline
		if iam("ListRolePolicies", &in, "list-role-policies", "--role-name", p.Name) {
			addInline(in, "role", p.Name)
		}
	default:
		p.Kind = "other"
	}
	return p, nil
}

func awsManaged(arn string) bool { return strings.Contains(arn, ":iam::aws:policy/") }

// shortErr keeps the useful part of an aws CLI error: "An error occurred
// (AccessDenied) when calling the X operation: User: … is not authorized…"
// becomes "AccessDenied".
func shortErr(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "An error occurred ("); i >= 0 {
		rest := msg[i+len("An error occurred ("):]
		if j := strings.Index(rest, ")"); j > 0 {
			return rest[:j]
		}
	}
	return msg
}

// PolicyDocument is one policy's JSON, pretty-printed: a managed policy's
// default version, or an inline policy as attached.
func (s *Service) PolicyDocument(profile string, p Policy) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), awsTimeout)
	defer cancel()
	var doc json.RawMessage
	if !p.Inline {
		if !strings.HasPrefix(p.Arn, "arn:aws") || strings.ContainsAny(p.Arn, " \t\n") {
			return "", fmt.Errorf("%q isn't a policy ARN", p.Arn)
		}
		out, err := s.aws(ctx, profile, "iam", "get-policy", "--policy-arn", p.Arn)
		if err != nil {
			return "", err
		}
		var gp struct {
			Policy struct{ DefaultVersionId string }
		}
		if json.Unmarshal(out, &gp) != nil || gp.Policy.DefaultVersionId == "" {
			return "", errors.New("unexpected answer from AWS (get-policy)")
		}
		out, err = s.aws(ctx, profile, "iam", "get-policy-version", "--policy-arn", p.Arn, "--version-id", gp.Policy.DefaultVersionId)
		if err != nil {
			return "", err
		}
		var v struct {
			PolicyVersion struct{ Document json.RawMessage }
		}
		if json.Unmarshal(out, &v) != nil {
			return "", errors.New("unexpected answer from AWS (get-policy-version)")
		}
		doc = v.PolicyVersion.Document
	} else {
		if !iamName.MatchString(p.Name) || !iamName.MatchString(p.Owner) {
			return "", fmt.Errorf("%q isn't a policy name", p.Name)
		}
		var args []string
		switch p.Via {
		case "user":
			args = []string{"get-user-policy", "--user-name", p.Owner}
		case "group":
			args = []string{"get-group-policy", "--group-name", p.Owner}
		case "role":
			args = []string{"get-role-policy", "--role-name", p.Owner}
		default:
			return "", fmt.Errorf("unknown policy owner %q", p.Via)
		}
		out, err := s.aws(ctx, profile, append(append([]string{"iam"}, args...), "--policy-name", p.Name)...)
		if err != nil {
			return "", err
		}
		var v struct{ PolicyDocument json.RawMessage }
		if json.Unmarshal(out, &v) != nil {
			return "", errors.New("unexpected answer from AWS")
		}
		doc = v.PolicyDocument
	}
	return prettyDocument(doc), nil
}

// prettyDocument indents a policy. The CLI already decodes it to JSON; an
// older one may leave it URL-encoded in a string.
func prettyDocument(doc json.RawMessage) string {
	var str string
	if json.Unmarshal(doc, &str) == nil {
		if dec, err := url.QueryUnescape(str); err == nil {
			str = dec
		}
		doc = json.RawMessage(str)
	}
	var buf bytes.Buffer
	if json.Indent(&buf, doc, "", "  ") != nil {
		return string(doc)
	}
	return buf.String()
}
