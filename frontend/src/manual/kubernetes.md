# Kubernetes: resources

The Kubernetes tab ({{key:sidebar.kube}}) has two views: **Resources**, the cluster the focused shell points at, and [Contexts](#kube-contexts), the clusters it can point at.

## What it shows

The header names the cluster; below it, the namespace. Sections, grouped:

| Group | Sections |
|---|---|
| Workloads | Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, Pods |
| Network | Services, Ingresses |
| Config | ConfigMaps, Secrets |
| Storage | PersistentVolumeClaims |
| Scaling | HorizontalPodAutoscalers |
| Cluster | Nodes, Events |

## Nothing loads until you ask

Opening the tab only works out the context from your kubeconfig — no request reaches the cluster. **Expand a section** to load it; only expanded sections refresh (at most every 10 seconds), and collapsing one stops it. The namespace list loads when you click the namespace field. Which sections are open is remembered while the window is open.

The tab follows the focused shell's context and namespace. Pick another namespace in the field to look there; your shell keeps its own. kubectl runs with that shell's own `PATH`, `KUBECONFIG` and AWS profile, so login helpers work as they do in the shell.

## Reading what's there

- **Pods**: kubectl's STATUS (Running, CrashLoopBackOff, Init:0/1…), ready containers, restarts (and why the last run ended, e.g. OOMKilled), age. Hover for node, IP, owner and each container's image.
- **Deployments**: ready out of desired, plus any condition that isn't true. **Click a deployment** to show only its pods (asked of the cluster by its labels); the chip on Pods shows all again.
- **Events**: newest first; warnings in red.
- Other kinds show the columns `kubectl get` would, with the full row on hover. A dot shows status where the row has one.

## Acting on resources

Resources named in terminal output (`pod/web-1`, `deployment.apps/api`) have the same Logs, Describe and YAML actions: {{mac:⌘-click|other:Ctrl-click}} or right-click them. See [Logs in the output](#logs).

Right-click for actions. They run as real `kubectl` commands in a new tab, carrying the shell's `KUBECONFIG` and AWS profile and naming the context and namespace, so they do exactly what you were looking at:

- **Pods**: **Logs** (double-click; per container, or from before the last restart), **Shell (exec)**, **Describe**, **Port Forward…**.
- **Port Forward…** (pods, deployments, services) opens the port-forward editor filled in: **Save & Start** keeps it running in the background — reconnecting when the pod restarts — and lists it in the [Network tab's Forwards](#network); **Run in Terminal** runs a one-off `kubectl port-forward` in a new tab instead.
- **Deployments, StatefulSets, DaemonSets**: logs of all pods, rollout status (and history for deployments), **View YAML**, **Describe**.
- **Services**: **Port Forward…** with the service's first port filled in. **Ingresses**: open each host in your browser.
- **Every kind**: **Describe**, **View YAML** (not for secrets), **Copy Name**.

Changes to the cluster — **Delete Pod…**, **Restart…**, **Scale…**, a CronJob's **Run Now…** — show the exact command, context, cluster and namespace, and run only when you confirm.

## Secrets

Secrets are listed by name, type and number of keys. Their values are read only when you ask:

- **View Decoded Values…** (or double-click) reads that one secret. Each key stays masked until you click its eye, or **Reveal All**; the copy button copies a value without showing it (base64 for binary data). Everything masks again when wtf loses focus, so a screen share can't catch it. The values live only in that dialog.
- **Decode in Terminal** prints `key=value` lines in a new tab, using kubectl's own `base64decode`.
