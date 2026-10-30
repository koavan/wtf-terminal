// Package ai sends prompts to whichever AI the user configured: the Claude
// API, any OpenAI-compatible endpoint, or any CLI tool. Nothing here runs on
// its own: a request goes out only when a feature asks, every text is passed
// through Hide Secrets' redaction first, and the first request to a provider
// waits until the user has seen exactly what will be sent.
package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/events"
)

const (
	// A piece of a reply as it streams in.
	EventChunk = "ai:chunk"
	// A reply finished, failed or was cancelled.
	EventDone = "ai:done"

	// A request nobody cancels still ends.
	requestTimeout = 5 * time.Minute
	// Replies stream to the UI at most this often.
	flushEvery       = 50 * time.Millisecond
	defaultMaxTokens = 4096
)

// Message is one turn of a conversation.
type Message struct {
	// "user" or "assistant".
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is what a feature asks.
type Request struct {
	// Use the ai-fast provider, for quick inline features.
	Fast bool `json:"fast"`
	// Use this provider instead (AI Providers' Test).
	Provider  string    `json:"provider"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"maxTokens"`
	// The focused shell's variables; its PATH finds a command provider's
	// program.
	Env map[string]string `json:"env"`
}

// Usage is what a request cost, as far as the provider says.
type Usage struct {
	// Whether the provider reported token counts (CLI tools don't).
	Counted      bool `json:"counted"`
	InputTokens  int  `json:"inputTokens"`
	OutputTokens int  `json:"outputTokens"`
	// US dollars, when the provider reports it (OpenRouter does); else 0.
	Cost float64 `json:"cost"`
}

// Chunk is EventChunk's payload.
type Chunk struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Done is EventDone's payload.
type Done struct {
	ID string `json:"id"`
	// The whole reply, including what was streamed.
	Text      string `json:"text"`
	Usage     Usage  `json:"usage"`
	Error     string `json:"error"`
	Cancelled bool   `json:"cancelled"`
	Millis    int64  `json:"millis"`
}

// ProviderInfo describes one configured provider.
type ProviderInfo struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Model string `json:"model"`
	// Where requests go: a host, or the command line.
	Destination string `json:"destination"`
	// Requests stay on this machine (a loopback URL).
	Local bool `json:"local"`
	// It needs an API key, and whether one was found.
	NeedsKey bool `json:"needsKey"`
	HasKey   bool `json:"hasKey"`
	// Where the key comes from: "keychain" or "$VARIABLE".
	KeyFrom string `json:"keyFrom"`
	Default bool   `json:"default"`
	Fast    bool   `json:"fast"`
}

// Status is the configured providers. With none, AI features stay hidden.
type Status struct {
	Enabled   bool           `json:"enabled"`
	Providers []ProviderInfo `json:"providers"`
}

// Outgoing is exactly what a request will send, for the preview.
type Outgoing struct {
	Provider ProviderInfo `json:"provider"`
	System   string       `json:"system"`
	Messages []Message    `json:"messages"`
	// How many secrets were replaced with [REDACTED].
	Redacted int `json:"redacted"`
	// Nothing has been sent to this provider (at this destination) yet:
	// show this before sending.
	FirstSend bool `json:"firstSend"`
}

// Redactor replaces the secrets in a text and says how many there were.
type Redactor func(text string) (string, int)

// provider streams one reply.
type provider interface {
	stream(ctx context.Context, system string, msgs []Message, maxTokens int, out func(string)) (Usage, error)
}

type Service struct {
	config   func() config.Config
	redact   Redactor
	stateDir string
	client   *http.Client

	mu       sync.Mutex
	inFlight map[string]context.CancelFunc
	keys     map[string]keyLookup // by provider name
	seen     map[string]string    // provider name → destination previewed
}

func NewService(stateDir string, cfg func() config.Config, redact Redactor) *Service {
	return &Service{
		config:   cfg,
		redact:   redact,
		stateDir: stateDir,
		client:   &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()},
		inFlight: map[string]context.CancelFunc{},
		keys:     map[string]keyLookup{},
	}
}

// Status lists the configured providers. It reads the keychain, so call it
// when AI settings or a feature are opened, not on a timer.
func (s *Service) Status() Status {
	c := s.config()
	var st Status
	for _, name := range sortedNames(c.AI.Providers) {
		st.Providers = append(st.Providers, s.info(c, name))
	}
	st.Enabled = len(st.Providers) > 0
	return st
}

func (s *Service) info(c config.Config, name string) ProviderInfo {
	p := c.AI.Providers[name]
	in := ProviderInfo{
		Name: name, Kind: p.Kind, Model: p.Model,
		Default: name == c.AIDefault, Fast: name == c.AIFast,
	}
	switch p.Kind {
	case "command":
		in.Destination = strings.Join(p.Command, " ")
	default:
		u, err := url.Parse(baseURL(p))
		if err == nil {
			in.Destination = u.Host
			in.Local = loopback(u.Hostname())
		}
		in.NeedsKey = p.Kind == "anthropic" || !in.Local
		k := s.key(name, p)
		in.HasKey, in.KeyFrom = k.key != "", k.from
	}
	return in
}

