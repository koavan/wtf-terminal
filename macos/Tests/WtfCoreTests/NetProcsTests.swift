import Testing
@testable import WtfCore

@Test func addresses() {
    #expect(Net.splitAddress("[::1]:3000") == ("::1", "3000"))
    #expect(Net.splitAddress("*:5432") == ("*", "5432"))
    #expect(Net.splitAddress("plain") == ("plain", ""))
    #expect(!Net.isExposed("127.0.0.1") && !Net.isExposed("::1") && Net.isExposed("0.0.0.0") && Net.isExposed("*"))
    let a = Conn(pid: 1, process: "node", protocol: "TCP", local: "127.0.0.1:3000", state: "LISTEN")
    let b = Conn(pid: 1, process: "node", protocol: "TCP", local: "[::1]:3000", state: "LISTEN")
    let c = Conn(pid: 1, process: "node", protocol: "TCP", local: "*:3000", state: "LISTEN")
    #expect(Net.mergeListeners([a, b, c]).map(\.local) == ["127.0.0.1:3000", "*:3000"])
    #expect(Net.serviceName("5432") == "postgres" && Net.serviceName("1") == nil)
    #expect(Net.stateLabel("CLOSE_WAIT") == "close wait" && Net.stateLabel("ESTABLISHED") == nil)
}

@Test func whichConnectionsShow() {
    let mine = Conn(pid: 10, process: "ssh", protocol: "TCP", local: "10.0.0.2:5555", remote: "1.2.3.4:22", state: "ESTABLISHED", session: "s1")
    let other = Conn(pid: 20, process: "Slack", protocol: "TCP", local: "10.0.0.2:6000", remote: "5.6.7.8:443", state: "ESTABLISHED")
    let forward = Conn(pid: 30, process: "ssh", protocol: "TCP", local: "127.0.0.1:5432", state: "LISTEN")
    let all = [mine, other, forward]
    #expect(Net.visible(all, terminalsOnly: true, filter: "", forwardPorts: ["5432"]).map(\.pid) == [10, 30])
    #expect(Net.visible(all, terminalsOnly: false, filter: "slack", forwardPorts: []).map(\.pid) == [20])
    #expect(Net.visible(all, terminalsOnly: true, filter: "pid: 20", forwardPorts: []).map(\.pid) == [20])
}

@Test func processes() {
    #expect(Procs.cpu(0.44) == "0.4%" && Procs.cpu(23.4) == "23%")
    #expect(Procs.bytes(500) == "1 KB" && Procs.bytes(52_428_800) == "50 MB" && Procs.bytes(1_610_612_736) == "1.5 GB")
    #expect(Procs.elapsed(45) == "45s" && Procs.elapsed(725) == "12m" && Procs.elapsed(11_100) == "3h 5m" && Procs.elapsed(187_200) == "2d 4h")
    let sh = Proc(pid: 100, ppid: 1, name: "zsh", cpu: 0, memory: 10, session: "s1", shell: true)
    let vim = Proc(pid: 120, ppid: 100, name: "vim", cpu: 5, memory: 50, session: "s1")
    let node = Proc(pid: 110, ppid: 100, name: "node", cpu: 30, memory: 40, session: "s1")
    let child = Proc(pid: 130, ppid: 110, name: "esbuild", cpu: 1, memory: 5, session: "s1")
    #expect(Procs.tree([child, vim, node, sh]).map { "\($0.proc.name):\($0.depth)" } == ["zsh:0", "node:1", "esbuild:2", "vim:1"])
    #expect(Procs.sorted([sh, vim, node], by: .cpu).map(\.name) == ["node", "vim", "zsh"])
    #expect(Procs.sorted([sh, vim, node], by: .name).map(\.name) == ["node", "vim", "zsh"])
    #expect(Procs.matches(vim, "120") && !Procs.matches(vim, "emacs"))
}

@Test func usageFormats() {
    #expect(Usage.cpu(-1) == "…" && Usage.cpu(0.44) == "0.4%" && Usage.cpu(12.4) == "12%")
    #expect(Usage.bytes(512) == "512 B" && Usage.bytes(860_160) == "840 KB" && Usage.bytes(432_013_312) == "412 MB" && Usage.bytes(1_288_490_189) == "1.2 GB")
    let u = Go.usage.Usage(app: .init(cpu: 1, memory: 100), window: .init(cpu: 2, memory: 50), programs: .init(cpu: 50, memory: 999), interval: 0)
    #expect(Usage.total(u).cpu == -1 && Usage.total(u).memory == 150)
}
