package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"apcode/internal/model"
)

// ErrMissingAPIKey is returned when no API key is configured.
var ErrMissingAPIKey = fmt.Errorf("free cloud provider: API key not configured")

// ErrProviderNotConfigured is returned when the free cloud provider is not enabled.
var ErrProviderNotConfigured = fmt.Errorf("free cloud provider is not configured")

// FreeCloudConfig configures the free cloud provider.
type FreeCloudConfig struct {
	// Enabled controls whether the free cloud provider is active.
	Enabled bool
	// BaseURL is the OpenAI-compatible API endpoint.
	BaseURL string
	// APIKey is the API key for the provider (read from env var or config).
	APIKey string
	// DefaultModel is the model to use when none is specified.
	DefaultModel string
	// Timeout is the HTTP timeout for requests.
	Timeout time.Duration
	// HTTPClient allows tests to inject a client (e.g. against httptest.Server).
	HTTPClient *http.Client
}

// FreeCloudProvider implements ModelProvider for free cloud models
// using OpenAI-compatible HTTP APIs.
type FreeCloudProvider struct {
	cfg    FreeCloudConfig
	client *http.Client
	md     *model.ModelMetadata
	name   string
	maxTok int
	mu     sync.RWMutex
}

// NewFreeCloudProvider creates a free cloud provider.
// If cfg is not enabled or has no valid base URL, IsReady returns false.
func NewFreeCloudProvider(cfg FreeCloudConfig) (*FreeCloudProvider, error) {
	if !cfg.Enabled {
		return nil, ErrProviderNotConfigured
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("free cloud provider: base URL is empty")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("free cloud provider: invalid base URL %q: %w", cfg.BaseURL, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("free cloud provider: base URL must use http or https scheme")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	p := &FreeCloudProvider{
		cfg:    cfg,
		client: client,
		name:   cfg.DefaultModel,
		maxTok: 512,
	}
	return p, nil
}

// WithModel sets the default model for the provider.
func (p *FreeCloudProvider) WithModel(modelID string) *FreeCloudProvider {
	p.mu.Lock()
	p.name = modelID
	p.mu.Unlock()
	return p
}

// WithMaxTokens sets the default max tokens.
func (p *FreeCloudProvider) WithMaxTokens(n int) *FreeCloudProvider {
	if n < 64 {
		n = 64
	}
	if n > 4096 {
		n = 4096
	}
	p.mu.Lock()
	p.maxTok = n
	p.mu.Unlock()
	return p
}

// ensureAPIKey returns the API key or ErrMissingAPIKey.
func (p *FreeCloudProvider) ensureAPIKey() error {
	if strings.TrimSpace(p.cfg.APIKey) == "" {
		return ErrMissingAPIKey
	}
	return nil
}

// Generate produces a completion via the free cloud API.
func (p *FreeCloudProvider) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	if err := p.ensureAPIKey(); err != nil {
		return nil, fmt.Errorf("free cloud: %w", err)
	}
	p.mu.RLock()
	name := p.name
	maxTok := p.maxTok
	p.mu.RUnlock()
	if p.cfg.DefaultModel == "" && name == "" {
		return nil, fmt.Errorf("free cloud: no model configured")
	}
	modelName := name
	if modelName == "" {
		modelName = p.cfg.DefaultModel
	}
	payload := map[string]any{
		"model":  modelName,
		"prompt": req.Prompt,
		"stream": false,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	} else if maxTok > 0 {
		payload["max_tokens"] = maxTok
	}
	if len(req.Images) > 0 {
		payload["images"] = req.Images
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("free cloud: encode request failed: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(p.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("free cloud: build request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("free cloud: request cancelled")
		}
		return nil, fmt.Errorf("free cloud: provider unavailable (%w)", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("free cloud: read response failed: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("free cloud: invalid API key")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("free cloud: rate limit exceeded")
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("free cloud: provider returned status %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("free cloud: decode response failed: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("free cloud: no choices in response")
	}
	text := out.Choices[0].Message.Content
	tokens := len(strings.Fields(text))
	if tokens == 0 {
		tokens = 1
	}
	return &GenerateResponse{
		Text:            text,
		TokensGenerated: tokens,
		FinishReason:    "stop",
	}, nil
}

// Stream produces tokens incrementally from the free cloud API.
func (p *FreeCloudProvider) Stream(ctx context.Context, req GenerateRequest) (<-chan Token, error) {
	if err := p.ensureAPIKey(); err != nil {
		return nil, fmt.Errorf("free cloud: %w", err)
	}
	p.mu.RLock()
	name := p.name
	maxTok := p.maxTok
	p.mu.RUnlock()
	if p.cfg.DefaultModel == "" && name == "" {
		return nil, fmt.Errorf("free cloud: no model configured")
	}
	modelName := name
	if modelName == "" {
		modelName = p.cfg.DefaultModel
	}
	payload := map[string]any{
		"model":  modelName,
		"prompt": req.Prompt,
		"stream": true,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	} else if maxTok > 0 {
		payload["max_tokens"] = maxTok
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("free cloud: encode request failed: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(p.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("free cloud: build request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("free cloud: request cancelled")
		}
		return nil, fmt.Errorf("free cloud: provider unavailable (%w)", err)
	}
	ch := make(chan Token, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		for {
			var line struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := dec.Decode(&line); err != nil {
				if err == io.EOF {
					ch <- Token{Done: true}
					return
				}
				ch <- Token{Done: true, Error: fmt.Errorf("free cloud: stream decode error: %w", err)}
				return
			}
			if len(line.Choices) == 0 {
				continue
			}
			content := line.Choices[0].Delta.Content
			if content != "" {
				ch <- Token{Text: content}
			}
			if line.Choices[0].FinishReason != "" {
				ch <- Token{Done: true}
				return
			}
		}
	}()
	return ch, nil
}

// GenerateStructured generates and extracts the first JSON object.
func (p *FreeCloudProvider) GenerateStructured(ctx context.Context, req GenerateRequest) (map[string]any, string, error) {
	resp, err := p.Generate(ctx, req)
	if err != nil {
		return nil, "", err
	}
	raw := strings.TrimSpace(resp.Text)
	obj, extractErr := extractFirstJSONObject(raw)
	if extractErr != nil {
		return nil, raw, ErrNoJSON
	}
	return obj, raw, nil
}

// Metadata reports the provider identity.
func (p *FreeCloudProvider) Metadata() Metadata {
	p.mu.RLock()
	name := p.name
	md := p.md
	p.mu.RUnlock()
	result := Metadata{Provider: "free-cloud", Runtime: "free-cloud", ModelID: name}
	if md != nil {
		result.ModelID = md.ID
		result.Model = md.Name
	}
	return result
}

// IsReady reports whether the provider can generate.
func (p *FreeCloudProvider) IsReady(ctx context.Context) bool {
	if !p.cfg.Enabled {
		return false
	}
	if err := p.ensureAPIKey(); err != nil {
		return false
	}
	if strings.TrimSpace(p.cfg.BaseURL) == "" {
		return false
	}
	return true
}

// SetModelMetadata sets the model metadata for display.
func (p *FreeCloudProvider) SetModelMetadata(m *model.ModelMetadata) {
	p.mu.Lock()
	p.md = m
	p.mu.Unlock()
}
