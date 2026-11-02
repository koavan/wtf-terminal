package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dayanraj/wtf/internal/config"
)

var runTool = Tool{Name: "run", Description: "Runs a command", Schema: map[string]any{
	"type": "object", "properties": map[string]any{"argv": map[string]any{"type": "array"}}}}

// conversation is a question, the model's call, and the tool's result.
func conversation() []Turn {
	return []Turn{
		{Role: "user", Text: "why? token hunter2"},
		{Role: "assistant", Text: "Looking.", Calls: []ToolCall{{ID: "c1", Name: "run", Input: json.RawMessage(`{"argv":["kubectl","get","pods"]}`)}}},
		{Role: "user", Results: []ToolResult{{CallID: "c1", Content: "pod-1 CrashLoopBackOff hunter2"}}},
	}
}

func TestAnthropicTools(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking logs."}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c2","name":"run","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"argv\":[\"kubectl\","}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"logs\",\"pod-1\"]}"}}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"c3","name":"run","input":{}}}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":40}}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer srv.Close()
	s := newService(t, oneProvider("claude", config.AIProvider{Kind: "anthropic", Model: "m", BaseURL: srv.URL}))
	s.SetKey("claude", "k")

	first := Request{System: "sys", Messages: []Message{{Role: "user", Content: "why?"}}}
	if _, err := NewToolSession(s, false, nil, first, false); err == nil {
		t.Fatal("started without showing the first send")
	}
	ts, err := NewToolSession(s, false, nil, first, true)
	if err != nil {
		t.Fatal(err)
	}
	var streamed string
	reply, u, err := ts.Send(context.Background(), "sys", conversation(), []Tool{runTool}, 0, func(s string) { streamed += s })
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Checking logs." || streamed != reply.Text || len(reply.Calls) != 2 {
		t.Fatalf("reply %+v", reply)
	}
	if c := reply.Calls[0]; c.ID != "c2" || string(c.Input) != `{"argv":["kubectl","logs","pod-1"]}` || string(reply.Calls[1].Input) != "{}" {
		t.Errorf("calls %+v", reply.Calls)
	}
	if u != (Usage{Counted: true, InputTokens: 100, OutputTokens: 40}) {
		t.Errorf("usage %+v", u)
	}

	msgs := got["messages"].([]any)
	call := msgs[1].(map[string]any)["content"].([]any)[1].(map[string]any)
	result := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if call["type"] != "tool_use" || call["id"] != "c1" || result["type"] != "tool_result" || result["tool_use_id"] != "c1" ||
		result["content"] != "pod-1 CrashLoopBackOff [REDACTED]" {
		t.Errorf("sent call %v result %v", call, result)
	}
	if first := msgs[0].(map[string]any)["content"].([]any)[0].(map[string]any); first["text"] != "why? token [REDACTED]" {
		t.Errorf("sent question %v", first)
	}
	if tools := got["tools"].([]any); tools[0].(map[string]any)["input_schema"] == nil {
		t.Errorf("tools %v", tools)
	}
	// The second session doesn't need the preview.
	if _, err := NewToolSession(s, false, nil, first, false); err != nil {
		t.Errorf("second session: %v", err)
	}
}

func TestOpenAITools(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		for _, e := range []string{
			`{"choices":[{"delta":{"role":"assistant","content":"Checking."}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"run","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"argv\":[\"dig\","}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"example.com\"]}"}}]}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":50,"completion_tokens":9}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer srv.Close()
	s := newService(t, oneProvider("local", config.AIProvider{Kind: "openai", Model: "qwen", BaseURL: srv.URL + "/v1"}))
	ts, err := NewToolSession(s, false, nil, Request{Messages: []Message{{Role: "user", Content: "q"}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	reply, u, err := ts.Send(context.Background(), "sys", conversation(), []Tool{runTool}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Checking." || len(reply.Calls) != 1 || reply.Calls[0].ID != "call_9" || string(reply.Calls[0].Input) != `{"argv":["dig","example.com"]}` {
		t.Fatalf("reply %+v", reply)
	}
	if !u.Counted || u.InputTokens != 50 {
		t.Errorf("usage %+v", u)
	}
	msgs := got["messages"].([]any)
	asst := msgs[2].(map[string]any)
	tool := msgs[3].(map[string]any)
	fn := asst["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if msgs[0].(map[string]any)["role"] != "system" || fn["arguments"] != `{"argv":["kubectl","get","pods"]}` ||
		tool["role"] != "tool" || tool["tool_call_id"] != "c1" || tool["content"] != "pod-1 CrashLoopBackOff [REDACTED]" {
		t.Errorf("sent %v", msgs)
	}
	if got["tools"].([]any)[0].(map[string]any)["type"] != "function" {
		t.Errorf("tools %v", got["tools"])
	}
}

func TestCommandProviderHasNoTools(t *testing.T) {
	s := newService(t, oneProvider("cli", config.AIProvider{Kind: "command", Command: []string{"cat"}}))
	_, err := NewToolSession(s, false, nil, Request{Messages: []Message{{Role: "user", Content: "q"}}}, true)
	if !errors.Is(err, ErrNoTools) {
		t.Errorf("err %v", err)
	}
}
