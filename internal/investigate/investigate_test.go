package investigate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/dayanraj/wtf/internal/ai"
	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/events"
)

// fakeClaude answers each request with the next reply: a tool call
// ({"argv": …}) or final text.
type fakeClaude struct {
	mu      sync.Mutex
	replies []string
	bodies  []map[string]any
}

func (f *fakeClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	n := len(f.bodies) - 1
	reply := `{"text":"Done."}`
	if n < len(f.replies) {
		reply = f.replies[n]
	}
	f.mu.Unlock()
	fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1000,\"output_tokens\":0}}}\n\n")
	var m struct {
		Text string          `json:"text"`
		Argv json.RawMessage `json:"argv"`
	}
	json.Unmarshal([]byte(reply), &m)
	if m.Text != "" {
		t, _ := json.Marshal(m.Text)
		fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", t)
	}
	if m.Argv != nil {
		in, _ := json.Marshal(string(`{"argv":` + string(m.Argv) + `,"purpose":"check"}`))
		fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call%d\",\"name\":\"run\"}}\n\n", n)
		fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", in)
	}
	fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":50}}\n\n")
}

// shell answers EventRun like a UI would; it records what was typed.
type shell struct {
	mu     sync.Mutex
	lines  []string
	text   string
	refuse []string
	done   chan Done
}

func listen(t *testing.T, s *Service, output func(line string) StepResult) *shell {
	sh := &shell{done: make(chan Done, 1)}
	events.SetEmitter(func(name string, data any) {
		switch name {
		case EventRun:
			r := data.(Run)
			sh.mu.Lock()
			sh.lines = append(sh.lines, r.Line)
			sh.mu.Unlock()
			go func() { s.Result(r.ID, r.Step, output(r.Line)) }()
		case EventText:
			sh.mu.Lock()
			sh.text += data.(Text).Text
			sh.mu.Unlock()
		case EventRefused:
			sh.mu.Lock()
			sh.refuse = append(sh.refuse, data.(Refusal).Reason)
			sh.mu.Unlock()
		case EventDone:
			sh.done <- data.(Done)
		}
	})
	t.Cleanup(func() { events.SetEmitter(nil) })
	return sh
}

func (sh *shell) wait(t *testing.T) Done {
	t.Helper()
	select {
	case d := <-sh.done:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("no investigate:done")
		return Done{}
	}
}

func service(t *testing.T, f *fakeClaude) *Service {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	keyring.MockInit()
	c := config.Config{AIDefault: "claude", AI: config.AIConfig{Providers: map[string]config.AIProvider{
		"claude": {Kind: "anthropic", Model: "m", BaseURL: srv.URL}}}}
	a := ai.NewService(t.TempDir(), func() config.Config { return c }, func(s string) (string, int) {
		return strings.ReplaceAll(s, "hunter2", "[REDACTED]"), strings.Count(s, "hunter2")
	})
	a.SetKey("claude", "k")
	return NewService(a, nil)
}

func TestInvestigation(t *testing.T) {
	f := &fakeClaude{replies: []string{
		`{"text":"Listing pods.","argv":["kubectl","get","pods","-n","staging"]}`,
		`{"text":"Deleting it.","argv":["kubectl","delete","pod","checkout-1"]}`,
		`{"argv":["kubectl","logs","checkout-1","-n","staging"]}`,
		"{\"text\":\"Finding: checkout-1 can't reach the database.\\nEvidence: [2] shows connection refused.\\nNext:\\n```sh\\nkubectl rollout undo deploy/checkout -n staging\\n```\"}",
	}}
	s := service(t, f)
	sh := listen(t, s, func(line string) StepResult {
		if strings.Contains(line, "logs") {
			return StepResult{ExitCode: 0, Output: "dial tcp 10.0.0.5:5432: connection refused\npassword=hunter2"}
		}
		return StepResult{ExitCode: 0, Output: "checkout-1   0/1   CrashLoopBackOff   7"}
	})
	q := Question{Text: "why is checkout crashlooping?", Cwd: "/w"}
	if err := s.Start("i1", q, false); err == nil {
		t.Fatal("started without the first-send preview")
	}
	if err := s.Start("i1", q, true); err != nil {
		t.Fatal(err)
	}
	d := sh.wait(t)
	if d.Error != "" || d.Cancelled || d.Steps != 2 || d.Limit != "" {
		t.Fatalf("done %+v", d)
	}
	if !strings.HasPrefix(d.Finding, "Finding: checkout-1 can't reach the database.") || d.Fix != "kubectl rollout undo deploy/checkout -n staging" {
		t.Errorf("finding %q fix %q", d.Finding, d.Fix)
	}
	if want := []string{"kubectl get pods -n staging", "kubectl logs checkout-1 -n staging --tail=200"}; strings.Join(sh.lines, "|") != strings.Join(want, "|") {
		t.Errorf("typed %q", sh.lines)
	}
	if len(sh.refuse) != 1 || !strings.Contains(sh.refuse[0], "delete isn't read-only") {
		t.Errorf("refused %q", sh.refuse)
	}
	if !strings.Contains(sh.text, "Listing pods.") || !strings.Contains(sh.text, "Deleting it.") {
		t.Errorf("text %q", sh.text)
	}
	if !d.Usage.Counted || d.Usage.InputTokens != 4000 || d.Provider != "claude" {
		t.Errorf("usage %+v provider %q", d.Usage, d.Provider)
	}

	// What went back to the model: numbered results, the refusal, redacted output.
	f.mu.Lock()
	last := f.bodies[len(f.bodies)-1]
	f.mu.Unlock()
	sent, _ := json.Marshal(last["messages"])
	for _, want := range []string{
		`[1] $ kubectl get pods -n staging\nExit code 0. Output:\ncheckout-1`,
		`Refused: kubectl delete isn't read-only`,
		`[2] $ kubectl logs checkout-1 -n staging --tail=200`,
		`password=[REDACTED]`,
		`Question: why is checkout crashlooping?`,
	} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("sent lacks %q", want)
		}
	}
	if strings.Contains(string(sent), "hunter2") {
		t.Error("a secret was sent")
	}
}

