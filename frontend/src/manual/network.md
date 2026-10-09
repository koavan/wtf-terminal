# Network

The Network tab ({{key:sidebar.network}}) lists your port forwards, listening ports and open connections.

## Forwards

Every port forward wtf runs for you, in one place: [SSH tunnels](#ssh-tunnels) and **kubectl port-forwards**. Each shows where it goes and whether it's on — green when up, yellow while connecting or reconnecting, red with the error when it failed.

- ▶ / ■ starts or stops one; double-click to edit it.
- **+** adds one: an ssh tunnel (local, remote, SOCKS) or a **Kubernetes** forward — context, namespace, target (`svc/web`, `deploy/api`, `pod/name`) and the two ports.
- Right-click: **Open localhost:PORT in Browser**, **Copy localhost:PORT**, **Copy Command**, **Edit…**, **Delete**.

A kubectl forward runs in the background with the `KUBECONFIG`, AWS profile and `PATH` of the shell it was made in, so it reaches the same cluster and its login helper works. It listens on 127.0.0.1 only. When its pod restarts or goes away — which ends a kubectl port-forward — wtf starts it again, waiting longer between tries; one that never connects stops and shows kubectl's error. Like tunnels, forwards stop when wtf quits and can start when it opens.

## Listening and connections

- **Terminals** (default) shows only those from programs started in your terminals, labeled with their tab — click the label to jump there. **All** shows everything you own.
- **Listening**: ports with the program and a **local** / **network** pill — *network* means other machines can reach it. A port that belongs to one of your forwards says **forward · its name**, and shows under Terminals too.
- **Connections**: where each one goes, from which program.
- Right-click: **Open localhost:PORT in Browser**, go to the tab, copy addresses or the PID, or end the process (it asks first).
- Filter by port, host, process or `pid:1234`.

It uses `lsof` (macOS) or `ss` (Linux) and refreshes only while visible and the window is in front.
