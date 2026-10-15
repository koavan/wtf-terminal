import AppKit
import WtfCore

/// The SSH tab's dialogs: host editor, scp transfer, key install, key
/// generation in a terminal, tunnel editor, host keys and git identities.
/// Strings and rules follow the Svelte build's dialogs.

// MARK: - Host editor

@MainActor
enum HostEditor {
    static func show(_ h: SshHost?, prefill: HostDraft? = nil, in w: WindowController) {
        let store = SshStore.shared
        let main = store.list.mainConfig.isEmpty ? "~/.ssh/config" : WindowModel.prettyPath(store.list.mainConfig)
        let title = h != nil ? "Edit \(h!.alias)" : (prefill?.hostName.isEmpty == false ? "Save Known Host" : "New SSH Host")
        let sub = h.map { "In \(WindowModel.prettyPath($0.source)), line \($0.line)" } ?? "Added to \(main)"
        let s = FormSheet(title: title, subtitle: sub, width: 480)
        var d = HostDraft(h)
        if let prefill { d = prefill }
        func inherited(_ key: String, _ value: String, _ fallback: String) -> String {
            guard let h, !h.options.contains(where: { $0.key.lowercased() == key.lowercased() }), !value.isEmpty, value != h.alias else { return fallback }
            return "\(value) (from another entry)"
        }
        let name = s.text(d.name, placeholder: "web1")
        let address = s.text(d.hostName, placeholder: inherited("HostName", h?.hostName ?? "", "web1.example.com or 10.0.0.5"))
        let user = s.text(d.user, placeholder: inherited("User", h?.user ?? "", "your user name"))
        let port = s.text(d.port, placeholder: inherited("Port", h?.port ?? "", "22"))
        let key = NSComboBox()
        key.addItems(withObjectValues: store.keyFiles)
        key.stringValue = d.identityFile
        key.placeholderString = "your default keys"
        let jump = NSComboBox()
        jump.addItems(withObjectValues: store.list.hosts.filter { !$0.known && $0.alias != h?.alias }.map(\.alias))
        jump.stringValue = d.proxyJump
        jump.placeholderString = inherited("ProxyJump", h?.proxyJump ?? "", "none")
        let more = NSTextView()
        more.string = d.more
        more.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        more.isRichText = false
        let moreScroll = NSScrollView()
        moreScroll.documentView = more
        moreScroll.hasVerticalScroller = true
        moreScroll.borderType = .bezelBorder
        moreScroll.heightAnchor.constraint(equalToConstant: 60).isActive = true
        more.autoresizingMask = [.width]
        s.field("Name", name, help: "What you type after ssh. Separate several names with spaces.")
        s.field("Address", address)
        port.widthAnchor.constraint(equalToConstant: 80).isActive = true
        func column(_ label: String, _ control: NSView) -> NSStackView {
            let l = NSTextField(labelWithString: label)
            l.font = .systemFont(ofSize: 12, weight: .medium)
            let c = NSStackView(views: [l, control])
            c.orientation = .vertical
            c.alignment = .leading
            c.spacing = 4
            return c
        }
        user.widthAnchor.constraint(equalToConstant: 330).isActive = true
        s.add(NSStackView(views: [column("User", user), column("Port", port)]))
        s.field("Key file", key)
        s.field("Jump host", jump)
        s.field("More settings", moreScroll, help: "One per line, as in an ssh config file. ⌘↩ saves.")

        func save(connect: Bool) {
            var draft = HostDraft()
            draft.name = name.stringValue
            draft.hostName = address.stringValue
            draft.user = user.stringValue
            draft.port = port.stringValue
            draft.identityFile = key.stringValue
            draft.proxyJump = jump.stringValue
            draft.more = more.string
            let options: [Go.sshhosts.Option]
            do { options = try draft.options() } catch {
                s.error = error.localizedDescription
                return
            }
            let patterns = draft.name.trimmingCharacters(in: .whitespaces).split(whereSeparator: \.isWhitespace).map(String.init)
            let file = h?.source ?? ""
            let entry = Go.sshhosts.Entry(file: file, line: h?.line ?? 0,
                                          version: store.list.versions[h?.source ?? store.list.mainConfig] ?? "",
                                          patterns: patterns, options: options)
            Task {
                do {
                    store.list = try await Services.sshhosts.Save(entry)
                    store.changed()
                    s.close()
                    let first = patterns.first ?? ""
                    Toast.show(h != nil ? "Saved \(first)" : "Added \(first) to \(main)", in: w.rootView)
                    if connect, let added = store.list.hosts.first(where: { !$0.known && $0.alias == first }) {
                        w.open(hosts: [added], .here)
                    }
                } catch {
                    s.error = error.localizedDescription
                }
            }
        }
        var list: [(String, Bool, () -> Void)] = []
        if h == nil { list.append(("Save & Connect", false, { save(connect: true) })) }
        list.append(("Save", true, { save(connect: false) }))
        s.buttons(list, left: h.map { host in [("Delete…", { s.close(); confirmDelete(host, in: w) })] } ?? [])
        s.present(in: w.window, focus: name)
    }

