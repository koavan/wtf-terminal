package uicore

import (
	"testing"

	"github.com/dayanraj/wtf/internal/sshhosts"
)

func TestShellQuoteOnlyWhenNeeded(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/local/bin": "/usr/local/bin", "my file": "'my file'", "it's": `'it'\''s'`, "-rf": "'-rf'", "": "''",
	} {
		if got := ShellQuote(in); got != want {
			t.Errorf("%q → %s, want %s", in, got, want)
		}
	}
}

func TestUseIdentity(t *testing.T) {
	i := sshhosts.GitIdentity{Name: "work", AuthorName: "Ana B", KeyPath: "/k/id ed"}
	want := `export GIT_SSH_COMMAND='ssh -i '\''/k/id ed'\'' -o IdentitiesOnly=yes' GIT_AUTHOR_NAME='Ana B' GIT_COMMITTER_NAME='Ana B' WTF_GIT_IDENTITY=work; unset GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL`
	if got := UseIdentity(i); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
