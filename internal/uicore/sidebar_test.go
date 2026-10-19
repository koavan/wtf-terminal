package uicore

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSidebarStateRestoresWhatIsValid(t *testing.T) {
	s := ParseSidebarState(json.RawMessage(`{"open":true,"width":900,"pinned":"relative","tab":"kube","sshSection":"bogus","processSort":"memory","future":{"x":1}}`))
	if !s.Open || s.Width != 520 || s.Pinned != "" || s.Tab != SidebarKube || s.SSHSection != "hosts" {
		t.Errorf("got %+v", s)
	}
	// Fields this build doesn't use are kept.
	var o map[string]json.RawMessage
	if err := json.Unmarshal(s.JSON(), &o); err != nil {
		t.Fatal(err)
	}
	if string(o["processSort"]) != `"memory"` || string(o["future"]) != `{"x":1}` || string(o["pinned"]) != "null" {
		t.Errorf("saved %s", s.JSON())
	}
	if got := ParseSidebarState(json.RawMessage(`"junk"`)); !reflect.DeepEqual(got, NewSidebarState()) {
		t.Errorf("junk gave %+v", got)
	}
	if got := ClampSidebarWidth(100); got != 220 {
		t.Errorf("ClampSidebarWidth(100) = %v", got)
	}
}

func TestGenerateSectionRoundTrips(t *testing.T) {
	s := ParseSidebarState(json.RawMessage(`{"generateSection":"password"}`))
	if s.GenerateSection != "password" {
		t.Errorf("got %q", s.GenerateSection)
	}
	if got := ParseSidebarState(json.RawMessage(`{"generateSection":"bogus"}`)).GenerateSection; got != "key" {
		t.Errorf("bogus gave %q", got)
	}
	s.GenerateSection = "key"
	if got := ParseSidebarState(s.JSON()).GenerateSection; got != "key" {
		t.Errorf("round trip gave %q", got)
	}
}

func TestHistoryScopeRoundTrips(t *testing.T) {
	if got := ParseSidebarState(json.RawMessage(`{"historyScope":"all"}`)).HistoryScope; got != "all" {
		t.Errorf("got %q", got)
	}
	if got := ParseSidebarState(json.RawMessage(`{"historyScope":"x"}`)).HistoryScope; got != "pane" {
		t.Errorf("got %q", got)
	}
}

func TestPaneViewsRoundTrip(t *testing.T) {
	s := ParseSidebarState(json.RawMessage(`{"networkScope":"all","processSort":"memory","processScope":"bogus"}`))
	if s.NetworkScope != "all" || s.ProcessSort != "memory" || s.ProcessScope != "terminals" {
		t.Errorf("got %+v", s)
	}
	s.ProcessScope = "all"
	back := ParseSidebarState(s.JSON())
	if back.ProcessScope != "all" || back.NetworkScope != "all" {
		t.Errorf("round trip gave %+v", back)
	}
	// A pinned root survives too.
	s.Pinned = "/work"
	if got := ParseSidebarState(s.JSON()).Pinned; got != "/work" {
		t.Errorf("pinned gave %q", got)
	}
}
