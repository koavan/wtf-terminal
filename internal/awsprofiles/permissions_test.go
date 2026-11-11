package awsprofiles

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeIAM answers profile calls by "service operation".
func fakeIAM(t *testing.T, answers map[string]string) *[]string {
	t.Helper()
	old := runAWS
	t.Cleanup(func() { runAWS = old })
	var calls []string
	runAWS = func(_ context.Context, _, _ string, _ []string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		a, ok := answers[args[0]+" "+args[1]]
		if !ok {
			return nil, errors.New("An error occurred (AccessDenied) when calling the X operation: no")
		}
		return []byte(a), nil
	}
	return &calls
}

func TestPermissionsUser(t *testing.T) {
	s := testService(t, "", "[profile dev]\nregion = eu-west-1\n")
	calls := fakeIAM(t, map[string]string{
		"sts get-caller-identity":          `{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/ops/alice","UserId":"AIDA"}`,
		"iam get-user":                     `{"User":{"UserName":"alice","PermissionsBoundary":{"PermissionsBoundaryArn":"arn:aws:iam::123456789012:policy/Boundary"}}}`,
		"iam list-attached-user-policies":  `{"AttachedPolicies":[{"PolicyName":"ReadOnlyAccess","PolicyArn":"arn:aws:iam::aws:policy/ReadOnlyAccess"}]}`,
		"iam list-user-policies":           `{"PolicyNames":["extra"]}`,
		"iam list-groups-for-user":         `{"Groups":[{"GroupName":"devs"}]}`,
		"iam list-attached-group-policies": `{"AttachedPolicies":[{"PolicyName":"Deploy","PolicyArn":"arn:aws:iam::123456789012:policy/Deploy"}]}`,
		// list-group-policies is refused.
	})
	p, err := s.Permissions("dev")
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "user" || p.Name != "alice" || !reflect.DeepEqual(p.Groups, []string{"devs"}) {
		t.Fatalf("%+v", p)
	}
	want := []Policy{
		{Name: "Boundary", Arn: "arn:aws:iam::123456789012:policy/Boundary", Via: "boundary", Owner: "alice"},
		{Name: "ReadOnlyAccess", Arn: "arn:aws:iam::aws:policy/ReadOnlyAccess", Via: "user", Owner: "alice", AWSManaged: true},
		{Name: "extra", Inline: true, Via: "user", Owner: "alice"},
		{Name: "Deploy", Arn: "arn:aws:iam::123456789012:policy/Deploy", Via: "group", Owner: "devs"},
	}
	if !reflect.DeepEqual(p.Policies, want) {
		t.Fatalf("policies:\n%+v", p.Policies)
	}
	if !reflect.DeepEqual(p.Problems, []string{"iam:ListGroupPolicies: AccessDenied"}) {
		t.Fatalf("problems: %q", p.Problems)
	}
	if !strings.Contains((*calls)[0], "--region eu-west-1 --profile dev") {
		t.Fatalf("first call: %s", (*calls)[0])
	}
}

func TestPermissionsRoleAndRoot(t *testing.T) {
	s := testService(t, "", "")
	fakeIAM(t, map[string]string{
		"sts get-caller-identity":         `{"Account":"123456789012","Arn":"arn:aws:sts::123456789012:assumed-role/AWSReservedSSO_Admin_abc/alice@example.com","UserId":"AROA"}`,
		"iam get-role":                    `{"Role":{"RoleName":"AWSReservedSSO_Admin_abc"}}`,
		"iam list-attached-role-policies": `{"AttachedPolicies":[{"PolicyName":"AdministratorAccess","PolicyArn":"arn:aws:iam::aws:policy/AdministratorAccess"}]}`,
		"iam list-role-policies":          `{"PolicyNames":[]}`,
	})
	p, err := s.Permissions("sso")
	if err != nil || p.Kind != "role" || p.Name != "AWSReservedSSO_Admin_abc" || p.Session != "alice@example.com" || len(p.Policies) != 1 || len(p.Problems) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	fakeIAM(t, map[string]string{"sts get-caller-identity": `{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:root","UserId":"1"}`})
	if p, err := s.Permissions("root"); err != nil || p.Kind != "root" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestPolicyDocument(t *testing.T) {
	s := testService(t, "", "")
	calls := fakeIAM(t, map[string]string{
		"iam get-policy":         `{"Policy":{"DefaultVersionId":"v3"}}`,
		"iam get-policy-version": `{"PolicyVersion":{"Document":{"Version":"2012-10-17","Statement":[]}}}`,
		"iam get-role-policy":    `{"PolicyDocument":"%7B%22Version%22%3A%222012-10-17%22%7D"}`,
	})
	doc, err := s.PolicyDocument("p", Policy{Name: "X", Arn: "arn:aws:iam::aws:policy/X"})
	if err != nil || !strings.Contains(doc, "\n  \"Statement\": []") {
		t.Fatalf("%q %v", doc, err)
	}
	if !strings.Contains((*calls)[1], "--version-id v3") {
		t.Fatalf("calls: %q", *calls)
	}
	doc, err = s.PolicyDocument("p", Policy{Name: "inl", Inline: true, Via: "role", Owner: "r"})
	if err != nil || doc != "{\n  \"Version\": \"2012-10-17\"\n}" {
		t.Fatalf("%q %v", doc, err)
	}
	if _, err := s.PolicyDocument("p", Policy{Name: "--x y", Inline: true, Via: "role", Owner: "r"}); err == nil {
		t.Fatal("bad name accepted")
	}
}
