package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"
)

const (
	appDirName     = "agentslab"
	configFileName = "config.yaml"
	envConfigPath  = "AGENTLAB_CONFIG"
)

type Config struct {
	DefaultProvider string           `mapstructure:"default_provider" yaml:"default_provider"`
	Providers       []ProviderConfig `mapstructure:"providers"        yaml:"providers"`

	Provider string       `mapstructure:"provider" yaml:"provider"`
	Ollama   OllamaConfig `mapstructure:"ollama"   yaml:"ollama"`
	OpenAI   OpenAIConfig `mapstructure:"openai"   yaml:"openai"`
}

type OllamaConfig struct {
	Endpoint      string `mapstructure:"endpoint"       yaml:"endpoint"`
	Model         string `mapstructure:"model"          yaml:"model"`
	ContextWindow int    `mapstructure:"context_window" yaml:"context_window"`
	Think         string `mapstructure:"think"          yaml:"think"`
}

type OpenAIConfig struct {
	BaseURL string `mapstructure:"base_url" yaml:"base_url"`
	APIKey  string `mapstructure:"api_key"  yaml:"api_key"`
	Model   string `mapstructure:"model"    yaml:"model"`
}

type ProviderConfig struct {
	Name     string           `mapstructure:"name"     yaml:"name"`
	Type     string           `mapstructure:"type"     yaml:"type"`
	APIKey   string           `mapstructure:"api_key"  yaml:"api_key"`
	Settings ProviderSettings `mapstructure:"settings" yaml:"settings"`
}

type ProviderSettings struct {
	BaseURL       string `mapstructure:"base_url"       yaml:"base_url"`
	Endpoint      string `mapstructure:"endpoint"       yaml:"endpoint"`
	Model         string `mapstructure:"model"          yaml:"model"`
	ContextWindow int    `mapstructure:"context_window" yaml:"context_window"`
	Think         string `mapstructure:"think"          yaml:"think"`
}

func (p ProviderConfig) Model() string {
	return p.Settings.Model
}

func (p ProviderConfig) ContextWindow() int {
	switch p.Type {
	case "ollama":
		return p.Settings.ContextWindow
	default:
		return 0
	}
}

func (p ProviderConfig) Thinking() string {
	switch p.Type {
	case "ollama":
		return p.Settings.Think
	default:
		return ""
	}
}

func (c Config) ProviderByName(name string) (ProviderConfig, error) {
	if name == "" {
		name = c.DefaultProvider
	}
	for _, provider := range c.Providers {
		if provider.Name == name {
			return provider, nil
		}
	}
	return ProviderConfig{}, fmt.Errorf("provider %q is not configured", name)
}

func DefaultPath() (string, error) {
	if configured := os.Getenv(envConfigPath); configured != "" {
		return configured, nil
	}

	var base string
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	} else {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("find user config directory: %w", err)
		}
	}

	return filepath.Join(base, appDirName, configFileName), nil
}

func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	var cfg Config
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %s: %w", path, err)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("providers is required")
	}
	if c.DefaultProvider == "" {
		return fmt.Errorf("default_provider is required")
	}

	seen := make(map[string]struct{}, len(c.Providers))
	for _, provider := range c.Providers {
		if provider.Name == "" {
			return fmt.Errorf("provider name is required")
		}
		if _, ok := seen[provider.Name]; ok {
			return fmt.Errorf("duplicate provider %q", provider.Name)
		}
		seen[provider.Name] = struct{}{}
		if err := validateProvider(provider); err != nil {
			return err
		}
	}
	if _, ok := seen[c.DefaultProvider]; !ok {
		return fmt.Errorf("default_provider %q is not configured", c.DefaultProvider)
	}
	return nil
}

func validateProvider(provider ProviderConfig) error {
	switch provider.Type {
	case "ollama":
		if provider.Settings.Endpoint == "" {
			return fmt.Errorf("provider %q settings.endpoint is required", provider.Name)
		}
		if provider.Settings.Model == "" {
			return fmt.Errorf("provider %q settings.model is required", provider.Name)
		}
		if provider.Settings.ContextWindow < 0 {
			return fmt.Errorf("provider %q settings.context_window must not be negative", provider.Name)
		}
		switch provider.Settings.Think {
		case "", "false", "true", "low", "medium", "high":
		default:
			return fmt.Errorf(
				"provider %q settings.think must be one of false, true, low, medium, or high",
				provider.Name,
			)
		}
	case "openai":
		if provider.Settings.Model == "" {
			return fmt.Errorf("provider %q settings.model is required", provider.Name)
		}
		if provider.APIKey == "" && os.Getenv("OPENAI_API_KEY") == "" {
			return fmt.Errorf("provider %q api_key is required, or set OPENAI_API_KEY", provider.Name)
		}
	default:
		return fmt.Errorf("provider %q has unsupported type %q", provider.Name, provider.Type)
	}
	return nil
}

func (c *Config) normalize() {
	c.Provider = strings.TrimSpace(c.Provider)
	c.Ollama.Think = normalizeOllamaThink(c.Ollama.Think)
	c.OpenAI.BaseURL = strings.TrimRight(strings.TrimSpace(c.OpenAI.BaseURL), "/")
	c.OpenAI.APIKey = strings.TrimSpace(c.OpenAI.APIKey)
	c.OpenAI.Model = strings.TrimSpace(c.OpenAI.Model)
	if c.OpenAI.BaseURL == "" {
		c.OpenAI.BaseURL = "https://api.openai.com/v1"
	}
	c.normalizeLegacyProvider()
	c.DefaultProvider = strings.TrimSpace(c.DefaultProvider)
	for i := range c.Providers {
		c.Providers[i].Name = strings.TrimSpace(c.Providers[i].Name)
		c.Providers[i].Type = strings.TrimSpace(c.Providers[i].Type)
		c.Providers[i].APIKey = strings.TrimSpace(c.Providers[i].APIKey)
		c.Providers[i].Settings.BaseURL = strings.TrimRight(strings.TrimSpace(c.Providers[i].Settings.BaseURL), "/")
		c.Providers[i].Settings.Endpoint = strings.TrimRight(strings.TrimSpace(c.Providers[i].Settings.Endpoint), "/")
		c.Providers[i].Settings.Model = strings.TrimSpace(c.Providers[i].Settings.Model)
		c.Providers[i].Settings.Think = normalizeOllamaThink(c.Providers[i].Settings.Think)
		if c.Providers[i].Type == "openai" && c.Providers[i].Settings.BaseURL == "" {
			c.Providers[i].Settings.BaseURL = "https://api.openai.com/v1"
		}
	}
}

func (c *Config) normalizeLegacyProvider() {
	if len(c.Providers) > 0 || c.Provider == "" {
		return
	}
	c.DefaultProvider = c.Provider
	switch c.Provider {
	case "ollama":
		c.Providers = []ProviderConfig{{
			Name: c.Provider,
			Type: "ollama",
			Settings: ProviderSettings{
				Endpoint:      c.Ollama.Endpoint,
				Model:         c.Ollama.Model,
				ContextWindow: c.Ollama.ContextWindow,
				Think:         c.Ollama.Think,
			},
		}}
	case "openai":
		c.Providers = []ProviderConfig{{
			Name:   c.Provider,
			Type:   "openai",
			APIKey: c.OpenAI.APIKey,
			Settings: ProviderSettings{
				BaseURL: c.OpenAI.BaseURL,
				Model:   c.OpenAI.Model,
			},
		}}
	}
}

func normalizeOllamaThink(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1":
		return "true"
	case "0":
		return "false"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