    static func confirmDelete(_ h: SshHost, in w: WindowController) {
        let others = h.patterns.filter { $0 != h.alias }
        let msg = "Removes its entry from \(WindowModel.prettyPath(h.source)) (line \(h.line))."
            + (others.isEmpty ? "" : " The entry also names \(others.joined(separator: ", ")), which go with it.")
            + " A copy of the file is kept in wtf’s cache folder."
        Confirm.show(in: w.window, title: "Delete \(h.alias)?", message: msg, button: "Delete Host") {
            let store = SshStore.shared
            Task {
                do {
                    store.list = try await Services.sshhosts.Delete(h.source, h.line, store.list.versions[h.source] ?? "")
                    store.changed()
                    Toast.show("Deleted \(h.alias)", in: w.rootView)
                } catch {
                    Toast.show("Couldn’t delete \(h.alias): \(error.localizedDescription)", in: w.rootView)
                }
            }
        }
    }
}

// MARK: - scp

@MainActor
enum TransferSheet {
    static func show(host h: SshHost, upload: Bool, files: [String], in w: WindowController) {
        let s = FormSheet(title: upload ? "Upload to \(h.alias)" : "Download from \(h.alias)",
                          subtitle: "Copies with scp in your shell (a new tab if it’s busy), where you’ll see its progress")
        var locals = files
        var localDir = NSHomeDirectory() + "/Downloads"
        let preview = s.note("", mono: true)
        let listBox = NSStackView()
        listBox.orientation = .vertical
        listBox.alignment = .leading
        listBox.spacing = 2
        let remote = s.text(placeholder: upload ? "your home folder" : "/var/log/app.log")
        let localLabel = NSTextField(labelWithString: WindowModel.prettyPath(localDir))

        func command() -> String? {
            if upload { return locals.isEmpty ? nil : Ssh.upload(h, locals, remote.stringValue) }
            let r = remote.stringValue.trimmingCharacters(in: .whitespaces)
            return r.isEmpty ? nil : Ssh.download(h, r, localDir)
        }
        func refresh() {
            // One row per file, each with a × to leave it out.
            listBox.arrangedSubviews.forEach { $0.removeFromSuperview() }
            for f in locals {
                let name = NSTextField(labelWithString: WindowModel.prettyPath(f))
                name.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
                name.lineBreakMode = .byTruncatingMiddle
                name.widthAnchor.constraint(lessThanOrEqualToConstant: 330).isActive = true
                let x = ClosureButton(title: "×") {
                    locals.removeAll { $0 == f }
                    refresh()
                }
                x.isBordered = false
                x.setAccessibilityLabel("Remove \(WindowModel.prettyPath(f))")
                x.toolTip = "Remove"
                listBox.addArrangedSubview(NSStackView(views: [name, x]))
            }
            preview.stringValue = command() ?? ""
            s.button(upload ? "Upload" : "Download")?.isEnabled = command() != nil
        }
        func choose() {
            let p = upload ? NSOpenPanel() : NSOpenPanel()
            p.title = upload ? "Upload to \(h.alias)" : "Download into"
            p.prompt = "Choose"
            p.canChooseFiles = upload
            p.canChooseDirectories = true
            p.allowsMultipleSelection = upload
            p.showsHiddenFiles = upload
            p.canCreateDirectories = !upload
            if !upload { p.directoryURL = URL(fileURLWithPath: localDir) }
            p.beginSheetModal(for: s.panel) { r in
                guard r == .OK else { return }
                if upload {
                    for u in p.urls where !locals.contains(u.path) { locals.append(u.path) }
                } else if let u = p.url {
                    localDir = u.path
                    localLabel.stringValue = WindowModel.prettyPath(localDir)
                }
                refresh()
            }
        }
        let chooseButton = ClosureButton(title: "Choose…") { choose() }
        if upload {
            let filesCol = NSStackView(views: [listBox, chooseButton])
            filesCol.orientation = .vertical
            filesCol.alignment = .leading
            s.field("Files and folders", filesCol)
            s.field("Into folder on \(h.alias)", remote, help: "A path on the server, like /tmp or ~/deploy. Empty means your home folder.")
        } else {
            s.field("File or folder on \(h.alias)", remote)
            s.field("Into folder here", NSStackView(views: [localLabel, chooseButton]))
        }
        s.add(preview)
        let obs = FieldWatcher { refresh() }
        remote.delegate = obs
        s.buttons([(upload ? "Upload" : "Download", true, {
            guard let c = command() else { return }
            _ = obs
            s.close()
            w.runInShell(c)
        })])
        s.present(in: w.window, focus: remote)
        refresh()
        if upload, files.isEmpty { choose() }
    }
}

