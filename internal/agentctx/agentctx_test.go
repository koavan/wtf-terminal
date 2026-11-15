package agentctx

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dayanraj/wtf/internal/infra"
)

// fake resolves contexts from the variables alone.
type fake struct{}

var kubeDefs = map[string][2]string{
	"dev-eks":  {"arn:aws:eks:eu-west-1:111:cluster/dev", "dev"},
	"prod-eks": {"arn:aws:eks:eu-west-1:222:cluster/prod", ""},
	"kind":     {"kind", "default"},
}

var accounts = map[string]string{"dev": "111111111111", "prod": "222222222222"}

func (fake) Resolve(env map[string]string) infra.Context {
	var c infra.Context
	if env["KUBECONFIG"] != "/other" {
		c.KubeContext = "dev-eks"
	} else {
		c.KubeContext = "kind"
	}
	c.KubeCluster, c.KubeNamespace = kubeDefs[c.KubeContext][0], kubeDefs[c.KubeContext][1]
	switch {
	case env["WTF_AWS_ENV_KEYS"] != "":
		c.AWSProfile, c.AWSRegion = infra.EnvCredentials, env["AWS_REGION"]
	case env["AWS_PROFILE"] != "":
		c.AWSProfile = env["AWS_PROFILE"]
		c.AWSAccount = accounts[c.AWSProfile]
		c.AWSRegion = env["AWS_REGION"]
		if c.AWSRegion == "" {
			c.AWSRegion = "eu-west-1"
		}
	}
	c.GCPProject = env["CLOUDSDK_CORE_PROJECT"]
	if c.GCPProject == "" && env["CLOUDSDK_ACTIVE_CONFIG_NAME"] == "other" {
		c.GCPProject = "other-project"
	}
	if strings.HasPrefix(env["PWD"], "/repo") {
		c.Git = &infra.Git{Branch: "feature", Upstream: "origin/feature", Remote: "origin"}
	}
	return c
}

func (fake) KubeContext(env map[string]string, name string) (string, string, bool) {
	d, ok := kubeDefs[name]
	return d[0], d[1], ok
}

func (fake) DockerContext(env map[string]string, name string) (string, string) {
	if name == "" {
		if h := env["DOCKER_HOST"]; h != "" && env["DOCKER_CONTEXT"] == "" {
			return "", h
		}
		name = env["DOCKER_CONTEXT"]
	}
	switch name {
	case "", "default":
		return "default", "the local daemon"
	case "prod":
		return "prod", "ssh://deploy@prod-1"
	}
	return name, ""
}

func (fake) AzureSubscription(env map[string]string, want string) (string, string, bool) {
	switch want {
	case "", "Dev", "1111":
		return "Dev", "1111", true
	case "Prod", "2222":
		return "Prod", "2222", true
	}
	return "", "", false
}

