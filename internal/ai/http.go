package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// anthropicProvider talks to the Claude Messages API.
type anthropicProvider struct {
	client           *http.Client
	base, model, key string
	extra            map[string]string
}

func (p anthropicProvider) stream(ctx context.Context, system string, msgs []Message, maxTokens int, out func(string)) (Usage, error) {
	body := map[string]any{
		"model":      p.model,
		"max_tokens": maxTokens,
		"messages":   msgs,
		"stream":     true,
	}
	if system != "" {
		body["system"] = system
	}
	headers := withExtra(p.extra, map[string]string{"x-api-key": p.key, "anthropic-version": "2023-06-01"})
	resp, err := post(ctx, p.client, p.base+"/v1/messages", headers, body)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()

	var u Usage
	err = readSSE(resp.Body, func(event, data string) error {
		var m struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
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
		case "content_block_delta":
			if m.Delta.Type == "text_delta" {
				out(m.Delta.Text)
			}
		case "message_delta":
			u.OutputTokens = m.Usage.OutputTokens
		case "error":
			return errors.New(m.Error.Message)
		}
		return nil
	})
	return u, err
}

// openaiProvider talks to any OpenAI-compatible chat completions endpoint.
type openaiProvider struct {
	client           *http.Client
	base, model, key string
	extra            map[string]string
}

func (p openaiProvider) stream(ctx context.Context, system string, msgs []Message, _ int, out func(string)) (Usage, error) {
	// No token limit: servers disagree on its name (max_tokens or
	// max_completion_tokens) and some refuse the other one.
	all := make([]Message, 0, len(msgs)+1)
	if system != "" {
		all = append(all, Message{Role: "system", Content: system})
	}
	all = append(all, msgs...)
	body := map[string]any{
		"model":          p.model,
		"messages":       all,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
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
		return Usage{}, err
	}
	defer resp.Body.Close()

	var u Usage
	err = readSSE(resp.Body, func(_, data string) error {
		if data == "[DONE]" {
			return errStop
		}
		var m struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
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
		for _, c := range m.Choices {
			if c.Delta.Content != "" {
				out(c.Delta.Content)
			}
		}
		if m.Usage != nil {
			u = Usage{Counted: true, InputTokens: m.Usage.PromptTokens, OutputTokens: m.Usage.CompletionTokens, Cost: m.Usage.Cost}
		}
		return nil
	})
	return u, err
}

// post sends a JSON body and returns the response if it's a 2xx, else the
// error the server gave.
func post(ctx context.Context, client *http.Client, target string, headers map[string]string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return nil, fmt.Errorf("%s: %s%s", resp.Status, errorText(msg), hint(msg))
}

// withExtra is a provider's extra headers with wtf's own on top.
func withExtra(extra, own map[string]string) map[string]string {
	h := make(map[string]string, len(extra)+len(own))
	for k, v := range extra {
		h[k] = v
	}
	for k, v := range own {
		h[k] = v
	}
	return h
}

// hint says how to fix an error the user can fix in wtf.
func hint(body []byte) string {
	if bytes.Contains(body, []byte("anthropic-workspace-id")) {
		return " In Settings → AI, Edit… this provider and add the extra header anthropic-workspace-id: <your workspace ID> (console.anthropic.com → Workspaces), or use a key made inside a workspace."
	}
	return ""
}

// errorText is the message in an API error body ({"error": {"message"}} or
// {"error": "…"}), else the body itself.
func errorText(body []byte) string {
	var e struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil {
		var inner struct {
			Message string `json:"message"`
		}
		var s string
		switch {
		case json.Unmarshal(e.Error, &inner) == nil && inner.Message != "":
			return inner.Message
		case json.Unmarshal(e.Error, &s) == nil && s != "":
			return s
		case e.Message != "":
			return e.Message
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		text = text[:500] + "…"
	}
	if text == "" {
		return "no details"
	}
	return text
}

// errStop ends readSSE early without an error.
var errStop = errors.New("stop")

// readSSE calls fn for each server-sent event: its type (may be "") and its
// data lines joined.
func readSSE(r io.Reader, fn func(event, data string) error) error {
	br := bufio.NewReader(r)
	var event string
	var data []string
	dispatch := func() error {
		if len(data) == 0 {
			event = ""
			return nil
		}
		err := fn(event, strings.Join(data, "\n"))
		event, data = "", nil
		return err
	}
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if e := dispatch(); e != nil {
					return stopped(e)
				}
			case strings.HasPrefix(line, ":"):
			default:
				field, value, _ := strings.Cut(line, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					event = value
				case "data":
					data = append(data, value)
				}
			}
		}
		if err == io.EOF {
			return stopped(dispatch())
		}
		if err != nil {
			return err
		}
	}
}

func stopped(err error) error {
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}
