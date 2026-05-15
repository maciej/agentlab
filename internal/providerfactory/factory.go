package providerfactory

import (
	"fmt"

	"agentlab/internal/config"
	"agentlab/internal/ollama"
	"agentlab/internal/openai"
	"agentlab/internal/provider"
)

func NewClient(providerConfig config.ProviderConfig) (provider.Client, error) {
	switch providerConfig.Type {
	case "ollama":
		return ollama.NewClientWithOptions(providerConfig.Settings.Endpoint, ollama.ClientOptions{
			Think: ollama.ThinkMode(providerConfig.Settings.Think),
		}), nil
	case "openai":
		return openai.NewClient(openai.ClientOptions{
			BaseURL: providerConfig.Settings.BaseURL,
			APIKey:  providerConfig.APIKey,
		}), nil
	default:
		return nil, fmt.Errorf("provider %q has unsupported type %q", providerConfig.Name, providerConfig.Type)
	}
}
