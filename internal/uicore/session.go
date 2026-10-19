package uicore

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

// The session.json shapes the Svelte build writes (spec: file version 2 with
// per-window sessions, each version 1), so every build restores the others'
// windows. A port of macos/Sources/WtfCore/SavedSession.swift. Decoding is
// lenient: a wrongly typed field reads as absent and a malformed tab or
// group is skipped, not fatal. encoding/json fails a whole struct on one bad
// field, so each field is read on its own from json.RawMessage.

// SavedNode is a leaf (Cwd set) or a split (Dir, Ratio, A, B).
type SavedNode struct {
	// Leaf
	Cwd         *string
	GitIdentity *string
	// Only when moving between windows: the live shell and its scrollback.
	SessionID *string
	Buffer    *string
	// Split
	Dir   *string
	Ratio *float64
	A, B  *SavedNode
}

// SavedTab is one tab. Every field is written, null when absent.
type SavedTab struct {
	GroupID *int
	Root    *SavedNode
	Focused *int
	Zoomed  *int
	Title   *string
}

// TabPayload is what moves a tab to another window (window:adopt / a new
// window's bootstrap).
type TabPayload struct {
	Kind  string
	Tab   SavedTab
	Group *PayloadGroup
}

// PayloadGroup is the group a moving tab was in.
type PayloadGroup struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// SavedGroup is one tab group.
type SavedGroup struct {
	ID        *int    `json:"id,omitempty"`
	Name      *string `json:"name,omitempty"`
	Color     *string `json:"color,omitempty"`
	Collapsed *bool   `json:"collapsed,omitempty"`
}

// SavedSession is one window.
type SavedSession struct {
	Version  *int
	FontSize *float64
	// Nil when absent.
	Tabs   []SavedTab
	Groups []SavedGroup
	Active *int
	// Fields this build doesn't use (the Svelte build's sidebar state),
	// kept so saving doesn't drop them.
	Extra map[string]json.RawMessage
}

var errNotObject = errors.New("not a JSON object")

// ParseSavedSession reads one window's session; an error only when it isn't
// a JSON object.
func ParseSavedSession(data []byte) (SavedSession, error) {
	var s SavedSession
	err := s.UnmarshalJSON(data)
	return s, err
}

// ParseTabPayload reads a moving tab. Like the Swift, only the tab's own
// fields are lenient: kind and tab must be there, and a group must be well formed.
func ParseTabPayload(data []byte) (TabPayload, error) {
	var p TabPayload
	err := p.UnmarshalJSON(data)
	return p, err
}

// Reading

