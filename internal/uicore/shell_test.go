package uicore

import (
	"strings"
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

// More cases of the Svelte build's shell tests and gitIdentities tests.

func TestShellQuoteWhatTheShellWouldInterpret(t *testing.T) {
	for in, want := range map[string]string{
		"src/main.go":     "src/main.go",
		"My Docs/a b.txt": "'My Docs/a b.txt'",
		"$HOME":           "'$HOME'",
		"a;rm -rf x":      "'a;rm -rf x'",
		"*.go":            "'*.go'",
		"~/x":             "'~/x'",
	} {
		if got := ShellQuote(in); got != want {
			t.Errorf("%q → %s, want %s", in, got, want)
		}
	}
}

func testIdentity(f func(*sshhosts.GitIdentity)) sshhosts.GitIdentity {
	i := sshhosts.GitIdentity{ID: "1", Name: "work", AuthorName: "Ada L", AuthorEmail: "ada@work.example", KeyPath: "/Users/ada/.ssh/id_work"}
	if f != nil {
		f(&i)
	}
	return i
}

func TestUseIdentityOffersOnlyTheKeyAndCommitsAsTheAuthor(t *testing.T) {
	want := "export GIT_SSH_COMMAND='ssh -i /Users/ada/.ssh/id_work -o IdentitiesOnly=yes'" +
		" GIT_AUTHOR_NAME='Ada L' GIT_COMMITTER_NAME='Ada L'" +
		" GIT_AUTHOR_EMAIL=ada@work.example GIT_COMMITTER_EMAIL=ada@work.example WTF_GIT_IDENTITY=work"
	if got := UseIdentity(testIdentity(nil)); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestUseIdentityQuotesAKeyPathTwice(t *testing.T) {
	got := UseIdentity(testIdentity(func(i *sshhosts.GitIdentity) { i.KeyPath = "/Users/a b/.ssh/it's" }))
	want := `GIT_SSH_COMMAND='ssh -i '\''/Users/a b/.ssh/it'\''\'\'''\''s'\'' -o IdentitiesOnly=yes'`
	if !strings.Contains(got, want) {
		t.Errorf("got  %s\nwant it to contain %s", got, want)
	}
}

func TestUseIdentityFallsBackToGitConfigForABlankAuthor(t *testing.T) {
	got := UseIdentity(testIdentity(func(i *sshhosts.GitIdentity) { i.AuthorName, i.AuthorEmail = "", "" }))
	if want := "WTF_GIT_IDENTITY=work; unset GIT_AUTHOR_NAME GIT_COMMITTER_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL"; !strings.HasSuffix(got, want) {
		t.Errorf("got %s", got)
	}
}

func TestStopIdentityUnsetsEveryVariable(t *testing.T) {
	for _, v := range []string{"GIT_SSH_COMMAND", "GIT_AUTHOR_NAME", "GIT_COMMITTER_EMAIL", "WTF_GIT_IDENTITY"} {
		if !strings.Contains(StopIdentity, v) {
			t.Errorf("StopIdentity lacks %s", v)
		}
	}
}
