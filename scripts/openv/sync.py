#!/usr/bin/env python3
"""Maintain the OpenV requirements project from any machine or agent session.

The live OpenV project (workspace "Desktop Machine Shop", project "OpenV
Platform" by default) is the source of truth for WHAT the platform must do.
This tool exists so that keeping it current is a one-command operation from
any environment with Python 3 — no PowerShell, no extra dependencies.

Configuration (environment variables, overridable by flags):

  OPENV_API_URL     e.g. https://openv-production.up.railway.app
  OPENV_EMAIL       account to act as (must already exist, except `register`)
  OPENV_PASSWORD    its password
  OPENV_WORKSPACE   workspace name        (default: Desktop Machine Shop)
  OPENV_PROJECT     project name          (default: OpenV Platform)

Commands:

  register                     create the account (open registration)
  bootstrap [--def FILE]       idempotent full load of the seed definition:
                               ensure workspace + project, save profile,
                               create/update artifacts (matched by title),
                               create missing links, capture a baseline when
                               anything changed. Safe to re-run.
    [--invite-admin EMAIL]     also add EMAIL as workspace admin
  vv                           phase 2: verification methods + evidence run
  status [--def FILE]          live vs. seed counts and V&V coverage summary
  export [--out FILE]          download the live project export JSON
  api METHOD PATH [JSON]       authenticated ad-hoc call, e.g.
                               api POST /api/v1/artifacts '{"project_id":...}'

Never commit credentials. In Claude Code cloud sessions, set the OPENV_*
variables in the cloud environment configuration.
"""

import argparse
import http.cookiejar
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import date

DEFAULT_DEF = os.path.join(
    os.path.dirname(__file__), "..", "..", "examples", "openv-multi-agent-suite", "requirements.json"
)


class Client:
    def __init__(self, base, org_id=None):
        self.base = base.rstrip("/")
        self.org_id = org_id
        jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def call(self, method, path, body=None):
        req = urllib.request.Request(self.base + path, method=method)
        req.add_header("Content-Type", "application/json; charset=utf-8")
        if self.org_id:
            req.add_header("X-Org-ID", self.org_id)
        data = json.dumps(body).encode() if body is not None else None
        try:
            with self.opener.open(req, data=data) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            detail = e.read().decode(errors="replace")[:500]
            raise SystemExit(f"{method} {path} -> {e.code}: {detail}")
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return raw.decode(errors="replace")

    def login(self, email, password):
        self.call("POST", "/api/v1/auth/login", {"email": email, "password": password})

    def ensure_workspace(self, name, create=False):
        """Select the named workspace; only bootstrap may create it (create=True).

        Every other command fails loudly when the account can't see the
        workspace — silently creating one here once produced a duplicate
        empty workspace when the account had been removed from the real one.
        """
        orgs = (self.call("GET", "/api/v1/orgs") or {}).get("orgs") or []
        org = next((o for o in orgs if o.get("name") == name), None)
        if org is None and create:
            self.call("POST", "/api/v1/orgs", {"name": name})
            orgs = (self.call("GET", "/api/v1/orgs") or {}).get("orgs") or []
            org = next((o for o in orgs if o.get("name") == name), None)
        if org is None:
            raise SystemExit(
                f"workspace {name!r} is not visible to {getattr(self, 'email', 'this account')} — "
                "check OPENV_WORKSPACE, make sure the account is a member, or run bootstrap to create it"
            )
        self.org_id = org["id"]
        return org

    def find_project(self, name):
        projects = self.call("GET", "/api/v1/projects") or []
        return next((p for p in projects if p.get("name") == name), None)


def connect(args, need_project=True):
    c = Client(args.api)
    c.login(args.email, args.password)
    c.ensure_workspace(args.workspace)
    project = c.find_project(args.project)
    if need_project and project is None:
        raise SystemExit(f"project {args.project!r} not found — run bootstrap first")
    return c, project


