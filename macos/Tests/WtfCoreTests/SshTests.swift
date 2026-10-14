import Testing
@testable import WtfCore

private func host(_ f: (inout SshHost) -> Void) -> SshHost {
    var h = SshHost(alias: "web1")
    f(&h)
    return h
}

@Test func sshCommandsAndText() {
    #expect(Ssh.command(host { _ in }) == "ssh web1")
    let known = host { $0.known = true; $0.port = "2222"; $0.alias = "10.0.0.5" }
    #expect(Ssh.command(known) == "ssh -p 2222 10.0.0.5")
    #expect(Ssh.sftp(known) == "sftp -P 2222 10.0.0.5")
    // Config hosts carry their own port.
    #expect(Ssh.command(host { $0.port = "2222" }) == "ssh web1")
    #expect(Ssh.detail(host { $0.alias = "b"; $0.hostName = "bastion.example.com"; $0.user = "ops"; $0.port = "2222"; $0.proxyJump = "jump" })
            == "ops@bastion.example.com:2222 via jump")
    #expect(Ssh.detail(host { $0.hostName = "web1" }) == "")
    #expect(Ssh.facts(host { $0.forwardAgent = "yes"; $0.strictHostKeyChecking = "no" }).map(\.label) == ["agent fwd", "hostkey no"])
    #expect(Ssh.matches(host { $0.user = "ops" }, "WEB ops"))
    #expect(!Ssh.matches(host { _ in }, "web db"))
    #expect(Ssh.groupName([host { $0.source = "/h/.ssh/config.d/prod" }, host { $0.source = "/h/.ssh/config.d/prod" }]) == "prod")
    #expect(Ssh.groupName([host { $0.source = "/h/.ssh/config" }]) == "SSH")
}

@Test func scpAndKeyCommands() {
    let h = host { $0.known = true; $0.port = "2222" }
    #expect(Ssh.upload(h, ["/h/my file", "/tmp/x"], " /srv ", home: "/h") == "scp -r -P 2222 ~/'my file' /tmp/x web1:/srv")
    #expect(Ssh.download(host { _ in }, "/var/log/app.log", "/h/Downloads", home: "/h") == "scp -r web1:/var/log/app.log ~/Downloads")
    #expect(Ssh.installKey("/h/.ssh/id_ed25519", host { _ in }, home: "/h") == "ssh-copy-id -i ~/.ssh/id_ed25519.pub web1")
    #expect(Ssh.keygen(type: "rsa", name: "work", comment: "me at work", addToAgent: true, keychain: true)
            == "ssh-keygen -t rsa -b 4096 -C 'me at work' -f ~/.ssh/work && ssh-add --apple-use-keychain ~/.ssh/work")
    #expect(Ssh.freeKeyName("id_ed25519", taken: ["~/.ssh/id_ed25519"]) == "id_ed25519_2")
    #expect(Ssh.isKeyTool("sudo ssh-add -D") && !Ssh.isKeyTool("ssh web1"))
}

@Test func hostEditorDraft() throws {
    let h = host {
        $0.patterns = ["web1", "w1"]
        $0.options = [.init(key: "hostname", value: "10.0.0.5"), .init(key: "IdentityFile", value: "\"~/my key\""),
                      .init(key: "IdentityFile", value: "~/.ssh/other"), .init(key: "ForwardAgent", value: "yes")]
    }
    let d = HostDraft(h)
    #expect(d.name == "web1 w1" && d.hostName == "10.0.0.5" && d.identityFile == "~/my key")
    #expect(d.more == "IdentityFile ~/.ssh/other\nForwardAgent yes")
    var e = d
    e.more = "ForwardAgent=yes\n  \nLocalForward 8080 localhost:80"
    #expect(try e.options().map { "\($0.key) \($0.value)" } == ["HostName 10.0.0.5", "IdentityFile ~/my key", "ForwardAgent yes", "LocalForward 8080 localhost:80"])
    e.more = "Compression"
    #expect(throws: HostDraft.MissingValue.self) { try e.options() }
}

@Test func tunnels() {
    var d = TunnelDraft(host: "bastion")
    d.listenPort = "5432"; d.destHost = "db.internal"; d.destPort = "5432"
    #expect(d.spec == "5432:db.internal:5432")
    d.destHost = "::1"
    #expect(d.spec == "5432:[::1]:5432")
    let t = Go.sshhosts.TunnelStatus(host: "bastion", kind: "local", spec: "127.0.0.1:5432:db:5433")
    #expect(Tunnels.describe(t) == "127.0.0.1:5432 → db:5433 via bastion")
    #expect(Tunnels.localPort(t) == "5432")
    let back = TunnelDraft(t)
    #expect(back.listenPort == "127.0.0.1:5432" && back.destHost == "db" && back.destPort == "5433")
    let k = Go.sshhosts.TunnelStatus(kind: "kubectl", spec: "8080:80", namespace: "shop", resource: "svc/web")
    #expect(Tunnels.describe(k) == "localhost:8080 → svc/web:80 in shop" && Tunnels.localPort(k) == "8080")
    #expect(Tunnels.describe(.init(host: "b", kind: "dynamic", spec: "1080")) == "SOCKS proxy on localhost:1080 via b")
    #expect(Tunnels.localPort(.init(kind: "remote", spec: "8080:localhost:80")) == nil)
    #expect(Tunnels.describe(.init(host: "b", kind: "remote", spec: "8080:localhost:80")) == "b:8080 → localhost:80 here")
}

@Test func hostKeyVerdicts() {
    let a = Go.sshhosts.HostKey(fingerprint: "A"), b = Go.sshhosts.HostKey(fingerprint: "B")
    #expect(HostKeyVerdict.of(scanned: [], stored: [a]) == "The server offered no keys.")
    #expect(HostKeyVerdict.of(scanned: [a], stored: []) == "None of these keys are stored yet.")
    #expect(HostKeyVerdict.of(scanned: [a], stored: [a]) == "All of the server’s keys match the stored ones.")
    #expect(HostKeyVerdict.of(scanned: [a, b], stored: [a]) == "1 of the server’s 2 keys match the stored ones.")
    #expect(HostKeyVerdict.of(scanned: [b], stored: [a]) == "None of the server’s keys match the stored ones.")
}
