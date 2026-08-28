# Quick Start Guide

OpenV is a requirements and V&V platform with a built-in multi-agent suite:
projects hold versioned artifacts (requirements, test cases, hazards, design
items) with traceability links, and AI agents — running on **your own
machine** through vendor CLIs — can draft, review, and interview for you.

## Prerequisites

- **Docker Desktop** (Docker + Docker Compose). That's it — no Go, Node, or
  PostgreSQL needed on the host; everything builds and runs in containers.
- For agent runs later: at least one vendor CLI on your machine
  (Claude Code, Codex CLI, or Gemini CLI) with your own subscription login.

## 1. Start the stack

```bash
git clone https://github.com/desktopmachineshop/OpenV
cd OpenV
docker compose up -d        # or: make up
```

- **App**: http://localhost:3000
- **API**: http://localhost:8080 (health: `GET /health`)
- **Postgres**: localhost:5432

The API creates and migrates its schema automatically on first boot.

## 2. Register your first user

Open http://localhost:3000 and register with email + password.

- The **first user registered becomes the platform admin**.
- A **personal workspace** (organization) is created for you automatically,
  seeded with default agents and a starter crew.
- Optional: enable **Sign in with Google** by setting `GOOGLE_CLIENT_ID` and
  `GOOGLE_CLIENT_SECRET` (e.g. in a `.env` file next to
  `docker-compose.yml`); the authorized redirect URI is
  `${PUBLIC_URL}/api/v1/auth/google/callback`.

You can create additional **company workspaces** and invite members from the
workspace switcher; each API request runs in your active workspace (the
`X-Org-ID` header, handled by the UI).

## 3. Create a project

Click **New Project** (optionally from a template). You become the project's
owner; teammates get access via project members or workspace people-teams.

## 4. Run the guided wizard

The fastest way to seed a project is the **guided requirements wizard**
(Start guided setup on the project page):

- Step through vision, users/personas, needs, requirements, NFRs, and
  hazards; answers are saved per step.
- A **copilot chat** sits beside the wizard — when a runner is online it
  suggests personas, needs, and requirements as one-click "Add to wizard"
  cards.
- Finish with **Commit**: drafts become real, linked artifacts. You can
  resume an in-progress session or start a "modify" session later without
  losing artifact links.

You can of course also create artifacts and traceability links by hand from
the project views, record test runs, and watch the V&V coverage dashboards.

## 5. Connect a personal runner (for AI agents)

Agent runs execute on runners you control. The easiest setup is the **Agent
Connector**:

1. Launch any agent action in the UI (or open Settings → My Runner). If no
   runner is online, OpenV prompts you to set one up.
2. **Download** the connector bundle from the prompt (served at
   `GET /api/v1/public/connector/download?os=windows|linux`). Operators must
   build the bundles once with `make connector-dist` (they land in `./dist`,
   which compose mounts into the API container).
3. Unzip, run `openv-connector` once (it registers the `openv-connector://`
   link handler), then click **Pair connector** in OpenV. The pairing code is
   one-time and short-lived; the connector exchanges it for your personal
   runner key and starts the runner in its console window.
4. Sign your vendor CLIs in from **user settings → Agent sign-ins** (or run
   `claude login` etc. in a terminal yourself).

Your personal runner only claims runs **you** launched, using your own CLI
subscription on your own machine. See `docs/agents.md` for the manual
`agentd` setup, workspace worker keys, and the always-on hosted runner tier.

## Using the API directly

All non-auth endpoints require credentials — a browser session cookie, or a
Bearer worker key / run token (see `docs/api-spec.md`). A quick session-based
smoke test:

```bash
# Register (first user = admin) and keep the session cookie
curl -c cookies.txt -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"a-strong-password","name":"You"}'

# Create a project (server assigns the UUID)
curl -b cookies.txt -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"Demo project","description":"First project"}'

# Create an artifact in it
curl -b cookies.txt -X POST http://localhost:8080/api/v1/artifacts \
  -H "Content-Type: application/json" \
  -d '{"project_id":"<project-id-from-above>","type":"requirement",
       "title":"System shall respond within 100ms","body":"..."}'
```

## Stopping and resetting

```bash
docker compose down        # stop
docker compose down -v     # stop and DELETE all data (database, uploads)
```

## Troubleshooting

- **Frontend can't reach the API** — check `docker compose ps`; the API waits
  for Postgres's healthcheck, so give it ~30s on first boot. Logs:
  `docker compose logs api`.
- **"authentication required" from the API** — every non-`/auth`,
  non-`/public` endpoint needs a session cookie or Bearer token; log in
  first.
- **Agent runs stay queued** — no runner is online. Open the connector (or
  start `agentd`), and check Settings for provider status.
- **Connector download says bundles aren't built** — run
  `make connector-dist` on the host serving the API.

## Where to go next

- `docs/architecture.md` — system design and the multi-agent topology
- `docs/api-spec.md` — auth model and full route inventory
- `docs/data-model.md` — database schema overview
- `docs/agents.md` — runners, crews, automations, interviews
- `docs/DEVELOPMENT.md` — Docker-based development workflow
