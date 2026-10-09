# Network

The Network tab ({{key:sidebar.network}}) lists listening ports and open connections.

- **Terminals** (default) shows only those from programs started in your terminals, labeled with their tab — click the label to jump there. **All** shows everything you own.
- **Listening**: ports with the program and a **local** / **network** pill — *network* means other machines can reach it.
- **Connections**: where each one goes, from which program.
- Right-click: **Open localhost:PORT in Browser**, go to the tab, copy addresses or the PID, or end the process (it asks first).
- Filter by port, host, process or `pid:1234`.

It uses `lsof` (macOS) or `ss` (Linux) and refreshes only while visible and the window is in front.