/// Calls back on every edit of a text field.
@MainActor
final class FieldWatcher: NSObject, NSTextFieldDelegate, NSComboBoxDelegate {
    private let changed: () -> Void
    init(_ changed: @escaping () -> Void) { self.changed = changed }
    func controlTextDidChange(_ note: Notification) { changed() }
    func comboBoxSelectionDidChange(_ note: Notification) { DispatchQueue.main.async { MainActor.assumeIsolated { self.changed() } } }
}

// MARK: - Keys

@MainActor
enum KeyInstallSheet {
    static func show(key: Go.sshhosts.Key?, host: SshHost?, in w: WindowController) {
        SshStore.shared.whenLoaded(keys: true, hosts: true) { build(key: key, host: host, in: w) }
    }

    private static func build(key: Go.sshhosts.Key?, host: SshHost?, in w: WindowController) {
        let store = SshStore.shared
        let keys = store.keys.keys.filter { !$0.path.isEmpty }
        let hosts = store.list.hosts
        let s = FormSheet(title: "Install Public Key", subtitle: "Adds the key to the server’s ~/.ssh/authorized_keys, so you can log in with it")
        let keyLabels = keys.map { "\($0.name) — \($0.type)\($0.comment.isEmpty ? "" : " · \($0.comment)")" }
        let defaultKey = key.flatMap { k in keys.firstIndex { $0.path == k.path } } ?? keys.firstIndex(where: \.inAgent) ?? 0
        let keyPop = s.popup(keyLabels, selected: defaultKey)
        let hostPop = s.popup(["Choose a host"] + hosts.map { "\($0.alias)\($0.known && !$0.port.isEmpty ? ":" + $0.port : "")" },
                              selected: host.flatMap { h in hosts.firstIndex { Ssh.key($0) == Ssh.key(h) }.map { $0 + 1 } } ?? 0)
        hostPop.item(at: 0)?.isEnabled = false
        let preview = s.note("", mono: true)
        func current() -> (Go.sshhosts.Key, SshHost)? {
            let ki = keyPop.indexOfSelectedItem, hi = hostPop.indexOfSelectedItem - 1
            guard ki >= 0, ki < keys.count, hi >= 0, hi < hosts.count else { return nil }
            return (keys[ki], hosts[hi])
        }
        func refresh() {
            preview.stringValue = current().map { Ssh.installKey($0.0.path, $0.1) } ?? ""
            s.button("Install in Terminal")?.isEnabled = current() != nil
        }
        let watch = PopupWatcher(refresh)
        keyPop.target = watch
        keyPop.action = #selector(PopupWatcher.changed)
        hostPop.target = watch
        hostPop.action = #selector(PopupWatcher.changed)
        s.field("Key", keyPop)
        s.field("Host", hostPop)
        s.add(s.note("ssh-copy-id runs in your shell (a new tab if it’s busy) and asks for your password on the server if it needs it. Keys already there aren’t added twice."))
        s.add(preview)
        s.buttons([("Install in Terminal", true, {
            _ = watch
            guard let (k, h) = current() else { return }
            Task {
                if !k.hasPub {
                    do { try await Services.sshhosts.WritePublicKey(k.path) } catch {
                        s.error = error.localizedDescription
                        return
                    }
                }
                s.close()
                w.runInShell(Ssh.installKey(k.path, h))
            }
        })])
        s.present(in: w.window)
        refresh()
    }
}

/// Calls back when a popup's choice changes.
@MainActor
final class PopupWatcher: NSObject {
    private let run: () -> Void
    init(_ run: @escaping () -> Void) { self.run = run }
    @objc func changed() { run() }
}

