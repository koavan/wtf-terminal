package ai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/events"
	"github.com/zalando/go-keyring"
)

// recorder collects the events a request emits.
type recorder struct {
	mu     sync.Mutex
	chunks map[string]string
	done   chan Done
}

func record(t *testing.T) *recorder {
	r := &recorder{chunks: map[string]string{}, done: make(chan Done, 8)}
	events.SetEmitter(func(name string, data any) {
		switch name {
		case EventChunk:
			c := data.(Chunk)
			r.mu.Lock()
			r.chunks[c.ID] += c.Text
			r.mu.Unlock()
		case EventDone:
			r.done <- data.(Done)
		}
	})
	t.Cleanup(func() { events.SetEmitter(nil) })
	return r
}

func (r *recorder) wait(t *testing.T) Done {
	t.Helper()
	select {
	case d := <-r.done:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("no ai:done")
		return Done{}
	}
}

// fakeRedact hides the word "hunter2".
func fakeRedact(s string) (string, int) {
	n := strings.Count(s, "hunter2")
	return strings.ReplaceAll(s, "hunter2", "[REDACTED]"), n
}

func newService(t *testing.T, c config.Config) *Service {
	t.Helper()
	keyring.MockInit()
	return NewService(t.TempDir(), func() config.Config { return c }, fakeRedact)
}

func oneProvider(name string, p config.AIProvider) config.Config {
	return config.Config{AIDefault: name, AIFast: name, AI: config.AIConfig{Providers: map[string]config.AIProvider{name: p}}}
}

func ask(text string) Request {
	return Request{System: "be brief", Messages: []Message{{Role: "user", Content: text}}}
}

func TestAnthropicStream(t *testing.T) {
	var got map[string]any
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header
		json.NewDecoder(r.Body).Decode(&got)
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":1}}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n")
		fmt.Fprint(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"lo ✓\"}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n")
	}))
	defer srv.Close()

	s := newService(t, oneProvider("claude", config.AIProvider{Kind: "anthropic", Model: "m", BaseURL: srv.URL}))
	rec := record(t)
	if err := s.Ask("1", ask("hi"), true); err == nil {
		t.Fatal("asked without a key")
	}
	if err := s.SetKey("claude", "sk-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Ask("1", ask("my password is hunter2"), true); err != nil {
		t.Fatal(err)
	}
	d := rec.wait(t)
	if d.Error != "" || d.Text != "Hello ✓" || rec.chunks["1"] != "Hello ✓" {
		t.Errorf("done %+v, chunks %q", d, rec.chunks["1"])
	}
	if d.Usage != (Usage{Counted: true, InputTokens: 12, OutputTokens: 5}) {
		t.Errorf("usage %+v", d.Usage)
	}
	if header.Get("x-api-key") != "sk-test" || header.Get("anthropic-version") == "" {
		t.Errorf("headers %v", header)
	}
	msgs := got["messages"].([]any)
	if got["system"] != "be brief" || msgs[0].(map[string]any)["content"] != "my password is [REDACTED]" {
		t.Errorf("sent %v", got)
	}
}

func TestOpenAIStreamAndErrors(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		if got["model"] == "missing" {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"model 'missing' not found"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"kubectl \"}}]}\n\n")
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"get pods\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"cost\":0.0001}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	// A loopback server needs no key.
	s := newService(t, oneProvider("local", config.AIProvider{Kind: "openai", Model: "qwen", BaseURL: srv.URL + "/v1"}))
	rec := record(t)
	if err := s.Ask("a", ask("list pods"), true); err != nil {
		t.Fatal(err)
	}
	d := rec.wait(t)
	if d.Error != "" || d.Text != "kubectl get pods" || auth != "" {
		t.Errorf("done %+v auth %q", d, auth)
	}
	if d.Usage != (Usage{Counted: true, InputTokens: 7, OutputTokens: 3, Cost: 0.0001}) {
		t.Errorf("usage %+v", d.Usage)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages %v", msgs)
	}

	s = newService(t, oneProvider("local", config.AIProvider{Kind: "openai", Model: "missing", BaseURL: srv.URL + "/v1"}))
	if err := s.Ask("b", ask("x"), true); err != nil {
		t.Fatal(err)
	}
	if d := rec.wait(t); d.Error != "404 Not Found: model 'missing' not found" {
		t.Errorf("error %q", d.Error)
	}
}

