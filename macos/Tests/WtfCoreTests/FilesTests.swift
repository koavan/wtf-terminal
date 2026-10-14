import Testing
@testable import WtfCore

// The cases of frontend/src/lib/gitstatus.test.ts and fileActions.test.ts.

private func change(_ p: String, _ s: String, staged: Bool = false, unstaged: Bool = true) -> Go.files.GitChange {
    Go.files.GitChange(path: p, status: s, staged: staged, unstaged: unstaged)
}

private let idx = GitIndex(Go.files.GitStatus(root: "/repo", branch: "main", upstream: "origin/main", changes: [
    change("/repo/src/app.go", "modified"), change("/repo/src/lib/x.go", "conflict"),
    change("/repo/new/", "untracked"), change("/repo/README.md", "added"),
]))

@Test func knowsEachChangedFile() {
    #expect(idx.file("/repo/src/app.go")?.status == "modified")
    #expect(idx.file("/repo/README.md")?.status == "added")
    #expect(idx.file("/repo/src/other.go") == nil)
}

@Test func everythingInAnUntrackedFolderIsNew() {
    #expect(idx.file("/repo/new/deep/file.txt")?.status == "untracked")
    #expect(idx.folder("/repo/new") == .untracked)
    #expect(idx.folder("/repo/new/deep") == .untracked)
}

@Test func foldersShowTheChangeThatMattersMost() {
    #expect(idx.folder("/repo/src") == .conflict)
    #expect(idx.folder("/repo/src/lib") == .conflict)
    #expect(idx.folder("/repo") == .conflict)
    #expect(idx.folder("/repo/docs") == nil)
    #expect(idx.count == 4)
    #expect(idx.folder("/") == nil)
}

@Test func branchLabels() {
    #expect(GitIndex.branchLabel(Go.files.GitStatus(branch: "main", ahead: 2, behind: 1)) == "main ↑2 ↓1")
    #expect(GitIndex.branchLabel(Go.files.GitStatus(branch: "abc1234", detached: true)) == "detached at abc1234")
    #expect(GitIndex.branchLabel(Go.files.GitStatus()) == "no commits yet")
}

private let menuIdx = GitIndex(Go.files.GitStatus(root: "/repo", branch: "main", changes: [
    change("/repo/src/app.go", "modified"), change("/repo/src/new file.go", "untracked"),
    change("/repo/staged.go", "modified", staged: true, unstaged: false),
]))

@Test func gitMenus() {
    #expect(menuIdx.menu("/repo/src/app.go", dir: false) == [
        .heading("Git: Modified · not staged"),
        .run(label: "Show Changes", command: "git diff HEAD -- src/app.go"),
        .run(label: "Show History", command: "git log --follow --oneline -- src/app.go"),
    ])
    #expect(menuIdx.menu("/repo/src/new file.go", dir: false) == [
        .heading("Git: Untracked (new)"),
        .run(label: "Show Changes", command: "git diff --no-index -- /dev/null 'src/new file.go'"),
    ])
    #expect(menuIdx.menu("/repo/staged.go", dir: false)[1] == .run(label: "Show Changes", command: "git diff --cached -- staged.go"))
    #expect(menuIdx.menu("/repo/README.md", dir: false) == [.run(label: "Show History", command: "git log --follow --oneline -- README.md")])
    #expect(menuIdx.menu("/repo/src", dir: true).count == 4)
    #expect(menuIdx.menu("/elsewhere/x.go", dir: false).isEmpty)
    #expect(GitIndex.summary(change("", "modified", staged: true, unstaged: true)) == "Modified · staged and unstaged")
}

private func entry(_ path: String, dir: Bool = false) -> Go.files.Entry {
    Go.files.Entry(name: Paths.base(path), path: path, dir: dir)
}

@Test func treeRowsAndFilter() {
    var t = FileTree()
    t.folders["/r"] = .init(entries: [entry("/r/src", dir: true), entry("/r/docs", dir: true), entry("/r/README.md")], truncated: true)
    t.folders["/r/src"] = .init(entries: [entry("/r/src/app.go"), entry("/r/src/util.go")])
    t.expanded = ["/r/src"]
    // src, its two files, docs, README.md, and the "more" marker.
    #expect(t.rows("/r").count == 6)
    // A match deep down keeps the open folders leading to it.
    #expect(t.rows("/r", filter: "util") == [.entry(entry("/r/src", dir: true), depth: 0), .entry(entry("/r/src/util.go"), depth: 1)])
    #expect(t.rows("/r", filter: "nothing").isEmpty)
    #expect(Paths.relative("/r", "/r/src/a.go") == "src/a.go" && Paths.relative("/r", "/r") == "." && Paths.relative("/r", "/x") == "/x")
    #expect(Paths.parent("/a") == "/" && Paths.base("/") == "/")
    t.visit("pane:1", "/a"); t.visit("pane:1", "/a"); t.visit("pane:1", "/b")
    #expect(t.history["pane:1"] == ["/a", "/b"])
}
