# Justfile for MCP API Gateway

# Run syntax check, type auditing, and tests
validate:
    go vet ./...
    go test -v ./...

# Build local target binary
build:
	go build -o mcp-gateway main.go
	go build -o mcp-cli cmd/mcp-cli/main.go

# Build local CLI only
build-cli:
	go build -o mcp-cli cmd/mcp-cli/main.go

# Cross-compile CLI for macOS, Linux, and Windows
build-cli-all:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/mcp-cli-linux-amd64 cmd/mcp-cli/main.go
	GOOS=darwin GOARCH=amd64 go build -o dist/mcp-cli-darwin-amd64 cmd/mcp-cli/main.go
	GOOS=darwin GOARCH=arm64 go build -o dist/mcp-cli-darwin-arm64 cmd/mcp-cli/main.go
	GOOS=windows GOARCH=amd64 go build -o dist/mcp-cli-windows-amd64.exe cmd/mcp-cli/main.go

# Start local server in development mode
run:
	go run main.go

# Build the production Docker container
docker-build:
    docker build -t mcp-api-gateway:latest .

# Run docker-compose cluster
up:
    docker-compose up -d --build

# Shutdown docker-compose cluster
down:
    docker-compose down
