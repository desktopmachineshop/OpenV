#!/usr/bin/env bash
# Refactor step M14 (docs/plans/codebase-refactor.md, K5): Handler embeds
# HandlerDeps, so a handler dependency is declared once, as a HandlerDeps
# field plus one line in its wire_*.go stage.
#
# Its class R commit is exactly what this script writes on its parent; the
# Refactor guard re-runs it there and requires that commit byte for byte
# (CONTRIBUTING.md, "Refactor PRs"). internal/tools/embeddeps does the work:
# it renames each Handler field NewHandler copies from HandlerDeps as it is
# to the HandlerDeps field's name, in every selector whose receiver is a
# Handler or *Handler (production and test files of internal/api), deletes
# the field and its element of NewHandler's literal, embeds HandlerDeps in
# Handler and assigns it in NewHandler. The values NewHandler derives (the
# cookie SameSite and Secure, the trimmed frontend URL, the rate limiters)
# stay private fields where they are. See internal/tools/README.md.
#
# Usage: scripts/refactor/embed_deps.sh   (from anywhere in the repository)
set -euo pipefail
cd "$(dirname "$0")/../.."
go run ./internal/tools/embeddeps internal/api
