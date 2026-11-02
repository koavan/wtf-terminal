package investigate

import (
	"strings"
	"testing"
)

func TestCheckAccepts(t *testing.T) {
	for in, want := range map[string]string{
		"kubectl get pods -n staging":                   "kubectl get pods -n staging",
		"kubectl -n staging describe pod checkout-1":    "kubectl -n staging describe pod checkout-1",
		"kubectl logs checkout-1 -n staging --previous": "kubectl logs checkout-1 -n staging --previous --tail=200",
		"kubectl logs checkout-1 --tail=50":             "kubectl logs checkout-1 --tail=50",
		"kubectl get events --sort-by=.lastTimestamp":   "kubectl get events --sort-by=.lastTimestamp",
		"kubectl rollout status deploy/checkout":        "kubectl rollout status deploy/checkout --watch=false",
		"kubectl get secrets -n staging":                "kubectl get secrets -n staging",
		"kubectl get deploy checkout -o yaml":           "kubectl get deploy checkout -o yaml",
		"kubectl auth can-i list pods":                  "kubectl auth can-i list pods",
		"helm history checkout -n staging":              "helm history checkout -n staging",
		"helm get notes checkout":                       "helm get notes checkout",
		"aws ec2 describe-instances --region us-east-1": "aws ec2 describe-instances --region us-east-1 --no-cli-pager",
		"aws sts get-caller-identity":                   "aws sts get-caller-identity --no-cli-pager",
		"aws logs tail /aws/lambda/x --since 10m":       "aws logs tail /aws/lambda/x --since 10m --no-cli-pager",
		"aws ecs list-services --cluster prod":          "aws ecs list-services --cluster prod --no-cli-pager",
		"gcloud container clusters describe prod":       "gcloud container clusters describe prod",
		"docker logs web":                               "docker logs web --tail 200",
		"docker stats":                                  "docker stats --no-stream",
		"dig +short api.example.com":                    "dig +short api.example.com",
		"nslookup example.com":                          "nslookup example.com",
		"curl -sI https://example.com/health":           "curl -sI https://example.com/health --max-time 10",
		"curl --head -H X-Debug:1 https://example.com":  "curl --head -H X-Debug:1 https://example.com --max-time 10",
		"ping example.com":                              "ping -c 3 example.com",
		"ping -c 2 example.com":                         "ping -c 2 example.com",
	} {
		c, err := Check(strings.Fields(in))
		if err != nil {
			t.Errorf("%s: refused: %v", in, err)
			continue
		}
		if c.Line != want {
			t.Errorf("%s:\n got %s\nwant %s", in, c.Line, want)
		}
	}
}

func TestCheckRefuses(t *testing.T) {
	for _, in := range []string{
		"kubectl delete pod x",
		"kubectl rollout restart deploy/x",
		"kubectl scale deploy x --replicas=0",
		"kubectl exec -it x -- sh",
		"kubectl apply -f x.yaml",
		"kubectl edit deploy x",
		"kubectl logs -f x",
		"kubectl logs x --follow",
		"kubectl get pods -w",
		"kubectl get pods --context prod",
		"kubectl get pods --kubeconfig /tmp/k",
		"kubectl get pods --as admin",
		"kubectl get secret db -o yaml",
		"kubectl get secrets -ojsonpath={.data}",
		"kubectl get --raw /api",
		"kubectl config view --raw",
		"kubectl cluster-info dump",
		"kubectl port-forward x 8080",
		"helm upgrade x ./chart",
		"helm rollback x 1",
		"helm get values x",
		"helm get manifest x",
		"helm list --kube-context prod",
		"aws ec2 terminate-instances --instance-ids i-1",
		"aws secretsmanager get-secret-value --secret-id x",
		"aws ssm get-parameter --name x --with-decryption",
		"aws ecr get-login-password",
		"aws sts get-session-token",
		"aws ec2 describe-instances --profile prod",
		"aws logs tail /x --follow",
		"aws s3 cp s3://a/b .",
		"gcloud compute instances delete x",
		"gcloud secrets versions access latest --secret x",
		"docker rm x",
		"docker logs -f x",
		"docker exec x sh",
		"curl https://example.com",
		"curl -I -X POST https://example.com",
		"curl -I -d x=1 https://example.com",
		"curl -I -o /tmp/x https://example.com",
		"curl -sIo /tmp/x https://example.com",
		"curl -I -u a:b https://example.com",
		"curl -I file:///etc/passwd",
		"curl -I -K cfg https://example.com",
		"ping -f example.com",
		"ping -c 100 example.com",
		"nslookup",
		"rm -rf /",
		"bash -c ls",
		"sh",
		"/usr/bin/kubectl get pods",
		"terraform apply",
	} {
		if c, err := Check(strings.Fields(in)); err == nil {
			t.Errorf("%s: accepted as %s", in, c.Line)
		}
	}
}

func TestLineQuotes(t *testing.T) {
	c, err := Check([]string{"kubectl", "get", "pods", "-l", "app=checkout,tier in (web)", "-o", "jsonpath={.items[*].status.phase}"})
	if err != nil {
		t.Fatal(err)
	}
	want := `kubectl get pods -l 'app=checkout,tier in (web)' -o 'jsonpath={.items[*].status.phase}'`
	if c.Line != want {
		t.Errorf("line %s", c.Line)
	}
	// Shell syntax stays a literal argument.
	c, err = Check([]string{"dig", "x.com; rm -rf ~", "$(id)", "it's"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Line != `dig 'x.com; rm -rf ~' '$(id)' 'it'\''s'` {
		t.Errorf("line %s", c.Line)
	}
	if _, err := Check([]string{"dig", "a\nrm -rf /"}); err == nil {
		t.Error("a newline was accepted")
	}
}
