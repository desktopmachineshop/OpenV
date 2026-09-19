#!/bin/sh
# Runs from the nginx image's entrypoint before nginx starts (the base image
# executes every /docker-entrypoint.d/*.sh when the command is `nginx`).
#
# A non-production copy of OpenV installs as its own app. Without this, staging
# and the live service install from different origins but land on the home
# screen as the same white "OpenV" tile, and the one you reach for is a coin
# toss. OPENV_APP_VARIANT=staging switches the installed name, the icons and
# the theme colour to the amber staging set.
#
# The variant is resolved by convention — manifest.<variant>.json beside the
# built app — so another deployment can ship its own manifest and icons
# without touching this script. An unknown variant leaves the app exactly as
# built rather than serving a manifest that is not there.
#
# The app is reached from a different ORIGIN than production, so the browser
# already treats the two as separate installs; this only makes them look
# separate to the person choosing between them.
set -eu

variant="${OPENV_APP_VARIANT:-}"
[ -n "$variant" ] || exit 0

root=/usr/share/nginx/html
manifest="manifest.${variant}.json"

if [ ! -f "$root/$manifest" ]; then
    echo "openv: OPENV_APP_VARIANT=$variant but $manifest is not in this build; app identity unchanged" >&2
    exit 0
fi

# Read the display name and theme out of the variant's own manifest, so this
# script never holds a second copy of them that could drift.
name=$(sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$root/$manifest" | head -n 1)
theme=$(sed -n 's/.*"theme_color"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$root/$manifest" | head -n 1)
touch_icon="apple-touch-icon-${variant}.png"
[ -f "$root/$touch_icon" ] || touch_icon="apple-touch-icon.png"

# Every substitution replaces the whole attribute rather than appending to it,
# so re-running the entrypoint on a restarted container is a no-op instead of
# stacking prefixes.
tmp=$(mktemp)
sed \
    -e "s|<link rel=\"manifest\" href=\"[^\"]*\"|<link rel=\"manifest\" href=\"/$manifest\"|" \
    -e "s|<link rel=\"apple-touch-icon\" href=\"[^\"]*\"|<link rel=\"apple-touch-icon\" href=\"/$touch_icon\"|" \
    -e "s|<meta name=\"apple-mobile-web-app-title\" content=\"[^\"]*\"|<meta name=\"apple-mobile-web-app-title\" content=\"$name\"|" \
    -e "s|<meta name=\"theme-color\" content=\"[^\"]*\"|<meta name=\"theme-color\" content=\"$theme\"|" \
    -e "s|<title>[^<]*</title>|<title>$name</title>|" \
    "$root/index.html" > "$tmp"
cat "$tmp" > "$root/index.html"
rm -f "$tmp"

echo "openv: app identity is '$name' (OPENV_APP_VARIANT=$variant)"
