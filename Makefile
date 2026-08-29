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
