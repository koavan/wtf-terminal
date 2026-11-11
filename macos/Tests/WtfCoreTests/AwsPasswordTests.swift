import Foundation
import Testing
@testable import WtfCore

@Test func awsRows() {
    var p = AwsProfile(name: "ci", kind: "keys", accessKeyId: "AKIAABCDEFGH0001", hasSessionToken: true, region: "us-east-1")
    #expect(Aws.detail(p) == "AKIA…0001 (temporary) · us-east-1")
    p.kind = "role"
    p.account = "123456789012"
    p.region = ""
    #expect(Aws.detail(p) == "Assume role · 123456789012")
    #expect(Aws.shortArn("arn:aws:iam::123456789012:user/ci-bot") == "123456789012:user/ci-bot")
    #expect(Aws.useCommand("o'k x") == "export AWS_PROFILE='o'\\''k x'")
    #expect(Aws.matches(p, "1234") && !Aws.matches(p, "zzz"))
    let now = Aws.date("2026-10-14T12:00:00Z")!
    let k = Go.awsprofiles.AccessKey(id: "AKIAABCDEFGH0001", status: "Active", created: "2026-09-14T12:00:00Z",
                                     lastUsed: "2026-10-13T10:00:00Z", lastUsedService: "s3", current: true)
    #expect(Aws.keyLine(k, now: now) == "AKIA…0001 (this one) · active · 30 days old · used yesterday (s3)")
}

@Test func awsDrafts() throws {
    let p = AwsProfile(name: "admin", kind: "role", settings: [.init(key: "Region", value: "eu-west-1"), .init(key: "role_arn", value: "arn:x"),
                                                                .init(key: "duration_seconds", value: "3600")])
    let d = Aws.draft(of: p)
    #expect(d.region == "eu-west-1" && d.fields["role_arn"] == "arn:x" && d.more == "duration_seconds = 3600")
    let g = try Aws.goDraft(d, original: "admin", versions: ["/c": "v"])
    #expect(g.settings.map(\.key) == ["region", "role_arn", "duration_seconds"] && !g.useKeys && g.accessKeyId.isEmpty)
    var bad = d
    bad.more = "nonsense"
    #expect(throws: Aws.BadLine.self) { try Aws.goDraft(bad, original: "", versions: [:]) }
}

@Test func passwords() {
    var o = Passwords.Options()
    #expect(Passwords.entropyBits(o) == 155)
    o.lower = false; o.upper = false; o.symbols = false; o.length = 10
    #expect(Passwords.entropyBits(o) == 33)
    var all = Passwords.Options()
    all.length = 4
    for _ in 0..<50 {
        let pw = Passwords.generate(all)
        #expect(pw.count == 4)
        #expect(pw.contains { Passwords.lower.contains($0) } && pw.contains { Passwords.upper.contains($0) }
            && pw.contains { Passwords.digits.contains($0) } && pw.contains { Passwords.symbols.contains($0) })
    }
    all.avoidAmbiguous = true
    all.length = 128
    #expect(!Passwords.generate(all).contains { Passwords.lookAlikes.contains($0) })
    var none = Passwords.Options()
    none.lower = false; none.upper = false; none.digits = false; none.symbols = false
    #expect(Passwords.generate(none) == "" && Passwords.entropyBits(none) == 0)
    // Values past the last whole multiple of n are drawn again.
    var draws: [UInt32] = [4_294_967_295, 13]
    #expect(Passwords.randomInt(10) { draws.removeFirst() } == 3)
}

// More cases of the Svelte build's passwords tests.

@Test func passwordsUseOnlyTheChosenClasses() {
    var digits = Passwords.Options()
    digits.lower = false; digits.upper = false; digits.symbols = false; digits.length = 30
    let pw = Passwords.generate(digits)
    #expect(pw.count == 30 && pw.allSatisfy { Passwords.digits.contains($0) })
    let all = String(Passwords.classes(Passwords.Options()).joined())
    #expect(!all.contains { "'\"\\` ".contains($0) })
}

@Test func passwordsClampTheLength() {
    var o = Passwords.Options()
    o.length = 1
    #expect(Passwords.generate(o).count == 4)
    o.length = 1000
    #expect(Passwords.generate(o).count == 128)
}

@Test func passwordsDrawEvenly() {
    var counts = [Int](repeating: 0, count: 10)
    for _ in 0..<20000 { counts[Passwords.randomInt(10)] += 1 }
    #expect(counts.allSatisfy { $0 > 1700 }, "\(counts)")
}
