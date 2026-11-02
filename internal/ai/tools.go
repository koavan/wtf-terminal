package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Tool calling, for agents (#27): the model asks for a tool by name with
// JSON input, the caller runs it and sends the result back, until the model
// answers in text. Only the anthropic and openai kinds can; a command
// provider can't, and a session refuses to start rather than fake it by
// parsing free text. Everything sent is redacted like Ask's text.

// Tool is a function the model may call.
type Tool struct {
	Name        string
	Description string
	// The input's JSON schema (an object).
	Schema map[string]any
}

// ToolCall is the model asking for a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult answers a call.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// Turn is one message of a tool conversation: the user's text and tool
// results, or the model's text and tool calls.
type Turn struct {
	// "user" or "assistant".
	Role    string
	Text    string
	Calls   []ToolCall
	Results []ToolResult
}

// toolProvider is a provider that can call tools.
type toolProvider interface {
	turn(ctx context.Context, system string, turns []Turn, tools []Tool, maxTokens int, out func(string)) (Turn, Usage, error)
}

// ToolSession is a tool conversation with one provider.
type ToolSession struct {
	s    *Service
	p    toolProvider
	Info ProviderInfo
}

// ErrNoTools says the chosen provider can't call tools.
var ErrNoTools = errors.New("this needs an AI provider that can call tools: the Claude API or an OpenAI-compatible endpoint, not a command-line tool")

// NewToolSession opens a tool conversation with the default (or, with
// fast, the quick) provider. first is the session's opening request, for
// the first-send rule: when nothing has been sent to the provider yet, shown
// must say the user saw Preview(first).
func NewToolSession(s *Service, fast bool, env map[string]string, first Request, shown bool) (*ToolSession, error) {
	first.Fast = fast
	out, err := s.Preview(first)
	if err != nil {
		return nil, err
	}
	if out.FirstSend && !shown {
		return nil, errors.New("show what will be sent before the first request to " + out.Provider.Name)
	}
	c := s.config()
	p, err := s.build(out.Provider.Name, c.AI.Providers[out.Provider.Name], env)
	if err != nil {
		return nil, err
	}
	tp, ok := p.(toolProvider)
	if !ok {
		return nil, ErrNoTools
	}
	if out.FirstSend {
		s.markSeen(out.Provider.Name, out.Provider.Destination)
	}
	return &ToolSession{s: s, p: tp, Info: out.Provider}, nil
}

// Send sends the conversation and returns the model's turn; its text also
// streams to out. Every text and tool result is redacted first.
func (t *ToolSession) Send(ctx context.Context, system string, turns []Turn, tools []Tool, maxTokens int, out func(string)) (Turn, Usage, error) {
	system, _ = t.s.redact(system)
	sent := make([]Turn, len(turns))
	for i, tn := range turns {
		tn.Text, _ = t.s.redact(tn.Text)
		if len(tn.Results) > 0 {
			rs := make([]ToolResult, len(tn.Results))
			for j, r := range tn.Results {
				r.Content, _ = t.s.redact(r.Content)
				rs[j] = r
			}
			tn.Results = rs
		}
		sent[i] = tn
	}
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if out == nil {
		out = func(string) {}
	}
	return t.p.turn(ctx, system, sent, tools, maxTokens, out)
}

func input(raw json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// ── Anthropic ────────────────────────────────────────────────────────────

func (p anthropicProvider) turn(ctx context.Context, system string, turns []Turn, tools []Tool, maxTokens int, out func(string)) (Turn, Usage, error) {
	var msgs []map[string]any
	for _, t := range turns {
		var blocks []map[string]any
		for _, r := range t.Results {
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": r.CallID, "content": r.Content, "is_error": r.IsError})
		}
		if t.Text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": t.Text})
		}
		for _, c := range t.Calls {
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": input(c.Input)})
		}
		if len(blocks) == 0 {
			blocks = append(blocks, map[string]any{"type": "text", "text": "(empty)"})
		}
		msgs = append(msgs, map[string]any{"role": t.Role, "content": blocks})
	}
	var defs []map[string]any
	for _, t := range tools {
		defs = append(defs, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
	}
	body := map[string]any{"model": p.model, "max_tokens": maxTokens, "messages": msgs, "stream": true}
	if system != "" {
		body["system"] = system
	}
	if len(defs) > 0 {
		body["tools"] = defs
	}
	headers := withExtra(p.extra, map[string]string{"x-api-key": p.key, "anthropic-version": "2023-06-01"})
	resp, err := post(ctx, p.client, p.base+"/v1/messages", headers, body)
	if err != nil {
		return Turn{}, Usage{}, err
	}
	defer resp.Body.Close()

	reply := Turn{Role: "assistant"}
	var text strings.Builder
	type block struct {
		call *ToolCall
		json strings.Builder
	}
	blocks := map[int]*block{}
	var order []int
	var u Usage
	err = readSSE(resp.Body, func(_, data string) error {
		var m struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &m) != nil {
			return nil
		}
		switch m.Type {
		case "message_start":
			u.Counted = true
			u.InputTokens = m.Message.Usage.InputTokens
			u.OutputTokens = m.Message.Usage.OutputTokens
		case "content_block_start":
			if m.ContentBlock.Type == "tool_use" {
				blocks[m.Index] = &block{call: &ToolCall{ID: m.ContentBlock.ID, Name: m.ContentBlock.Name}}
				order = append(order, m.Index)
			}
		case "content_block_delta":
			switch m.Delta.Type {
			case "text_delta":
				text.WriteString(m.Delta.Text)
				out(m.Delta.Text)
			case "input_json_delta":
				if b := blocks[m.Index]; b != nil {
					b.json.WriteString(m.Delta.PartialJSON)
				}
			}
		case "message_delta":
			u.OutputTokens = m.Usage.OutputTokens
		case "error":
			return errors.New(m.Error.Message)
		}
		return nil
	})
	reply.Text = text.String()
	for _, i := range order {
		b := blocks[i]
		b.call.Input = input(json.RawMessage(b.json.String()))
		reply.Calls = append(reply.Calls, *b.call)
	}
	return reply, u, err
}

// ── OpenAI-compatible ────────────────────────────────────────────────────

func (p openaiProvider) turn(ctx context.Context, system string, turns []Turn, tools []Tool, _ int, out func(string)) (Turn, Usage, error) {
	var msgs []map[string]any
	if system != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": system})
	}
	for _, t := range turns {
		if t.Role == "assistant" {
			m := map[string]any{"role": "assistant", "content": t.Text}
			if len(t.Calls) > 0 {
				var calls []map[string]any
				for _, c := range t.Calls {
					calls = append(calls, map[string]any{"id": c.ID, "type": "function",
						"function": map[string]any{"name": c.Name, "arguments": string(input(c.Input))}})
				}
				m["tool_calls"] = calls
			}
			msgs = append(msgs, m)
			continue
		}
		for _, r := range t.Results {
			content := r.Content
			if r.IsError {
				content = "Error: " + content
			}
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": r.CallID, "content": content})
		}
		if t.Text != "" {
			msgs = append(msgs, map[string]any{"role": "user", "content": t.Text})
		}
	}
	var defs []map[string]any
	for _, t := range tools {
		defs = append(defs, map[string]any{"type": "function",
			"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}})
	}
	body := map[string]any{
		"model": p.model, "messages": msgs, "stream": true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(defs) > 0 {
		body["tools"] = defs
	}
	headers := withExtra(p.extra, map[string]string{})
	if p.key != "" {
		headers["Authorization"] = "Bearer " + p.key
		if u, err := url.Parse(p.base); err == nil && strings.HasSuffix(u.Hostname(), ".azure.com") {
			headers["api-key"] = p.key
		}
	}
	resp, err := post(ctx, p.client, p.base+"/chat/completions", headers, body)
	if err != nil {
		return Turn{}, Usage{}, err
	}
	defer resp.Body.Close()

	reply := Turn{Role: "assistant"}
	var text strings.Builder
	type call struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*call{}
	var u Usage
	err = readSSE(resp.Body, func(_, data string) error {
		if data == "[DONE]" {
			return errStop
		}
		var m struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int     `json:"prompt_tokens"`
				CompletionTokens int     `json:"completion_tokens"`
				Cost             float64 `json:"cost"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &m) != nil {
			return nil
		}
		if m.Error != nil {
			return errors.New(m.Error.Message)
		}
		for _, ch := range m.Choices {
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				out(ch.Delta.Content)
			}
			for _, tc := range ch.Delta.ToolCalls {
				c := calls[tc.Index]
				if c == nil {
					c = &call{}
					calls[tc.Index] = c
				}
				if tc.ID != "" {
					c.id = tc.ID
				}
				if tc.Function.Name != "" {
					c.name = tc.Function.Name
				}
				c.args.WriteString(tc.Function.Arguments)
			}
		}
		if m.Usage != nil {
			u = Usage{Counted: true, InputTokens: m.Usage.PromptTokens, OutputTokens: m.Usage.CompletionTokens, Cost: m.Usage.Cost}
		}
		return nil
	})
	reply.Text = text.String()
	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for n, i := range idx {
		c := calls[i]
		id := c.id
		if id == "" {
			id = fmt.Sprintf("call_%d", n)
		}
		reply.Calls = append(reply.Calls, ToolCall{ID: id, Name: c.name, Input: input(json.RawMessage(c.args.String()))})
	}
	return reply, u, err
}
