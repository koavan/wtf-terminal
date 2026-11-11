import Testing
@testable import WtfCore

// The same cases as TestSlug in internal/worktree.
@Test func worktreeSlug() {
    let cases = ["feat/login": "feat-login", "fix login  bug": "fix-login-bug", "-weird-": "weird", "日本": "worktree", "v1.2_rc": "v1.2_rc"]
    for (input, want) in cases { #expect(Worktrees.slug(input) == want, "\(input)") }
}

@Test func worktreeRepoHelpers() {
    let repo = Go.worktree.Repo(
        main: "/src/api", name: "api", current: "/src/api.worktrees/login",
        worktrees: [
            .init(path: "/src/api", branch: "main", head: "1111111", main: true),
            .init(path: "/src/api.worktrees/login", branch: "feat/login", head: "2222222"),
            .init(path: "/src/api.worktrees/gone", branch: "", head: "3333333", prunable: true),
        ],
        branches: ["feat/login", "main", "fix/x"], parent: "/src/api.worktrees")
    #expect(Worktrees.free(repo) == ["fix/x"])
    #expect(Worktrees.current(repo) == "feat/login")
    #expect(Worktrees.suggest(repo, branch: "fix/y z") == "/src/api.worktrees/fix-y-z")
    #expect(Worktrees.containing("/src/api.worktrees/login/cmd", in: repo)?.branch == "feat/login")
    #expect(Worktrees.containing("/src/api/cmd", in: repo)?.main == true)
    #expect(Worktrees.containing("/src/apix", in: repo) == nil)
    #expect(Worktrees.title(repo.worktrees[2], home: "/src") == "detached at 3333333 — ~/api.worktrees/gone (folder gone)")
    #expect(Worktrees.title(repo.worktrees[0], home: "") == "main — /src/api (main checkout)")
}

@Test func worktreeBranchLabel() {
    #expect(Worktrees.label(.init()) == "")
    #expect(Worktrees.label(.init(branch: "feat/login", ahead: 2, behind: 1)) == "feat/login ↑2 ↓1")
    #expect(Worktrees.label(.init(branch: "1a2b3c4", detached: true)) == "detached at 1a2b3c4")
}
