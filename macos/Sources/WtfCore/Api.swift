// The API tab's request, apart from its views: titles, which headers hold
// credentials, what is kept in the saved session, and the query string
// shown as a list of parameters.
import Foundation

public enum Api {
    public typealias Request = Go.apiclient.Request
    public typealias Field = Go.apiclient.Header

    public static let methods = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]

    /// A new tab's request.
    public static var blank: Request { Request(method: "GET") }

    /// What the tab is called: the method and where it goes.
    public static func title(_ r: Request?) -> String {
        guard let r else { return "API" }
        var target = r.url.trimmingCharacters(in: .whitespaces)
        if let s = target.range(of: "://") { target = String(target[s.upperBound...]) }
        if let q = target.firstIndex(where: { $0 == "?" || $0 == "#" }) { target = String(target[..<q]) }
        if target.hasSuffix("/"), target.count > 1 { target.removeLast() }
        if target.isEmpty { return "API" }
        return (r.method.isEmpty ? "GET" : r.method.uppercased()) + " " + target
    }

    /// Header names whose values are credentials by definition or by name:
    /// shown masked, and never written to the saved session.
    public static func isSecret(_ name: String) -> Bool {
        let n = name.lowercased().trimmingCharacters(in: .whitespaces)
        if ["authorization", "proxy-authorization", "cookie", "set-cookie"].contains(n) { return true }
        return ["token", "secret", "password", "api-key", "apikey", "api_key", "session"].contains { n.contains($0) }
    }

    /// The request as written to the saved session: credentials left out.
    public static func forSaving(_ r: Request) -> Request {
        var r = r
        r.headers = r.headers.map { isSecret($0.name) ? Field(name: $0.name, value: "", off: $0.off) : $0 }
        return r
    }

    // MARK: Query parameters

    /// The URL's query as name/value pairs, decoded.
    public static func params(_ url: String) -> [Field] {
        guard let query = split(url).query, !query.isEmpty else { return [] }
        return query.split(separator: "&", omittingEmptySubsequences: true).map { part in
            let kv = part.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
            let name = String(kv[0]), value = kv.count > 1 ? String(kv[1]) : ""
            return Field(name: name.removingPercentEncoding ?? name, value: value.removingPercentEncoding ?? value)
        }
    }

    /// The URL with its query rebuilt from `params` (off ones left out).
    public static func url(_ url: String, params: [Field]) -> String {
        let parts = split(url)
        let on = params.filter { !$0.off && !($0.name.isEmpty && $0.value.isEmpty) }
        var out = parts.base
        if !on.isEmpty {
            out += "?" + on.map { p in
                encode(p.name) + (p.value.isEmpty && !p.name.isEmpty ? "" : "=" + encode(p.value))
            }.joined(separator: "&")
        }
        if let f = parts.fragment { out += "#" + f }
        return out
    }

    /// Characters a query name or value may hold as they are.
    private static let queryValue: CharacterSet = {
        var s = CharacterSet.urlQueryAllowed
        s.remove(charactersIn: "&=+#?")
        return s
    }()

    private static func encode(_ s: String) -> String {
        s.addingPercentEncoding(withAllowedCharacters: queryValue) ?? s
    }

    /// The URL before its "?", the query, and the fragment.
    static func split(_ url: String) -> (base: String, query: String?, fragment: String?) {
        var rest = Substring(url)
        var fragment: String?
        if let h = rest.firstIndex(of: "#") {
            fragment = String(rest[rest.index(after: h)...])
            rest = rest[..<h]
        }
        guard let q = rest.firstIndex(of: "?") else { return (String(rest), nil, fragment) }
        return (String(rest[..<q]), String(rest[rest.index(after: q)...]), fragment)
    }

    // MARK: Response

    /// "1.2 KB": the size of a response body.
    public static func size(_ bytes: Int) -> String {
        if bytes < 1024 { return "\(bytes) B" }
        let kb = Double(bytes) / 1024
        if kb < 1024 { return String(format: kb < 10 ? "%.1f KB" : "%.0f KB", kb) }
        let mb = kb / 1024
        return String(format: mb < 10 ? "%.1f MB" : "%.0f MB", mb)
    }

    /// "240 ms", "1.3 s": how long a request took.
    public static func duration(ms: Int) -> String {
        ms < 1000 ? "\(ms) ms" : String(format: "%.1f s", Double(ms) / 1000)
    }

    /// Whether a body labelled with this content type is JSON.
    public static func isJSON(contentType: String) -> Bool {
        let t = contentType.lowercased().split(separator: ";").first.map { $0.trimmingCharacters(in: .whitespaces) } ?? ""
        return t == "application/json" || t.hasSuffix("+json")
    }
}
