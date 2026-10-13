#!/bin/sh
# Runs from the nginx image's entrypoint before nginx starts (the base image
# executes every /docker-entrypoint.d/*.sh when the command is `nginx`).
#
# What crawlers are told (docs/plans/search-and-ai-visibility.md, 4.1). The
# build writes robots.txt, sitemap.xml and llms.txt under .openv-crawl/ with
# the site's address left as a placeholder (frontend/src/site/crawl.ts),
# because one image serves openv.app, staging and every self-hosted copy, and
# only a deployment that says where it lives should advertise a sitemap.
#
# OPENV_SITE_URL is the canonical address of the public site, scheme and host
# only (https://openv.app). With it set:
# - the three files are written to the web root with the address filled in;
# - every response served under any OTHER host name (the platform's own
#   *.up.railway.app address, say) carries X-Robots-Tag: noindex, so search
#   engines index the site once, under its own name.
# Unset (a self-hosted copy that has not named its address), robots.txt is
# written without its Sitemap line and the other two files are absent.
# OPENV_NOINDEX set (staging) wins over both: robots.txt allows everything,
# so that the noindex header from 45-openv-noindex.sh is fetched and seen,
# and there is no sitemap or llms.txt to advertise.
#
# nginx serves each file by exact match (nginx.conf), so one that is not
# written here is a 404 rather than the app's index.html.
#
# OPENV_WEB_ROOT and OPENV_NGINX_CONF_DIR exist for the test
# (frontend/scripts/crawl-entrypoint.test.mjs); the image uses the defaults.
set -eu

root="${OPENV_WEB_ROOT:-/usr/share/nginx/html}"
out="${OPENV_NGINX_CONF_DIR:-/etc/nginx/openv}/robots-host.conf"
templates="$root/.openv-crawl"
placeholder="__OPENV_SITE_URL__"

site="${OPENV_SITE_URL:-}"
site="${site%/}"
host=""
if [ -n "$site" ]; then
    host="${site#http://}"
    host="${host#https://}"
    case "$site" in
        http://*|https://*) ;;
        *) host="" ;;
    esac
    # A host name with an optional port and nothing else: the value goes into
    # a sed replacement and an nginx map, so anything wider is refused.
    case "$host" in
        ""|*[!A-Za-z0-9.:-]*)
            echo "openv: OPENV_SITE_URL=$site is not scheme://host; crawl files written as if it were unset" >&2
            site=""
            host=""
            ;;
    esac
    host=$(printf '%s' "${host%%:*}" | tr 'A-Z' 'a-z')
fi

noindex=1
case "${OPENV_NOINDEX:-}" in
    ""|0|false|off|no) noindex="" ;;
esac

rm -f "$root/robots.txt" "$root/sitemap.xml" "$root/llms.txt"

robots_default=""
if [ -n "$noindex" ]; then
    printf '%s\n' \
        "# Generated at container start by 46-openv-crawl.sh." \
        "# This deployment is kept out of search results by X-Robots-Tag; the" \
        "# pages stay fetchable so that crawlers see it." \
        "User-agent: *" \
        "Allow: /" > "$root/robots.txt"
    echo "openv: robots.txt allows all; no sitemap (OPENV_NOINDEX set)"
elif [ ! -f "$templates/robots.txt" ]; then
    echo "openv: $templates is not in this build; no crawl files written" >&2
elif [ -n "$site" ]; then
    for f in robots.txt sitemap.xml llms.txt; do
        sed "s|$placeholder|$site|g" "$templates/$f" > "$root/$f"
    done
    robots_default="noindex, nofollow"
    echo "openv: robots.txt, sitemap.xml and llms.txt for $site; other host names are noindex"
else
    sed "/^Sitemap: /d" "$templates/robots.txt" > "$root/robots.txt"
    echo "openv: robots.txt without a sitemap (OPENV_SITE_URL unset)"
fi

{
    echo "# Generated at container start by 46-openv-crawl.sh; do not edit."
    echo "map \$host \$openv_host_robots {"
    echo "    default \"$robots_default\";"
    if [ -n "$robots_default" ]; then
        echo "    \"$host\" \"\";"
    fi
    echo "}"
} > "$out"
