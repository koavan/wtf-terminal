package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/zalando/go-keyring"
)

// Preset fills in the Settings window's Add Provider form. Everything it
// sets can be changed there.
type Preset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// "Cloud API", "On this computer", "Command-line tool", "Other".
	Group    string   `json:"group"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	BaseURL  string   `json:"baseUrl"`
	Model    string   `json:"model"`
	Command  []string `json:"command"`
	NeedsKey bool     `json:"needsKey"`
	// Where to get an API key, or how to install the tool.
	Help string `json:"help"`
}

var presets = []Preset{
	{ID: "anthropic", Label: "Claude API (Anthropic)", Group: "Cloud API", Name: "claude", Kind: "anthropic", Model: "claude-sonnet-5-5", NeedsKey: true,
		Help: "Create a key at console.anthropic.com → API Keys."},
	{ID: "openai", Label: "OpenAI", Group: "Cloud API", Name: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", NeedsKey: true,
		Help: "Create a key at platform.openai.com → API keys."},
	{ID: "gemini", Label: "Google Gemini", Group: "Cloud API", Name: "gemini", Kind: "openai", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", NeedsKey: true,
		Help: "Create a key at aistudio.google.com → Get API key."},
	{ID: "openrouter", Label: "OpenRouter (many models, one key)", Group: "Cloud API", Name: "openrouter", Kind: "openai", BaseURL: "https://openrouter.ai/api/v1", NeedsKey: true,
		Help: "Create a key at openrouter.ai → Keys. Replies show their cost."},
	{ID: "groq", Label: "Groq", Group: "Cloud API", Name: "groq", Kind: "openai", BaseURL: "https://api.groq.com/openai/v1", NeedsKey: true,
		Help: "Create a key at console.groq.com → API Keys."},
	{ID: "mistral", Label: "Mistral", Group: "Cloud API", Name: "mistral", Kind: "openai", BaseURL: "https://api.mistral.ai/v1", NeedsKey: true,
		Help: "Create a key at console.mistral.ai → API Keys."},
	{ID: "deepseek", Label: "DeepSeek", Group: "Cloud API", Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", NeedsKey: true,
		Help: "Create a key at platform.deepseek.com → API keys."},
	{ID: "ollama", Label: "Ollama", Group: "On this computer", Name: "ollama", Kind: "openai", BaseURL: "http://localhost:11434/v1",
		Help: "Nothing leaves this computer. Install from ollama.com, then `ollama pull <model>`."},
	{ID: "lmstudio", Label: "LM Studio", Group: "On this computer", Name: "lmstudio", Kind: "openai", BaseURL: "http://localhost:1234/v1",
		Help: "Nothing leaves this computer. Start the server in LM Studio's Developer tab."},
	{ID: "claude-code", Label: "Claude Code (claude -p)", Group: "Command-line tool", Name: "claude-code", Kind: "command", Command: []string{"claude", "-p"},
		Help: "Uses Claude Code's own login and settings."},
	{ID: "codex", Label: "Codex CLI (codex exec)", Group: "Command-line tool", Name: "codex", Kind: "command", Command: []string{"codex", "exec", "-"},
		Help: "Uses the Codex CLI's own login and settings."},
	{ID: "gemini-cli", Label: "Gemini CLI", Group: "Command-line tool", Name: "gemini-cli", Kind: "command", Command: []string{"gemini"},
		Help: "Uses the Gemini CLI's own login and settings."},
	{ID: "llm", Label: "llm (Simon Willison's)", Group: "Command-line tool", Name: "llm", Kind: "command", Command: []string{"llm"},
		Help: "Uses llm's default model; add -m <model> to choose one."},
	{ID: "custom-openai", Label: "Other OpenAI-compatible endpoint", Group: "Other", Name: "", Kind: "openai",
		Help: "Any server that speaks OpenAI's chat completions API: vLLM, llama.cpp, Azure OpenAI, a company gateway…"},
	{ID: "custom-anthropic", Label: "Other Anthropic-compatible endpoint", Group: "Other", Name: "", Kind: "anthropic", NeedsKey: true,
		Help: "A proxy or gateway that speaks the Claude Messages API."},
	{ID: "custom-command", Label: "Other command-line tool", Group: "Other", Name: "", Kind: "command",
		Help: "Any program that reads a prompt on stdin and writes the reply to stdout."},
}

// Presets lists the providers the Add Provider form offers.
func (s *Service) Presets() []Preset { return slices.Clone(presets) }

const modelsTimeout = 15 * time.Second

// Models asks a provider which models it has, for the Settings window's
// model picker. key is the one typed in the form; when it's empty, the key
// saved for name (or p's api-key-env) is used. Not for command providers.
func (s *Service) Models(name string, p config.AIProvider, key string) ([]string, error) {
	if p.Kind == "command" {
		return nil, errors.New("a command-line tool picks its own model")
	}
	if key == "" && name != "" {
		k := s.key(name, p)
		if k.err != nil {
			return nil, k.err
		}
		key = k.key
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelsTimeout)
	defer cancel()
	base := baseURL(p)
	target := base + "/models"
	if p.Kind == "anthropic" {
		target = base + "/v1/models?limit=1000"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	if key != "" {
		if p.Kind == "anthropic" {
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: %s%s", resp.Status, errorText(body), hint(body))
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("the model list isn't in a format wtf knows: %v", err)
	}
	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// MoveKey moves a provider's key to its new name, after a rename.
func (s *Service) MoveKey(old, name string) error {
	if old == name {
		return nil
	}
	v, err := keyring.Get(keyringService, old)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return keyringError(err)
	}
	if err := keyring.Set(keyringService, name, v); err != nil {
		return keyringError(err)
	}
	_ = keyring.Delete(keyringService, old)
	s.mu.Lock()
	delete(s.keys, old)
	s.keys[name] = keyLookup{key: v, from: "keychain"}
	s.mu.Unlock()
	return nil
}
