#!/bin/sh
# Runs from the nginx image's entrypoint before nginx starts (the base image
# executes every /docker-entrypoint.d/*.sh when the command is `nginx`).
#
# The frontend proxies /api/ to the OpenV API on the same origin, so the
# browser's session cookie is first-party — see docs/plans/mobile-support.md.
# Two things about that upstream are only known at run time:
#
# - API_UPSTREAM: host:port of the API on the private network
#   (Railway: <api-service>.railway.internal:8080; compose: api:8080).
# - The DNS server that can resolve it. nginx never reads /etc/resolv.conf
#   for the `resolver` directive, and resolving the name once at startup
#   would pin a private IP that changes on every API redeploy, so the
#   container's nameservers are written into a resolver directive and the
#   upstream is proxied through a variable, which makes nginx re-resolve it.
set -eu

out=/etc/nginx/openv/api-proxy.conf
upstream="${API_UPSTREAM:-api:8080}"
upstream="${upstream#http://}"
upstream="${upstream%/}"

nameservers=""
for ns in $(awk '/^nameserver/ { print $2 }' /etc/resolv.conf 2>/dev/null); do
    case "$ns" in
        *:*) nameservers="$nameservers [$ns]" ;;
        *)   nameservers="$nameservers $ns" ;;
    esac
done
if [ -z "$nameservers" ]; then
    # Docker's embedded DNS; only reached when resolv.conf is unreadable.
    nameservers=" 127.0.0.11"
fi

cat > "$out" <<CONF
# Generated at container start by 40-openv-api-proxy.sh; do not edit.
resolver${nameservers} valid=10s ipv6=on;
set \$openv_api_upstream "http://${upstream}";
CONF

echo "openv: proxying /api/ to http://${upstream} (resolver${nameservers})"