def load_def(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def cmd_register(args):
    c = Client(args.api)
    name = args.name or args.email.split("@")[0]
    c.call("POST", "/api/v1/auth/register", {"email": args.email, "password": args.password, "name": name})
    print(f"registered {args.email}")


def cmd_bootstrap(args):
    definition = load_def(args.def_file)
    c = Client(args.api)
    c.login(args.email, args.password)
    org = c.ensure_workspace(args.workspace, create=True)
    print(f"workspace {args.workspace}: {org['id']}")

    if args.invite_admin:
        try:
            c.call("POST", f"/api/v1/orgs/{org['id']}/members", {"email": args.invite_admin, "role": "admin"})
            print(f"added {args.invite_admin} as workspace admin")
        except SystemExit as e:  # already a member is fine
            print(f"admin invite: {e}")

    project = c.find_project(args.project)
    if project is None:
        project = c.call(
            "POST",
            "/api/v1/projects",
            {
                "name": args.project,
                "description": "The requirements definition of OpenV itself: multi-agent development "
                "suite, multi-tenant SaaS, predictable BYO-AI execution.",
            },
        )
        print(f"project created: {project['id']}")
    else:
        print(f"project exists: {project['id']}")
    pid = project["id"]

    if definition.get("profile"):
        c.call("PUT", f"/api/v1/projects/{pid}/profile", definition["profile"])
        print("profile saved")

    live = c.call("GET", f"/api/v1/artifacts?project_id={pid}") or []
    by_title = {a["title"]: a for a in live}
    ids = {}  # def key -> live artifact id
    created = updated = 0
    sort = 0
    for a in definition["artifacts"]:
        sort += 10
        parent_id = None
        if a.get("parent"):
            if a["parent"] not in ids:
                raise SystemExit(f"parent {a['parent']} not created yet (artifact {a['key']})")
            parent_id = ids[a["parent"]]
        existing = by_title.get(a["title"])
        if existing is None:
            body = {
                "project_id": pid,
                "type": a["type"],
                "title": a["title"],
                "body": a.get("body", ""),
                "sort_order": sort,
            }
            if parent_id:
                body["parent_id"] = parent_id
            if a.get("attributes"):
                body["attributes"] = a["attributes"]
            made = c.call("POST", "/api/v1/artifacts", body)
            ids[a["key"]] = made["id"]
            created += 1
        else:
            ids[a["key"]] = existing["id"]
            want_attrs = dict(existing.get("attributes") or {})
            want_attrs.update(a.get("attributes") or {})
            if (
                existing.get("body") != a.get("body", "")
                or existing.get("type") != a["type"]
                or want_attrs != (existing.get("attributes") or {})
            ):
                body = {
                    "type": a["type"],
                    "title": a["title"],
                    "body": a.get("body", ""),
                    "attributes": want_attrs,
                }
                if existing.get("parent_id"):
                    body["parent_id"] = existing["parent_id"]
                if existing.get("sort_order") is not None:
                    body["sort_order"] = existing["sort_order"]
                c.call("PUT", f"/api/v1/artifacts/{existing['id']}", body)
                updated += 1
    print(f"artifacts: {created} created, {updated} updated, {len(definition['artifacts'])} total in seed")

    live_links = c.call("GET", f"/api/v1/links?project_id={pid}") or []
    have = {(l.get("from_id"), l.get("to_id"), l.get("type")) for l in live_links}
    made_links = 0
    for l in definition["links"]:
        key = (ids[l["from"]], ids[l["to"]], l["type"])
        if key not in have:
            c.call("POST", "/api/v1/links", {"from_id": key[0], "to_id": key[1], "type": key[2]})
            made_links += 1
    print(f"links: {made_links} created, {len(definition['links'])} total in seed")

    if created or updated or made_links:
        c.call("POST", f"/api/v1/projects/{pid}/baselines", {"name": f"sync {date.today().isoformat()}"})
        print("baseline captured")
    else:
        print("no changes — no baseline")


# Verification methods per requirement title; "" status means honestly not yet
# verified. Mirrors examples/openv-multi-agent-suite/load-vv.ps1.
VV_METHODS = {
    "Hierarchical versioned modules": ("demonstration", "verified"),
    "Typed artifacts with attributes": ("demonstration", "verified"),
    "Semantically constrained links": ("test", ""),
    "Artifact version history": ("demonstration", "verified"),
    "Project baselines": ("demonstration", "verified"),
    "Export and reporting": ("demonstration", "verified"),
    "Guided product definition wizard": ("demonstration", "verified"),
    "Interview invitation links": ("test", ""),
    "Conversational AI elicitation": ("test", ""),
    "Coverage computation": ("test", ""),
    "Traceability matrix": ("test", ""),
    "Gap reporting": ("test", ""),
    "Test run recording": ("demonstration", "verified"),
    "Personal spaces": ("demonstration", "verified"),
    "Company workspaces": ("test", ""),
    "Per-project access grants": ("test", ""),
    "Tenant isolation": ("test", ""),
    "Authentication": ("demonstration", ""),
    "GUI-editable agent definitions": ("demonstration", "verified"),
    "Lean agent context": ("analysis", "verified"),
    "Proposal-gated writes": ("test", ""),
    "User-composable crews": ("demonstration", "verified"),
    "Unified kanban": ("demonstration", "verified"),
    "Manual, scheduled and triggered automation": ("analysis", "verified"),
    "Code repository operation": ("demonstration", ""),
    "Bring-your-own-AI personal runners": ("demonstration", "verified"),
    "Personal runner scoping": ("test", ""),
    "First-refusal routing": ("test", ""),
    "Token-only hosted runners": ("test", ""),
    "Hosted repo exclusion": ("test", ""),
    "Hashed credentials, shown once": ("test", ""),
    "On-demand Agent Connector": ("test", ""),
    "Credential-free pairing links": ("test", ""),
}

VV_EVIDENCE = {
    "Authorization matrix suite": "go test internal/api authz matrix suite green",
    "Tenant isolation end-to-end": "Live two-account check: all cross-org access denied; team-grant escalation correct",
    "Claim routing suite": "Go claim-routing suite green; live grace-window reservation and hosted takeover confirmed",
    "Personal key rotation suite": "Go workerkeys suite green: rotation revokes prior key",
    "Hosted token-mode smoke test": "openv-worker image booted with API key: logged_in=true API key mode; repo runs excluded",
    "Connector pairing end-to-end": "Live on Windows host: deep-link pair, single-use + expiry enforced, agentd online, clean uninstall",
    "Agent proposal loop end-to-end": "Live run: claim -> run token -> proposal -> approve -> artifact materialized",
    "Link rule validation suite": "Go link validation suite green",
    "V&V dashboard verification": "Coverage, matrix and gaps verified against known project state after snake_case fix",
    "Interview elicitation flow": "Live: external link chat produced interview-tagged draft artifacts",
}


def cmd_vv(args):
    c, project = connect(args)
    pid = project["id"]
    artifacts = c.call("GET", f"/api/v1/artifacts?project_id={pid}") or []
    by_title = {a["title"]: a for a in artifacts}

    updated = 0
    for title, (method, status) in VV_METHODS.items():
        a = by_title.get(title)
        if a is None:
            print(f"MISSING: {title}")
            continue
        attrs = dict(a.get("attributes") or {})
        attrs["verification_method"] = method
        if status:
            attrs["verification_status"] = status
        body = {"type": a["type"], "title": a["title"], "body": a.get("body", ""), "attributes": attrs}
        if a.get("parent_id"):
            body["parent_id"] = a["parent_id"]
        if a.get("sort_order") is not None:
            body["sort_order"] = a["sort_order"]
        c.call("PUT", f"/api/v1/artifacts/{a['id']}", body)
        updated += 1
    print(f"requirements updated: {updated}")

    run = c.call(
        "POST",
        f"/api/v1/projects/{pid}/test-runs",
        {
            "name": f"Platform verification evidence {date.today().isoformat()}",
            "description": "Automated Go test suites (authz matrix, claim routing, key rotation, link "
            "rules) plus live end-to-end checks performed during the phase 1-3 and Agent Connector builds.",
        },
    )
    print(f"test run: {run['id']}")
    results = 0
    for title, notes in VV_EVIDENCE.items():
        tc = by_title.get(title)
        if tc is None:
            print(f"MISSING TC: {title}")
            continue
        c.call(
            "POST",
            f"/api/v1/test-runs/{run['id']}/results",
            {"test_case_id": tc["id"], "status": "pass", "notes": notes, "evidence": []},
        )
        results += 1
    print(f"results recorded: {results}")
    c.call("PUT", f"/api/v1/test-runs/{run['id']}", {"status": "completed"})
    coverage = c.call("GET", f"/api/v1/projects/{pid}/vv/coverage")
    print("summary:", json.dumps((coverage or {}).get("summary"), separators=(",", ":")))


def cmd_status(args):
    definition = load_def(args.def_file)
    c, project = connect(args)
    pid = project["id"]
    artifacts = c.call("GET", f"/api/v1/artifacts?project_id={pid}") or []
    links = c.call("GET", f"/api/v1/links?project_id={pid}") or []
    print(f"project: {project['name']} ({pid})")
    print(f"artifacts: {len(artifacts)} live / {len(definition['artifacts'])} in seed")
    print(f"links: {len(links)} live / {len(definition['links'])} in seed")
    live_titles = {a["title"] for a in artifacts}
    missing = [a["title"] for a in definition["artifacts"] if a["title"] not in live_titles]
    if missing:
        print("missing from live:", ", ".join(missing))
    coverage = c.call("GET", f"/api/v1/projects/{pid}/vv/coverage")
    print("coverage:", json.dumps((coverage or {}).get("summary"), separators=(",", ":")))


def cmd_export(args):
    c, project = connect(args)
    out = args.out or f"openv-export-{date.today().isoformat()}.json"
    data = c.call("GET", f"/api/v1/projects/{project['id']}/export")
    with open(out, "w", encoding="utf-8") as f:
        if isinstance(data, str):
            f.write(data)
        else:
            json.dump(data, f, indent=2)
    print(f"exported to {out}")


def cmd_api(args):
    c, _ = connect(args, need_project=False)
    body = json.loads(args.body) if args.body else None
    result = c.call(args.method.upper(), args.path, body)
    print(json.dumps(result, indent=2) if not isinstance(result, str) else result)


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--api", default=os.environ.get("OPENV_API_URL", "http://localhost:8080"))
    p.add_argument("--email", default=os.environ.get("OPENV_EMAIL"))
    p.add_argument("--password", default=os.environ.get("OPENV_PASSWORD"))
    p.add_argument("--workspace", default=os.environ.get("OPENV_WORKSPACE", "Desktop Machine Shop"))
    p.add_argument("--project", default=os.environ.get("OPENV_PROJECT", "OpenV Platform"))
    sub = p.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("register", help="create the account (open registration)")
    s.add_argument("--name", default=None)
    s.set_defaults(fn=cmd_register)

    s = sub.add_parser("bootstrap", help="idempotent full load of the seed definition")
    s.add_argument("--def", dest="def_file", default=DEFAULT_DEF)
    s.add_argument("--invite-admin", default=None)
    s.set_defaults(fn=cmd_bootstrap)

    s = sub.add_parser("vv", help="assign verification methods and record evidence run")
    s.set_defaults(fn=cmd_vv)

    s = sub.add_parser("status", help="live vs. seed counts and coverage")
    s.add_argument("--def", dest="def_file", default=DEFAULT_DEF)
    s.set_defaults(fn=cmd_status)

    s = sub.add_parser("export", help="download the live project export JSON")
    s.add_argument("--out", default=None)
    s.set_defaults(fn=cmd_export)

    s = sub.add_parser("api", help="authenticated ad-hoc API call")
    s.add_argument("method")
    s.add_argument("path")
    s.add_argument("body", nargs="?", default=None)
    s.set_defaults(fn=cmd_api)

    args = p.parse_args()
    if not args.email or not args.password:
        raise SystemExit("set OPENV_EMAIL and OPENV_PASSWORD (or --email/--password)")
    args.fn(args)


if __name__ == "__main__":
    main()
