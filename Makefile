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

# What `make check` and `make check-fast` compare the working tree with, by
# its merge base with HEAD: the release-notes check wants a new bullet
# relative to that, and check-fast tests the Go packages changed since then.
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

## Run the gates CI runs on a pull request, in their order, before pushing.
## From .github/workflows/ci.yml: the backend job (gofmt over ./cmd
## ./internal; go vet and go test over the root, ./cmd/... and ./internal/...;
## the refactor tools' Python tests; the Postgres vector assertion) and the
## frontend job (tsc, lint, vitest, the refactor tools' Node tests, vite
## build). From .github/workflows/release-notes.yml: the release-notes job.
## The Postgres-backed Go tests run only when OPENV_TEST_DATABASE_URL points
## at a database, as it does in CI; they skip otherwise. CI runs them twice,
## on postgres:15 (no vector extension) and on pgvector (backend-pgvector),
## and fails if either variant only skipped (a skipped subtest counts), the
## stored-data freeze's schema and purge goldens (refactor plan S3) included;
## locally there is one server, so the assertion requires whichever variant
## that server supports to have run. The boot harness in cmd/server (refactor
## plan S4a's profiles, S4b's environment matrix and misconfigured boots)
## reads the same variable, and runs on both CI legs; with it set, check
## fails if any of its tests or boots only skipped, or if a golden under
## cmd/server/testdata/boot/ has no boot that passed. So does the API tour
## beside it (refactor plan S5a-S5e): check fails if a tour test only
## skipped, if TestTourCoverage (which also holds the union across slices,
## cmd/server/testdata/tour/coverage.txt, to the plan's 90% floor),
## TestTourGoldensAreClaimed, TestTourStream (the event-stream reader S5b's
## areas use), TestTourAccountOptions, TestTourMailCatcher or
## TestTourStandIns (what S5c's areas rely on: adopted accounts and server
## settings, the mail catcher, the stand-ins), TestTourWorker (the runner's
## requests S5d's areas send: claim bodies, key and run-token actors, a
## literal body) or TestTourMatrix (S5e's authorization matrix: phantom
## paths, the cell classifier, rows read back) did not pass, or if a golden under
## cmd/server/testdata/tour/<slice>/ has no area test (the one its "test"
## field names) that passed. On macOS, where Go ignores SSL_CERT_FILE,
## TestTourStandIns and the areas with stand-ins (their goldens carry the
## tour's test certificate authority) skip, and check accepts exactly those
## skips and names them. With the variable set, the plain go test skips the
## tour and the three boot tests, so that each runs once, in its own block.
## The release-notes job requires a new bullet relative to the merge base
## with BASE_REF, which is what CI's comparison of the merge commit with its
## first parent amounts to; set NO_RELEASE_NOTES=1 for a pull request that
## carries the no-release-notes label. From
## .github/workflows/refactor-guard.yml: the Refactor guard job, over the
## commits since that merge base, with the labels the pull request will carry
## in LABELS (space-separated): `make check LABELS="refactor refactor:tooling
## no-release-notes"` runs the refactor class checks and, as CI does for a
## refactor, fails if internal/archtest/ratchets.json can be tightened (the
## tightened file is left in the working tree to commit). Like the job, it
## runs the merge base's copy of refactor_guard.py when there is one. It
## reads commits, so commit first. Needs a host Go toolchain, Node with
## `npm ci` run in frontend/, Python 3 and git; no Docker. The Docker builds,
## e2e and security scans stay in CI (`make vuln` and `make secrets` run the
## last two locally).
check:
	@unformatted="$$(gofmt -l ./cmd ./internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; echo "$$unformatted"; \
		echo "Run 'gofmt -w ./cmd ./internal' to fix."; exit 1; \
	fi
	go vet . ./cmd/... ./internal/...
	@if [ -z "$$OPENV_TEST_DATABASE_URL" ]; then \
		echo "go test . ./cmd/... ./internal/..."; go test . ./cmd/... ./internal/...; \
	else \
		echo "go test -skip '^(TestTour.*|TestBootSmoke|TestBootProfiles|TestBootMisconfigured)\$$' . ./cmd/... ./internal/... (the tour and boots run below)"; \
		go test -skip '^(TestTour.*|TestBootSmoke|TestBootProfiles|TestBootMisconfigured)$$' . ./cmd/... ./internal/...; \
	fi
	python3 -m unittest scripts/refactor/classify_commits_test.py scripts/refactor/refactor_guard_test.py
	@if [ -z "$$OPENV_TEST_DATABASE_URL" ]; then \
		echo "OPENV_TEST_DATABASE_URL is unset: the Postgres-backed tests skipped (CI runs them)"; \
	else \
		log="$$(mktemp)"; \
		go test ./internal/persistence/postgres/ -count=1 -v \
			-run '^(TestVectorReconcileNoopWhenExtensionAbsent|TestNearestByEmbeddingVectorUnavailable|TestNearestByEmbedding|TestDuplicateCandidates|TestEmbeddingRepositoryUpsert|TestVectorReconcileCreatesWhenExtensionAppears|TestSchemaGolden|TestPurgeCatalog)$$' > "$$log"; rc=$$?; \
		ran() { for t in "$$@"; do grep -q -- "^--- PASS: $$t " "$$log" && ! grep -Eq -- "--- SKIP: $$t( |/)" "$$log" || return 1; done; }; \
		if [ $$rc -ne 0 ]; then cat "$$log"; \
		elif ran TestVectorReconcileNoopWhenExtensionAbsent TestNearestByEmbeddingVectorUnavailable TestSchemaGolden TestPurgeCatalog; then \
			echo "Postgres tests ran without the vector extension (CI's postgres:15 leg)"; \
		elif ran TestNearestByEmbedding TestDuplicateCandidates TestEmbeddingRepositoryUpsert TestVectorReconcileCreatesWhenExtensionAppears TestSchemaGolden TestPurgeCatalog; then \
			echo "Postgres tests ran with the vector extension (CI's backend-pgvector leg)"; \
		else cat "$$log"; echo "neither the vector nor the no-vector Postgres tests ran; they only skipped"; rc=1; fi; \
		rm -f "$$log"; exit $$rc; \
	fi
	@if [ -z "$$OPENV_TEST_DATABASE_URL" ]; then \
		echo "OPENV_TEST_DATABASE_URL is unset: the boot harness (cmd/server, refactor plan S4) skipped (CI runs it)"; \
	else \
		log="$$(mktemp)"; \
		go test ./cmd/server/ -count=1 -v -run '^(TestBootSmoke|TestBootProfiles|TestBootMisconfigured)$$' > "$$log"; rc=$$?; \
		ran() { for t in TestBootSmoke TestBootProfiles TestBootMisconfigured; do grep -q -- "^--- PASS: $$t " "$$log" && ! grep -Eq -- "--- SKIP: $$t( |/)" "$$log" || return 1; done; }; \
		booted() { for g in cmd/server/testdata/boot/*.txt; do grep -Eq -- "^ +--- PASS: TestBoot(Smoke|Profiles|Misconfigured)/$$(basename "$$g" .txt) " "$$log" || { echo "no boot passed for $$g"; return 1; }; done; }; \
		if [ $$rc -ne 0 ]; then cat "$$log"; \
		elif ran && booted; then \
			echo "the boot harness ran, every boot of it (refactor plan S4)"; \
		else cat "$$log"; echo "the boot harness, or a boot of it, only skipped"; rc=1; fi; \
		rm -f "$$log"; exit $$rc; \
	fi
	@if [ -z "$$OPENV_TEST_DATABASE_URL" ]; then \
		echo "OPENV_TEST_DATABASE_URL is unset: the API tour (cmd/server, refactor plan S5) skipped (CI runs it)"; \
	else \
		log="$$(mktemp)"; \
		go test ./cmd/server/ -count=1 -v -run '^TestTour' > "$$log"; rc=$$?; \
		linux=" "; if [ "$$(uname -s)" = Darwin ]; then linux=" TestTourStandIns "; \
			for g in $$(grep -l -- "SSL_CERT_FILE=<the tour's test certificate authority>" cmd/server/testdata/tour/*/*.json); do \
				linux="$$linux$$(sed -n 's/^  "test": "\(TestTour[A-Za-z0-9]*\)",$$/\1/p' "$$g") "; done; fi; \
		toured() { for s in $$(sed -n 's/^ *--- SKIP: \(TestTour[A-Za-z0-9]*\).*/\1/p' "$$log"); do \
				case "$$linux" in *" $$s "*) ;; *) return 1;; esac; done; \
			for t in TestTourCoverage TestTourGoldensAreClaimed TestTourStream TestTourAccountOptions TestTourMailCatcher \
				TestTourStandIns TestTourWorker TestTourMatrix; do case "$$linux" in *" $$t "*) continue;; esac; \
				grep -q -- "^--- PASS: $$t " "$$log" || return 1; done; \
			for g in cmd/server/testdata/tour/*/*.json; do \
				t="$$(sed -n 's/^  "test": "\(TestTour[A-Za-z0-9]*\)",$$/\1/p' "$$g")"; \
				case "$$linux" in *" $$t "*) continue;; esac; \
				{ [ -n "$$t" ] && grep -q -- "^--- PASS: $$t " "$$log"; } || { echo "no tour area passed for $$g"; return 1; }; \
			done; }; \
		if [ $$rc -ne 0 ]; then cat "$$log"; \
		elif toured; then \
			echo "the API tour ran, every area of it (refactor plan S5)"; \
			[ "$$linux" = " " ] || echo "except on this macOS host, where Go ignores SSL_CERT_FILE: run$${linux}on Linux (CI does)"; \
		else cat "$$log"; echo "the API tour, or an area of it, only skipped"; rc=1; fi; \
		rm -f "$$log"; exit $$rc; \
	fi
	python3 -m unittest scripts/release_notes_test.py
	python3 scripts/release_notes.py check RELEASE_NOTES.md
	@if [ -n "$(NO_RELEASE_NOTES)" ]; then \
		echo "NO_RELEASE_NOTES is set: not requiring a release note (the no-release-notes label)"; \
	else \
		base="$$(git merge-base "$(BASE_REF)" HEAD)" || { \
			echo "cannot find where HEAD left $(BASE_REF): run 'git fetch origin master' or set BASE_REF"; exit 1; }; \
		base_notes="$$(mktemp)"; \
		if ! git show "$$base:RELEASE_NOTES.md" > "$$base_notes"; then rm -f "$$base_notes"; exit 1; fi; \
		python3 scripts/release_notes.py check-pr --base "$$base_notes" RELEASE_NOTES.md; rc=$$?; \
		rm -f "$$base_notes"; exit $$rc; \
	fi
	@base="$$(git merge-base "$(BASE_REF)" HEAD)" || { \
		echo "cannot find where HEAD left $(BASE_REF): run 'git fetch origin master' or set BASE_REF"; exit 1; }; \
	guard=scripts/refactor/refactor_guard.py; tmp=""; \
	if git cat-file -e "$$base:$$guard" 2>/dev/null; then \
		tmp="$$(mktemp -d)" && mkdir -p "$$tmp/scripts/refactor" && git show "$$base:$$guard" > "$$tmp/$$guard" && \
		{ ! git cat-file -e "$$base:scripts/release_notes.py" 2>/dev/null || \
			git show "$$base:scripts/release_notes.py" > "$$tmp/scripts/release_notes.py"; } || \
		{ rm -rf "$$tmp"; exit 1; }; \
		guard="$$tmp/$$guard"; \
	fi; \
	python3 "$$guard" --base "$$base" --head HEAD --summary "" $(foreach l,$(LABELS),--label '$(l)'); rc=$$?; \
	[ -z "$$tmp" ] || rm -rf "$$tmp"; exit $$rc
	@case " $(LABELS) " in *" refactor "*|*" refactor:"*) \
		UPDATE_RATCHETS=1 go test -count=1 -run '^TestArchitecture$$' ./internal/archtest && \
		{ git diff --exit-code -- internal/archtest/ratchets.json || { \
			echo "ratchets.json can be tightened: commit the regenerated file above (a refactor may not leave it loose)"; exit 1; }; } ;; \
	esac
	cd frontend && npx tsc --noEmit
	cd frontend && npm run lint
	cd frontend && npm test
	cd frontend && node --test 'scripts/*.test.mjs'
	cd frontend && npm run build

## A quick gate to run while working, well under a minute and without Docker:
## gofmt over ./cmd ./internal, go vet over the module, go test -short on the
## Go packages changed (committed or not) since the merge base with BASE_REF
## (a change under a package's testdata counts for that package) and on the
## packages that import one of them (their tests included), so a golden in a
## dependent package is checked too; then the whole-tree architecture
## ratchets in internal/archtest (import edges, size budgets, env reads,
## bans), the S6 SSE and domain-event tests in internal/api, which read every
## package's sources whether or not internal/api imports it (an event name
## sent from internal/orchestration, say), and the frontend type check.
## `make check` is still the gate before pushing.
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
		mod="$$(go list -m)"; \
		pkgs="$$( { echo "$$pkgs"; \
			go list -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' . ./cmd/... ./internal/... \
			| awk -v mod="$$mod" -v changed="$$(echo $$pkgs)" ' \
				BEGIN { n = split(changed, c, " "); for (i = 1; i <= n; i++) want[c[i] == "." ? mod : mod "/" substr(c[i], 3)] = 1 } \
				{ for (i = 2; i <= NF; i++) if ($$i in want) { print ($$1 == mod ? "." : "./" substr($$1, length(mod) + 2)); break } }'; \
			} | sort -u)"; \
		echo "go test -short" $$pkgs; go test -short $$pkgs; \
	fi
	go test ./internal/archtest
	go test -short -run '^(TestSSE|TestEventPayload)' ./internal/api
	cd frontend && npx tsc --noEmit