// object is a JSON object's members; false when it isn't one.
func object(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

func lenientString(raw json.RawMessage) *string {
	var s string
	if isNull(raw) || json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

func lenientBool(raw json.RawMessage) *bool {
	var b bool
	if isNull(raw) || json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}

func lenientFloat(raw json.RawMessage) *float64 {
	var f float64
	if isNull(raw) || json.Unmarshal(raw, &f) != nil {
		return nil
	}
	return &f
}

// lenientInt takes whole numbers, written as 3 or 3.0, as Swift's decoder does.
func lenientInt(raw json.RawMessage) *int {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || !(t[0] == '-' || (t[0] >= '0' && t[0] <= '9')) {
		return nil
	}
	if n, err := strconv.ParseInt(string(t), 10, 64); err == nil {
		i := int(n)
		return &i
	}
	f, err := strconv.ParseFloat(string(t), 64)
	if err != nil || f != math.Trunc(f) || f < math.MinInt64 || f >= math.MaxInt64 {
		return nil
	}
	i := int(f)
	return &i
}

func parseNode(raw json.RawMessage) *SavedNode {
	m, ok := object(raw)
	if !ok {
		return nil
	}
	return &SavedNode{
		Cwd:         lenientString(m["cwd"]),
		GitIdentity: lenientString(m["gitIdentity"]),
		SessionID:   lenientString(m["sessionId"]),
		Buffer:      lenientString(m["buffer"]),
		Dir:         lenientString(m["dir"]),
		Ratio:       lenientFloat(m["ratio"]),
		A:           parseNode(m["a"]),
		B:           parseNode(m["b"]),
	}
}

func (n *SavedNode) UnmarshalJSON(data []byte) error {
	p := parseNode(data)
	if p == nil {
		return errNotObject
	}
	*n = *p
	return nil
}

func parseTab(raw json.RawMessage) (SavedTab, bool) {
	m, ok := object(raw)
	if !ok {
		return SavedTab{}, false
	}
	return SavedTab{
		GroupID: lenientInt(m["groupId"]),
		Root:    parseNode(m["root"]),
		Focused: lenientInt(m["focused"]),
		Zoomed:  lenientInt(m["zoomed"]),
		Title:   lenientString(m["title"]),
	}, true
}

func (t *SavedTab) UnmarshalJSON(data []byte) error {
	p, ok := parseTab(data)
	if !ok {
		return errNotObject
	}
	*t = p
	return nil
}

func parseGroup(raw json.RawMessage) (SavedGroup, bool) {
	m, ok := object(raw)
	if !ok {
		return SavedGroup{}, false
	}
	return SavedGroup{
		ID:        lenientInt(m["id"]),
		Name:      lenientString(m["name"]),
		Color:     lenientString(m["color"]),
		Collapsed: lenientBool(m["collapsed"]),
	}, true
}

func (g *SavedGroup) UnmarshalJSON(data []byte) error {
	p, ok := parseGroup(data)
	if !ok {
		return errNotObject
	}
	*g = p
	return nil
}

// lenientList is the elements of an array that parse, nil when it isn't one.
func lenientList[T any](raw json.RawMessage, parse func(json.RawMessage) (T, bool)) []T {
	var items []json.RawMessage
	if isNull(raw) || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		if v, ok := parse(it); ok {
			out = append(out, v)
		}
	}
	return out
}

var knownSessionKeys = map[string]bool{"version": true, "fontSize": true, "tabs": true, "groups": true, "active": true}

func (s *SavedSession) UnmarshalJSON(data []byte) error {
	m, ok := object(data)
	if !ok {
		return errNotObject
	}
	*s = SavedSession{
		Version:  lenientInt(m["version"]),
		FontSize: lenientFloat(m["fontSize"]),
		// Skip malformed tabs and groups one by one.
		Tabs:   lenientList(m["tabs"], parseTab),
		Groups: lenientList(m["groups"], parseGroup),
		Active: lenientInt(m["active"]),
		Extra:  map[string]json.RawMessage{},
	}
	for k, v := range m {
		if !knownSessionKeys[k] {
			s.Extra[k] = v
		}
	}
	return nil
}

func (p *TabPayload) UnmarshalJSON(data []byte) error {
	m, ok := object(data)
	if !ok {
		return errNotObject
	}
	kind := lenientString(m["kind"])
	if kind == nil {
		return errors.New("payload: kind is missing")
	}
	tab, ok := parseTab(m["tab"])
	if !ok {
		return errors.New("payload: tab is missing")
	}
	var group *PayloadGroup
	if !isNull(m["group"]) {
		gm, ok := object(m["group"])
		name, color := lenientString(gm["name"]), lenientString(gm["color"])
		if !ok || name == nil || color == nil {
			return errors.New("payload: malformed group")
		}
		group = &PayloadGroup{Name: *name, Color: *color}
	}
	*p = TabPayload{Kind: *kind, Tab: tab, Group: group}
	return nil
}

// Writing

func (n SavedNode) MarshalJSON() ([]byte, error) {
	if n.Cwd != nil {
		return json.Marshal(struct {
			Cwd         string  `json:"cwd"`
			GitIdentity *string `json:"gitIdentity,omitempty"`
			SessionID   *string `json:"sessionId,omitempty"`
			Buffer      *string `json:"buffer,omitempty"`
		}{*n.Cwd, n.GitIdentity, n.SessionID, n.Buffer})
	}
	return json.Marshal(struct {
		Dir   *string    `json:"dir,omitempty"`
		Ratio *float64   `json:"ratio,omitempty"`
		A     *SavedNode `json:"a,omitempty"`
		B     *SavedNode `json:"b,omitempty"`
	}{n.Dir, n.Ratio, n.A, n.B})
}

func (t SavedTab) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		GroupID *int       `json:"groupId"`
		Root    *SavedNode `json:"root"`
		Focused *int       `json:"focused"`
		Zoomed  *int       `json:"zoomed"`
		Title   *string    `json:"title"`
	}{t.GroupID, t.Root, t.Focused, t.Zoomed, t.Title})
}

func (p TabPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind  string        `json:"kind"`
		Tab   SavedTab      `json:"tab"`
		Group *PayloadGroup `json:"group,omitempty"`
	}{p.Kind, p.Tab, p.Group})
}

func (s SavedSession) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range s.Extra {
		out[k] = v
	}
	set := func(k string, present bool, v any) {
		if present {
			out[k] = v
		}
	}
	set("version", s.Version != nil, s.Version)
	set("fontSize", s.FontSize != nil, s.FontSize)
	set("tabs", s.Tabs != nil, s.Tabs)
	set("groups", s.Groups != nil, s.Groups)
	set("active", s.Active != nil, s.Active)
	return json.Marshal(out)
}
