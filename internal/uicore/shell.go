package uicore

import (
	"regexp"
	"strings"

	"github.com/dayanraj/wtf/internal/sshhosts"
)

// Shell helpers: quoting, and switching a shell to a git identity. A port of
// macos/Sources/WtfCore/Shell.swift.

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// ShellQuote quotes for bash/zsh: as-is when safe, otherwise single-quoted.
func ShellQuote(s string) string {
	if s != "" && !strings.HasPrefix(s, "-") && shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ClearLine is Ctrl-U: it clears a half-typed command line before wtf
// types one.
const ClearLine = "\x15"

// IdentityVar is the variable a shell switched to a git identity reports.
const IdentityVar = "WTF_GIT_IDENTITY"

// StopIdentity is the command that puts a shell's git back as it was.
const StopIdentity = "unset GIT_SSH_COMMAND GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL " + IdentityVar

// UseIdentity is the command that makes git in this shell use the
// identity's key, and commit as its author.
func UseIdentity(i sshhosts.GitIdentity) string {
	// git runs GIT_SSH_COMMAND through sh, so the key path is quoted inside it too.
	ssh := "ssh -i " + ShellQuote(i.KeyPath) + " -o IdentitiesOnly=yes"
	set := []string{"GIT_SSH_COMMAND=" + ShellQuote(ssh)}
	var unset []string
	for _, f := range []struct {
		value string
		names []string
	}{
		{i.AuthorName, []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"}},
		{i.AuthorEmail, []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"}},
	} {
		if f.value == "" {
			unset = append(unset, f.names...)
			continue
		}
		for _, n := range f.names {
			set = append(set, n+"="+ShellQuote(f.value))
		}
	}
	set = append(set, IdentityVar+"="+ShellQuote(i.Name))
	cmd := "export " + strings.Join(set, " ")
	if len(unset) > 0 {
		cmd += "; unset " + strings.Join(unset, " ")
	}
	return cmd
}
