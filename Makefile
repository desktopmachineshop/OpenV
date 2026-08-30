# OpenV build targets.
#
# The API/frontend stack runs in Docker; the agent worker binaries (agentd,
# openv-mcp) run on the HOST next to your vendor CLIs (claude/codex/gemini).
# `worker` cross-compiles Windows binaries via Docker; use `worker-unix` on
# Linux/macOS hosts. See docs/agents.md.

GO_IMAGE := golang:1.25

.PHONY: build up down prod-up prod-down worker worker-unix worker-image connector-dist test backup restore

## Build all Docker images.
build:
	docker compose build

## Start the full stack (Postgres, API, frontend).
up:
	docker compose up -d

## Stop the stack.
down:
	docker compose down

## Start the stack with the production overlay (requires .env — see docs/operations.md).
prod-up:
	docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

## Stop the production stack.
prod-down:
	docker compose -f docker-compose.yml -f docker-compose.prod.yml down

## Back up the openv database plus the openv-data and uploads volumes into a
## single timestamped bundle: backups/openv-backup-<stamp>.tar.gz, then prune
## bundles older than BACKUP_RETENTION_DAYS (default 7). Runs the shared
## scripts/backup.sh recipe in a one-shot sidecar container — the same recipe
## the opt-in backup sidecar loops on (docker-compose.backup.yml). The stack
## must be up. See docs/operations.md.
backup:
	docker compose -f docker-compose.yml -f docker-compose.backup.yml run --rm --no-deps backup --once
	@echo "Backup written to backups/ (newest openv-backup-*.tar.gz)"

## Restore a backup made by `make backup`. The bundle must live in backups/.
## Usage: make restore BACKUP=backups/openv-backup-<stamp>.tar.gz
## Stops the API while data is swapped, then starts it again. DESTRUCTIVE:
## replaces the database and the contents of the data/uploads volumes.
restore:
	@test -n "$(BACKUP)" || { echo "Usage: make restore BACKUP=backups/openv-backup-<stamp>.tar.gz"; exit 1; }
	docker stop openv-api
	docker run --rm --volumes-from openv-api -v "$(CURDIR)/backups:/backup" alpine sh -c '\
		set -e && \
		rm -rf /tmp/restore && mkdir -p /tmp/restore && \
		tar xzf "/backup/$(notdir $(BACKUP))" -C /tmp/restore && \
		find /data -mindepth 1 -delete && tar xzf /tmp/restore/openv-data.tar.gz -C /data && \
		find /uploads -mindepth 1 -delete && tar xzf /tmp/restore/uploads-data.tar.gz -C /uploads && \
		mkdir -p /backup/.restore && cp /tmp/restore/openv-db.sql /backup/.restore/openv-db.sql'
	docker cp backups/.restore/openv-db.sql openv-postgres:/tmp/openv-db.sql
	docker exec openv-postgres sh -c 'psql -U postgres -d openv -q -f /tmp/openv-db.sql'
	docker exec openv-postgres sh -c 'rm -f /tmp/openv-db.sql'
	rm -rf backups/.restore
	docker start openv-api
	@echo "Restore complete."

## Build host worker binaries for Windows (bin/agentd.exe, bin/openv-mcp.exe).
worker:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c 'GOOS=windows GOARCH=amd64 go build -o bin/agentd.exe ./cmd/agentd && GOOS=windows GOARCH=amd64 go build -o bin/openv-mcp.exe ./cmd/openv-mcp'

## Build host worker binaries for Linux/macOS (bin/agentd, bin/openv-mcp).
worker-unix:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c 'go build -o bin/agentd ./cmd/agentd && go build -o bin/openv-mcp ./cmd/openv-mcp'

## Build the hosted runner image (agentd + openv-mcp + vendor CLIs).
worker-image:
	docker build -f Dockerfile.worker -t openv-worker:latest .

## Build the Agent Connector downloads (dist/openv-connector-windows.exe and
## dist/openv-connector-linux): one self-contained executable per OS, with
## agentd and openv-mcp embedded (-tags embedpayload) and unpacked next to
## the connector on first run. The API serves these at
## /api/v1/public/connector/download.
connector-dist:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c '\
		mkdir -p dist cmd/openv-connector/payload && \
		GOOS=windows GOARCH=amd64 go build -o cmd/openv-connector/payload/agentd ./cmd/agentd && \
		GOOS=windows GOARCH=amd64 go build -o cmd/openv-connector/payload/openv-mcp ./cmd/openv-mcp && \
		GOOS=windows GOARCH=amd64 go build -tags embedpayload -o dist/openv-connector-windows.exe ./cmd/openv-connector && \
		GOOS=linux GOARCH=amd64 go build -o cmd/openv-connector/payload/agentd ./cmd/agentd && \
		GOOS=linux GOARCH=amd64 go build -o cmd/openv-connector/payload/openv-mcp ./cmd/openv-mcp && \
		GOOS=linux GOARCH=amd64 go build -tags embedpayload -o dist/openv-connector-linux ./cmd/openv-connector && \
		rm -rf cmd/openv-connector/payload'

## Run the Go test suite in Docker.
test:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GO_IMAGE) sh -c 'go test ./...'
