# OpenV build targets.
#
# The API/frontend stack runs in Docker; the agent worker binaries (agentd,
# openv-mcp) run on the HOST next to your vendor CLIs (claude/codex/gemini).
# `worker` cross-compiles Windows binaries via Docker; use `worker-unix` on
# Linux/macOS hosts. See docs/agents.md.

GO_IMAGE := golang:1.25
# Keep in step with the `Install govulncheck` step in
# .github/workflows/ci.yml so `make vuln` and CI scan with the same tool.
GOVULNCHECK_VERSION := v1.8.0

# Keep in step with GITLEAKS_VERSION in the `secrets` job of
# .github/workflows/ci.yml so `make secrets` and CI scan with the same tool.
GITLEAKS_VERSION := 8.21.2

# What `make check` and `make check-fast` compare the working tree with: the
# release-notes check wants a new bullet relative to it, and check-fast tests
# only the Go packages changed since its merge base with HEAD.
BASE_REF ?= origin/master

.PHONY: build up down prod-up prod-down worker worker-unix worker-image runner-pool-up runner-pool-down connector-dist mcp vapid-keys test check check-fast vuln secrets backup restore

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

## Run the CI vulnerability gate locally (the `vuln` job in
## .github/workflows/ci.yml): govulncheck over the server and worker binaries,
## then an audit of the frontend's production dependencies. Both scanners
## always run — a finding in the first does not hide the second's result —
## and any finding fails the target. Needs a host Go toolchain and npm; no
## Docker.
## govulncheck reads the advisory database over the network on every run.
## The npm side reads frontend/package-lock.json, so it works without a
## prior `npm install`.
vuln:
	GOTOOLCHAIN=auto go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@rc=0; \
	"$$(go env GOPATH)/bin/govulncheck" ./cmd/... ./internal/... || rc=1; \
	(cd frontend && npm audit --omit=dev --audit-level=high) || rc=1; \
	exit $$rc

## Run the CI secret-scan gate locally (the `secrets` job in
## .github/workflows/ci.yml): gitleaks over the commit history and the working
## tree, with the same pinned version CI uses. Any finding fails the target.
## Needs gitleaks on PATH — install it with:
##   go install github.com/gitleaks/gitleaks/v8@v$(GITLEAKS_VERSION)
## or `brew install gitleaks` (see https://github.com/gitleaks/gitleaks).
secrets:
	@rc=0; \
	gitleaks git --redact --verbose . || rc=1; \
	gitleaks dir --redact --verbose . || rc=1; \
	exit $$rc

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

## Build the MCP tool server for an agent session in this repo (bin/openv-mcp)
## with the host Go toolchain — no Docker. scripts/openv/mcp-server.sh builds
## it on demand too; this target just gets it out of the way first.
mcp:
	go build -o bin/openv-mcp ./cmd/openv-mcp

## Print a fresh VAPID key pair for web push notifications (REQ-109). One pair
## per deployment: paste the three lines into the API service's environment.
## Rotating the pair invalidates every existing subscription. See
## docs/operations.md.
vapid-keys:
	@go run ./cmd/openv-vapid

## Build the hosted runner image (agentd + openv-mcp + vendor CLIs).
worker-image:
	docker build -f Dockerfile.worker -t openv-worker:latest .

## Start the transient runner pool alongside the stack: POOL pre-warmed runner
## nodes (default 2) members can lease from the UI instead of installing the
## connector. Requires RUNNER_POOL_KEY set to the same value as the API's (put
## it in .env). See docs/agents.md.
runner-pool-up:
	@test -n "$(RUNNER_POOL_KEY)$$(grep -s '^RUNNER_POOL_KEY=' .env)" || \
		{ echo "Set RUNNER_POOL_KEY (in .env or the environment) first: openssl rand -hex 32"; exit 1; }
	docker compose --profile runner-pool up -d --scale runner-pool=$(or $(POOL),2) runner-pool