func TestOf(t *testing.T) {
	sh := Shell{Env: map[string]string{"AWS_PROFILE": "dev", "PATH": "/bin"}, Cwd: "/repo/app"}
	cases := []struct {
		cmd  string
		want []string
	}{
		{"ls -la", nil},
		{"go test ./...", nil},
		{"kubectl get pods", []string{"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace dev"}},
		{"kubectl --context prod-eks -n payments rollout restart deploy/api",
			[]string{"kubectl: context prod-eks (from --context), cluster arn:aws:eks:eu-west-1:222:cluster/prod (from --context), namespace payments (from -n)"}},
		{"kubectl --context=kind delete pod x", []string{"kubectl: context kind (from --context), namespace default (from --context)"}},
		{"kubectl get pods -A", []string{"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace all namespaces (from -A)"}},
		{"kubectl -npayments get pods", []string{"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace payments (from -n)"}},
		{"KUBECONFIG=/other kubectl get ns", []string{"kubectl: context kind (from KUBECONFIG=), namespace default (from KUBECONFIG=)"}},
		{"kubectl --context nope get ns", []string{"kubectl: context nope, which isn't in the kubeconfig files (from --context)"}},
		{"kubectl --context $CTX get pods", []string{"kubectl: context not known from this command (from --context)"}},
		{"kubectl -n \"$NS\" get pods", []string{"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace not known from this command (from -n)"}},
		{"kubectl config use-context prod-eks && kubectl delete pod x", []string{
			"kubectl: switches context to prod-eks, context prod-eks (from use-context), cluster arn:aws:eks:eu-west-1:222:cluster/prod (from use-context), namespace default (from use-context)"}},
		{"kubectl version --client", nil},
		{"helm --kube-context prod-eks upgrade api ./chart -n api", []string{
			"helm: context prod-eks (from --kube-context), cluster arn:aws:eks:eu-west-1:222:cluster/prod (from --kube-context), namespace api (from -n)"}},
		{"helm template ./chart", nil},
		{"aws s3 ls", []string{"aws: profile dev, account 111111111111, region eu-west-1"}},
		{"aws --profile prod --region us-east-1 s3 rm s3://b/x", []string{"aws: profile prod (from --profile), account 222222222222 (from --profile), region us-east-1 (from --region)"}},
		{"AWS_PROFILE=prod aws sts get-caller-identity", []string{"aws: profile prod (from AWS_PROFILE=), account 222222222222 (from AWS_PROFILE=), region eu-west-1"}},
		{"export AWS_PROFILE=prod; aws s3 ls", []string{"aws: profile prod, account 222222222222, region eu-west-1"}},
		{"AWS_PROFILE=$(pick) aws s3 ls", []string{"aws: profile not known from this command (from AWS_PROFILE=)"}},
		{"aws-vault exec prod -- aws s3 ls", []string{"aws: profile prod (from aws-vault exec), account 222222222222 (from aws-vault exec), region eu-west-1"}},
		{"aws configure list", nil},
		{"sudo -E kubectl get nodes", []string{"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace dev"}},
		{"env AWS_PROFILE=prod timeout 30 aws s3 ls", []string{"aws: profile prod (from AWS_PROFILE=), account 222222222222 (from AWS_PROFILE=), region eu-west-1"}},
		{`bash -c 'kubectl -n x get po'`, []string{"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace x (from -n)"}},
		{`eval "$(make-cmd)" && echo kubectl`, nil},
		{`eval "kubectl delete ns $NS"`, []string{"kubectl: context not known from this command (from eval)"}},
		{"gcloud compute instances list", []string{"gcloud: project none set"}},
		{"gcloud --project p1 compute instances delete vm", []string{"gcloud: project p1 (from --project)"}},
		{"gcloud --configuration other sql instances list", []string{"gcloud: project other-project (from --configuration)"}},
		{"ssh -i ~/.ssh/k -J bastion admin@db1 uptime", []string{"ssh: host admin@db1, through bastion (from -J)"}},
		{"ssh -p 2222 host", []string{"ssh: host host"}},
		{"git status", nil},
		{"git push", []string{"git: branch feature, pushes to origin/feature"}},
		{"git push --force origin main", []string{"git: branch feature, pushes to origin main, force yes (from --force)"}},
		{"git push origin +main", []string{"git: branch feature, pushes to origin +main, force yes (from +main)"}},
		{"git -C /elsewhere push", nil},
		{"kubectl get pods | grep api && aws s3 ls # check", []string{
			"kubectl: context dev-eks, cluster arn:aws:eks:eu-west-1:111:cluster/dev, namespace dev",
			"aws: profile dev, account 111111111111, region eu-west-1"}},
		{"echo 'kubectl delete ns prod'", nil},
		{"docker ps", []string{"docker: context default, daemon the local daemon"}},
		{"docker --context prod compose up -d", []string{"docker: context prod (from --context), daemon ssh://deploy@prod-1 (from --context)"}},
		{"docker -c nope ps", []string{"docker: context nope (from --context), daemon not known: no such context (from --context)"}},
		{"docker run -c 512 alpine", []string{"docker: context default, daemon the local daemon"}},
		{"DOCKER_HOST=tcp://10.0.0.5:2376 docker ps", []string{"docker: daemon tcp://10.0.0.5:2376 (from DOCKER_HOST=)"}},
		{"docker -H ssh://x ps", []string{"docker: daemon ssh://x (from -H)"}},
		{"docker context use prod && docker rm -f api", []string{"docker: switches context to prod, context prod (from context use), daemon ssh://deploy@prod-1 (from context use)"}},
		{"docker context ls", nil},
		{"az vm list", []string{"az: subscription Dev (1111)"}},
		{"az group delete -n rg --subscription Prod", []string{"az: subscription Prod (2222) (from --subscription)"}},
		{"az account set --subscription 2222; az group list", []string{"az: switches subscription to 2222, subscription Prod (2222) (from account set)"}},
		{"az --subscription nope vm list", []string{"az: subscription nope, which isn't in the CLI's profile (from --subscription)"}},
		{"az login", nil},
	}
	for _, c := range cases {
		got := Lines(Of(c.cmd, sh, fake{}))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s\n got  %q\n want %q", c.cmd, got, c.want)
		}
	}
}

