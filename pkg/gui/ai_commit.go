package gui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jesseduffield/lazygit/pkg/config"
)

const (
	aiProviderAnthropic = "anthropic"
	aiProviderGemini    = "gemini"

	defaultAnthropicModel = "claude-sonnet-4-5-20250929"
	defaultGeminiModel    = "gemini-2.5-flash"

	defaultAIMaxTokens = 500
)

type aiCommitSettings struct {
	provider string
	model    string
	envKey   string
}

// Anthropic API types for making direct API calls
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
	Error   *anthropicError    `json:"error,omitempty"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Gemini API types for making direct API calls
type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// Default prompt following Claude Code's commit message style
const defaultCommitPrompt = `Generate a git commit message by analyzing the diff below.

Guidelines:
1. Summarize the nature of changes:
   - "add" = wholly new feature or file
   - "update" = enhancement to existing feature
   - "fix" = bug fix
   - "refactor" = code restructuring without behavior change
   - "test" = adding or updating tests
   - "docs" = documentation changes
   - "chore" = maintenance tasks, deps, config

2. Focus on WHY rather than WHAT:
   - Don't just describe the code changes
   - Explain the purpose and motivation
   - What problem does this solve?

3. Keep it concise:
   - 1-2 sentences for the main message
   - Add a body paragraph if changes need explanation
   - Be specific and meaningful

4. Required format:
   Your commit message here.

   🤖 Generated with lazygit AI

5. Accuracy is critical:
   - Use "add" only for completely new features
   - Use "update" for enhancements to existing code
   - Use "fix" only for actual bug fixes
   - Be precise about what changed and why