func TestRemoteOpenAINeedsKey(t *testing.T) {
	s := newService(t, oneProvider("openai", config.AIProvider{Kind: "openai", Model: "gpt"}))
	st := s.Status()
	if !st.Enabled || !st.Providers[0].NeedsKey || st.Providers[0].HasKey || st.Providers[0].Destination != "api.openai.com" {
		t.Errorf("status %+v", st)
	}
	if err := s.Ask("1", ask("x"), true); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("err %v", err)
	}
	t.Setenv("WTF_TEST_KEY", "k")
	s = newService(t, oneProvider("openai", config.AIProvider{Kind: "openai", Model: "gpt", APIKeyEnv: "WTF_TEST_KEY"}))
	if p := s.Status().Providers[0]; !p.HasKey || p.KeyFrom != "$WTF_TEST_KEY" {
		t.Errorf("env key %+v", p)
	}
}

func TestCommandProvider(t *testing.T) {
	// The tool echoes its stdin back in upper case.
	s := newService(t, oneProvider("tool", config.AIProvider{Kind: "command", Command: []string{"tr", "a-z", "A-Z"}}))
	rec := record(t)
	if err := s.Ask("1", ask("hello hunter2"), true); err != nil {
		t.Fatal(err)
	}
	d := rec.wait(t)
	if d.Error != "" || d.Text != "BE BRIEF\n\nHELLO [REDACTED]" || d.Usage.Counted {
		t.Errorf("done %+v", d)
	}

	s = newService(t, oneProvider("tool", config.AIProvider{Kind: "command", Command: []string{"sh", "-c", "echo not logged in >&2; exit 3"}}))
	s.Ask("2", ask("x"), true)
	if d := rec.wait(t); d.Error != "sh: not logged in" {
		t.Errorf("error %q", d.Error)
	}

	s = newService(t, oneProvider("tool", config.AIProvider{Kind: "command", Command: []string{"no-such-ai-tool"}}))
	s.Ask("3", ask("x"), true)
	if d := rec.wait(t); !strings.Contains(d.Error, "isn't installed") {
		t.Errorf("error %q", d.Error)
	}
}

func TestCancel(t *testing.T) {
	s := newService(t, oneProvider("tool", config.AIProvider{Kind: "command", Command: []string{"sh", "-c", "printf partial; sleep 30"}}))
	rec := record(t)
	if err := s.Ask("1", ask("x"), true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	s.Cancel("1")
	d := rec.wait(t)
	if !d.Cancelled || d.Error != "" || d.Text != "partial" || time.Since(start) > 5*time.Second {
		t.Errorf("done %+v after %s", d, time.Since(start))
	}
}

func TestFirstSendNeedsPreview(t *testing.T) {
	s := newService(t, oneProvider("tool", config.AIProvider{Kind: "command", Command: []string{"cat"}}))
	rec := record(t)
	out, err := s.Preview(ask("token hunter2"))
	if err != nil || !out.FirstSend || out.Redacted != 1 || out.Messages[0].Content != "token [REDACTED]" {
		t.Fatalf("preview %+v %v", out, err)
	}
	if err := s.Ask("1", ask("x"), false); err == nil {
		t.Fatal("first request sent without a preview")
	}
	if err := s.Ask("1", ask("x"), true); err != nil {
		t.Fatal(err)
	}
	rec.wait(t)
	// Seen now, and remembered by a new service on the same state folder.
	s2 := NewService(s.stateDir, s.config, fakeRedact)
	if out, _ := s2.Preview(ask("x")); out.FirstSend {
		t.Error("asked again after the preview was seen")
	}
}

func TestNoProvider(t *testing.T) {
	s := newService(t, config.Config{})
	if s.Status().Enabled {
		t.Error("enabled with no providers")
	}
	if _, err := s.Preview(ask("x")); err == nil {
		t.Error("previewed with no providers")
	}
}

func TestReadSSE(t *testing.T) {
	in := "event: a\ndata: one\ndata: two\n\n:comment\r\ndata: three\r\n\r\ndata: tail"
	var got []string
	err := readSSE(strings.NewReader(in), func(event, data string) error {
		got = append(got, event+"="+data)
		return nil
	})
	want := []string{"a=one\ntwo", "=three", "=tail"}
	if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %q %v", got, err)
	}
}