func TestEnvKeys(t *testing.T) {
	sh := Shell{Env: map[string]string{"WTF_AWS_ENV_KEYS": "1", "AWS_REGION": "eu-central-1"}}
	if got := Lines(Of("aws s3 ls", sh, fake{})); !slices.Equal(got, []string{"aws: profile env credentials, region eu-central-1"}) {
		t.Errorf("env keys: %q", got)
	}
	// An explicit --profile wins over access keys in the environment.
	if got := Lines(Of("aws --profile prod s3 ls", sh, fake{})); !slices.Equal(got, []string{
		"aws: profile prod (from --profile), account 222222222222 (from --profile), region eu-central-1"}) {
		t.Errorf("--profile over env keys: %q", got)
	}
}

func TestTerraform(t *testing.T) {
	dir := t.TempDir()
	inited := filepath.Join(dir, "live")
	os.MkdirAll(filepath.Join(inited, ".terraform"), 0o755)
	os.WriteFile(filepath.Join(inited, ".terraform", "environment"), []byte("prod\n"), 0o644)
	bare := filepath.Join(dir, "bare")
	os.MkdirAll(filepath.Join(bare, ".terraform"), 0o755)
	sh := Shell{Env: map[string]string{}, Cwd: dir}
	cases := []struct {
		cmd  string
		want []string
	}{
		{"terraform fmt", nil},
		{"terraform plan", []string{"terraform: folder " + dir + ", workspace none: the folder isn't initialized"}},
		{"cd live && terraform apply", []string{"terraform: folder " + inited + " (from cd), workspace prod"}},
		{"terraform -chdir=bare destroy", []string{"terraform: folder " + bare + " (from -chdir), workspace default"}},
		{"TF_WORKSPACE=stage terraform -chdir=live apply", []string{"terraform: folder " + inited + " (from -chdir), workspace stage (from TF_WORKSPACE)"}},
		{"cd $D && terraform apply", []string{"terraform: folder not known from this command (from cd)"}},
		{"terraform -chdir=live workspace select dev", []string{"terraform: folder " + inited + " (from -chdir), switches workspace to dev"}},
	}
	for _, c := range cases {
		got := Lines(Of(c.cmd, sh, fake{}))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s\n got  %q\n want %q", c.cmd, got, c.want)
		}
	}
}

func TestWords(t *testing.T) {
	got := words(`a "b c" 'd $e' "$f" x\ y && z|w; #c`)
	var texts []string
	for _, w := range got {
		s := w.text
		if w.dynamic {
			s += "*"
		}
		texts = append(texts, s)
	}
	want := []string{"a", "b c", "d $e", "$f*", "x y", "&&", "z", "|", "w", ";"}
	if !slices.Equal(texts, want) {
		t.Errorf("words: %q, want %q", texts, want)
	}
}
