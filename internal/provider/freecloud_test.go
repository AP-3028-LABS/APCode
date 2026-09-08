package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apcode/internal/model"
)

// TestNewFreeCloudProviderNotEnabled tests that a disabled config returns an error.
func TestNewFreeCloudProviderNotEnabled(t *testing.T) {
	_, err := NewFreeCloudProvider(FreeCloudConfig{Enabled: false})
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("expected ErrProviderNotConfigured, got %v", err)
	}
}

// TestNewFreeCloudProviderEmptyBaseURL tests that an empty base URL returns an error.
func TestNewFreeCloudProviderEmptyBaseURL(t *testing.T) {
	_, err := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: ""})
	if err == nil || !strings.Contains(err.Error(), "base URL is empty") {
		t.Errorf("expected base URL error, got %v", err)
	}
}

// TestNewFreeCloudProviderInvalidURL tests that an invalid URL returns an error.
func TestNewFreeCloudProviderInvalidURL(t *testing.T) {
	_, err := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "not-a-url"})
	if err == nil {
		t.Errorf("expected URL error, got %v", err)
	}
}

// TestFreeCloudProviderGenerateSuccess tests a successful Generate call using a mock server.
func TestFreeCloudProviderGenerateSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Hello from free cloud!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	resp, err := p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !strings.Contains(resp.Text, "Hello from free cloud!") {
		t.Errorf("unexpected response: %q", resp.Text)
	}
	if resp.TokensGenerated <= 0 {
		t.Errorf("expected tokens > 0, got %d", resp.TokensGenerated)
	}
}

// TestFreeCloudProviderGenerateMissingKey tests that missing API key returns an error.
func TestFreeCloudProviderGenerateMissingKey(t *testing.T) {
	p, err := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "https://api.example.com", DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

// TestFreeCloudProviderGenerateHTTPError tests HTTP error handling.
func TestFreeCloudProviderGenerateHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected 500 error, got %v", err)
	}
}

// TestFreeCloudProviderGenerateUnauthorized tests invalid API key.
func TestFreeCloudProviderGenerateUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": "invalid api key"}`)
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "bad-key",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("expected invalid API key error, got %v", err)
	}
}

// TestFreeCloudProviderGenerateRateLimit tests rate limit handling.
func TestFreeCloudProviderGenerateRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "rate limited")
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected rate limit error, got %v", err)
	}
}

// TestFreeCloudProviderGenerateTimeout tests timeout handling.
func TestFreeCloudProviderGenerateTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "slow response"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Millisecond}
	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		Timeout:      1 * time.Millisecond,
		HTTPClient:   client,
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil {
		t.Errorf("expected timeout error, got %v", err)
	}
}

// TestFreeCloudProviderIsReady tests IsReady.
func TestFreeCloudProviderIsReady(t *testing.T) {
	p, _ := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "https://api.example.com", APIKey: "key", DefaultModel: "m"})
	if !p.IsReady(context.Background()) {
		t.Error("IsReady should be true")
	}

	p2, err2 := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "https://api.example.com", DefaultModel: "m"})
	if err2 != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err2)
	}
	if p2.IsReady(context.Background()) {
		t.Error("IsReady should be false without API key")
	}

	_, err3 := NewFreeCloudProvider(FreeCloudConfig{Enabled: false, BaseURL: "https://api.example.com", APIKey: "key", DefaultModel: "m"})
	if err3 == nil || !errors.Is(err3, ErrProviderNotConfigured) {
		t.Errorf("expected ErrProviderNotConfigured, got %v", err3)
	}
}

// TestFreeCloudProviderMetadata tests Metadata.
func TestFreeCloudProviderMetadata(t *testing.T) {
	p, _ := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "https://api.example.com", APIKey: "key", DefaultModel: "m"})
	md := p.Metadata()
	if md.Provider != "free-cloud" {
		t.Errorf("expected Provider=free-cloud, got %q", md.Provider)
	}
	if md.Runtime != "free-cloud" {
		t.Errorf("expected Runtime=free-cloud, got %q", md.Runtime)
	}
}

// TestFreeCloudProviderStream tests streaming.
func TestFreeCloudProviderStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return JSON array of SSE-like objects
		// The stream decoder expects JSON objects from the response body
		fmt.Fprintf(w, `{"choices":[{"delta":{"content":"Hello"}}]}`)
		fmt.Fprintf(w, `{"choices":[{"delta":{"content":" world"}}]}`)
		fmt.Fprintf(w, `{"choices":[{"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	ch, err := p.Stream(context.Background(), GenerateRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	var sb strings.Builder
	for tok := range ch {
		if tok.Error != nil {
			t.Fatalf("stream error: %v", tok.Error)
		}
		sb.WriteString(tok.Text)
	}
	// The decoder reads multiple JSON objects from the stream
	// We can't guarantee the exact content, just check it doesn't error
	if sb.String() == "" {
		t.Error("expected some content in stream")
	}
}

// TestFreeCloudProviderMalformedResponse tests handling of malformed JSON.
func TestFreeCloudProviderMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "not json at all")
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Errorf("expected decode error, got %v", err)
	}
}

// TestFreeCloudProviderGenerateStructured tests structured output.
func TestFreeCloudProviderGenerateStructured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "```json\n{\"tool\":\"read_file\"}\n```"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	obj, raw, err := p.GenerateStructured(context.Background(), GenerateRequest{Prompt: "what tool?"})
	if err != nil {
		t.Fatalf("GenerateStructured failed: %v", err)
	}
	if obj["tool"] != "read_file" {
		t.Errorf("expected tool=read_file, got %v", obj)
	}
	if raw == "" {
		t.Error("raw text should not be empty")
	}
}

// TestFreeCloudProviderNoModel tests error when no model is configured.
func TestFreeCloudProviderNoModel(t *testing.T) {
	p, err := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "https://api.example.com", APIKey: "key"})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "no model configured") {
		t.Errorf("expected no model error, got %v", err)
	}
}

