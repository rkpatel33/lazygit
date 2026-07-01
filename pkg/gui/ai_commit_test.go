package gui

import (
	"testing"

	"github.com/jesseduffield/lazygit/pkg/config"
)

func TestResolveAICommitSettings(t *testing.T) {
	tests := []struct {
		name     string
		config   config.AIConfig
		provider string
		model    string
		envKey   string
	}{
		{
			name:     "defaults to anthropic",
			config:   config.AIConfig{},
			provider: aiProviderAnthropic,
			model:    defaultAnthropicModel,
			envKey:   "ANTHROPIC_API_KEY",
		},
		{
			name: "explicit anthropic model override",
			config: config.AIConfig{
				Provider: "anthropic",
				Model:    "claude-opus-4-20250514",
			},
			provider: aiProviderAnthropic,
			model:    "claude-opus-4-20250514",
			envKey:   "ANTHROPIC_API_KEY",
		},
		{
			name: "gemini defaults",
			config: config.AIConfig{
				Provider: "gemini",
			},
			provider: aiProviderGemini,
			model:    defaultGeminiModel,
			envKey:   "GEMINI_API_KEY",
		},
		{
			name: "gemini model override",
			config: config.AIConfig{
				Provider: "gemini",
				Model:    "gemini-2.5-flash-lite",
			},
			provider: aiProviderGemini,
			model:    "gemini-2.5-flash-lite",
			envKey:   "GEMINI_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := resolveAICommitSettings(tt.config)
			if settings.provider != tt.provider {
				t.Fatalf("provider = %q, want %q", settings.provider, tt.provider)
			}
			if settings.model != tt.model {
				t.Fatalf("model = %q, want %q", settings.model, tt.model)
			}
			if settings.envKey != tt.envKey {
				t.Fatalf("envKey = %q, want %q", settings.envKey, tt.envKey)
			}
		})
	}
}
