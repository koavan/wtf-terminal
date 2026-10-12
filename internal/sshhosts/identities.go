package sshhosts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Git identities: who a tab works as on GitHub. Each pairs an SSH key with
// the author name and email to commit as. Using one in a tab exports
// GIT_SSH_COMMAND (that key only) and GIT_AUTHOR_*/GIT_COMMITTER_* in its
// shell, so two tabs can push to GitHub as two different users. Nothing in
// ~/.ssh or ~/.gitconfig changes; the identities live in wtf's own file.

// GitIdentity is one saved identity.
type GitIdentity struct {
	ID string `json:"id"`
	// What the user calls it ("work", "personal"); unique.
	Name        string `json:"name"`
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	// Absolute path of the private key.
	KeyPath string `json:"keyPath"`
	// The GitHub account the key logged in as at the last test, and when (Unix ms).
	GitHubUser string `json:"githubUser"`
	TestedAt   int64  `json:"testedAt"`
}

// GitHubTest is the outcome of logging in to GitHub with an identity's key.
type GitHubTest struct {
	OK bool `json:"ok"`
	// The account GitHub greeted, when OK.
	User string `json:"user"`
	// What ssh said, when not OK.
	Message string `json:"message"`
}

type identityStore struct {
	file string
	mu   sync.Mutex
	list []GitIdentity
}

func newIdentityStore(file string) *identityStore {
	st := &identityStore{file: file, list: []GitIdentity{}}
	if data, err := os.ReadFile(file); err == nil {
		var saved struct {
			Identities []GitIdentity `json:"identities"`
		}
		if json.Unmarshal(data, &saved) == nil {
			for _, id := range saved.Identities {
				if id.ID != "" && validateIdentity(&id, st.list) == nil {
					st.list = append(st.list, id)
				}
			}
		}
	}
	return st
}

func (st *identityStore) snapshot() []GitIdentity {
	return append([]GitIdentity{}, st.list...)
}

// save writes the identities. Called with mu held.
func (st *identityStore) save() error {
	data, err := json.MarshalIndent(struct {
		Identities []GitIdentity `json:"identities"`
	}{st.list}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.file), 0o700); err != nil {
		return err
	}
	tmp := st.file + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.file)
}

func (st *identityStore) index(id string) int {
	for i, x := range st.list {
		if x.ID == id {
			return i
		}
	}
	return -1
}

// validateIdentity checks an identity against the others in list (it may
// be among them, by ID). Everything ends up on a shell command line, so
// control characters are refused outright.
func validateIdentity(id *GitIdentity, list []GitIdentity) error {
	id.Name = strings.TrimSpace(id.Name)
	id.AuthorName = strings.TrimSpace(id.AuthorName)
	id.AuthorEmail = strings.TrimSpace(id.AuthorEmail)
	switch {
	case id.Name == "":
		return errors.New("give the identity a name")
	case len(id.Name) > 64:
		return errors.New("the name is too long")
	case id.KeyPath == "" || !filepath.IsAbs(id.KeyPath):
		return errors.New("choose an SSH key")
	}
	for _, v := range []string{id.Name, id.AuthorName, id.AuthorEmail, id.KeyPath} {
		if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return errors.New("names, email and key path can't contain control characters")
		}
	}
	if id.AuthorEmail != "" && !strings.Contains(id.AuthorEmail, "@") {
		return fmt.Errorf("%q isn't an email address", id.AuthorEmail)
	}
	for _, other := range list {
		if other.ID != id.ID && strings.EqualFold(other.Name, id.Name) {
			return fmt.Errorf("there's already an identity called %q", other.Name)
		}
	}
	return nil
}

// GitIdentities lists the saved identities.
func (s *Service) GitIdentities() []GitIdentity {
	s.identities.mu.Lock()
	defer s.identities.mu.Unlock()
	return s.identities.snapshot()
}

