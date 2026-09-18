#!/bin/sh
# Runs from the nginx image's entrypoint before nginx starts (the base image
# executes every /docker-entrypoint.d/*.sh when the command is `nginx`).
#
# A non-production copy of the app — staging, a customer's preview instance —
# must not turn up in search results next to the real one. OPENV_NOINDEX=1
# makes every response carry X-Robots-Tag, which is the header form of
# <meta name="robots">: a crawler that fetches any URL here is told not to
# index it.
#
# Deliberately NOT a robots.txt Disallow. Disallow stops a crawler fetching
# the page at all, so it never reads the noindex and the URL can still be
# listed from inbound links alone. The header is what actually removes a
# host from an index, and it needs the crawl to happen to be seen.
set -eu

out=/etc/nginx/openv/noindex.conf

case "${OPENV_NOINDEX:-}" in
    ""|0|false|off|no)
        # Indexable: leave the build-time default in place. Rewritten rather
        # than skipped so a restart without the variable undoes a previous
        # run's header.
        cat > "$out" <<'CONF'
# Generated at container start by 45-openv-noindex.sh; do not edit.
# OPENV_NOINDEX is unset: this deployment is indexable.
CONF
        ;;
    *)
        cat > "$out" <<'CONF'
# Generated at container start by 45-openv-noindex.sh; do not edit.
add_header X-Robots-Tag "noindex, nofollow, noarchive" always;
CONF
        echo "openv: X-Robots-Tag noindex is on (OPENV_NOINDEX set)"
        ;;
esac
