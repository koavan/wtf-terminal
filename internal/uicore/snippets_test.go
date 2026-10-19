package uicore

import (
	"reflect"
	"testing"

	"github.com/dayanraj/wtf/internal/snippets"
)

// The placeholder cases of SnippetTests.swift are in workspaces_test.go
// (TestSnippetFields, TestRunbookFill); these are the snippet's own rules.

func TestSnippetOneLiner(t *testing.T) {
	s := snippets.Snippet{Name: "Restart", Command: "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}} {{end}}",
		Params: []snippets.Param{{Name: "name", Description: "The deployment", Choices: []string{"web", "worker"}}}}
	fields := SnippetFields(s)
	if SnippetIsRunbook(s) || !reflect.DeepEqual(names(fields), []string{"ns", "name"}) || fields[1].Description != "The deployment" {
		t.Errorf("fields %+v", fields)
	}
	expectText(t, SnippetSubtitle(s), s.Command)
	expectText(t, SnippetTooltip(s), s.Command)
	s.Description = "Restarts it"
	expectText(t, SnippetSubtitle(s), "Restarts it")
}

func TestRunbookRows(t *testing.T) {
	r := snippets.Snippet{Name: "Drain", Tags: []string{"k8s"}, Steps: []snippets.Step{{Title: "Cordon", Command: "kubectl cordon {{node}}"},
		{Command: "kubectl drain {{node}} {{flags=--force}}"}}, Params: []snippets.Param{{Name: "flags", Raw: true}}}
	fields := SnippetFields(r)
	if !SnippetIsRunbook(r) || !reflect.DeepEqual(names(fields), []string{"node", "flags"}) || !fields[1].Raw {
		t.Errorf("fields %+v", fields)
	}
	expectText(t, SnippetSubtitle(r), "Cordon → step 2")
	expectText(t, SnippetTooltip(r), "1. kubectl cordon {{node}}\n2. kubectl drain {{node}} {{flags=--force}}")
	if !SnippetMatches(r, "K8S") || !SnippetMatches(r, "drain") || SnippetMatches(r, "helm") || !SnippetMatches(r, " ") {
		t.Error("matches")
	}
}
