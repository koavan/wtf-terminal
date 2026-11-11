package uicore

import (
	"reflect"
	"testing"

	"github.com/dayanraj/wtf/internal/files"
)

// The cases of the Svelte build's gitstatus tests and fileActions tests.

func gitChange(p, s string, staged, unstaged bool) files.GitChange {
	return files.GitChange{Path: p, Status: s, Staged: staged, Unstaged: unstaged}
}

func testIndex() *GitIndex {
	return NewGitIndex(files.GitStatus{Root: "/repo", Branch: "main", Upstream: "origin/main", Changes: []files.GitChange{
		gitChange("/repo/src/app.go", "modified", false, true), gitChange("/repo/src/lib/x.go", "conflict", false, true),
		gitChange("/repo/new/", "untracked", false, true), gitChange("/repo/README.md", "added", false, true),
	}})
}

func statusOf(x *GitIndex, path string) string {
	c, ok := x.File(path)
	if !ok {
		return "<none>"
	}
	return c.Status
}

func folderOf(x *GitIndex, path string) ChangeKind {
	k, ok := x.Folder(path)
	if !ok {
		return "<none>"
	}
	return k
}

func TestKnowsEachChangedFile(t *testing.T) {
	idx := testIndex()
	for path, want := range map[string]string{
		"/repo/src/app.go": "modified", "/repo/README.md": "added", "/repo/src/other.go": "<none>",
	} {
		if got := statusOf(idx, path); got != want {
			t.Errorf("File(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestEverythingInAnUntrackedFolderIsNew(t *testing.T) {
	idx := testIndex()
	if got := statusOf(idx, "/repo/new/deep/file.txt"); got != "untracked" {
		t.Errorf("got %q", got)
	}
	for _, p := range []string{"/repo/new", "/repo/new/deep"} {
		if got := folderOf(idx, p); got != ChangeUntracked {
			t.Errorf("Folder(%q) = %q", p, got)
		}
	}
}

func TestFoldersShowTheChangeThatMattersMost(t *testing.T) {
	idx := testIndex()
	for path, want := range map[string]ChangeKind{
		"/repo/src": ChangeConflict, "/repo/src/lib": ChangeConflict, "/repo": ChangeConflict,
		"/repo/docs": "<none>", "/": "<none>",
	} {
		if got := folderOf(idx, path); got != want {
			t.Errorf("Folder(%q) = %q, want %q", path, got, want)
		}
	}
	if idx.Count() != 4 {
		t.Errorf("Count = %d", idx.Count())
	}
}

func TestBranchLabels(t *testing.T) {
	for _, c := range []struct {
		s    files.GitStatus
		want string
	}{
		{files.GitStatus{Branch: "main", Ahead: 2, Behind: 1}, "main ↑2 ↓1"},
		{files.GitStatus{Branch: "abc1234", Detached: true}, "detached at abc1234"},
		{files.GitStatus{}, "no commits yet"},
	} {
		if got := GitBranchLabel(c.s); got != c.want {
			t.Errorf("GitBranchLabel(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestGitMenus(t *testing.T) {
	idx := NewGitIndex(files.GitStatus{Root: "/repo", Branch: "main", Changes: []files.GitChange{
		gitChange("/repo/src/app.go", "modified", false, true), gitChange("/repo/src/new file.go", "untracked", false, true),
		gitChange("/repo/staged.go", "modified", true, false),
	}})
	check := func(got, want []GitMenuItem) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	}
	check(idx.Menu("/repo/src/app.go", false), []GitMenuItem{
		gitHeading("Git: Modified · not staged"),
		gitRun("Show Changes", "git diff HEAD -- src/app.go"),
		gitRun("Show History", "git log --follow --oneline -- src/app.go"),
	})
	check(idx.Menu("/repo/src/new file.go", false), []GitMenuItem{
		gitHeading("Git: Untracked (new)"),
		gitRun("Show Changes", "git diff --no-index -- /dev/null 'src/new file.go'"),
	})
	check(idx.Menu("/repo/staged.go", false)[1:2], []GitMenuItem{gitRun("Show Changes", "git diff --cached -- staged.go")})
	check(idx.Menu("/repo/README.md", false), []GitMenuItem{gitRun("Show History", "git log --follow --oneline -- README.md")})
	if n := len(idx.Menu("/repo/src", true)); n != 4 {
		t.Errorf("folder menu has %d items", n)
	}
	if m := idx.Menu("/elsewhere/x.go", false); len(m) != 0 {
		t.Errorf("outside menu %+v", m)
	}
	if got := GitChangeSummary(gitChange("", "modified", true, true)); got != "Modified · staged and unstaged" {
		t.Errorf("summary %q", got)
	}
}

func treeEntry(path string, dir bool) files.Entry {
	return files.Entry{Name: BaseName(path), Path: path, Dir: dir}
}

func TestTreeRowsAndFilter(t *testing.T) {
	tr := NewFileTree()
	tr.Folders["/r"] = FileFolder{Entries: []files.Entry{treeEntry("/r/src", true), treeEntry("/r/docs", true), treeEntry("/r/README.md", false)}, Truncated: true}
	tr.Folders["/r/src"] = FileFolder{Entries: []files.Entry{treeEntry("/r/src/app.go", false), treeEntry("/r/src/util.go", false)}}
	tr.Expanded["/r/src"] = true
	// src, its two files, docs, README.md, and the "more" marker.
	if n := len(tr.Rows("/r", "")); n != 6 {
		t.Errorf("%d rows", n)
	}
	// A match deep down keeps the open folders leading to it.
	want := []FileRow{{Entry: treeEntry("/r/src", true), Depth: 0}, {Entry: treeEntry("/r/src/util.go", false), Depth: 1}}
	if got := tr.Rows("/r", "util"); !reflect.DeepEqual(got, want) {
		t.Errorf("filtered %+v", got)
	}
	if got := tr.Rows("/r", "nothing"); len(got) != 0 {
		t.Errorf("no match gave %+v", got)
	}
	if RelativePath("/r", "/r/src/a.go") != "src/a.go" || RelativePath("/r", "/r") != "." || RelativePath("/r", "/x") != "/x" {
		t.Error("RelativePath")
	}
	if ParentPath("/a") != "/" || BaseName("/") != "/" {
		t.Error("ParentPath or BaseName")
	}
	tr.Visit("pane:1", "/a")
	tr.Visit("pane:1", "/a")
	tr.Visit("pane:1", "/b")
	if got := tr.History["pane:1"]; !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Errorf("history %v", got)
	}
}

// More cases of the Svelte build's shell tests (paths) and fileActions tests.

func TestPathsRelativeParentAndBase(t *testing.T) {
	for _, c := range [][3]string{
		{"/w/proj", "/w/proj/src/a.go", "src/a.go"},
		{"/w/proj", "/w/proj", "."},
		{"/w/proj", "/w/project/a.go", "/w/project/a.go"},
		{"/w/proj", "/etc/hosts", "/etc/hosts"},
		{"", "/etc/hosts", "/etc/hosts"},
	} {
		if got := RelativePath(c[0], c[1]); got != c[2] {
			t.Errorf("RelativePath(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	if got := ParentPath("/a/b/c"); got != "/a/b" {
		t.Errorf("ParentPath = %q", got)
	}
	if got := BaseName("/a/b/c.txt"); got != "c.txt" {
		t.Errorf("BaseName = %q", got)
	}
}

func TestGitFolderMenu(t *testing.T) {
	idx := NewGitIndex(files.GitStatus{Root: "/repo", Branch: "main", Changes: []files.GitChange{
		gitChange("/repo/src/app.go", "modified", false, true),
	}})
	want := []GitMenuItem{
		gitHeading("Git: changes inside"),
		gitRun("Show Status of Folder", "git status -- src"),
		gitRun("Show Changes in Folder", "git diff HEAD -- src"),
		gitRun("Show Folder History", "git log --oneline -- src"),
	}
	if got := idx.Menu("/repo/src", true); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}