@MainActor
enum KeyGenSheet {
    /// New SSH Key: ssh-keygen in a terminal, which asks for the passphrase there.
    static func show(in w: WindowController) {
        let store = SshStore.shared
        let taken = Set(store.keys.keys.map { WindowModel.prettyPath($0.path) })
        let s = FormSheet(title: "New SSH Key", subtitle: "Creates a key pair in ~/.ssh")
        let type = s.popup(Ssh.keyTypes.map(\.label))
        let name = s.text(Ssh.freeKeyName("id_ed25519", taken: taken))
        let comment = s.text(placeholder: "you@this-computer")
        let agent = NSButton(checkboxWithTitle: "Load it into ssh-agent afterwards, remembering the passphrase in the Keychain", target: nil, action: nil)
        agent.state = .on
        let preview = s.note("", mono: true)
        var touched = false
        func refresh() {
            preview.stringValue = Ssh.keygen(type: Ssh.keyTypes[max(0, type.indexOfSelectedItem)].id, name: name.stringValue,
                                             comment: comment.stringValue, addToAgent: agent.state == .on, keychain: true)
            s.button("Generate in Terminal")?.isEnabled = !name.stringValue.trimmingCharacters(in: .whitespaces).isEmpty
        }
        let watch = FieldWatcher {
            touched = true
            refresh()
        }
        name.delegate = watch
        comment.delegate = watch
        let popWatch = PopupWatcher {
            if !touched { name.stringValue = Ssh.freeKeyName(Ssh.keyTypes[max(0, type.indexOfSelectedItem)].file, taken: taken) }
            refresh()
        }
        type.target = popWatch
        type.action = #selector(PopupWatcher.changed)
        agent.target = popWatch
        agent.action = #selector(PopupWatcher.changed)
        s.field("Type", type)
        let savedAs = s.note("")
        func saved() { savedAs.stringValue = "Saved as ~/.ssh/\(name.stringValue.trimmingCharacters(in: .whitespaces).isEmpty ? "…" : name.stringValue.trimmingCharacters(in: .whitespaces)) and .pub" }
        saved()
        let savedWatch = FieldWatcher { saved(); watch.controlTextDidChange(Notification(name: NSControl.textDidChangeNotification)) }
        name.delegate = savedWatch
        objc_setAssociatedObject(name, "savedWatch", savedWatch, .OBJC_ASSOCIATION_RETAIN)
        s.field("File name", name)
        s.add(savedAs)
        s.field("Comment", comment, help: "A label stored with the key, shown in authorized_keys on servers.")
        s.add(agent)
        s.add(s.note("ssh-keygen runs in your shell (a new tab if it’s busy) and asks for a passphrase there. Leave it empty for none."))
        s.add(preview)
        s.buttons([("Generate in Terminal", true, {
            _ = (watch, popWatch)
            let n = name.stringValue.trimmingCharacters(in: .whitespaces)
            Task {
                do {
                    guard try await Services.sshhosts.KeyNameFree(n) else {
                        s.error = "~/.ssh/\(n) already exists — choose another name"
                        return
                    }
                    s.close()
                    w.runInShell(preview.stringValue)
                } catch {
                    s.error = error.localizedDescription
                }
            }
        })])
        s.present(in: w.window, focus: name)
        refresh()
    }
}

// MARK: - Tunnels

@MainActor
enum TunnelEditor {
    static let kinds = [("local", "Local -L"), ("remote", "Remote -R"), ("dynamic", "SOCKS -D"), ("kubectl", "Kubernetes port-forward")]
    static let explain = [
        "local": "Reach a service near the server — a database, an internal web app — on a port of this computer.",
        "remote": "Let the server reach a service on this computer, through a port on the server.",
        "dynamic": "A SOCKS proxy on this computer that sends traffic out through the server — point a browser at it.",
        "kubectl": "Reach a pod, service or deployment in a cluster on a port of this computer. If its pod restarts, the forward reconnects.",
    ]

    static func show(_ t: Go.sshhosts.TunnelStatus?, host: SshHost?, prefill: TunnelDraft? = nil, in w: WindowController) {
        // "Through host" lists your ssh hosts.
        SshStore.shared.whenLoaded(hosts: true) { build(t, host: host, prefill: prefill, in: w) }
    }

