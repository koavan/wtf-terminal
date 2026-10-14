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
