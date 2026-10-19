package uicore

import (
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/kube"
)

func TestKubectlCommands(t *testing.T) {
	full := KubeTarget{Context: "prod", Namespace: "shop", Env: map[string]string{"KUBECONFIG": "/work/kube config", "AWS_PROFILE": "devops"}}
	expectText(t, full.Logs("web-1", "app", false), "KUBECONFIG='/work/kube config' AWS_PROFILE=devops kubectl --context prod -n shop logs -f --tail=200 web-1 -c app")
	bare := KubeTarget{Context: "prod", Namespace: "shop"}
	expectText(t, bare.TriggerCronJob("backup", time.UnixMilli(1_700_000_000_000)), "kubectl --context prod -n shop create job --from=cronjob/backup backup-manual-loyw3v28")
	expectText(t, KubeTarget{Context: "arn:aws:eks:x:1:cluster/a b", Namespace: "shop"}.Restart("statefulset", "db"),
		"kubectl --context 'arn:aws:eks:x:1:cluster/a b' -n shop rollout restart statefulset/db")
	expectText(t, bare.Logs("w", "", true), "kubectl --context prod -n shop logs --previous w")
	expectText(t, bare.Exec("w", ""), "kubectl --context prod -n shop exec -it w -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'")
	expectText(t, bare.Scale("deployment", "api", 3), "kubectl --context prod -n shop scale deployment/api --replicas=3")
}

func TestPodsAndRows(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	created := now.Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	p := kube.Pod{Name: "web-1", Status: "Running", Ready: 1, Total: 2, Restarts: 2, Created: created,
		Containers: []kube.Container{{Name: "app", LastReason: "OOMKilled"}}}
	if PodTone(p) != ToneWait {
		t.Error("running, half ready")
	}
	expectText(t, PodDetail(p, now), "Running · 1/2 ready · 2 restarts (OOMKilled) · 3h")
	p.Status = "CrashLoopBackOff"
	if PodTone(p) != ToneBad {
		t.Error("crash loop")
	}
	p.Status = "Completed"
	if PodTone(p) != ToneDone {
		t.Error("completed")
	}
	expectText(t, KubeAge(created, now), "3h")
	expectText(t, KubeAge("", now), "")
	if tone, ok := RowTone("statefulsets", map[string]string{"READY": "0/3"}); !ok || tone != ToneBad {
		t.Error("statefulset 0/3")
	}
	if tone, ok := RowTone("nodes", map[string]string{"STATUS": "NotReady,SchedulingDisabled"}); !ok || tone != ToneBad {
		t.Error("node not ready")
	}
	if _, ok := RowTone("configmaps", map[string]string{"DATA": "2"}); ok {
		t.Error("configmaps have no tone")
	}
	expectText(t, RowSummary("services", map[string]string{"TYPE": "ClusterIP", "CLUSTER-IP": "10.0.0.1", "EXTERNAL-IP": "<none>", "PORT(S)": "80/TCP", "AGE": "3d"}),
		"ClusterIP · 10.0.0.1 · 80/TCP · 3d")
	expectText(t, FirstPort("80:30080/TCP"), "80")
	expectText(t, FirstPort(""), "")
	d := kube.Deployment{Desired: 3, Ready: 0, UpToDate: 2, Created: created}
	if DeploymentTone(d) != ToneBad {
		t.Error("deployment 0/3")
	}
	expectText(t, DeploymentDetail(d, now), "0/3 ready · 2 up to date · 3h")
}

func TestContexts(t *testing.T) {
	c := kube.KubeContext{Name: "dev", Namespace: "api", Server: "https://ABC.gr7.us-west-2.eks.amazonaws.com", Auth: "exec",
		ExecCommand: "/usr/local/bin/aws", ExecArgs: []string{"eks", "get-token"}, EKSCluster: "sales", EKSRegion: "us-west-2", AWSProfile: "devops"}
	expectText(t, ContextDetail(c), "sales (EKS us-west-2) · ns api · aws eks get-token · profile devops")
	expectText(t, AuthLabel(kube.KubeContext{Auth: "client-cert"}), "client certificate")
	expectText(t, ClusterLabel(kube.KubeContext{Cluster: "kind", Server: "https://127.0.0.1:6443"}), "127.0.0.1")
	if !ChangesKubeconfig("aws eks update-kubeconfig --name x") || ChangesKubeconfig("kubectl get pods") {
		t.Error("ChangesKubeconfig")
	}
	expectText(t, EKSAdd("sales", "us-west-2", "devops", "", "/home/me/.kube/config"),
		"aws eks update-kubeconfig --name sales --region us-west-2 --profile devops --kubeconfig /home/me/.kube/config")
}