    private static func build(_ t: Go.sshhosts.TunnelStatus?, host: SshHost?, prefill: TunnelDraft?, in w: WindowController) {
        var d = t.map(TunnelDraft.init) ?? prefill ?? TunnelDraft(host: host?.alias ?? "", port: host.map { $0.known && $0.port != "22" ? $0.port : "" } ?? "")
        let editing = t != nil
        let s = FormSheet(title: editing ? "Edit Port Forward" : "New Port Forward",
                          subtitle: d.kind == "kubectl" ? "A kubectl port-forward, run in the background while it’s on"
                              : "A port forward over ssh, run in the background while it’s on", width: 500)
        let kind = s.popup(kinds.map(\.1), selected: kinds.firstIndex { $0.0 == d.kind } ?? 0)
        let explainer = s.note("")
        let hostField = NSComboBox()
        hostField.addItems(withObjectValues: SshStore.shared.list.hosts.filter { !$0.known }.map(\.alias))
        hostField.stringValue = d.host
        hostField.placeholderString = "bastion"
        let portField = s.text(d.port, placeholder: "config")
        let listen = s.text(d.listenPort, placeholder: "8080")
        let destHost = s.text(d.destHost, placeholder: "localhost")
        let destPort = s.text(d.destPort, placeholder: "80")
        let context = s.text(d.context, placeholder: "kubectl context")
        let namespace = s.text(d.namespace, placeholder: "default")
        let resource = s.text(d.resource, placeholder: "svc/web, deploy/api or pod/name")
        let name = s.text(d.name, placeholder: "optional, e.g. staging db")
        let auto = NSButton(checkboxWithTitle: "Start when wtf opens", target: nil, action: nil)
        auto.state = d.autoStart ? .on : .off
        let preview = s.note("", mono: true)
        let hint = s.note("")

        let sshRow = s.field("Through host and ssh port", NSStackView(views: [hostField, portField]))
        portField.widthAnchor.constraint(equalToConstant: 80).isActive = true
        let kubeRow = s.field("Context and namespace", NSStackView(views: [context, namespace]))
        let listenRow = s.field("Port here", listen)
        let destRow = s.field("Destination", NSStackView(views: [destHost, destPort]))
        destPort.widthAnchor.constraint(equalToConstant: 80).isActive = true
        // Its own field: a view lives in one row only.
        let targetPort = s.text(d.destPort, placeholder: "80")
        targetPort.widthAnchor.constraint(equalToConstant: 80).isActive = true
        let resRow = s.field("Target and its port", NSStackView(views: [resource, targetPort]))

        func read() {
            d.kind = kinds[max(0, kind.indexOfSelectedItem)].0
            d.host = hostField.stringValue
            d.port = portField.stringValue
            d.listenPort = listen.stringValue
            d.destHost = destHost.stringValue
            d.destPort = (d.kind == "kubectl" ? targetPort : destPort).stringValue
            d.context = context.stringValue
            d.namespace = namespace.stringValue
            d.resource = resource.stringValue
            d.name = name.stringValue
            d.autoStart = auto.state == .on
        }
        func refresh() {
            read()
            let k = d.kind
            explainer.stringValue = explain[k] ?? ""
            let kube = k == "kubectl"
            sshRow.isHidden = kube
            kubeRow.isHidden = !kube
            resRow.isHidden = !kube
            destRow.isHidden = kube || k == "dynamic"
            (listenRow.subviews.first as? NSTextField)?.stringValue = k == "dynamic" ? "SOCKS port here" : (k == "remote" ? "Port on server" : "Port here")
            let via = d.host.trimmingCharacters(in: .whitespaces)
            (destRow.subviews.first as? NSTextField)?.stringValue = k == "remote" ? "Destination, as seen from here" : "Destination, as seen from \(via.isEmpty ? "the host" : via)"
            s.button("Run in Terminal")?.isHidden = !kube
            let spec = d.spec.isEmpty ? "…" : d.spec
            if kube {
                let env = d.env.isEmpty ? TunnelDraft.kubectlEnv(w.activePane?.env ?? [:]) : d.env
                let prefix = (env["KUBECONFIG"].map { "KUBECONFIG=\(Shell.quote($0)) " } ?? "") + (env["AWS_PROFILE"].map { "AWS_PROFILE=\(Shell.quote($0)) " } ?? "")
                preview.stringValue = "\(prefix)kubectl --context \(Shell.quote(d.context.isEmpty ? "…" : d.context)) --namespace \(Shell.quote(d.namespace.isEmpty ? "…" : d.namespace)) port-forward \(d.resource.isEmpty ? "…" : d.resource) \(spec)"
                hint.stringValue = "Runs with the \(editing ? "saved" : "focused shell’s") KUBECONFIG, AWS profile and PATH, so it reaches the same cluster. Listens on 127.0.0.1 only."
            } else {
                let flag = k == "dynamic" ? "-D" : (k == "remote" ? "-R" : "-L")
                preview.stringValue = "ssh -N \(flag) \(spec) \(d.port.isEmpty ? "" : "-p \(d.port) ")\(d.host.isEmpty ? "…" : d.host)"
                hint.stringValue = "Runs without prompts, so the host must accept a key from ssh-agent or a key without a passphrase."
            }
            if editing, let t, t.state != "stopped", t.state != "failed" { hint.stringValue += " Saving restarts it." }
        }
        let watch = FieldWatcher { refresh() }
        for f in [portField, listen, destHost, destPort, targetPort, context, namespace, resource, name] { f.delegate = watch }
        hostField.delegate = watch
        let pw = PopupWatcher { refresh() }
        kind.target = pw
        kind.action = #selector(PopupWatcher.changed)
        s.field("Kind", kind)
        s.add(explainer)
        s.field("Name", name)
        s.add(auto)
        s.add(preview)
        s.add(hint)

        func save(start: Bool) {
            _ = (watch, pw)
            read()
            if d.kind == "kubectl", !editing, d.env.isEmpty { d.env = TunnelDraft.kubectlEnv(w.activePane?.env ?? [:]) }
            let tunnel = Go.sshhosts.Tunnel(id: t?.id ?? "", name: d.name.trimmingCharacters(in: .whitespaces), host: d.host.trimmingCharacters(in: .whitespaces),
                                            port: d.port.trimmingCharacters(in: .whitespaces), kind: d.kind, spec: d.spec, autoStart: d.autoStart,
                                            context: d.context.trimmingCharacters(in: .whitespaces), namespace: d.namespace.trimmingCharacters(in: .whitespaces),
                                            resource: d.resource.trimmingCharacters(in: .whitespaces), env: d.env)
            Task {
                do {
                    var list = try await Services.sshhosts.SaveTunnel(tunnel)
                    if start, let id = t?.id ?? list.last?.id { list = try await Services.sshhosts.StartTunnel(id) }
                    SshStore.shared.tunnels = list
                    SshStore.shared.changed()
                    s.close()
                    if !start { Toast.show(editing ? "Saved tunnel" : "Added tunnel", in: w.rootView) }
                } catch {
                    s.error = error.localizedDescription
                }
            }
        }
        var buttons: [(String, Bool, () -> Void)] = []
        if !editing {
            buttons.append(("Run in Terminal", false, { [weak s] in
                guard let s else { return }
                read()
                guard d.kind == "kubectl" else { return }
                s.close()
                w.model.open(command: preview.stringValue)
            }))
            buttons.append(("Save", false, { save(start: false) }))
            buttons.append(("Save & Start", true, { save(start: true) }))
        } else {
            buttons.append(("Save", true, { save(start: false) }))
        }
        s.buttons(buttons)
        s.button("Run in Terminal")?.toolTip = "A one-off kubectl port-forward in a new tab, not saved"
        s.present(in: w.window, focus: d.kind == "kubectl" ? context : hostField)
        refresh()
    }
}

