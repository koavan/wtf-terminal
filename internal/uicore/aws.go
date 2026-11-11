package uicore

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/awsprofiles"
	"github.com/dayanraj/wtf/internal/infra"
)

// The AWS Profiles tab's rules: row text, tooltips, key ages, and the
// editor's draft (what a profile's form shows, and what it sends back).
// A port of macos/Sources/WtfCore/Aws.swift.

var AWSKindLabel = map[string]string{"keys": "Access keys", "role": "Assume role", "sso": "SSO", "process": "Credential process", "none": "Settings only"}

// AWSField is one config key a kind of profile has, with its form label
// and placeholder.
type AWSField struct{ Key, Label, Placeholder string }

// AWSKindFields are the fields each kind of profile has.
var AWSKindFields = map[string][]AWSField{
	"keys": {},
	"role": {{"role_arn", "Role ARN", "arn:aws:iam::123456789012:role/Admin"},
		{"source_profile", "Source profile", "the profile whose keys assume the role"},
		{"mfa_serial", "MFA device ARN", "optional"}, {"external_id", "External ID", "optional"}},
	"sso": {{"sso_session", "SSO session", "a [sso-session] name in ~/.aws/config"},
		{"sso_account_id", "Account ID", "123456789012"}, {"sso_role_name", "Role name", "AdministratorAccess"},
		{"sso_start_url", "Start URL (without a session)", "optional"},
		{"sso_region", "SSO region (without a session)", "optional"}},
	"process": {{"credential_process", "Command", "/usr/local/bin/get-creds --profile x"}},
}

var AWSRegions = []string{"us-east-1", "us-east-2", "us-west-1", "us-west-2", "ca-central-1", "sa-east-1", "eu-west-1", "eu-west-2",
	"eu-west-3", "eu-central-1", "eu-central-2", "eu-north-1", "eu-south-1", "ap-south-1", "ap-southeast-1",
	"ap-southeast-2", "ap-northeast-1", "ap-northeast-2", "ap-east-1", "me-central-1", "af-south-1"}

var AWSOutputs = []string{"", "json", "yaml", "yaml-stream", "text", "table"}

func awsKindLabel(kind string) string {
	if l, ok := AWSKindLabel[kind]; ok {
		return l
	}
	return kind
}

// AWSShortKey is `AKIA…0001`.
func AWSShortKey(id string) string {
	r := []rune(id)
	if len(r) > 8 {
		return string(r[:4]) + "…" + string(r[len(r)-4:])
	}
	return id
}

// AWSDetail is `AKIA…0001 · 123456789012 · us-east-1`.
func AWSDetail(p awsprofiles.Profile) string {
	kind := awsKindLabel(p.Kind)
	if p.Kind == "keys" {
		kind = AWSShortKey(p.AccessKeyID)
		if p.HasSessionToken {
			kind += " (temporary)"
		}
	}
	return joinDot(kind, p.Account, p.Region)
}

// AWSTooltip lists where a profile lives and what else it holds; pretty
// shortens a path (PrettyPath with the home folder).
func AWSTooltip(p awsprofiles.Profile, credentialsFile, configFile string, pretty func(string) string) string {
	lines := []string{"Kind: " + awsKindLabel(p.Kind)}
	if p.AccessKeyID != "" {
		lines = append(lines, "Access key: "+p.AccessKeyID)
	}
	if p.InCredentials {
		lines = append(lines, "In "+pretty(credentialsFile))
	}
	if p.InConfig {
		lines = append(lines, "In "+pretty(configFile))
	}
	for _, s := range p.CredentialsOther {
		lines = append(lines, s.Key+" = "+s.Value+" (in the credentials file)")
	}
	if len(p.SourceFor) > 0 {
		lines = append(lines, "Source profile for "+strings.Join(p.SourceFor, ", "))
	}
	return strings.Join(lines, "\n")
}

// AWSMatches is the side pane's filter: name, key, region and account.
func AWSMatches(p awsprofiles.Profile, query string) bool {
	q := strings.ToLower(trimBlanks(query))
	return q == "" || strings.Contains(strings.ToLower(p.Name+" "+p.AccessKeyID+" "+p.Region+" "+p.Account), q)
}

var arnPrefix = regexp.MustCompile(`^arn:aws[\w-]*:(iam|sts)::`)

// AWSShortArn is `123456789012:user/ci-bot` from an sts/iam ARN.
func AWSShortArn(arn string) string { return arnPrefix.ReplaceAllString(arn, "") }

// AWSDaysSince is whole days from an RFC 3339 time to now; 0 when it
// isn't one.
func AWSDaysSince(iso string, now time.Time) int {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return 0
	}
	return int(math.Floor(now.Sub(t).Hours() / 24))
}

