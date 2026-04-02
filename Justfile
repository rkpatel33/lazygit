build:
    go build -o lazygit -ldflags "-X main.version=$(git describe --tags --abbrev=0 2>/dev/null || echo dev)-custom -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.buildSource=local" .
