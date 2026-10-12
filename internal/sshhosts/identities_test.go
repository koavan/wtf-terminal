package sshhosts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGitIdentities(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "id_work")
	os.WriteFile(key, []byte("key"), 0o600)
	file := filepath.Join(dir, "state", "git-identities.json")
	s := &Service{identities: newIdentityStore(file)}

	list, err := s.SaveGitIdentity(GitIdentity{Name: " work ", AuthorName: "Ada", AuthorEmail: "ada@work.example", KeyPath: key})
	if err != nil || len(list) != 1 || list[0].ID == "" || list[0].Name != "work" {
		t.Fatalf("save: %v %+v", err, list)
	}
	work := list[0]

	for _, bad := range []GitIdentity{
		{Name: "WORK", KeyPath: key},                         // same name
		{Name: "x", KeyPath: filepath.Join(dir, "missing")},  // no key file
		{Name: "x", KeyPath: key, AuthorEmail: "nope"},       // not an email
		{Name: "x", KeyPath: key, AuthorName: "a\nrm -rf ~"}, // control character
		{Name: "", KeyPath: key},
	} {
		if _, err := s.SaveGitIdentity(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	// A remembered GitHub user goes when the key changes.
	s.identities.list[0].GitHubUser = "ada-work"
	work.AuthorName = "Ada L"
	if l, _ := s.SaveGitIdentity(work); l[0].GitHubUser != "ada-work" {
		t.Errorf("kept key, lost user: %+v", l[0])
	}
	key2 := filepath.Join(dir, "id_other")
	os.WriteFile(key2, []byte("key"), 0o600)
	work.KeyPath = key2
	if l, _ := s.SaveGitIdentity(work); l[0].GitHubUser != "" {
		t.Errorf("new key, kept user: %+v", l[0])
	}

	// Saved, private, and read back.
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", err, fi)
	}
	again := newIdentityStore(file)
	if len(again.list) != 1 || again.list[0].AuthorName != "Ada L" {
		t.Errorf("reloaded %+v", again.list)
	}

	if l, err := s.DeleteGitIdentity(work.ID); err != nil || len(l) != 0 {
		t.Errorf("delete: %v %+v", err, l)
	}
}

func TestParseGitHubTest(t *testing.T) {
	ok := parseGitHubTest("Hi ada-work! You've successfully authenticated, but GitHub does not provide shell access.\n")
	if !ok.OK || ok.User != "ada-work" {
		t.Errorf("%+v", ok)
	}
	if r := parseGitHubTest("git@github.com: Permission denied (publickey).\n"); r.OK || !strings.Contains(r.Message, "refused the key") {
		t.Errorf("%+v", r)
	}
	if r := parseGitHubTest("Host key verification failed.\n"); r.OK || !strings.Contains(r.Message, "known_hosts") {
		t.Errorf("%+v", r)
	}
}

func TestGitHubTestArgsOfferOnlyTheKey(t *testing.T) {
	args := githubTestArgs("/k/id")
	for _, want := range []string{"IdentitiesOnly=yes", "BatchMode=yes", "/k/id", "git@github.com"} {
		if !slices.Contains(args, want) {
			t.Errorf("%v lacks %s", args, want)
		}
	}
}