// AWSAgo is "today", "yesterday" or "N days ago".
func AWSAgo(iso string, now time.Time) string {
	switch d := AWSDaysSince(iso, now); {
	case d <= 0:
		return "today"
	case d == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", d)
	}
}

// AWSKeyLine is an access key's line under its profile, after Check
// Access Keys.
func AWSKeyLine(k awsprofiles.AccessKey, now time.Time) string {
	used := "never used"
	if k.LastUsed != "" {
		used = "used " + AWSAgo(k.LastUsed, now)
		if k.LastUsedService != "" {
			used += " (" + k.LastUsedService + ")"
		}
	}
	this := ""
	if k.Current {
		this = " (this one)"
	}
	return fmt.Sprintf("%s%s · %s · %d days old · %s", AWSShortKey(k.ID), this, strings.ToLower(k.Status), AWSDaysSince(k.Created, now), used)
}

// AWSInUse is the profile the focused shell uses ("" for exported keys);
// ctx may be nil.
func AWSInUse(ctx *infra.Context) string {
	if ctx == nil || ctx.AWSProfile == "env credentials" {
		return ""
	}
	return ctx.AWSProfile
}

func AWSUseCommand(name string) string { return "export AWS_PROFILE=" + ShellQuote(name) }

// AWSDraft is the editor's form, as typed.
type AWSDraft struct {
	Name, Kind                                 string
	AccessKeyID, SecretAccessKey, SessionToken string
	RemoveSessionToken                         bool
	Region, Output                             string
	// The kind's fields (AWSKindFields), by key.
	Fields map[string]string
	// Other settings, one "key = value" a line.
	More string
}

// NewAWSDraft is an empty form for a new profile.
func NewAWSDraft() AWSDraft { return AWSDraft{Kind: "keys", Fields: map[string]string{}} }

// AWSDraftOf fills the form from a profile.
func AWSDraftOf(p awsprofiles.Profile) AWSDraft {
	d := NewAWSDraft()
	d.Name = p.Name
	d.Kind = p.Kind
	d.AccessKeyID = p.AccessKeyID
	setting := func(k string) string {
		for _, s := range p.Settings {
			if strings.ToLower(s.Key) == k {
				return s.Value
			}
		}
		return ""
	}
	d.Region = setting("region")
	d.Output = setting("output")
	known := map[string]bool{"region": true, "output": true}
	for _, f := range AWSKindFields[p.Kind] {
		d.Fields[f.Key] = setting(f.Key)
		known[f.Key] = true
	}
	var more []string
	for _, s := range p.Settings {
		if !known[strings.ToLower(s.Key)] {
			more = append(more, s.Key+" = "+s.Value)
		}
	}
	d.More = strings.Join(more, "\n")
	return d
}

// AWSBadLineError is a "more settings" line that isn't key = value.
type AWSBadLineError struct{ Line string }

func (e AWSBadLineError) Error() string {
	return "“" + e.Line + "” should look like “key = value”"
}

var awsSettingLine = regexp.MustCompile(`^\s*([^=\s]+)\s*=\s*(.*)$`)

// AWSGoDraft is what Save and PlanSave take. It fails on a "more
// settings" line that isn't key = value.
func AWSGoDraft(d AWSDraft, original string, versions map[string]string) (awsprofiles.Draft, error) {
	var settings []awsprofiles.Setting
	if r := trimBlanks(d.Region); r != "" {
		settings = append(settings, awsprofiles.Setting{Key: "region", Value: r})
	}
	if d.Output != "" {
		settings = append(settings, awsprofiles.Setting{Key: "output", Value: d.Output})
	}
	for _, f := range AWSKindFields[d.Kind] {
		if v := trimBlanks(d.Fields[f.Key]); v != "" {
			settings = append(settings, awsprofiles.Setting{Key: f.Key, Value: v})
		}
	}
	for _, line := range strings.Split(d.More, "\n") {
		if trimBlanks(line) == "" {
			continue
		}
		m := awsSettingLine.FindStringSubmatch(line)
		if m == nil || trimBlanks(m[2]) == "" {
			return awsprofiles.Draft{}, AWSBadLineError{trimBlanks(line)}
		}
		settings = append(settings, awsprofiles.Setting{Key: m[1], Value: trimBlanks(m[2])})
	}
	g := awsprofiles.Draft{Original: original, Name: trimBlanks(d.Name), Versions: versions, Settings: settings}
	if d.Kind == "keys" {
		g.UseKeys = true
		g.AccessKeyID = trimBlanks(d.AccessKeyID)
		g.SecretAccessKey = trimBlanks(d.SecretAccessKey)
		g.SessionToken = trimBlanks(d.SessionToken)
		g.RemoveSessionToken = d.RemoveSessionToken
	}
	return g, nil
}
