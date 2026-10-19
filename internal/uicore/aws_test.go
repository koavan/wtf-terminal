package uicore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/awsprofiles"
	"github.com/dayanraj/wtf/internal/infra"
)

func TestAWSRows(t *testing.T) {
	p := awsprofiles.Profile{Name: "ci", Kind: "keys", AccessKeyID: "AKIAABCDEFGH0001", HasSessionToken: true, Region: "us-east-1"}
	expectText(t, AWSDetail(p), "AKIA…0001 (temporary) · us-east-1")
	p.Kind = "role"
	p.Account = "123456789012"
	p.Region = ""
	expectText(t, AWSDetail(p), "Assume role · 123456789012")
	expectText(t, AWSShortArn("arn:aws:iam::123456789012:user/ci-bot"), "123456789012:user/ci-bot")
	expectText(t, AWSUseCommand("o'k x"), `export AWS_PROFILE='o'\''k x'`)
	if !AWSMatches(p, "1234") || AWSMatches(p, "zzz") {
		t.Error("matches")
	}
	now, _ := time.Parse(time.RFC3339, "2026-10-14T12:00:00Z")
	k := awsprofiles.AccessKey{ID: "AKIAABCDEFGH0001", Status: "Active", Created: "2026-09-14T12:00:00Z",
		LastUsed: "2026-10-13T10:00:00Z", LastUsedService: "s3", Current: true}
	expectText(t, AWSKeyLine(k, now), "AKIA…0001 (this one) · active · 30 days old · used yesterday (s3)")
	expectText(t, AWSAgo("2026-10-14T11:00:00.5Z", now), "today")
	expectText(t, AWSAgo("junk", now), "today")
	expectText(t, AWSInUse(&infra.Context{AWSProfile: "env credentials"}), "")
	expectText(t, AWSInUse(&infra.Context{AWSProfile: "ops"}), "ops")
	expectText(t, AWSInUse(nil), "")
	p.InConfig = true
	p.SourceFor = []string{"a", "b"}
	expectText(t, AWSTooltip(p, "/h/.aws/credentials", "/h/.aws/config", func(s string) string { return PrettyPath(s, "/h") }),
		"Kind: Assume role\nAccess key: AKIAABCDEFGH0001\nIn ~/.aws/config\nSource profile for a, b")
}

func TestAWSDrafts(t *testing.T) {
	p := awsprofiles.Profile{Name: "admin", Kind: "role", Settings: []awsprofiles.Setting{{Key: "Region", Value: "eu-west-1"},
		{Key: "role_arn", Value: "arn:x"}, {Key: "duration_seconds", Value: "3600"}}}
	d := AWSDraftOf(p)
	if d.Region != "eu-west-1" || d.Fields["role_arn"] != "arn:x" || d.More != "duration_seconds = 3600" {
		t.Errorf("draft %+v", d)
	}
	g, err := AWSGoDraft(d, "admin", map[string]string{"/c": "v"})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range g.Settings {
		keys = append(keys, s.Key)
	}
	if !reflect.DeepEqual(keys, []string{"region", "role_arn", "duration_seconds"}) || g.UseKeys || g.AccessKeyID != "" {
		t.Errorf("go draft %+v", g)
	}
	bad := d
	bad.More = "nonsense"
	var badLine AWSBadLineError
	if _, err := AWSGoDraft(bad, "", nil); !errors.As(err, &badLine) || badLine.Line != "nonsense" {
		t.Errorf("bad line: %v", err)
	}
	bad.More = "key ="
	if _, err := AWSGoDraft(bad, "", nil); !errors.As(err, &badLine) {
		t.Errorf("empty value: %v", err)
	}
}
