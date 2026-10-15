import Foundation
import Testing
@testable import WtfCore

func b64url(_ s: String) -> String {
    Data(s.utf8).base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}
let TOKEN = b64url(#"{"alg":"RS256","typ":"JWT"}"#) + "." + b64url(#"{"sub":"ada","iss":"https://idp","exp":1791829800,"iat":1791826200,"aud":["api"]}"#) + ".c2lnbmF0dXJl"
let PEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
func b64(_ s: String) -> String { Data(s.utf8).base64EncodedString() }

@Test func base64Rules() {
    #expect(Inspect.base64Bytes("+/8=") == [251, 255])
    #expect(Inspect.base64Bytes("-_8") == [251, 255])
    #expect(Inspect.base64Bytes("abcde") == nil && Inspect.base64Bytes("not base64!") == nil)
    #expect(Inspect.utf8([0xEF, 0xBB, 0xBF, 0x41]) == "A" && Inspect.utf8([0, 0x41]) == nil && Inspect.utf8([0xFF]) == nil)
    #expect(Inspect.utf8(Array("a\tb\nc".utf8)) == "a\tb\nc")
}

@Test func jwtDecoding() {
    let j = Inspect.decodeJwt(TOKEN)
    #expect(j?.alg == "RS256" && j?.signed == true)
    #expect(j?.claims["sub"]?.string == "ada")
    #expect(j?.times.map(\.claim) == ["exp", "iat"])
    #expect(j?.times.first?.date == Date(timeIntervalSince1970: 1791829800))
    #expect(Inspect.decodeJwt(b64url(#"{"alg":"none"}"#) + "." + b64url(#"{"a":1}"#) + ".")?.signed == false)
    for bad in ["", "a.b", "a.b.c", b64url("[1]") + "." + b64url("{}") + ".x", b64url("{}") + "." + b64url("nope") + ".x"] {
        #expect(Inspect.decodeJwt(bad) == nil, "\(bad)")
    }
    #expect(Inspect.firstJwt("Authorization: Bearer \(TOKEN) sent") == TOKEN)
}

@Test func base64Decoding() {
    #expect(Inspect.decodeBase64(b64("hunter2-password")) == .init(kind: .text, size: 16, text: "hunter2-password"))
    #expect(Inspect.decodeBase64(b64(#"{"a":1}"#)) == .init(kind: .json, size: 7, text: "{\n  \"a\": 1\n}"))
    #expect(Inspect.decodeBase64(b64(PEM))?.kind == .certificate)
    let jwt = Inspect.decodeBase64(b64(TOKEN + "\n"))
    #expect(jwt?.kind == .jwt && jwt?.text == TOKEN)
    let bin = Inspect.decodeBase64(Data([0, 1, 2] + Array("hello".utf8)).base64EncodedString())
    #expect(bin?.kind == .binary)
    #expect(bin?.text == "00000000  " + "00 01 02 68 65 6c 6c 6f".padding(toLength: 47, withPad: " ", startingAt: 0) + "  |...hello|")
    #expect(Inspect.hexDump([UInt8](repeating: 0, count: 17)).split(separator: "\n").count == 2)
    #expect(Inspect.hexDump([]) == "")
}

@Test func looksAndYaml() {
    #expect(Inspect.looksBase64("cGFzc3dvcmQxMjM=") && Inspect.looksBase64("U2VjcmV0VmFsdWU"))
    #expect(!Inspect.looksBase64("deployment") && !Inspect.looksBase64("12345678") && !Inspect.looksBase64("short="))
    #expect(Inspect.yamlBase64At("  password: cGFzc3dvcmQxMjM=", 14) == "cGFzc3dvcmQxMjM=")
    #expect(Inspect.yamlBase64At("  password: cGFzc3dvcmQxMjM=", 3) == nil)
    #expect(Inspect.yamlBase64At("  name: payments", 10) == nil)
}

@Test func tlsTargets() {
    #expect(Inspect.tlsTarget("https://api.example.com/v1/x?y=1") == "https://api.example.com")
    for t in ["db.internal:5432", "example.com", "10.0.0.5:8443"] { #expect(Inspect.tlsTarget(t) == t) }
    for t in ["main.go", "config.yaml", "hello", "two words.com", "http://plain.example.com", ""] { #expect(Inspect.tlsTarget(t) == nil, "\(t)") }
    #expect(Inspect.isCertFile("a.pem") && Inspect.isCertFile("ca.crt") && Inspect.isCertFile("x.CER") && Inspect.isCertFile("b.der"))
    #expect(!Inspect.isCertFile("id_rsa") && !Inspect.isCertFile("a.key") && !Inspect.isCertFile("a.pub"))
}

@Test func selectionMenus() {
    func labels(_ s: String, _ secrets: Bool = true) -> [String] { Inspect.selectionEntries(s, secrets: secrets).map(\.label) }
    #expect(labels(PEM) == ["Inspect Certificate…"])
    #expect(labels("token=\(TOKEN)") == ["Inspect JWT…"])
    #expect(labels("cGFzc3dvcmQxMjM=") == ["Decode Base64…"])
    #expect(labels("https://example.com:8443/") == ["Inspect TLS Certificate of example.com:8443…"])
    #expect(labels(TOKEN, false) == [] && labels("cGFzc3dvcmQxMjM=", false) == [])
    #expect(labels(PEM, false) == ["Inspect Certificate…"])
    #expect(labels("just words") == [])
    #expect(Inspect.selectionEntries(" \(TOKEN) ").first?.action == .inspectJwt(TOKEN))
    #expect(Inspect.selectionEntries("https://example.com:8443/").first?.action == .inspectTls("https://example.com:8443"))
}

@Test func sizes() {
    #expect(Inspect.size(16) == "16 bytes" && Inspect.size(1536) == "1.5 KB" && Inspect.size(5, bytes: "B") == "5 B")
}
