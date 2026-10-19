package uicore

import (
	"strconv"
	"strings"

	"github.com/dayanraj/wtf/internal/snippets"
)

// Snippets: one-line commands with {{placeholders}}, or runbooks of steps,
// from the snippets files (internal/snippets). Ports of the snippet parts
// of lib/snippets.svelte.ts (macos/Sources/WtfCore/Snippets.swift). The
// placeholder helpers (Fields, Fill, Field.InitialValue) are in
// workspaces.go.

func SnippetIsRunbook(s snippets.Snippet) bool { return len(s.Steps) > 0 }

func snippetCommands(s snippets.Snippet) []string {
	if !SnippetIsRunbook(s) {
		return []string{s.Command}
	}
	out := make([]string, len(s.Steps))
	for i, st := range s.Steps {
		out[i] = st.Command
	}
	return out
}

// SnippetFields are the values it asks for, from the command or each step,
// in order of first use.
func SnippetFields(s snippets.Snippet) []Field { return Fields(snippetCommands(s), s.Params) }

// SnippetMatches is the side pane's filter: name, description, command,
// steps and tags.
func SnippetMatches(s snippets.Snippet, query string) bool {
	q := strings.ToLower(trimBlanks(query))
	if q == "" {
		return true
	}
	hay := []string{s.Name, s.Description, s.Command}
	for _, st := range s.Steps {
		hay = append(hay, st.Command)
	}
	hay = append(hay, s.Tags...)
	return strings.Contains(strings.ToLower(strings.Join(hay, " ")), q)
}

// SnippetSubtitle is the row's second line: the description, the step
// titles, or the command.
func SnippetSubtitle(s snippets.Snippet) string {
	if s.Description != "" {
		return s.Description
	}
	if !SnippetIsRunbook(s) {
		return s.Command
	}
	titles := make([]string, len(s.Steps))
	for i, st := range s.Steps {
		titles[i] = st.Title
		if st.Title == "" {
			titles[i] = "step " + strconv.Itoa(i+1)
		}
	}
	return strings.Join(titles, " → ")
}

// SnippetTooltip is what it runs: the command, or the steps numbered.
func SnippetTooltip(s snippets.Snippet) string {
	if !SnippetIsRunbook(s) {
		return s.Command
	}
	lines := make([]string, len(s.Steps))
	for i, st := range s.Steps {
		lines[i] = strconv.Itoa(i+1) + ". " + st.Command
	}
	return strings.Join(lines, "\n")
}
