package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"
)

const (
	appDirName     = "agentslab"
	configFileName = "config.yaml"
	envConfigPath  = "AGENTLAB_CONFIG"
)

type Config struct {
	Provider string       `mapstructure:"provider" yaml:"provider"`
	Ollama   OllamaConfig `mapstructure:"ollama"   yaml:"ollama"`
}

type OllamaConfig struct {
	Endpoint      string `mapstructure:"endpoint"       yaml:"endpoint"`
	Model         string `mapstructure:"model"          yaml:"model"`
	ContextWindow int    `mapstructure:"context_window" yaml:"context_window"`
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
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %s: %w", path, err)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if c.Provider != "ollama" {
		return fmt.Errorf("unsupported provider %q", c.Provider)
	}
	if c.Ollama.Endpoint == "" {
		return fmt.Errorf("ollama.endpoint is required")
	}
	if c.Ollama.Model == "" {
		return fmt.Errorf("ollama.model is required")
	}
	if c.Ollama.ContextWindow < 0 {
		return fmt.Errorf("ollama.context_window must not be negative")
	}
	return nil
}
