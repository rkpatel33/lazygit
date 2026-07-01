build:
    go build -o lazygit -ldflags "-X main.version=$(git describe --tags --abbrev=0 2>/dev/null || echo dev)-custom -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.buildSource=local" .

# Real API smoke test — hits Anthropic/Gemini with a sample diff (keys from ~/.config/lazygit/.env)
smoke-ai:
    go test -tags=smoke ./pkg/gui -run TestSmokeAICommitProviders -count=1 -v
