package provider

import (
	"fmt"

	"github.com/edmundo/edcode/internal/config"
)

func ProvidersFromConfig(cfg *config.Config) (*Registry, error) {
	reg := NewRegistry()
	for name, pc := range cfg.Providers {
		var p Provider
		models := toModelConfig(pc.Models)
		switch name {
		case "openai", "openrouter", "ollama", "openai-compatible":
			p = NewOpenAIProvider(name, pc.APIKey, pc.BaseURL, pc.Default, models)
		case "anthropic":
			p = NewAnthropicProvider(name, pc.APIKey, pc.BaseURL, pc.Default, models)
		default:
			baseURL := pc.BaseURL
			if baseURL == "" {
				baseURL = fmt.Sprintf("https://api.%s.com/v1", name)
			}
			p = NewOpenAIProvider(name, pc.APIKey, baseURL, pc.Default, models)
		}
		reg.Register(p)
	}
	return reg, nil
}

func toModelConfig(cfgs []config.ModelConfig) []ModelConfig {
	out := make([]ModelConfig, len(cfgs))
	for i, c := range cfgs {
		out[i] = ModelConfig{
			ID: c.ID, Name: c.Name,
			ContextLen: c.ContextLen, OutputLen: c.OutputLen,
		}
	}
	return out
}
