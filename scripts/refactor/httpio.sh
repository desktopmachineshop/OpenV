#!/usr/bin/env bash
# Refactor step X1 (docs/plans/codebase-refactor.md §6.7): internal/api's
# JSON responses and request decodes go through respond.go's helpers.
#
# Each X1 pull request after X1a, which adds the helpers, is one class R
# commit holding exactly what this script writes on its parent, for the
# areas of docs/areas.json that pull request covers; the Refactor guard
# re-runs it there and requires that commit byte for byte (CONTRIBUTING.md,
# "Refactor PRs"). internal/tools/httpio does the work: it swaps the exact
# statement sequences a handler writes JSON or decodes a body with, one for
# one and in place, for the helper that makes the same calls on the
# writer, and prints every candidate it leaves, with its reason. It writes
# nothing unless respond.go declares the helpers exactly as
# internal/tools/httpio/helpers.go does. See internal/tools/README.md.
#
# Usage: scripts/refactor/httpio.sh <area> [<area>...]   (from anywhere in the repository)
set -euo pipefail
if [ "$#" -eq 0 ]; then
	echo "usage: scripts/refactor/httpio.sh <area> [<area>...]" >&2
	exit 2
fi
cd "$(dirname "$0")/../.."
areas="$1"
shift
for a in "$@"; do
	areas="$areas,$a"
done
go run ./internal/tools/httpio -area "$areas" internal/api
