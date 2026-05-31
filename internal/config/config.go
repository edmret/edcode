package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Providers map[string]ProviderConfig `yaml:"providers"`
	Agents    map[string]AgentConfig    `yaml:"agents"`
	Pipeline  PipelineConfig            `yaml:"pipeline"`
	MCP       map[string]MCPConfig      `yaml:"mcp"`
	Session   SessionConfig             `yaml:"session"`
}

type ProviderConfig struct {
	APIKey  string            `yaml:"api_key"`
	BaseURL string            `yaml:"base_url"`
	Models  []ModelConfig     `yaml:"models"`
	Default string            `yaml:"default_model"`
	Options map[string]any    `yaml:"options"`
	Env     map[string]string `yaml:"env"`
}

type ModelConfig struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	ContextLen int    `yaml:"context_length"`
	OutputLen  int    `yaml:"output_length"`
}

type AgentConfig struct {
	Description string            `yaml:"description"`
	Model       string            `yaml:"model"`
	Prompt      string            `yaml:"prompt"`
	PromptFile  string            `yaml:"prompt_file"`
	MaxSteps    int               `yaml:"max_steps"`
	Temperature float64           `yaml:"temperature"`
	TopP        float64           `yaml:"top_p"`
	Tools       []string          `yaml:"tools"`
	Permission  PermissionConfig  `yaml:"permission"`
	Hidden      bool              `yaml:"hidden"`
	Color       string            `yaml:"color"`
}

type PermissionConfig struct {
	Read    string   `yaml:"read"`
	Write   string   `yaml:"write"`
	Edit    string   `yaml:"edit"`
	Bash    string   `yaml:"bash"`
	Glob    string   `yaml:"glob"`
	Grep    string   `yaml:"grep"`
	Web     string   `yaml:"web"`
	Custom  map[string]string `yaml:"custom"`
}

type PipelineConfig struct {
	Steps []StepConfig `yaml:"steps"`
}

type StepConfig struct {
	Name     string `yaml:"name"`
	Agent    string `yaml:"agent"`
	Prompt   string `yaml:"prompt"`
	MaxSteps int    `yaml:"max_steps"`
	Model    string `yaml:"model"`
}

type MCPConfig struct {
	Type        string            `yaml:"type"`
	Command     string            `yaml:"command"`
	Args        []string          `yaml:"args"`
	URL         string            `yaml:"url"`
	Enabled     bool              `yaml:"enabled"`
	Environment map[string]string `yaml:"environment"`
	Headers     map[string]string `yaml:"headers"`
}

type SessionConfig struct {
	MaxTokens   int  `yaml:"max_tokens"`
	AutoCompact bool `yaml:"auto_compact"`
	MaxMessages int  `yaml:"max_messages"`
}

func Default() *Config {
	return &Config{
		Providers: map[string]ProviderConfig{
			"openai": {
				BaseURL: "https://api.openai.com/v1",
				Default: "gpt-4o",
				Models: []ModelConfig{
					{ID: "gpt-4o", Name: "GPT-4o", ContextLen: 128000, OutputLen: 16384},
					{ID: "gpt-4o-mini", Name: "GPT-4o Mini", ContextLen: 128000, OutputLen: 16384},
				},
				Env: map[string]string{"OPENAI_API_KEY": "api_key"},
			},
			"anthropic": {
				BaseURL: "https://api.anthropic.com/v1",
				Default: "claude-sonnet-4-20250514",
				Models: []ModelConfig{
					{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", ContextLen: 200000, OutputLen: 8192},
					{ID: "claude-haiku-3-5-20241022", Name: "Claude Haiku 3.5", ContextLen: 200000, OutputLen: 8192},
				},
				Env: map[string]string{"ANTHROPIC_API_KEY": "api_key"},
			},
			"openrouter": {
				BaseURL: "https://openrouter.ai/api/v1",
				Default: "anthropic/claude-sonnet-4-20250514",
				Env:     map[string]string{"OPENROUTER_API_KEY": "api_key"},
			},
			"ollama": {
				BaseURL: "http://localhost:11434/v1",
				Default: "llama3",
				Env:     map[string]string{"OLLAMA_API_KEY": "api_key"},
			},
		},
		Agents: map[string]AgentConfig{
			"default": {
				Description: "Default agent with full tool access",
				Model:       "openai/gpt-4o",
				MaxSteps:    25,
				Temperature: 0.7,
				Tools:       []string{"read", "write", "edit", "bash", "glob", "grep", "web"},
				Permission: PermissionConfig{
					Read: "allow", Write: "allow", Edit: "allow",
					Bash: "ask", Glob: "allow", Grep: "allow", Web: "allow",
				},
			},
			"planner": {
				Description: "Analyzes tasks and produces a plan",
				Model:       "openai/gpt-4o",
				MaxSteps:    10,
				Temperature: 0.3,
				Tools:       []string{"read", "glob", "grep"},
				Permission: PermissionConfig{
					Read: "allow", Write: "deny", Edit: "deny",
					Bash: "deny", Glob: "allow", Grep: "allow", Web: "allow",
				},
			},
			"coder": {
				Description: "Writes and edits code",
				Model:       "openai/gpt-4o",
				MaxSteps:    30,
				Temperature: 0.2,
				Tools:       []string{"read", "write", "edit", "bash", "glob", "grep"},
				Permission: PermissionConfig{
					Read: "allow", Write: "allow", Edit: "allow",
					Bash: "allow", Glob: "allow", Grep: "allow", Web: "deny",
				},
			},
			"reviewer": {
				Description: "Reviews changes for correctness and quality",
				Model:       "openai/gpt-4o",
				MaxSteps:    10,
				Temperature: 0.1,
				Tools:       []string{"read", "glob", "grep", "bash"},
				Permission: PermissionConfig{
					Read: "allow", Write: "deny", Edit: "deny",
					Bash: "allow", Glob: "allow", Grep: "allow", Web: "deny",
				},
			},
		},
		Pipeline: PipelineConfig{
			Steps: []StepConfig{
				{Name: "plan", Agent: "planner", Prompt: "Analyze the request and create a detailed plan."},
				{Name: "implement", Agent: "coder", Prompt: "Implement the plan, writing all necessary code."},
				{Name: "review", Agent: "reviewer", Prompt: "Review the implementation for correctness and quality."},
			},
		},
		MCP: map[string]MCPConfig{},
		Session: SessionConfig{
			MaxTokens:   200000,
			AutoCompact: true,
			MaxMessages: 50,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	resolveProviderEnv(cfg)
	return cfg, nil
}

func resolveProviderEnv(cfg *Config) {
	for name, p := range cfg.Providers {
		for envVar, field := range p.Env {
			if val, ok := os.LookupEnv(envVar); ok {
				switch field {
				case "api_key":
					if p.APIKey == "" {
						p.APIKey = val
					}
				}
			}
		}
		if p.APIKey == "" {
			if val, ok := os.LookupEnv(fmt.Sprintf("%s_API_KEY", name)); ok {
				p.APIKey = val
			}
		}
		cfg.Providers[name] = p
	}
}

func DefaultPath() string {
	if p := os.Getenv("EDCODE_CONFIG"); p != "" {
		return p
	}
	cwd, _ := os.Getwd()
	for _, name := range []string{"edcode.yaml", "edcode.yml", ".edcode.yaml"} {
		p := filepath.Join(cwd, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(cwd, "edcode.yaml")
}
