package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAMLConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
# Comments are allowed in YAML config.
default_provider: local
providers:
  - name: local
    type: ollama
    settings:
      endpoint: http://localhost:11434
      model: gemma4:26b
      context_window: 32768
      think: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DefaultProvider != "local" {
		t.Fatalf("DefaultProvider = %q, want local", cfg.DefaultProvider)
	}
	provider, err := cfg.ProviderByName("")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Name != "local" {
		t.Fatalf("Provider name = %q, want local", provider.Name)
	}
	if provider.Type != "ollama" {
		t.Fatalf("Provider type = %q, want ollama", provider.Type)
	}
	if provider.Settings.Endpoint != "http://localhost:11434" {
		t.Fatalf("Endpoint = %q", provider.Settings.Endpoint)
	}
	if provider.Model() != "gemma4:26b" {
		t.Fatalf("Model() = %q", provider.Model())
	}
	if provider.ContextWindow() != 32768 {
		t.Fatalf("ContextWindow() = %d, want 32768", provider.ContextWindow())
	}
	if provider.Thinking() != "true" {
		t.Fatalf("Thinking() = %q, want true", provider.Thinking())
	}
}

func TestLoadOpenAIConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
default_provider: api
providers:
  - name: api
    type: openai
    api_key: test-key
    settings:
      model: gpt-5.4
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	provider, err := cfg.ProviderByName("api")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Type != "openai" {
		t.Fatalf("Provider type = %q, want openai", provider.Type)
	}
	if provider.Settings.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL = %q", provider.Settings.BaseURL)
	}
	if provider.APIKey != "test-key" {
		t.Fatalf("APIKey = %q", provider.APIKey)
	}
	if provider.Model() != "gpt-5.4" {
		t.Fatalf("Model() = %q, want gpt-5.4", provider.Model())
	}
}

func TestOpenAIConfigAllowsEnvironmentAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
default_provider: api
providers:
  - name: api
    type: openai
    settings:
      model: gpt-5.4
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	provider, err := cfg.ProviderByName("")
	if err != nil {
		t.Fatal(err)
	}
	if provider.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty config value", provider.APIKey)
	}
}

func TestLoadMultipleProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
default_provider: local
providers:
  - name: api
    type: openai
    api_key: test-key
    settings:
      model: gpt-5.4
  - name: local
    type: ollama
    settings:
      endpoint: http://localhost:11434
      model: qwen3-coder
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defaultProvider, err := cfg.ProviderByName("")
	if err != nil {
		t.Fatal(err)
	}
	if defaultProvider.Name != "local" {
		t.Fatalf("default provider name = %q, want local", defaultProvider.Name)
	}
	apiProvider, err := cfg.ProviderByName("api")
	if err != nil {
		t.Fatal(err)
	}
	if apiProvider.Type != "openai" {
		t.Fatalf("api provider type = %q, want openai", apiProvider.Type)
	}
}

func TestLoadLegacyOllamaConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
provider: ollama
ollama:
  endpoint: http://localhost:11434
  model: gemma4:26b
  context_window: 32768
  think: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DefaultProvider != "ollama" {
		t.Fatalf("DefaultProvider = %q, want ollama", cfg.DefaultProvider)
	}
	provider, err := cfg.ProviderByName("")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Type != "ollama" || provider.Model() != "gemma4:26b" {
		t.Fatalf("provider = %#v", provider)
	}
}

func TestDefaultPathUsesEnvironmentOverride(t *testing.T) {
	t.Setenv(envConfigPath, "/tmp/agentlab.yaml")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if path != "/tmp/agentlab.yaml" {
		t.Fatalf("DefaultPath() = %q, want /tmp/agentlab.yaml", path)
	}
}