func TestStepLimitAndStop(t *testing.T) {
	var replies []string
	for range MaxSteps + 3 {
		replies = append(replies, `{"argv":["kubectl","get","pods"]}`)
	}
	f := &fakeClaude{replies: replies}
	s := service(t, f)
	sh := listen(t, s, func(string) StepResult { return StepResult{Output: "ok"} })
	if err := s.Start("i2", Question{Text: "q"}, true); err != nil {
		t.Fatal(err)
	}
	d := sh.wait(t)
	if d.Steps != MaxSteps || d.Limit != "steps" || d.Error != "" {
		t.Fatalf("done %+v", d)
	}
	f.mu.Lock()
	sent, _ := json.Marshal(f.bodies[len(f.bodies)-1]["messages"])
	f.mu.Unlock()
	if !strings.Contains(string(sent), "limit (12 commands) is reached") {
		t.Error("the model wasn't told to finish")
	}

	// Stop while a command runs.
	block := make(chan struct{})
	s2 := service(t, &fakeClaude{replies: []string{`{"argv":["kubectl","get","pods"]}`}})
	sh2 := listen(t, s2, func(string) StepResult { <-block; return StepResult{} })
	if err := s2.Start("i3", Question{Text: "q"}, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		sh2.mu.Lock()
		n := len(sh2.lines)
		sh2.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s2.Stop("i3")
	if d := sh2.wait(t); !d.Cancelled {
		t.Errorf("stopped: %+v", d)
	}
	close(block)
}

func TestShellBusyEnds(t *testing.T) {
	s := service(t, &fakeClaude{replies: []string{`{"argv":["kubectl","get","pods"]}`}})
	sh := listen(t, s, func(string) StepResult { return StepResult{Error: "the shell is busy"} })
	s.Start("i4", Question{Text: "q"}, true)
	if d := sh.wait(t); d.Error != "the shell is busy" {
		t.Errorf("done %+v", d)
	}
}

func TestClip(t *testing.T) {
	var lines []string
	for i := range 500 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	c := clip(strings.Join(lines, "\n"))
	if !strings.HasPrefix(c, "line 0\n") || !strings.HasSuffix(c, "line 499") || !strings.Contains(c, "(300 lines left out)") {
		t.Errorf("clip: %.80q … %q", c, c[len(c)-20:])
	}
}

func TestRecentInFacts(t *testing.T) {
	s := NewService(nil, nil)
	var long []string
	for i := range 50 {
		long = append(long, fmt.Sprintf("line %d", i))
	}
	r, err := s.Request(Question{Text: "why did cat fail?", Recent: []Recent{
		{Command: "ls", ExitCode: 0, Output: "a"},
		{Command: "make", ExitCode: 2, Output: strings.Join(long, "\n")},
		{Command: "echo hi", ExitCode: 0, Output: "hi"},
		{Command: "cat /etc/os-relase", ExitCode: 1, Output: "cat: /etc/os-relase: No such file or directory"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	msg := r.Messages[0].Content
	for _, want := range []string{
		"The last commands in this shell, oldest first:\n$ make   (exit 2)\n… (20 earlier lines)\nline 20\n",
		"$ cat /etc/os-relase   (exit 1)\ncat: /etc/os-relase: No such file or directory\n\nQuestion: why did cat fail?",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("facts lack %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "$ ls ") {
		t.Error("more than 3 recent commands were sent")
	}
	if !strings.Contains(r.System, "answer from it right away without running anything") {
		t.Error("the prompt doesn't say to answer from recent output")
	}
}

func TestDeclined(t *testing.T) {
	f := &fakeClaude{replies: []string{
		`{"argv":["aws","route53","list-hosted-zones"]}`,
		`{"text":"You can export each zone with list-resource-record-sets."}`,
	}}
	s := service(t, f)
	sh := listen(t, s, func(string) StepResult { return StepResult{Declined: true} })
	if err := s.Start("i5", Question{Text: "is it possible to back up route53?"}, true); err != nil {
		t.Fatal(err)
	}
	d := sh.wait(t)
	if d.Error != "" || d.Steps != 1 || d.Declined != 1 || !strings.HasPrefix(d.Finding, "You can export") {
		t.Fatalf("done %+v", d)
	}
	f.mu.Lock()
	sent, _ := json.Marshal(f.bodies[1]["messages"])
	system, _ := f.bodies[0]["system"].(string)
	f.mu.Unlock()
	if !strings.Contains(string(sent), "The user declined to run this command.") {
		t.Errorf("the model wasn't told: %s", sent)
	}
	if !strings.Contains(system, "A general question") || !strings.Contains(system, "approve or decline") {
		t.Error("the prompt doesn't say commands need approval or that general questions need none")
	}
}
