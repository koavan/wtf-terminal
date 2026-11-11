package uicore

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dayanraj/wtf/internal/files"
)

// The Files tab's logic: the folder tree from cached listings (filtered,
// expanded), git decorations, and the git menu's commands. A port of
// macos/Sources/WtfCore/Files.swift.

// RelativePath is path relative to base when inside it ("src/main.go"),
// else absolute.
func RelativePath(base, path string) string {
	if base == "" {
		return path
	}
	if path == base {
		return "."
	}
	b := base
	if !strings.HasSuffix(b, "/") {
		b += "/"
	}
	if strings.HasPrefix(path, b) {
		return path[len(b):]
	}
	return path
}

// ParentPath is the folder holding path; "/" at the top.
func ParentPath(path string) string {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "/"
	}
	return path[:i]
}

// BaseName is a path's last part; "/" for the root.
func BaseName(path string) string {
	if path == "/" {
		return "/"
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

// InsidePath reports whether path is dir or inside it.
func InsidePath(path, dir string) bool {
	if path == dir {
		return true
	}
	if dir == "/" {
		return strings.HasPrefix(path, "/")
	}
	return strings.HasPrefix(path, dir+"/")
}

// ChangeKind is what git says happened to a path.
type ChangeKind string

const (
	ChangeModified  ChangeKind = "modified"
	ChangeAdded     ChangeKind = "added"
	ChangeDeleted   ChangeKind = "deleted"
	ChangeRenamed   ChangeKind = "renamed"
	ChangeUntracked ChangeKind = "untracked"
	ChangeConflict  ChangeKind = "conflict"
)

// changeKindOf reads a GitChange's status; anything unknown counts as modified.
func changeKindOf(status string) ChangeKind {
	switch k := ChangeKind(status); k {
	case ChangeModified, ChangeAdded, ChangeDeleted, ChangeRenamed, ChangeUntracked, ChangeConflict:
		return k
	}
	return ChangeModified
}

// Letter is the letter beside a changed file, as in most editors.
func (k ChangeKind) Letter() string {
	switch k {
	case ChangeAdded:
		return "A"
	case ChangeDeleted:
		return "D"
	case ChangeRenamed:
		return "R"
	case ChangeUntracked:
		return "U"
	case ChangeConflict:
		return "!"
	}
	return "M"
}

func (k ChangeKind) Label() string {
	switch k {
	case ChangeAdded:
		return "Added"
	case ChangeDeleted:
		return "Deleted"
	case ChangeRenamed:
		return "Renamed"
	case ChangeUntracked:
		return "Untracked (new)"
	case ChangeConflict:
		return "Merge conflict"
	}
	return "Modified"
}

// Which kind wins when a folder holds several: the one that most needs attention.
var changeRank = []ChangeKind{ChangeConflict, ChangeModified, ChangeDeleted, ChangeRenamed, ChangeAdded, ChangeUntracked}

// GitIndex is what the file tree shows of git, built once per `git status`.
type GitIndex struct {
	Status files.GitStatus
	files  map[string]files.GitChange
	// Untracked folders: everything inside them is new.
	newFolders []string
	// Folders holding changes, with the kind that matters most.
	folders map[string]ChangeKind
}

func NewGitIndex(s files.GitStatus) *GitIndex {
	x := &GitIndex{Status: s, files: map[string]files.GitChange{}, folders: map[string]ChangeKind{}}
	rootLen := utf8.RuneCountInString(s.Root)
	for _, c := range s.Changes {
		isDir := strings.HasSuffix(c.Path, "/")
		path := strings.TrimSuffix(c.Path, "/")
		if isDir {
			x.newFolders = append(x.newFolders, path)
		} else {
			x.files[path] = c
		}
		kind := changeKindOf(c.Status)
		dir := ParentPath(path)
		for utf8.RuneCountInString(dir) >= rootLen && dir != path {
			if was, ok := x.folders[dir]; !ok || slices.Index(changeRank, was) > slices.Index(changeRank, kind) {
				x.folders[dir] = kind
			}
			if dir == s.Root || dir == "/" {
				break
			}
			dir = ParentPath(dir)
		}
		if isDir {
			x.folders[path] = ChangeUntracked
		}
	}
	return x
}

// File is a file's change; false when it's unchanged (or not in the repository).
func (x *GitIndex) File(path string) (files.GitChange, bool) {
	if c, ok := x.files[path]; ok {
		return c, true
	}
	for _, d := range x.newFolders {
		if strings.HasPrefix(path, d+"/") {
			return files.GitChange{Path: path, Status: string(ChangeUntracked), Unstaged: true}, true
		}
	}
	return files.GitChange{}, false
}

// Folder is the most pressing kind of change inside a folder; false when none.
func (x *GitIndex) Folder(path string) (ChangeKind, bool) {
	if k, ok := x.folders[path]; ok {
		return k, true
	}
	for _, d := range x.newFolders {
		if InsidePath(path, d) {
			return ChangeUntracked, true
		}
	}
	return "", false
}

// Count is how many changes git reported.
func (x *GitIndex) Count() int { return len(x.Status.Changes) }

// GitBranchLabel is "main ↑2 ↓1".
func GitBranchLabel(s files.GitStatus) string {
	var first string
	switch {
	case s.Detached:
		first = "detached at " + s.Branch
	case s.Branch == "":
		first = "no commits yet"
	default:
		first = s.Branch
	}
	parts := []string{first}
	if s.Ahead > 0 {
		parts = append(parts, "↑"+strconv.Itoa(s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, "↓"+strconv.Itoa(s.Behind))
	}
	return strings.Join(parts, " ")
}

// GitChangeSummary is "Modified · staged and unstaged".
func GitChangeSummary(c files.GitChange) string {
	kind := changeKindOf(c.Status)
	if kind == ChangeUntracked || kind == ChangeConflict {
		return kind.Label()
	}
	switch {
	case c.Staged && c.Unstaged:
		return kind.Label() + " · staged and unstaged"
	case c.Staged:
		return kind.Label() + " · staged"
	}
	return kind.Label() + " · not staged"
}

// GitMenuItem is one item of the git part of a file or folder's menu: a
// heading (Label only), or a git command to run from the repository root,
// in a new tab.
type GitMenuItem struct {
	Heading bool
	Label   string
	Command string
}

func gitHeading(label string) GitMenuItem { return GitMenuItem{Heading: true, Label: label} }
func gitRun(label, command string) GitMenuItem {
	return GitMenuItem{Label: label, Command: command}
}

// Menu is the git part of a file or folder's menu.
func (x *GitIndex) Menu(path string, dir bool) []GitMenuItem {
	root := x.Status.Root
	if root == "" || !InsidePath(path, root) {
		return nil
	}
	rel := ShellQuote(RelativePath(root, path))
	var out []GitMenuItem
	if dir {
		if _, ok := x.Folder(path); ok {
			out = append(out, gitHeading("Git: changes inside"),
				gitRun("Show Status of Folder", "git status -- "+rel),
				gitRun("Show Changes in Folder", "git diff HEAD -- "+rel))
		}
		return append(out, gitRun("Show Folder History", "git log --oneline -- "+rel))
	}
	c, changed := x.File(path)
	if changed {
		var diff string
		switch {
		case c.Status == "untracked":
			diff = "git diff --no-index -- /dev/null " + rel
		case c.Status == "conflict":
			diff = "git diff -- " + rel
		case c.Staged && !c.Unstaged:
			diff = "git diff --cached -- " + rel
		default:
			diff = "git diff HEAD -- " + rel
		}
		out = append(out, gitHeading("Git: "+GitChangeSummary(c)), gitRun("Show Changes", diff))
	}
	if !changed || (c.Status != "untracked" && c.Status != "added") {
		out = append(out, gitRun("Show History", "git log --follow --oneline -- "+rel))
	}
	return out
}

// FileFolder is one folder's cached listing.
type FileFolder struct {
	// Nil while not listed.
	Entries   []files.Entry
	Error     string
	Truncated bool
}

// FileRow is one row of the tree: an entry, or (More) the marker for a
// folder that had more entries than were listed.
type FileRow struct {
	Entry files.Entry
	Depth int
	More  bool
}

// FileTree is the folder tree: cached listings, which folders are open, and
// the rows they make (folders first, as Go sorts them).
type FileTree struct {
	Folders  map[string]FileFolder
	Expanded map[string]bool
	// Back history per key ("pinned", or "pane:<id>"), at most 50 each.
	History map[string][]string
}

func NewFileTree() *FileTree {
	return &FileTree{Folders: map[string]FileFolder{}, Expanded: map[string]bool{}, History: map[string][]string{}}
}

// Rows are the rows under root. A filter keeps matching names, and the
// open folders leading to them.
func (t *FileTree) Rows(root, filter string) []FileRow {
	q := strings.ToLower(trimBlanks(filter))
	var walk func(dir string, depth int) []FileRow
	walk = func(dir string, depth int) []FileRow {
		f, ok := t.Folders[dir]
		if !ok || f.Entries == nil {
			return nil
		}
		var out []FileRow
		for _, e := range f.Entries {
			var below []FileRow
			if e.Dir && t.Expanded[e.Path] {
				below = walk(e.Path, depth+1)
			}
			match := q == "" || strings.Contains(strings.ToLower(e.Name), q)
			keptBelow := slices.ContainsFunc(below, func(r FileRow) bool { return !r.More })
			if match || keptBelow {
				out = append(out, FileRow{Entry: e, Depth: depth})
				out = append(out, below...)
			}
		}
		if f.Truncated && q == "" {
			out = append(out, FileRow{Depth: depth, More: true})
		}
		return out
	}
	return walk(root, 0)
}

// Visit adds path to key's back history, unless it's already the last.
func (t *FileTree) Visit(key, path string) {
	if t.History == nil {
		t.History = map[string][]string{}
	}
	h := t.History[key]
	if len(h) == 0 || h[len(h)-1] != path {
		h = append(h, path)
	}
	if len(h) > 50 {
		h = h[len(h)-50:]
	}
	t.History[key] = h
}
