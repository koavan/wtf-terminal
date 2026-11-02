package uicore

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
)

// The side pane's state as each window saves it in its session ("sidebar"
// in SavedSession.Extra), validated field by field like
// lib/sidebar.svelte.ts. Fields this build doesn't use are kept, so saving
// doesn't drop the Svelte build's. A port of
// macos/Sources/WtfCore/Sidebar.swift.

// SidebarTab is one of the side pane's tabs.
type SidebarTab string

const (
	SidebarFiles      SidebarTab = "files"
	SidebarSSH        SidebarTab = "ssh"
	SidebarAWS        SidebarTab = "aws"
	SidebarKube       SidebarTab = "kube"
	SidebarSnippets   SidebarTab = "snippets"
	SidebarWorkspaces SidebarTab = "workspaces"
	SidebarNetwork    SidebarTab = "network"
	SidebarProcesses  SidebarTab = "processes"
	SidebarHistory    SidebarTab = "history"
	SidebarPins       SidebarTab = "pins"
	SidebarFindings   SidebarTab = "findings"
	SidebarShare      SidebarTab = "share"
	SidebarGenerate   SidebarTab = "generate"
	// The incident investigator (#27); shown only with an AI provider.
	SidebarInvestigate SidebarTab = "investigate"
)

// SidebarTabs lists every tab, in order.
var SidebarTabs = []SidebarTab{
	SidebarFiles, SidebarSSH, SidebarAWS, SidebarKube, SidebarSnippets, SidebarWorkspaces,
	SidebarNetwork, SidebarProcesses, SidebarHistory, SidebarPins, SidebarFindings, SidebarShare, SidebarGenerate,
	SidebarInvestigate,
}

const (
	SidebarMinWidth     = 220.0
	SidebarMaxWidth     = 520.0
	SidebarDefaultWidth = 300.0
)

// SidebarState is the side pane's saved state. Start from NewSidebarState
// or ParseSidebarState: the zero value has no width or tab.
type SidebarState struct {
	Open  bool
	Width float64
	// The Files tab's locked root; "" follows the focused pane.
	Pinned          string
	Tab             SidebarTab
	SSHSection      string
	KubeView        string
	GenerateSection string
	HistoryScope    string
	// The Network and Processes tabs' views (shared by the app, saved per window).
	NetworkScope string
	ProcessScope string
	ProcessSort  string
	// Everything else in the saved object, as it was.
	rest map[string]json.RawMessage
}

// NewSidebarState is the state of a window that saved none.
func NewSidebarState() SidebarState {
	return SidebarState{
		Width:           SidebarDefaultWidth,
		Tab:             SidebarFiles,
		SSHSection:      "hosts",
		KubeView:        "cluster",
		GenerateSection: "key",
		HistoryScope:    "pane",
		NetworkScope:    "terminals",
		ProcessScope:    "terminals",
		ProcessSort:     "cpu",
	}
}

// ClampSidebarWidth keeps a width within bounds, in whole points.
func ClampSidebarWidth(w float64) float64 {
	return math.Round(math.Min(SidebarMaxWidth, math.Max(SidebarMinWidth, w)))
}

// ParseSidebarState reads the saved object, keeping each field that's
// valid; anything else (or junk) leaves the default.
func ParseSidebarState(raw json.RawMessage) SidebarState {
	s := NewSidebarState()
	o, ok := object(raw)
	if !ok {
		return s
	}
	s.rest = o
	// oneOf sets *dst when the field is a string among the allowed ones.
	oneOf := func(key string, dst *string, allowed ...string) {
		if v := lenientString(o[key]); v != nil && slices.Contains(allowed, *v) {
			*dst = *v
		}
	}
	if b := lenientBool(o["open"]); b != nil {
		s.Open = *b
	}
	if w := lenientFloat(o["width"]); w != nil && !math.IsInf(*w, 0) && !math.IsNaN(*w) {
		s.Width = ClampSidebarWidth(*w)
	}
	if p := lenientString(o["pinned"]); p != nil && strings.HasPrefix(*p, "/") {
		s.Pinned = *p
	}
	if t := lenientString(o["tab"]); t != nil && slices.Contains(SidebarTabs, SidebarTab(*t)) {
		s.Tab = SidebarTab(*t)
	}
	oneOf("sshSection", &s.SSHSection, "hosts", "keys", "git", "tunnels")
	oneOf("kubeView", &s.KubeView, "cluster", "contexts")
	oneOf("networkScope", &s.NetworkScope, "terminals", "all")
	oneOf("processScope", &s.ProcessScope, "terminals", "all")
	oneOf("processSort", &s.ProcessSort, "cpu", "memory", "name")
	oneOf("historyScope", &s.HistoryScope, "pane", "all")
	oneOf("generateSection", &s.GenerateSection, "key", "password")
	return s
}

// JSON is the object to save, with the fields this build doesn't use as
// they were.
func (s SidebarState) JSON() json.RawMessage {
	o := make(map[string]any, len(s.rest)+11)
	for k, v := range s.rest {
		o[k] = v
	}
	o["open"] = s.Open
	o["width"] = s.Width
	if s.Pinned != "" {
		o["pinned"] = s.Pinned
	} else {
		o["pinned"] = nil
	}
	o["tab"] = string(s.Tab)
	o["sshSection"] = s.SSHSection
	o["kubeView"] = s.KubeView
	o["generateSection"] = s.GenerateSection
	o["historyScope"] = s.HistoryScope
	o["networkScope"] = s.NetworkScope
	o["processScope"] = s.ProcessScope
	o["processSort"] = s.ProcessSort
	data, err := json.Marshal(o)
	if err != nil {
		// Only a width that isn't a number fails; save the rest.
		o["width"] = SidebarDefaultWidth
		data, _ = json.Marshal(o)
	}
	return data
}
