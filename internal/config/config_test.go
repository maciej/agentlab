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

	if cfg.Provider != "ollama" {
		t.Fatalf("Provider = %q, want ollama", cfg.Provider)
	}
	if cfg.Ollama.Endpoint != "http://localhost:11434" {
		t.Fatalf("Ollama.Endpoint = %q", cfg.Ollama.Endpoint)
	}
	if cfg.Ollama.Model != "gemma4:26b" {
		t.Fatalf("Ollama.Model = %q", cfg.Ollama.Model)
	}
	if cfg.Ollama.ContextWindow != 32768 {
		t.Fatalf("Ollama.ContextWindow = %d, want 32768", cfg.Ollama.ContextWindow)
	}
	if cfg.Ollama.Think != "true" {
		t.Fatalf("Ollama.Think = %q, want true", cfg.Ollama.Think)
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
