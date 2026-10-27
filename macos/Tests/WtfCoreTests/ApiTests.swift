import Foundation
import Testing
@testable import WtfCore

@Test func apiTitles() {
    #expect(Api.title(nil) == "API")
    #expect(Api.title(Api.blank) == "API")
    #expect(Api.title(.init(method: "post", url: "https://api.example.com/v1/users/?page=2#x")) == "POST api.example.com/v1/users")
    #expect(Api.title(.init(method: "", url: "localhost:8080")) == "GET localhost:8080")
}

@Test func apiSecretHeaders() {
    for n in ["Authorization", " cookie ", "X-Auth-Token", "X-API-Key", "apikey", "X-Session-Id", "Proxy-Authorization"] {
        #expect(Api.isSecret(n), "\(n)")
    }
    for n in ["Accept", "Content-Type", "User-Agent", "X-Request-Id"] {
        #expect(!Api.isSecret(n), "\(n)")
    }
    let r = Api.Request(method: "GET", url: "x", headers: [
        .init(name: "Authorization", value: "Bearer abc"), .init(name: "Accept", value: "*/*"), .init(name: "Cookie", value: "a=1", off: true),
    ])
    #expect(Api.forSaving(r).headers == [.init(name: "Authorization"), .init(name: "Accept", value: "*/*"), .init(name: "Cookie", off: true)])
}

@Test func apiParams() {
    let url = "https://x.io/a?q=hello%20world&flag&empty=&a%26b=c%3Dd#top"
    let p = Api.params(url)
    #expect(p == [.init(name: "q", value: "hello world"), .init(name: "flag"), .init(name: "empty"), .init(name: "a&b", value: "c=d")])
    #expect(Api.url(url, params: p) == "https://x.io/a?q=hello%20world&flag&empty&a%26b=c%3Dd#top")
    var edited = p
    edited[0].off = true
    edited.append(.init(name: "new", value: "1+1"))
    #expect(Api.url(url, params: edited) == "https://x.io/a?flag&empty&a%26b=c%3Dd&new=1%2B1#top")
    #expect(Api.url("https://x.io/a?b=1", params: []) == "https://x.io/a")
    #expect(Api.params("https://x.io") == [])
}

@Test func apiFormatting() {
    #expect(Api.size(512) == "512 B")
    #expect(Api.size(1536) == "1.5 KB")
    #expect(Api.size(3 << 20) == "3.0 MB")
    #expect(Api.duration(ms: 240) == "240 ms")
    #expect(Api.duration(ms: 1340) == "1.3 s")
    #expect(Api.isJSON(contentType: "application/json; charset=utf-8"))
    #expect(Api.isJSON(contentType: "application/problem+json"))
    #expect(!Api.isJSON(contentType: "text/html"))
}

@Test func apiTabsSaveWithoutCredentialsAndMoveWithThem() throws {
    let w = WindowModel()
    w.open(cwd: "/t")
    let r = Api.Request(method: "POST", url: "https://x.io", headers: [.init(name: "Authorization", value: "Bearer s3cret")], body: "{}")
    let id = w.openApi(r)
    #expect(w.title(w.tab(id)!) == "POST x.io")
    #expect(w.activePane?.kind == .api)

    // Saved: the request without its credential.
    let saved = try JSONDecoder().decode(SavedSession.self, from: JSONEncoder().encode(w.snapshot(fontSize: 13)))
    let back = WindowModel()
    #expect(back.restore(saved))
    let pane = back.panes.values.first { $0.kind == .api }
    #expect(pane?.api?.url == "https://x.io")
    #expect(pane?.api?.body == "{}")
    #expect(pane?.api?.headers == [.init(name: "Authorization")])
    #expect(back.panes.values.filter { $0.kind == .terminal }.count == 1)

    // Moved to another window: all of it.
    let payload = w.detach(id, buffers: { _ in "" })!
    let other = WindowModel()
    #expect(other.adopt(payload))
    #expect(other.activePane?.kind == .api)
    #expect(other.activePane?.api == r)
}

@Test func apiTabsDuplicateAndReopen() {
    let w = WindowModel()
    w.open(cwd: "/t")
    let r = Api.Request(method: "GET", url: "https://x.io/a")
    let id = w.openApi(r)
    let dup = w.openNextTo(id, duplicate: true)!
    #expect(w.focusedPane(w.tab(dup)!)?.api == r)
    w.close(dup)
    w.close(id)
    w.reopen()
    #expect(w.activePane?.kind == .api)
    #expect(w.activePane?.api == r)
    // Workspaces leave API tabs out.
    #expect(w.captureWorkspace().tabs.count == 1)
}