// chosen is the provider a request goes to.
func (s *Service) chosen(fast bool) (config.Config, string, error) {
	c := s.config()
	name := c.AIDefault
	if fast {
		name = c.AIFast
	}
	if name == "" {
		if len(c.AI.Providers) == 0 {
			return c, "", errors.New("no AI provider is set up: add one to config.toml")
		}
		return c, "", errors.New("set ai-default in config.toml to choose an AI provider")
	}
	return c, name, nil
}

// Preview returns exactly what Ask would send for this request.
func (s *Service) Preview(r Request) (Outgoing, error) {
	c, name, err := s.chosen(r.Fast)
	if r.Provider != "" {
		if _, ok := c.AI.Providers[r.Provider]; !ok {
			return Outgoing{}, fmt.Errorf("there's no AI provider named %q", r.Provider)
		}
		name, err = r.Provider, nil
	}
	if err != nil {
		return Outgoing{}, err
	}
	out := Outgoing{Provider: s.info(c, name)}
	var n int
	out.System, n = s.redact(r.System)
	out.Redacted += n
	for _, m := range r.Messages {
		text, n := s.redact(m.Content)
		out.Redacted += n
		out.Messages = append(out.Messages, Message{Role: m.Role, Content: text})
	}
	out.FirstSend = !s.wasSeen(name, out.Provider.Destination)
	return out, nil
}

// Ask sends a request and returns at once; the reply streams in as
// EventChunk events and ends with EventDone. id names it for Cancel. shown
// says the user saw Preview's result: the first request to a provider is
// refused without it.
func (s *Service) Ask(id string, r Request, shown bool) error {
	if id == "" {
		return errors.New("a request needs an id")
	}
	if len(r.Messages) == 0 {
		return errors.New("nothing to send")
	}
	out, err := s.Preview(r)
	if err != nil {
		return err
	}
	if out.FirstSend && !shown {
		return errors.New("show what will be sent before the first request to " + out.Provider.Name)
	}
	c := s.config()
	p, err := s.build(out.Provider.Name, c.AI.Providers[out.Provider.Name], r.Env)
	if err != nil {
		return err
	}
	if out.FirstSend {
		s.markSeen(out.Provider.Name, out.Provider.Destination)
	}
	maxTokens := r.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	s.mu.Lock()
	if _, busy := s.inFlight[id]; busy {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("request %s is already running", id)
	}
	s.inFlight[id] = cancel
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inFlight, id)
			s.mu.Unlock()
			cancel()
		}()
		start := time.Now()
		var all, pending strings.Builder
		last := start
		flush := func() {
			if pending.Len() > 0 {
				events.Emit(EventChunk, Chunk{ID: id, Text: pending.String()})
				pending.Reset()
			}
			last = time.Now()
		}
		usage, err := p.stream(ctx, out.System, out.Messages, maxTokens, func(text string) {
			all.WriteString(text)
			pending.WriteString(text)
			if time.Since(last) >= flushEvery {
				flush()
			}
		})
		flush()
		d := Done{ID: id, Text: all.String(), Usage: usage, Millis: time.Since(start).Milliseconds()}
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			d.Cancelled = true
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			d.Error = fmt.Sprintf("no reply after %s", requestTimeout)
		case err != nil:
			d.Error = err.Error()
		}
		events.Emit(EventDone, d)
	}()
	return nil
}

// Cancel stops a running request; its EventDone says Cancelled.
func (s *Service) Cancel(id string) {
	s.mu.Lock()
	cancel := s.inFlight[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ServiceShutdown stops every request still running.
func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.inFlight {
		cancel()
	}
	return nil
}

func (s *Service) build(name string, p config.AIProvider, env map[string]string) (provider, error) {
	if p.Kind == "command" {
		return commandProvider{argv: p.Command, env: env}, nil
	}
	k := s.key(name, p)
	if k.err != nil {
		return nil, k.err
	}
	u, _ := url.Parse(baseURL(p))
	if k.key == "" && (p.Kind == "anthropic" || u == nil || !loopback(u.Hostname())) {
		if p.APIKeyEnv != "" {
			return nil, fmt.Errorf("%s: $%s is empty", name, p.APIKeyEnv)
		}
		return nil, fmt.Errorf("%s needs an API key: add it in AI Providers…", name)
	}
	switch p.Kind {
	case "anthropic":
		return anthropicProvider{client: s.client, base: baseURL(p), model: p.Model, key: k.key, extra: p.Headers}, nil
	case "openai":
		return openaiProvider{client: s.client, base: baseURL(p), model: p.Model, key: k.key, extra: p.Headers}, nil
	}
	return nil, fmt.Errorf("%s: unknown kind %q", name, p.Kind)
}

func baseURL(p config.AIProvider) string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	if p.Kind == "anthropic" {
		return "https://api.anthropic.com"
	}
	return "https://api.openai.com/v1"
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