// MARK: - Host keys

@MainActor
enum HostKeysSheet {
    static func show(_ h: SshHost, in w: WindowController) {
        let port = h.known ? h.port : ""
        let s = FormSheet(title: "Host Keys — \(h.alias)", subtitle: "Reading known_hosts…", width: 520)
        // Fingerprints are selectable, to copy and compare.
        let storedList = s.note("", mono: true)
        let scanNote = s.note("")
        let scanList = s.note("", mono: true)
        var info: Go.sshhosts.HostKeys?
        var scanned: [Go.sshhosts.HostKey] = []
        var reallyForget = false

        func line(_ k: Go.sshhosts.HostKey, _ note: String) -> String {
            "\(k.type)\(k.type.hasPrefix("RSA") ? " \(k.bits)" : "")  \(k.fingerprint)  \(note)"
        }
        func stored() -> [Go.sshhosts.HostKey] { info?.stored ?? [] }
        func refresh() {
            guard let info else { return }
            (s.panel.contentView?.subviews.compactMap { $0 as? NSTextField }.first { $0.stringValue == "Reading known_hosts…" })?.stringValue = "Stored in known_hosts as \(info.target)"
            storedList.stringValue = stored().isEmpty
                ? "No keys stored for \(info.target). ssh will ask you to confirm the server’s key when you connect."
                : stored().map { line($0, $0.marker.isEmpty ? "\(WindowModel.prettyPath($0.file)):\($0.line)" : $0.marker) }.joined(separator: "\n")
            if !info.via.isEmpty {
                scanNote.stringValue = "Reached through \(info.via), so wtf can’t ask it directly. Connect, and ssh shows the key it’s offered."
            } else if scanned.isEmpty {
                scanNote.stringValue = "Ask \(info.hostName) on port \(info.port.isEmpty ? "22" : info.port) which keys it presents (ssh-keyscan). Nothing is saved until you choose."
            } else {
                let fps = Set(stored().filter { $0.marker.isEmpty }.map(\.fingerprint))
                let revoked = Set(stored().filter { $0.marker == "revoked" }.map(\.fingerprint))
                scanList.stringValue = scanned.map {
                    line($0, revoked.contains($0.fingerprint) ? "revoked" : fps.contains($0.fingerprint) ? "stored" : "not stored")
                }.joined(separator: "\n")
                scanNote.stringValue = HostKeyVerdict.of(scanned: scanned, stored: stored())
                    + " Compare fingerprints with ones from the server’s admin (ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub) before trusting new keys."
            }
            s.button("Scan")?.isHidden = !info.via.isEmpty
            s.button("Scan")?.title = scanned.isEmpty ? "Scan" : "Scan Again"
            let hasStored = stored().contains { $0.marker.isEmpty }
            let fps = Set(stored().filter { $0.marker.isEmpty }.map(\.fingerprint))
            let allStored = !scanned.isEmpty && scanned.allSatisfy { fps.contains($0.fingerprint) } && scanned.count == fps.count
            s.button("Trust Server’s Keys")?.isHidden = scanned.isEmpty || allStored
            s.button("Trust Server’s Keys")?.title = hasStored ? "Replace with Server’s Keys" : "Trust Server’s Keys"
            s.button("Forget Stored Keys")?.isHidden = !hasStored
            s.button("Forget Stored Keys")?.toolTip = "Removes the entries with ssh-keygen -R; the old file is kept as known_hosts.old"
            s.button("Trust Server’s Keys")?.toolTip = "Replaces the stored keys with these"
        }
        func load() {
            Task {
                do {
                    info = try await Services.sshhosts.HostKeys(h.alias, port)
                    refresh()
                } catch {
                    s.error = error.localizedDescription
                }
            }
        }
        s.field("Stored", storedList)
        s.field("On the server now", NSStackView(views: [scanNote]))
        s.add(scanList)
        var busy = false {
            didSet {
                for b in ["Scan", "Scan Again", "Trust Server’s Keys", "Forget Stored Keys", "Really Forget?"] { s.button(b)?.isEnabled = !busy }
            }
        }
        s.buttons([
            ("Scan", false, {
                busy = true
                Task {
                    defer { busy = false }
                    do {
                        scanned = try await Services.sshhosts.ScanHostKeys(h.alias, port)
                        s.error = nil
                        refresh()
                    } catch {
                        s.error = error.localizedDescription
                    }
                }
            }),
            ("Trust Server’s Keys", true, {
                guard let info else { return }
                busy = true
                Task {
                    defer { busy = false }
                    do {
                        try await Services.sshhosts.TrustHostKeys(h.alias, port, scanned.map(\.fingerprint))
                        Toast.show("Saved \(info.target)’s keys", in: w.rootView)
                        load()
                    } catch {
                        s.error = error.localizedDescription
                    }
                }
            }),
        ], cancel: "Close", left: [("Forget Stored Keys", {
            guard let info else { return }
            // Two steps in place: the first click asks, the second forgets.
            if !reallyForget {
                reallyForget = true
                s.button("Forget Stored Keys")?.title = "Really Forget?"
                return
            }
            reallyForget = false
            busy = true
            Task {
                defer { busy = false }
                do {
                    try await Services.sshhosts.ForgetHostKeys(h.alias, port)
                    Toast.show("Forgot \(info.target)’s keys", in: w.rootView)
                    if h.known { SshStore.shared.loadHosts() }
                    load()
                } catch {
                    s.error = error.localizedDescription
                }
            }
        })])
        s.present(in: w.window)
        load()
    }
}

