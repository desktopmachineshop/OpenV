#!/usr/bin/env bash
# Validates the user manual (manual/) with OpenD's own checks, as the docs
# site's build will: broken links, unknown context keys, undeclared values.
# OpenD is pinned to OPEND_REF and built once into OPEND_DIR (default
# .cache/opend); bump the pin to pick up a newer OpenD. Needs git, Node 22.12+
# and corepack (for pnpm). docs/docs-maintenance.md has the rules.
set -euo pipefail

OPEND_REPO="${OPEND_REPO:-https://github.com/desktopmachineshop/OpenD.git}"
OPEND_REF="${OPEND_REF:-90958ffe1dd6bd21f13d934040776a46d9b4cb06}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
dir="${OPEND_DIR:-$root/.cache/opend}"

if [ ! -f "$dir/.built-$OPEND_REF" ]; then
  rm -rf "$dir"
  git init -q "$dir"
  git -C "$dir" fetch -q --depth=1 "$OPEND_REPO" "$OPEND_REF"
  git -C "$dir" checkout -q FETCH_HEAD
  (cd "$dir" && corepack pnpm install --frozen-lockfile --silent && corepack pnpm exec tsc -b)
  touch "$dir/.built-$OPEND_REF"
fi

node "$dir/packages/cli/dist/bin.js" validate "$root/manual" --strict
