package providerfactory

import (
	"testing"

	"agentlab/internal/config"
)

func TestNewClientSupportsOllama(t *testing.T) {
	client, err := NewClient(config.ProviderConfig{
		Name: "local",
		Type: "ollama",
		Settings: config.ProviderSettings{
			Endpoint: "http://localhost:11434",
			Think:    "true",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestNewClientSupportsOpenAI(t *testing.T) {
	client, err := NewClient(config.ProviderConfig{
		Name:   "api",
		Type:   "openai",
		APIKey: "test-key",
		Settings: config.ProviderSettings{
			BaseURL: "https://api.openai.com/v1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestNewClientRejectsUnsupportedProvider(t *testing.T) {
	_, err := NewClient(config.ProviderConfig{Name: "claude", Type: "anthropic"})
	if err == nil {
		t.Fatal("NewClient() error is nil, want unsupported provider error")
	}
}