// SaveGitIdentity adds an identity (empty ID) or replaces one.
func (s *Service) SaveGitIdentity(in GitIdentity) ([]GitIdentity, error) {
	st := s.identities
	st.mu.Lock()
	defer st.mu.Unlock()
	if in.KeyPath != "" {
		if fi, err := os.Stat(in.KeyPath); err != nil || !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("no key file at %s", in.KeyPath)
		}
	}
	if err := validateIdentity(&in, st.list); err != nil {
		return nil, err
	}
	i := -1
	if in.ID != "" {
		if i = st.index(in.ID); i < 0 {
			return nil, errors.New("that identity no longer exists")
		}
		// The last test's answer is wtf's, not the editor's; with a different
		// key it no longer says who GitHub will see.
		in.GitHubUser, in.TestedAt = "", 0
		if old := st.list[i]; old.KeyPath == in.KeyPath {
			in.GitHubUser, in.TestedAt = old.GitHubUser, old.TestedAt
		}
		st.list[i] = in
	} else {
		in.ID = newID()
		in.GitHubUser, in.TestedAt = "", 0
		st.list = append(st.list, in)
	}
	if err := st.save(); err != nil {
		return nil, err
	}
	return st.snapshot(), nil
}

// DeleteGitIdentity forgets an identity. Its key stays in ~/.ssh; shells
// using it keep their variables until they change them.
func (s *Service) DeleteGitIdentity(id string) ([]GitIdentity, error) {
	st := s.identities
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.index(id)
	if i < 0 {
		return st.snapshot(), nil
	}
	st.list = append(st.list[:i], st.list[i+1:]...)
	if err := st.save(); err != nil {
		return nil, err
	}
	return st.snapshot(), nil
}

const githubTestTimeout = 15 * time.Second

// githubHello is GitHub's answer to `ssh -T git@github.com`.
var githubHello = regexp.MustCompile(`Hi ([A-Za-z0-9-]+(?:\[bot\])?)! You've successfully authenticated`)

// TestGitIdentity logs in to GitHub with only the identity's key, the way
// git will in a tab using it, and remembers the account GitHub names.
// BatchMode: ssh never prompts; a key with a passphrase must be in the agent.
func (s *Service) TestGitIdentity(id string) (GitHubTest, error) {
	s.identities.mu.Lock()
	i := s.identities.index(id)
	var ident GitIdentity
	if i >= 0 {
		ident = s.identities.list[i]
	}
	s.identities.mu.Unlock()
	if i < 0 {
		return GitHubTest{}, errors.New("that identity no longer exists")
	}

	ctx, cancel := context.WithTimeout(context.Background(), githubTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshTool("ssh"), githubTestArgs(ident.KeyPath)...)
	out, _ := cmd.CombinedOutput() // GitHub ends the session with exit 1 even on success
	res := parseGitHubTest(string(out))
	if ctx.Err() != nil {
		res = GitHubTest{Message: "GitHub didn't answer within 15 seconds"}
	}
	if res.OK {
		s.identities.mu.Lock()
		if i := s.identities.index(id); i >= 0 && s.identities.list[i].KeyPath == ident.KeyPath {
			s.identities.list[i].GitHubUser = res.User
			s.identities.list[i].TestedAt = time.Now().UnixMilli()
			_ = s.identities.save()
		}
		s.identities.mu.Unlock()
	}
	return res, nil
}

// githubTestArgs offers only this key (IdentitiesOnly), first, like
// GIT_SSH_COMMAND in a tab using the identity.
func githubTestArgs(keyPath string) []string {
	return []string{
		"-T", "-i", keyPath, "-o", "IdentitiesOnly=yes",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "git@github.com",
	}
}

func parseGitHubTest(out string) GitHubTest {
	if m := githubHello.FindStringSubmatch(out); m != nil {
		return GitHubTest{OK: true, User: m[1]}
	}
	msg := strings.TrimSpace(out)
	switch {
	case strings.Contains(msg, "Host key verification failed"):
		msg = "github.com isn't in known_hosts yet. Run `ssh -T git@github.com` once in a tab and check its fingerprint, or add it from Hosts → Server Host Keys."
	case strings.Contains(msg, "Permission denied (publickey)"):
		msg = "GitHub refused the key. Add its public key to the GitHub account (Settings → SSH and GPG keys). A key with a passphrase must be loaded into ssh-agent first."
	case msg == "":
		msg = "ssh gave no answer"
	default:
		if lines := strings.Split(msg, "\n"); len(lines) > 3 {
			msg = strings.Join(lines[len(lines)-3:], "\n")
		}
	}
	return GitHubTest{Message: msg}
}