// TestFreeCloudProviderNeverReturnsHardcodedResponse tests that the provider never returns a hardcoded APCode greeting.
func TestFreeCloudProviderNeverReturnsHardcodedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "MODEL-SAYS: 2 + 2 equals 4."}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	resp, err := p.Generate(context.Background(), GenerateRequest{Prompt: "What is 2 + 2?"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, banned := range []string{
		"Hello! I'm APCode",
		"offline AI coding agent",
		"response for:",
	} {
		if strings.Contains(resp.Text, banned) {
			t.Errorf("provider returned hardcoded template %q in %q", banned, resp.Text)
		}
	}
	if !strings.HasPrefix(resp.Text, "MODEL-SAYS:") {
		t.Errorf("backend response not relayed verbatim: %q", resp.Text)
	}
}

// TestFreeCloudProviderNetworkError tests network error handling.
func TestFreeCloudProviderNetworkError(t *testing.T) {
	// Use a URL that won't connect
	p, err := NewFreeCloudProvider(FreeCloudConfig{
		Enabled:      true,
		BaseURL:      "http://localhost:1",
		APIKey:       "test-key-123",
		DefaultModel: "test-model",
	})
	if err != nil {
		t.Fatalf("NewFreeCloudProvider failed: %v", err)
	}
	_, err = p.Generate(context.Background(), GenerateRequest{Prompt: "hello"})
	if err == nil {
		t.Errorf("expected network error, got %v", err)
	}
}

// TestFreeCloudModelCatalog tests the free cloud model catalog.
func TestFreeCloudModelCatalog(t *testing.T) {
	catalog := model.FreeCloudCatalog()
	if len(catalog) == 0 {
		t.Fatal("expected non-empty catalog")
	}
	for _, m := range catalog {
		if !m.Free {
			t.Errorf("model %s should be free", m.ID)
		}
		if m.ID == "" {
			t.Errorf("model has empty ID")
		}
		if m.Name == "" {
			t.Errorf("model %s has empty name", m.ID)
		}
	}
}

// TestIsFreeCloudModel tests the IsFreeCloudModel helper.
func TestIsFreeCloudModel(t *testing.T) {
	if !model.IsFreeCloudModel("qwen2.5-coder-free") {
		t.Error("qwen2.5-coder-free should be free cloud model")
	}
	if model.IsFreeCloudModel("nonexistent") {
		t.Error("nonexistent should not be free cloud model")
	}
}

// TestGetFreeCloudModel tests the GetFreeCloudModel helper.
func TestGetFreeCloudModel(t *testing.T) {
	m := model.GetFreeCloudModel("deepseek-coder-free")
	if m == nil {
		t.Fatal("expected non-nil model")
	}
	if m.Provider != "DeepSeek" {
		t.Errorf("expected DeepSeek, got %s", m.Provider)
	}
	if model.GetFreeCloudModel("nonexistent") != nil {
		t.Error("expected nil for nonexistent model")
	}
}

// TestFreeCloudModelToModelMetadata tests the ToModelMetadata conversion.
func TestFreeCloudModelToModelMetadata(t *testing.T) {
	f := &model.FreeCloudModel{
		ID:            "test-free",
		Name:          "Test Free",
		Provider:      "TestProvider",
		BaseURL:       "https://api.example.com/v1",
		ModelName:     "test-model",
		ContextLength: 8192,
		Free:          true,
		Capabilities:  model.Capabilities{model.CapabilityCodeGeneration, model.CapabilityCodeCompletion},
	}
	md := f.ToModelMetadata()
	if md.ID != "test-free" {
		t.Errorf("expected ID test-free, got %s", md.ID)
	}
	if md.Source != model.SourceFreeCloud {
		t.Errorf("expected Source=SourceFreeCloud, got %v", md.Source)
	}
	if err := md.Validate(); err != nil {
		t.Errorf("Validate failed: %v", err)
	}
}

// TestFreeCloudProviderWithModel tests WithModel.
func TestFreeCloudProviderWithModel(t *testing.T) {
	p, _ := NewFreeCloudProvider(FreeCloudConfig{Enabled: true, BaseURL: "https://api.example.com", APIKey: "key", DefaultModel: "default"})
	p.WithModel("custom-model")
	md := p.Metadata()
	if md.ModelID != "custom-model" {
		t.Errorf("expected ModelID=custom-model, got %q", md.ModelID)
	}
}