func TestNamedProvider(t *testing.T) {
	c := oneProvider("a", config.AIProvider{Kind: "command", Command: []string{"cat"}})
	c.AI.Providers["b"] = config.AIProvider{Kind: "command", Command: []string{"tr", "a-z", "A-Z"}}
	s := newService(t, c)
	rec := record(t)
	r := ask("x")
	r.System, r.Provider = "", "b"
	if err := s.Ask("1", r, true); err != nil {
		t.Fatal(err)
	}
	if d := rec.wait(t); d.Text != "X" {
		t.Errorf("text %q", d.Text)
	}
	r.Provider = "zzz"
	if _, err := s.Preview(r); err == nil {
		t.Error("previewed for a provider that doesn't exist")
	}
}

func TestModels(t *testing.T) {
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization")+r.Header.Get("x-api-key"), r.URL.Path
		if auth == "bad" || auth == "Bearer bad" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"b"},{"id":"a"},{"id":"b"}]}`)
	}))
	defer srv.Close()
	s := newService(t, oneProvider("o", config.AIProvider{Kind: "openai", Model: "m", BaseURL: srv.URL + "/v1"}))

	// From the form, before saving: the typed key.
	got, err := s.Models("", config.AIProvider{Kind: "openai", BaseURL: srv.URL + "/v1"}, "typed")
	if err != nil || fmt.Sprint(got) != "[a b]" || auth != "Bearer typed" || path != "/v1/models" {
		t.Errorf("openai: %v %v %q %q", got, err, auth, path)
	}
	// Editing a saved provider: its stored key.
	s.SetKey("o", "stored")
	s.Models("o", config.AIProvider{Kind: "openai", BaseURL: srv.URL + "/v1"}, "")
	if auth != "Bearer stored" {
		t.Errorf("stored key: %q", auth)
	}
	if _, err := s.Models("", config.AIProvider{Kind: "anthropic", BaseURL: srv.URL}, "bad"); err == nil || !strings.Contains(err.Error(), "invalid key") || path != "/v1/models" {
		t.Errorf("anthropic bad key: %v %q", err, path)
	}
	if _, err := s.Models("", config.AIProvider{Kind: "command"}, ""); err == nil {
		t.Error("listed a command's models")
	}
}

func TestMoveKey(t *testing.T) {
	c := oneProvider("new", config.AIProvider{Kind: "anthropic", Model: "m"})
	c.AI.Providers["old"] = c.AI.Providers["new"]
	s := newService(t, c)
	if err := s.SetKey("old", "k"); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveKey("old", "new"); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	for _, p := range st.Providers {
		if (p.Name == "new") != p.HasKey {
			t.Errorf("%s has key %v", p.Name, p.HasKey)
		}
	}
	if err := s.MoveKey("nothing", "x"); err != nil {
		t.Errorf("moving no key: %v", err)
	}
}

func TestPresetsAreValid(t *testing.T) {
	for _, p := range presets {
		model := p.Model
		if model == "" && p.Kind != "command" {
			model = "m"
		}
		name := p.Name
		if name == "" {
			name = "x"
		}
		if p.Kind == "command" && len(p.Command) == 0 {
			continue // the user types it
		}
		if p.BaseURL == "" && p.ID == "custom-openai" {
			continue
		}
		if _, err := config.CheckAIProvider(name, config.AIProvider{Kind: p.Kind, Model: model, BaseURL: p.BaseURL, Command: p.Command}); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
	}
}

func TestExtraHeadersAndWorkspaceHint(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header
		if r.Header.Get("anthropic-workspace-id") == "" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"This API key is not scoped to a workspace, so this request must include the anthropic-workspace-id header"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n")
	}))
	defer srv.Close()
	p := config.AIProvider{Kind: "anthropic", Model: "m", BaseURL: srv.URL}
	s := newService(t, oneProvider("c", p))
	s.SetKey("c", "k")
	rec := record(t)
	s.Ask("1", ask("x"), true)
	if d := rec.wait(t); !strings.Contains(d.Error, "Edit… this provider and add the extra header") {
		t.Errorf("no hint: %q", d.Error)
	}
	// The header goes along; it can't replace wtf's own.
	p.Headers = map[string]string{"anthropic-workspace-id": "wrkspc_1", "x-api-key": "sneaky"}
	s = newService(t, oneProvider("c", p))
	s.SetKey("c", "k")
	s.Ask("2", ask("x"), true)
	if d := rec.wait(t); d.Text != "OK" || got.Get("x-api-key") != "k" {
		t.Errorf("done %+v key %q", d, got.Get("x-api-key"))
	}
}
