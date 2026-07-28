//go:build smoke

package gui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const smokeSampleDiff = `diff --git a/README.md b/README.md
index 1234567..89abcde 100644
--- a/README.md
+++ b/README.md
@@ -1,3 +1,4 @@
 # lazygit
+Add smoke test for AI commit providers.
`

func TestSmokeAICommitProviders(t *testing.T) {
	prompt := "Write a one-line git commit message for this diff. Reply with only the commit message, no quotes or markdown."
	fullPrompt := fmt.Sprintf("%s\n\n```diff\n%s\n```", prompt, smokeSampleDiff)

	timeout := 30
	if v := os.Getenv("AI_SMOKE_TIMEOUT"); v != "" {
		if parsed, err := time.ParseDuration(v + "s"); err == nil {
			timeout = int(parsed.Seconds())
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	gui := &Gui{}
	ran := false

	for _, tc := range []struct {
		name   string
		envKey string
		model  string
		call   func(string) (string, error)
	}{
		{
			name:   "anthropic",
			envKey: "ANTHROPIC_API_KEY",
			model:  defaultAnthropicModel,
			call: func(apiKey string) (string, error) {
				return gui.callAnthropicAPI(ctx, defaultAnthropicModel, apiKey, fullPrompt)
			},
		},
		{
			name:   "gemini",
			envKey: "GEMINI_API_KEY",
			model:  defaultGeminiModel,
			call: func(apiKey string) (string, error) {
				return gui.callGeminiAPI(ctx, defaultGeminiModel, apiKey, fullPrompt)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiKey, err := loadEnvValue(tc.envKey)
			if err != nil || apiKey == "" {
				t.Skipf("%s not set in ~/.config/lazygit/.env", tc.envKey)
			}

			ran = true
			t.Logf("calling %s (%s)...", tc.name, tc.model)
			message, err := tc.call(apiKey)
			if err != nil {
				t.Fatalf("%s API call failed: %v", tc.name, err)
			}

			message = cleanMarkdownFormatting(message)
			if strings.TrimSpace(message) == "" {
				t.Fatalf("%s returned empty message", tc.name)
			}

			t.Logf("%s commit message: %q", tc.name, message)
		})
	}

	if !ran {
		t.Fatal("no API keys found in ~/.config/lazygit/.env (need ANTHROPIC_API_KEY and/or GEMINI_API_KEY)")
	}
}
