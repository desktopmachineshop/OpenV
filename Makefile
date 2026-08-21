# OpenV build targets.
#
# The API/frontend stack runs in Docker; the agent worker binaries (agentd,
# openv-mcp) run on the HOST next to your vendor CLIs (claude/codex/gemini).
# `worker` cross-compiles Windows binaries via Docker; use `worker-unix` on
# Linux/macOS hosts. See docs/agents.md.

GO_IMAGE := golang:1.25

.PHONY: build up down worker worker-unix worker-image connector-dist test

## Build all Docker images.
build:
	docker compose build

## Start the full stack (Postgres, API, frontend).
up:
	docker compose up -d

## Stop the stack.
down:
	docker compose down

## Build host worker binaries for Windows (bin/agentd.exe, bin/openv-mcp.exe).
worker:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c 'GOOS=windows GOARCH=amd64 go build -o bin/agentd.exe ./cmd/agentd && GOOS=windows GOARCH=amd64 go build -o bin/openv-mcp.exe ./cmd/openv-mcp'

## Build host worker binaries for Linux/macOS (bin/agentd, bin/openv-mcp).
worker-unix:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c 'go build -o bin/agentd ./cmd/agentd && go build -o bin/openv-mcp ./cmd/openv-mcp'

## Build the hosted runner image (agentd + openv-mcp + vendor CLIs).
worker-image:
	docker build -f Dockerfile.worker -t openv-worker:latest .

## Build the Agent Connector download bundles (dist/openv-connector-<os>.zip).
## The API serves these at /api/v1/public/connector/download.
connector-dist:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c '\
		apt-get update -qq && apt-get install -y -qq zip >/dev/null && \
		mkdir -p dist/win dist/linux && \
		GOOS=windows GOARCH=amd64 go build -o dist/win/openv-connector.exe ./cmd/openv-connector && \
		GOOS=windows GOARCH=amd64 go build -o dist/win/agentd.exe ./cmd/agentd && \
		GOOS=windows GOARCH=amd64 go build -o dist/win/openv-mcp.exe ./cmd/openv-mcp && \
		GOOS=linux GOARCH=amd64 go build -o dist/linux/openv-connector ./cmd/openv-connector && \
		GOOS=linux GOARCH=amd64 go build -o dist/linux/agentd ./cmd/agentd && \
		GOOS=linux GOARCH=amd64 go build -o dist/linux/openv-mcp ./cmd/openv-mcp && \
		cp docs/connector-readme.txt dist/win/README.txt && cp docs/connector-readme.txt dist/linux/README.txt && \
		cd dist/win && zip -q ../openv-connector-windows.zip * && cd ../linux && zip -q ../openv-connector-linux.zip * && cd .. && rm -rf win linux'

## Run the Go test suite in Docker.
test:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c 'go test ./...'