// MARK: - Git identities

@MainActor
enum GitIdentityEditor {
    static func show(_ i: Go.sshhosts.GitIdentity?, keyPath: String? = nil, in w: WindowController) {
        SshStore.shared.whenLoaded(keys: true) { build(i, keyPath: keyPath, in: w) }
    }

    private static func build(_ i: Go.sshhosts.GitIdentity?, keyPath: String?, in w: WindowController) {
        let store = SshStore.shared
        let s = FormSheet(title: i.map { "Edit \($0.name)" } ?? "New Git Identity",
                          subtitle: "An SSH key for GitHub and the author to commit as, for the tabs you choose", width: 470)
        let name = s.text(i?.name ?? "", placeholder: "work")
        var keys = store.keys.keys.filter { !$0.path.isEmpty }.map(\.path)
        let current = i?.keyPath ?? keyPath ?? ""
        let comments = Dictionary(store.keys.keys.map { ($0.path, $0) }, uniquingKeysWith: { a, _ in a })
        var labels = keys.map { p in
            WindowModel.prettyPath(p) + (comments[p].map { !$0.comment.isEmpty && $0.comment != $0.name ? " — \($0.comment)" : "" } ?? "")
        }
        if !current.isEmpty, !keys.contains(current) {
            keys.insert(current, at: 0)
            labels.insert("\(WindowModel.prettyPath(current)) (not found in ~/.ssh)", at: 0)
        }
        let key = s.popup(["Choose a key…"] + labels, selected: keys.firstIndex(of: current).map { $0 + 1 } ?? 0)
        key.item(at: 0)?.isEnabled = false
        let author = s.text(i?.authorName ?? "", placeholder: "Your Name (blank: from git config)")
        let email = s.text(i?.authorEmail ?? "", placeholder: "you@example.com (blank: from git config)")
        let useNow = NSButton(checkboxWithTitle: "Use it in this tab after saving", target: nil, action: nil)
        useNow.state = i == nil ? .on : .off
        let preview = s.note("", mono: true)
        func draft() -> Go.sshhosts.GitIdentity {
            let ki = key.indexOfSelectedItem - 1
            return Go.sshhosts.GitIdentity(id: i?.id ?? "", name: name.stringValue.trimmingCharacters(in: .whitespaces),
                                           authorName: author.stringValue.trimmingCharacters(in: .whitespaces),
                                           authorEmail: email.stringValue.trimmingCharacters(in: .whitespaces),
                                           keyPath: ki >= 0 && ki < keys.count ? keys[ki] : "")
        }
        func refresh() {
            let d = draft()
            preview.stringValue = d.name.isEmpty || d.keyPath.isEmpty ? ""
                : "A tab using it runs:\n" + Shell.useIdentity(.init(name: d.name, authorName: d.authorName, authorEmail: d.authorEmail, keyPath: d.keyPath))
            s.button(i == nil ? "Add Identity" : "Save")?.isEnabled = !d.name.isEmpty && !d.keyPath.isEmpty
        }
        let watch = FieldWatcher { refresh() }
        [name, author, email].forEach { $0.delegate = watch }
        let pw = PopupWatcher { refresh() }
        key.target = pw
        key.action = #selector(PopupWatcher.changed)
        s.field("Name", name, help: "Shown in the address bar of tabs using it.")
        s.field("SSH key", key, help: "git offers GitHub only this key, so GitHub knows which account you are.")
        let newKey = ClosureButton(title: "New key…") {
            s.close()
            w.showGenerator("key")
        }
        newKey.isBordered = false
        newKey.contentTintColor = .controlAccentColor
        s.add(newKey)
        s.field("Commit as", author)
        s.field("Email", email, help: "For GitHub to link commits to the account, use an email on it (or its noreply address).")
        s.add(useNow)
        s.add(preview)
        s.buttons([(i == nil ? "Add Identity" : "Save", true, {
            _ = (watch, pw)
            let d = draft()
            Task {
                do {
                    store.identities = try await Services.sshhosts.SaveGitIdentity(d)
                    store.changed()
                    s.close()
                    if useNow.state == .on {
                        if !w.runInShell(Shell.useIdentity(.init(name: d.name, authorName: d.authorName, authorEmail: d.authorEmail, keyPath: d.keyPath))) {
                            Toast.show("Opened a new tab as \(d.name): that one was busy", in: w.rootView)
                        }
                    } else {
                        Toast.show(i == nil ? "Added \(d.name)" : "Saved \(d.name)", in: w.rootView)
                    }
                } catch {
                    s.error = error.localizedDescription
                }
            }
        })])
        s.present(in: w.window, focus: name)
        refresh()
        if !store.keysLoaded { store.loadKeys() }
    }
}