## Stop the transient runner pool (leases end; the API reclaims the nodes).
runner-pool-down:
	docker compose --profile runner-pool stop runner-pool

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

## Run the gates CI runs on a pull request (.github/workflows/ci.yml), in its
## order, before pushing: the backend job (gofmt over ./cmd ./internal; go vet
## and go test over the root, ./cmd/... and ./internal/...), the release-notes
## job, and the frontend job (tsc, lint, vitest, vite build); then the tests of
## the refactor tools that live outside Go (frontend/scripts, scripts/refactor).
## The Postgres-backed Go tests run only when OPENV_TEST_DATABASE_URL points
## at a database, as it does in CI; they skip otherwise. The release-notes job
## requires a new bullet relative to BASE_REF; set NO_RELEASE_NOTES=1 for a
## pull request that carries the no-release-notes label. Needs a host Go
## toolchain, Node with `npm ci` run in frontend/, Python 3 and git; no Docker.
## The Docker builds, e2e and security scans stay in CI (`make vuln` and
## `make secrets` run the last two locally).
check:
	@unformatted="$$(gofmt -l ./cmd ./internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; echo "$$unformatted"; \
		echo "Run 'gofmt -w ./cmd ./internal' to fix."; exit 1; \
	fi
	go vet . ./cmd/... ./internal/...
	go test . ./cmd/... ./internal/...
	python3 -m unittest scripts/release_notes_test.py
	python3 scripts/release_notes.py check RELEASE_NOTES.md
	@if [ -n "$(NO_RELEASE_NOTES)" ]; then \
		echo "NO_RELEASE_NOTES is set: not requiring a release note (the no-release-notes label)"; \
	else \
		base_notes="$$(mktemp)"; \
		git show "$(BASE_REF):RELEASE_NOTES.md" > "$$base_notes" 2>/dev/null || : > "$$base_notes"; \
		python3 scripts/release_notes.py check-pr --base "$$base_notes" RELEASE_NOTES.md; rc=$$?; \
		rm -f "$$base_notes"; exit $$rc; \
	fi
	cd frontend && npx tsc --noEmit
	cd frontend && npm run lint
	cd frontend && npm test
	cd frontend && npm run build
	cd frontend && node --test 'scripts/*.test.mjs'
	python3 -m unittest scripts/refactor/classify_commits_test.py

## A quick gate to run while working, well under a minute and without Docker:
## gofmt over ./cmd ./internal, go vet over the module, go test -short on the
## Go packages changed (committed or not) since the merge base with BASE_REF
## (a change under a package's testdata counts for that package), and the
## frontend type check. `make check` is still the gate before pushing.
check-fast:
	@unformatted="$$(gofmt -l ./cmd ./internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; echo "$$unformatted"; \
		echo "Run 'gofmt -w ./cmd ./internal' to fix."; exit 1; \
	fi
	go vet . ./cmd/... ./internal/...
	@base="$$(git merge-base "$(BASE_REF)" HEAD 2>/dev/null || echo HEAD)"; \
	pkgs="$$( { git diff --name-only "$$base" -- '*.go' cmd internal; \
		git ls-files --others --exclude-standard -- '*.go' cmd internal; } \
		| while read -r f; do \
			case "$$f" in */testdata/*) d="$${f%%/testdata/*}" ;; */*) d="$${f%/*}" ;; *) d=. ;; esac; \
			case "$$d" in .|cmd/*|internal/*) ;; *) continue ;; esac; \
			if ls "$$d"/*.go >/dev/null 2>&1; then [ "$$d" = . ] && echo . || echo "./$$d"; fi; \
		done | sort -u)"; \
	if [ -z "$$pkgs" ]; then \
		echo "go test -short: no Go package changed since $$base"; \
	else \
		echo "go test -short" $$pkgs; go test -short $$pkgs; \
	fi
	cd frontend && npx tsc --noEmit
