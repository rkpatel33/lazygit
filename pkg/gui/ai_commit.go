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

// generateAICommitMessage calls the Anthropic API directly to create
// a commit message based on the staged changes.
//
// Returns the generated message, or an empty string if:
// - AI is disabled in config
// - The API call fails and fallbackOnError is true
// - The API call times out
func (gui *Gui) GenerateAICommitMessage() string {
	gui.c.LogAction("Generate AI Commit Message")
	aiConfig := gui.Config.GetUserConfig().Git.AI

	// Check if AI commits are enabled
	if !aiConfig.Enabled {
		gui.c.LogCommand("AI commit generation is disabled in config", false)
		return ""
	}

	// Get API key from .env file in the repo root
	apiKey, err := loadEnvValue("ANTHROPIC_API_KEY")
	if err != nil || apiKey == "" {
		gui.c.LogCommand(fmt.Sprintf("Error: ANTHROPIC_API_KEY not found in .env file: %v", err), false)
		gui.c.Toast(fmt.Sprintf("AI commit failed: %v", err))
		return ""
	}

	// Get the staged diff
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

	// Get prompt from config or use default
	prompt := aiConfig.Prompt
	if prompt == "" {
		prompt = defaultCommitPrompt
		gui.c.LogCommand("Using default commit message prompt", false)
	} else {
		gui.c.LogCommand("Using custom prompt from config", false)
	}

	// Build the full prompt with diff
	fullPrompt := fmt.Sprintf("%s\n\n```diff\n%s\n```", prompt, diff)

	// Set timeout (default to 10 seconds if not configured)
	timeout := aiConfig.Timeout
	if timeout == 0 {
		timeout = 10
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Make API call
	gui.c.LogCommand(fmt.Sprintf("Calling Anthropic API (timeout: %ds)...", timeout), false)
	startTime := time.Now()
	message, err := gui.callAnthropicAPI(ctx, apiKey, fullPrompt)
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

	// Clean markdown formatting (backticks, code fences)
	message = cleanMarkdownFormatting(message)

	gui.c.LogCommand("Successfully generated and cleaned commit message", false)
	gui.c.Toast("AI commit message generated successfully")

	return message
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
func (gui *Gui) callAnthropicAPI(ctx context.Context, apiKey string, prompt string) (string, error) {
	// Build request
	reqBody := anthropicRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 500,
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

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
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
// Claude sometimes adds to responses
func cleanMarkdownFormatting(message string) string {
	// Remove markdown code fences (```language or just ```)
	re1 := regexp.MustCompile(`(?m)^` + "```" + `\w*\n?`)
	message = re1.ReplaceAllString(message, "")

	re2 := regexp.MustCompile(`(?m)\n?` + "```" + `$`)
	message = re2.ReplaceAllString(message, "")

	re3 := regexp.MustCompile(`^` + "```" + `\w*`)
	message = re3.ReplaceAllString(message, "")

	re4 := regexp.MustCompile("```" + `$`)
	message = re4.ReplaceAllString(message, "")

	// Remove inline backticks at start/end of message
	message = strings.Trim(message, "`")
	message = strings.TrimSpace(message)

	// Remove any "**Rationale:**" or similar sections that Claude adds
	re5 := regexp.MustCompile(`(?s)\n\*\*.*?\*\*:.*`)
	message = re5.ReplaceAllString(message, "")

	return strings.TrimSpace(message)
}
