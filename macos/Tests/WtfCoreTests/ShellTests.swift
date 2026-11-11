import Testing
@testable import WtfCore

@Test func quotesOnlyWhenNeeded() {
    #expect(Shell.quote("/usr/local/bin") == "/usr/local/bin")
    #expect(Shell.quote("my file") == "'my file'")
    #expect(Shell.quote("it's") == #"'it'\''s'"#)
    #expect(Shell.quote("-rf") == "'-rf'")
    #expect(Shell.quote("") == "''")
}

@Test func identityCommand() {
    let i = Shell.GitIdentity(name: "work", authorName: "Ana B", authorEmail: "", keyPath: "/k/id ed")
    #expect(Shell.useIdentity(i) == #"export GIT_SSH_COMMAND='ssh -i '\''/k/id ed'\'' -o IdentitiesOnly=yes' GIT_AUTHOR_NAME='Ana B' GIT_COMMITTER_NAME='Ana B' WTF_GIT_IDENTITY=work; unset GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL"#)
}

// More cases of the Svelte build's shell tests and gitIdentities tests.

@Test func quotesWhatTheShellWouldInterpret() {
    #expect(Shell.quote("src/main.go") == "src/main.go")
    #expect(Shell.quote("My Docs/a b.txt") == "'My Docs/a b.txt'")
    #expect(Shell.quote("$HOME") == "'$HOME'")
    #expect(Shell.quote("a;rm -rf x") == "'a;rm -rf x'")
    #expect(Shell.quote("*.go") == "'*.go'")
    #expect(Shell.quote("~/x") == "'~/x'")
}

private func ident(authorName: String = "Ada L", authorEmail: String = "ada@work.example", keyPath: String = "/Users/ada/.ssh/id_work") -> Shell.GitIdentity {
    Shell.GitIdentity(name: "work", authorName: authorName, authorEmail: authorEmail, keyPath: keyPath)
}

@Test func identityOffersOnlyTheKeyAndCommitsAsTheAuthor() {
    #expect(Shell.useIdentity(ident()) == "export GIT_SSH_COMMAND='ssh -i /Users/ada/.ssh/id_work -o IdentitiesOnly=yes'"
        + " GIT_AUTHOR_NAME='Ada L' GIT_COMMITTER_NAME='Ada L'"
        + " GIT_AUTHOR_EMAIL=ada@work.example GIT_COMMITTER_EMAIL=ada@work.example WTF_GIT_IDENTITY=work")
}

@Test func identityQuotesAKeyPathTwice() {
    #expect(Shell.useIdentity(ident(keyPath: "/Users/a b/.ssh/it's"))
        .contains(#"GIT_SSH_COMMAND='ssh -i '\''/Users/a b/.ssh/it'\''\'\'''\''s'\'' -o IdentitiesOnly=yes'"#))
}

@Test func identityFallsBackToGitConfigForABlankAuthor() {
    #expect(Shell.useIdentity(ident(authorName: "", authorEmail: ""))
        .hasSuffix("WTF_GIT_IDENTITY=work; unset GIT_AUTHOR_NAME GIT_COMMITTER_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL"))
}