Analyze the git diff below and generate an appropriate commit message:`

func resolveAICommitSettings(aiConfig config.AIConfig) aiCommitSettings {
	provider := strings.ToLower(strings.TrimSpace(aiConfig.Provider))
	if provider == "" {
		provider = aiProviderAnthropic
	}

	model := strings.TrimSpace(aiConfig.Model)

	switch provider {
	case aiProviderGemini:
		if model == "" {
			model = defaultGeminiModel
		}
		return aiCommitSettings{
			provider: aiProviderGemini,
			model:    model,
			envKey:   "GEMINI_API_KEY",
		}
	default:
		if model == "" {
			model = defaultAnthropicModel
		}
		return aiCommitSettings{
			provider: aiProviderAnthropic,
			model:    model,
			envKey:   "ANTHROPIC_API_KEY",
		}
	}
}

// GenerateAICommitMessage calls the configured LLM provider to create
// a commit message based on the staged changes.
//
// Returns the generated message, or an empty string if:
// - AI is disabled in config
// - The API call fails and fallbackOnError is true
// - The API call times out
func (gui *Gui) GenerateAICommitMessage() string {
	gui.c.LogAction("Generate AI Commit Message")
	aiConfig := gui.Config.GetUserConfig().Git.AI

	if !aiConfig.Enabled {
		gui.c.LogCommand("AI commit generation is disabled in config", false)
		return ""
	}

	settings := resolveAICommitSettings(aiConfig)
	if aiConfig.Provider != "" && strings.ToLower(strings.TrimSpace(aiConfig.Provider)) != settings.provider {
		gui.c.LogCommand(fmt.Sprintf("Unknown AI provider %q, using anthropic", aiConfig.Provider), false)
	}

	apiKey, err := loadEnvValue(settings.envKey)
	if err != nil || apiKey == "" {
		gui.c.LogCommand(fmt.Sprintf("Error: %s not found in .env file: %v", settings.envKey, err), false)
		gui.c.Toast(fmt.Sprintf("AI commit failed: %v", err))
		return ""
	}

	gui.c.LogCommand("Getting staged diff...", false)
	gui.c.Toast("Generating AI commit message...")
	diff, err := gui.getStagedDiff()
	if err != nil {
		gui.c.LogCommand(fmt.Sprintf("Error: Failed to get staged diff: %v", err), false)
		if aiConfig.FallbackOnError {
			return ""
		}
		return ""
	}

	if diff == "" {
		gui.c.LogCommand("No staged changes found", false)
		return ""
	}

	prompt := aiConfig.Prompt
	if prompt == "" {
		prompt = defaultCommitPrompt
		gui.c.LogCommand("Using default commit message prompt", false)
	} else {
		gui.c.LogCommand("Using custom prompt from config", false)
	}

	fullPrompt := fmt.Sprintf("%s\n\n```diff\n%s\n```", prompt, diff)

	timeout := aiConfig.Timeout
	if timeout == 0 {
		timeout = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	gui.c.LogCommand(fmt.Sprintf("Calling %s API (%s, timeout: %ds)...", settings.provider, settings.model, timeout), false)
	startTime := time.Now()
	message, err := gui.callCommitMessageAPI(ctx, settings, apiKey, fullPrompt)
	elapsed := time.Since(startTime)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			gui.c.LogCommand("Error: AI commit generation timed out", false)
		} else {
			gui.c.LogCommand(fmt.Sprintf("Error: AI commit generation failed: %v", err), false)
		}
		if aiConfig.FallbackOnError {
			return ""
		}
		return ""
	}

	gui.c.LogCommand(fmt.Sprintf("AI generated commit message (%d characters) (%dms)", len(message), elapsed.Milliseconds()), false)

	message = cleanMarkdownFormatting(message)

	gui.c.LogCommand("Successfully generated and cleaned commit message", false)
	gui.c.Toast("AI commit message generated successfully")

	return message
}

func (gui *Gui) callCommitMessageAPI(ctx context.Context, settings aiCommitSettings, apiKey string, prompt string) (string, error) {
	switch settings.provider {
	case aiProviderGemini:
		return gui.callGeminiAPI(ctx, settings.model, apiKey, prompt)
	default:
		return gui.callAnthropicAPI(ctx, settings.model, apiKey, prompt)
	}
}

// getStagedDiff retrieves the diff of staged changes
func (gui *Gui) getStagedDiff() (string, error) {
	cmd := exec.Command("git", "diff", "--cached")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	return string(output), nil
}

// callAnthropicAPI makes a direct HTTP call to the Anthropic API
func (gui *Gui) callAnthropicAPI(ctx context.Context, model string, apiKey string, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: defaultAIMaxTokens,
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("API returned no content")
	}

	return apiResp.Content[0].Text, nil
}

// callGeminiAPI makes a direct HTTP call to the Google Gemini API
func (gui *Gui) callGeminiAPI(ctx context.Context, model string, apiKey string, prompt string) (string, error) {
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: defaultAIMaxTokens,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp geminiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("API returned no content")
	}

	return apiResp.Candidates[0].Content.Parts[0].Text, nil
}

// loadEnvValue reads a key from the .env file in the lazygit config directory.
func loadEnvValue(key string) (string, error) {
	envPath := filepath.Join(config.ConfigDir(), ".env")
	f, err := os.Open(envPath)
	if err != nil {
		return "", fmt.Errorf("cannot open %s: %w", envPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	prefix := key + "="
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):]), nil
		}
	}
	return "", fmt.Errorf("%s not found in %s", key, envPath)
}

// cleanMarkdownFormatting removes markdown code fences and backticks that
// LLMs sometimes add to responses
func cleanMarkdownFormatting(message string) string {
	re1 := regexp.MustCompile(`(?m)^` + "```" + `\w*\n?`)
	message = re1.ReplaceAllString(message, "")

	re2 := regexp.MustCompile(`(?m)\n?` + "```" + `$`)
	message = re2.ReplaceAllString(message, "")

	re3 := regexp.MustCompile(`^` + "```" + `\w*`)
	message = re3.ReplaceAllString(message, "")

	re4 := regexp.MustCompile("```" + `$`)
	message = re4.ReplaceAllString(message, "")

	message = strings.Trim(message, "`")
	message = strings.TrimSpace(message)

	re5 := regexp.MustCompile(`(?s)\n\*\*.*?\*\*:.*`)
	message = re5.ReplaceAllString(message, "")

	return strings.TrimSpace(message)
}
