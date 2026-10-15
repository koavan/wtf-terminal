// The token and certificate inspector's rules: JWTs, base64 values,
// certificates and TLS servers found in a terminal's output. Port of
// lib/inspect.ts (and isCertFile from lib/fileActions.ts).
import Foundation

public enum Inspect {
    public static let jwtPattern = #"\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*"#

    /// The first JWT-shaped run of text.
    public static func firstJwt(_ text: String) -> String? {
        text.range(of: jwtPattern, options: .regularExpression).map { String(text[$0]) }
    }

    /// Standard or URL-safe base64, padding optional; nil if it isn't.
    public static func base64Bytes(_ text: String) -> [UInt8]? {
        var s = String(text.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) })
        guard s.count >= 2, s.range(of: "^[A-Za-z0-9+/_-]+={0,2}$", options: .regularExpression) != nil else { return nil }
        s = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while s.hasSuffix("=") { s.removeLast() }
        guard s.count % 4 != 1 else { return nil }
        s += String(repeating: "=", count: (4 - s.count % 4) % 4)
        return Data(base64Encoded: s).map { [UInt8]($0) }
    }

    /// Strict UTF-8 that looks like text (no control characters but tab and line breaks).
    public static func utf8(_ bytes: [UInt8]) -> String? {
        var b = bytes[...]
        if b.starts(with: [0xEF, 0xBB, 0xBF]) { b = b.dropFirst(3) }
        // Strict: invalid sequences make it binary, not U+FFFD.
        guard let s = String(bytes: b, encoding: .utf8) else { return nil }
        for u in s.unicodeScalars where u.value <= 8 || (0x0E...0x1F).contains(u.value) || u.value == 0x7F { return nil }
        return s
    }

    public struct Jwt: Equatable, Sendable {
        public var header: OrderedJSON
        public var claims: OrderedJSON
        public var times: [(claim: String, date: Date)]
        public var alg: String
        public var signed: Bool

        public static func == (a: Jwt, b: Jwt) -> Bool {
            a.header == b.header && a.claims == b.claims && a.alg == b.alg && a.signed == b.signed
                && a.times.map(\.claim) == b.times.map(\.claim) && a.times.map(\.date) == b.times.map(\.date)
        }
    }

    static func jsonObject(_ part: String) -> OrderedJSON? {
        guard let bytes = base64Bytes(part), let text = utf8(bytes), let v = try? OrderedJSON.parse(text),
              case .object = v else { return nil }
        return v
    }

    /// A JWT's header and claims (the signature isn't checked).
    public static func decodeJwt(_ token: String) -> Jwt? {
        let parts = token.trimmingCharacters(in: .whitespacesAndNewlines).components(separatedBy: ".")
        guard parts.count == 3, let header = jsonObject(parts[0]), let claims = jsonObject(parts[1]) else { return nil }
        let times = ["exp", "nbf", "iat", "auth_time"].compactMap { c -> (String, Date)? in
            guard let n = claims[c]?.number, n.isFinite else { return nil }
            return (c, Date(timeIntervalSince1970: n))
        }
        return Jwt(header: header, claims: claims, times: times, alg: header["alg"]?.string ?? "", signed: !parts[2].isEmpty)
    }

    public enum Kind: String, Sendable { case certificate, jwt, json, text, binary }

    public struct Decoded: Equatable, Sendable {
        public var kind: Kind
        public var size: Int
        public var text: String
    }

    public static func hasPemCertificate(_ text: String) -> Bool {
        text.range(of: "-----BEGIN (?:TRUSTED )?CERTIFICATE-----", options: .regularExpression) != nil
    }

    /// A base64 value decoded, and what it turned out to be.
    public static func decodeBase64(_ input: String) -> Decoded? {
        guard let bytes = base64Bytes(input), !bytes.isEmpty else { return nil }
        guard let text = utf8(bytes) else { return Decoded(kind: .binary, size: bytes.count, text: hexDump(Array(bytes.prefix(256)))) }
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if hasPemCertificate(trimmed) { return Decoded(kind: .certificate, size: bytes.count, text: text) }
        if let t = firstJwt(trimmed), decodeJwt(t) != nil { return Decoded(kind: .jwt, size: bytes.count, text: trimmed) }
        if trimmed.hasPrefix("[") || trimmed.hasPrefix("{"), let v = try? OrderedJSON.parse(trimmed) {
            return Decoded(kind: .json, size: bytes.count, text: v.pretty())
        }
        return Decoded(kind: .text, size: bytes.count, text: text)
    }

    /// `00000000  00 01 02 …  |...|`, 16 bytes a row.
    public static func hexDump(_ bytes: [UInt8]) -> String {
        stride(from: 0, to: bytes.count, by: 16).map { off in
            let row = bytes[off..<min(off + 16, bytes.count)]
            let hex = row.map { String(format: "%02x", $0) }.joined(separator: " ")
            let ascii = String(row.map { (0x20..<0x7F).contains($0) ? Character(UnicodeScalar($0)) : "." })
            return String(format: "%08x", off) + "  " + hex.padding(toLength: 47, withPad: " ", startingAt: 0) + "  |" + ascii + "|"
        }.joined(separator: "\n")
    }

    /// Whether some text is likely base64 (not just a word that happens to fit).
    public static func looksBase64(_ text: String) -> Bool {
        let s = String(text.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) })
        guard s.count >= 8, s.range(of: "^[A-Za-z0-9+/_-]+={0,2}$", options: .regularExpression) != nil else { return false }
        let mixed = s.range(of: "[A-Z]", options: .regularExpression) != nil && s.range(of: "[a-z]", options: .regularExpression) != nil
        guard mixed || s.hasSuffix("=") else { return false }
        return decodeBase64(s) != nil
    }

    /// The base64 value of a YAML `key: value` line, if the index is on it.
    public static func yamlBase64At(_ line: String, _ index: Int) -> String? {
        let ns = line as NSString
        let re = try! NSRegularExpression(pattern: #"^(\s*[\w.-]+:\s+)([A-Za-z0-9+/]+={0,2})\s*$"#)
        guard let m = re.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)) else { return nil }
        let start = m.range(at: 1).length, value = ns.substring(with: m.range(at: 2))
        guard index >= start, index < start + m.range(at: 2).length, looksBase64(value) else { return nil }
        return value
    }

    /// A server to ask for its TLS certificate: `https://host`, `host:port` or a domain name.
    public static func tlsTarget(_ text: String) -> String? {
        let s = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty, s.count <= 300, s.rangeOfCharacter(from: .whitespacesAndNewlines) == nil else { return nil }
        if let r = s.range(of: #"^(?:https|wss|ldaps)://[^/\s?#]+"#, options: [.regularExpression, .caseInsensitive]) { return String(s[r]) }
        let re = try! NSRegularExpression(pattern: #"^((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:[a-z]{2,63}|xn--[a-z0-9-]+)|\d{1,3}(?:\.\d{1,3}){3})(?::(\d{1,5}))?/?$"#,
                                      options: .caseInsensitive)
        let ns = s as NSString
        guard let m = re.firstMatch(in: s, range: NSRange(location: 0, length: ns.length)) else { return nil }
        let host = ns.substring(with: m.range(at: 1))
        let port = m.range(at: 2).location == NSNotFound ? nil : ns.substring(with: m.range(at: 2))
        if port == nil, !(host.first?.isNumber ?? false),
           host.range(of: #"\.(go|ts|js|py|rb|rs|md|txt|json|ya?ml|toml|lock|sh|log|conf|cfg|ini|pem|crt|key)$"#,
                      options: [.regularExpression, .caseInsensitive]) != nil { return nil }
        return port.map { "\(host):\($0)" } ?? host
    }

    /// Files the Files tab offers to inspect as certificates.
    public static func isCertFile(_ path: String) -> Bool {
        path.range(of: #"\.(?:pem|crt|cer|cert|der|ca-bundle)$"#, options: [.regularExpression, .caseInsensitive]) != nil
    }

    public enum Action: Equatable, Sendable {
        case inspectJwt(String), decodeBase64(String), inspectCertText(String), inspectTls(String)
    }

    /// What a selection can be inspected as. `secrets` is false while Hide
    /// Secrets is on: no JWTs or base64 values then.
    public static func selectionEntries(_ selection: String, secrets: Bool = true) -> [(label: String, action: Action)] {
        guard !selection.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return [] }
        var out: [(String, Action)] = []
        if hasPemCertificate(selection) { out.append(("Inspect Certificate…", .inspectCertText(selection))) }
        let jwt = secrets ? firstJwt(selection) : nil
        if let jwt, decodeJwt(jwt) != nil {
            out.append(("Inspect JWT…", .inspectJwt(jwt)))
        } else if secrets, out.isEmpty, looksBase64(selection) {
            out.append(("Decode Base64…", .decodeBase64(selection)))
        }
        let target = !out.isEmpty || firstJwt(selection) != nil ? nil : tlsTarget(selection)
        if let target {
            let shown = target.replacingOccurrences(of: #"^\w+://"#, with: "", options: .regularExpression)
            out.append(("Inspect TLS Certificate of \(shown)…", .inspectTls(target)))
        }
        return out
    }

    /// `16 bytes`, `1.5 KB`.
    public static func size(_ n: Int, bytes: String = "bytes") -> String {
        n < 1024 ? "\(n) \(bytes)" : String(format: "%.1f KB", Double(n) / 1024)
    }
}
