import Foundation
import Testing
@testable import WtfCore

private let t = Kube.Target(context: "prod", namespace: "shop", env: ["KUBECONFIG": "/work/kube config", "AWS_PROFILE": "devops"])

@Test func kubectlCommands() {
    #expect(Kube.logs(t, pod: "web-1", container: "app") == "KUBECONFIG='/work/kube config' AWS_PROFILE=devops kubectl --context prod -n shop logs -f --tail=200 web-1 -c app")
    let bare = Kube.Target(context: "prod", namespace: "shop")
    #expect(Kube.triggerCronJob(bare, "backup", now: 1_700_000_000_000) == "kubectl --context prod -n shop create job --from=cronjob/backup backup-manual-loyw3v28")
    #expect(Kube.restart(Kube.Target(context: "arn:aws:eks:x:1:cluster/a b", namespace: "shop"), "statefulset", "db")
            == "kubectl --context 'arn:aws:eks:x:1:cluster/a b' -n shop rollout restart statefulset/db")
    #expect(Kube.logs(bare, pod: "w", previous: true) == "kubectl --context prod -n shop logs --previous w")
    #expect(Kube.exec(bare, pod: "w") == "kubectl --context prod -n shop exec -it w -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'")
    #expect(Kube.scale(bare, "deployment", "api", 3) == "kubectl --context prod -n shop scale deployment/api --replicas=3")
}

@Test func podsAndRows() {
    let now = Date(timeIntervalSince1970: 1_000_000)
    let created = ISO8601DateFormatter().string(from: now.addingTimeInterval(-3 * 3600))
    var p = Go.kube.Pod(name: "web-1", status: "Running", ready: 1, total: 2, restarts: 2, created: created,
                        containers: [.init(name: "app", lastReason: "OOMKilled")])
    #expect(Kube.tone(p) == .wait)
    #expect(Kube.podDetail(p, now: now) == "Running · 1/2 ready · 2 restarts (OOMKilled) · 3h")
    p.status = "CrashLoopBackOff"
    #expect(Kube.tone(p) == .bad)
    p.status = "Completed"
    #expect(Kube.tone(p) == .done)
    #expect(Kube.age(created, now: now) == "3h" && Kube.age("", now: now) == "")
    #expect(Kube.rowTone("statefulsets", ["READY": "0/3"]) == .bad)
    #expect(Kube.rowTone("nodes", ["STATUS": "NotReady,SchedulingDisabled"]) == .bad)
    #expect(Kube.rowTone("configmaps", ["DATA": "2"]) == nil)
    #expect(Kube.rowSummary("services", ["TYPE": "ClusterIP", "CLUSTER-IP": "10.0.0.1", "EXTERNAL-IP": "<none>", "PORT(S)": "80/TCP", "AGE": "3d"])
            == "ClusterIP · 10.0.0.1 · 80/TCP · 3d")
    #expect(Kube.firstPort("80:30080/TCP") == "80" && Kube.firstPort("") == "")
    let d = Go.kube.Deployment(desired: 3, ready: 0, upToDate: 2, created: created)
    #expect(Kube.tone(d) == .bad && Kube.deploymentDetail(d, now: now) == "0/3 ready · 2 up to date · 3h")
}

@Test func contexts() {
    let c = Go.kube.KubeContext(name: "dev", namespace: "api", server: "https://ABC.gr7.us-west-2.eks.amazonaws.com", auth: "exec",
                                execCommand: "/usr/local/bin/aws", execArgs: ["eks", "get-token"], eksCluster: "sales", eksRegion: "us-west-2", awsProfile: "devops")
    #expect(Kube.contextDetail(c) == "sales (EKS us-west-2) · ns api · aws eks get-token · profile devops")
    #expect(Kube.authLabel(.init(auth: "client-cert")) == "client certificate")
    #expect(Kube.clusterLabel(.init(cluster: "kind", server: "https://127.0.0.1:6443")) == "127.0.0.1")
    #expect(Kube.changesKubeconfig("aws eks update-kubeconfig --name x") && !Kube.changesKubeconfig("kubectl get pods"))
    #expect(Kube.eksAdd(cluster: "sales", region: "us-west-2", profile: "devops", alias: "", file: "/home/me/.kube/config")
            == "aws eks update-kubeconfig --name sales --region us-west-2 --profile devops --kubeconfig /home/me/.kube/config")
}

@Test func ruleDetail() {
    #expect(Kube.ruleDetail(Go.kube.Rule(resource: "pods", verbs: ["get", "list"])) == "get, list")
    #expect(Kube.ruleDetail(Go.kube.Rule(resource: "*.*", verbs: ["*"])) == "all verbs")
    #expect(Kube.ruleDetail(Go.kube.Rule(resource: "configmaps", names: ["app"], verbs: ["get"])) == "get · only: app")
}
