---
name: add-endpoint
description: Use when adding an HTTP endpoint or a new API area to the OpenV server, from its route and handler to the frontend call, or giving the handlers a new service.
---

# Add an endpoint

The recipes are in the area guides; follow them there.

- Server: [internal/api/README.md, Recipes](../../../internal/api/README.md#recipes),
  "Add an endpoint to an area", or "Add an API area" for a new area file.
- A new service for the handlers: [cmd/server/README.md, Recipes](../../../cmd/server/README.md#recipes),
  "Give the API a new dependency".
- Client: [frontend/src/api/README.md, Recipes](../../../frontend/src/api/README.md#recipes),
  "Add an endpoint call", or "Add an area module".

Scaffolds (`-n` previews; each prints the steps left when it is done):

- `go run ./internal/tools/scaffold api-area <name>`
- `node frontend/scripts/scaffold.mjs api-module <area>`

Before you finish: the area's `CLAUDE.md`, then "Before you finish" in the
root [CLAUDE.md](../../../CLAUDE.md).
