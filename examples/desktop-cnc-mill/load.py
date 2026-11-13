#!/usr/bin/env python3
"""Load the Desktop CNC Mill sample programme into an OpenV workspace.

    python3 examples/desktop-cnc-mill/load.py --check   # validate the seed, touch nothing
    python3 examples/desktop-cnc-mill/load.py --dump    # print the seed as JSON
    python3 examples/desktop-cnc-mill/load.py           # write it to the workspace

Reads OPENV_API_URL and OPENV_API_TOKEN (a workspace runner key), as
scripts/openv/sync.py does. It creates the fifteen projects that are not
there yet, files each under its parent, and then loads each one's
artifacts and links (the `refines` links cross from a child project to its
parent), its plan (tasks, milestones, what follows what, and which of a
parent's tasks wait for its children) and, on the mill, the release.

It is safe to run twice: a project and a task are matched by name, an
artifact by its title within its project, and a link by its two ends and
its type. Artifacts are created as drafts. Task progress is worked out
from the day the loader runs, so a task that has ended is done and one
under way is part done; a task that is already there is left as it is.
"""

import argparse
import json
import os
import sys
from datetime import date

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(HERE, "..", "..", "scripts", "openv"))

from mill_seed import FOLLOWS, PLANS, PROJECTS, RELEASE, SCHEDULE, WAITS_FOR_CHILDREN  # noqa: E402

# from-type, to-type for each link type the seed uses (docs/link-type-rules.md).
LINK_RULES = {
    "derives-from": ("requirement", "user-need"),
    "refines": ("requirement", "requirement"),
    "satisfies": ("design-item", "requirement"),
    "mitigates": ("design-item", "hazard"),
    "verifies": ("test-case", "requirement"),
    "validates": ("test-case", "user-need"),
}


def qualify(project, key):
    """A key in the seed is local to its project unless written CODE.KEY."""
    return key if "." in key else f"{project.code}.{key}"


def check():
    """Every key resolves, every link fits its rule, refines goes to the parent."""
    problems = []
    types, titles = {}, set()
    by_code = {p.code: p for p in PROJECTS}
    for p in PROJECTS:
        if p.parent and p.parent not in by_code:
            problems.append(f"{p.code}: parent {p.parent} is not a project")
        for a in p.artifacts:
            full = qualify(p, a["key"])
            if full in types:
                problems.append(f"duplicate key {full}")
            types[full] = a["type"]
            if (p.code, a["title"]) in titles:
                problems.append(f"{p.code}: two artifacts titled {a['title']!r}")
            titles.add((p.code, a["title"]))
    for p in PROJECTS:
        for a in p.artifacts:
            if a.get("parent") and qualify(p, a["parent"]) not in types:
                problems.append(f"{p.code}.{a['key']}: no parent {a['parent']}")
        for link in p.links:
            src, dst = qualify(p, link["from"]), qualify(p, link["to"])
            want = LINK_RULES.get(link["type"])
            if want is None:
                problems.append(f"{src}: unknown link type {link['type']}")
            elif dst not in types:
                problems.append(f"{src} {link['type']} {dst}: no such artifact")
            elif (types[src], types[dst]) != want:
                problems.append(f"{src} {link['type']} {dst}: needs {want[0]} to {want[1]}")
            target = dst.split(".")[0]
            if target != p.code and (link["type"] != "refines" or target != p.parent):
                problems.append(f"{src} {link['type']} {dst}: only refines may leave the project, and only to its parent")
        linked = {qualify(p, link["from"]) for link in p.links} | {qualify(p, link["to"]) for link in p.links}
        for q in PROJECTS:
            linked |= {qualify(q, link["to"]) for link in q.links}
        for a in p.artifacts:
            full = qualify(p, a["key"])
            if a["type"] in ("requirement", "design-item", "test-case") and full not in linked:
                problems.append(f"{full}: linked to nothing")
            if a["type"] == "requirement":
                if not any(link["type"] == "verifies" and qualify(p, link["to"]) == full for link in p.links) and not any(
                    link["type"] == "refines" and qualify(q, link["to"]) == full for q in PROJECTS for link in q.links
                ):
                    problems.append(f"{full}: no test case and no refinement")
                if p.parent and not any(link["type"] == "refines" and link["from"] == a["key"] for link in p.links):
                    problems.append(f"{full}: refines nothing in {p.parent}")
    return problems


def summary():
    for p in PROJECTS:
        counts = {}
        for a in p.artifacts:
            counts[a["type"]] = counts.get(a["type"], 0) + 1
        parts = ", ".join(f"{n} {t}" for t, n in sorted(counts.items()))
        print(f"{p.name} (under {p.parent or 'nothing'}): {parts}; {len(p.links)} links")


def progress(start, end, today, lead):
    """Percent complete of a task on the day the loader runs. A task that has
    ended is done; one under way is as far through as the calendar is, moved
    by the project's lead (ahead) or lag (behind) and rounded to 5."""
    if end < today:
        return 100
    if start > today:
        return 0
    span = max((end - start).days, 1)
    done = (today - start).days / span + lead
    return int(min(95, max(5, round(done * 20) * 5)))


def load_plan(c, p, pid, today):
    """The project's tasks, milestones and their links, matched by name."""
    tier, lead, titles = PLANS[p.code]
    plan = c.call("GET", f"/api/v1/projects/{pid}/plan") or {}
    have_ms = {m["name"]: m for m in plan.get("milestones") or []}
    cards = {w["title"]: w for w in (c.call("GET", f"/api/v1/projects/{pid}/work-items") or [])}
    if (plan.get("scheduling_mode") or "") != "manual":
        c.call("PUT", f"/api/v1/projects/{pid}/plan/settings", {"scheduling_mode": "manual"})

    release_key = ""
    if RELEASE["project"] == p.code:
        releases = {r["name"]: r for r in (c.call("GET", f"/api/v1/projects/{pid}/releases") or [])}
        rel = releases.get(RELEASE["name"]) or c.call(
            "POST", f"/api/v1/projects/{pid}/releases",
            {k: RELEASE[k] for k in ("name", "description", "target_date")} | {"status": "planned"},
        )
        release_key = "release:" + rel["id"]
        for a in c.call("GET", f"/api/v1/artifacts?project_id={pid}&type=requirement") or []:
            attrs = dict(a.get("attributes") or {})
            if not attrs.get("target_release"):
                attrs.pop("links_snapshot", None)
                attrs["target_release"] = rel["id"]
                c.call("PUT", f"/api/v1/artifacts/{a['id']}", {"type": a["type"], "title": a["title"], "body": a.get("body", ""), "attributes": attrs, "parent_id": a.get("parent_id"), "sort_order": a.get("sort_order")})

    def card(title, column):
        if title in cards:
            return cards[title]["id"], False
        made = c.call("POST", f"/api/v1/projects/{pid}/work-items", {"title": title, "column": column})
        return made["id"], True

    task_ids, created = {}, 0
    for phase, tasks, milestone in SCHEDULE[tier]:
        if milestone is None:
            ms_key = release_key
        else:
            name, when, criteria = milestone
            ms = have_ms.get(name) or c.call(
                "POST", f"/api/v1/projects/{pid}/plan/milestones",
                {"name": name, "target_date": when, "criteria": [{"metric": m, "target": t} for m, t in criteria]},
            )
            ms_key = ms.get("key") or ms["id"]
        # The phase's card sits where its tasks have got to.
        pcts = [progress(date.fromisoformat(s), date.fromisoformat(e), today, lead) for _, _, s, e in tasks]
        summary_id, new = card(phase, "done" if min(pcts) == 100 else "in-progress" if max(pcts) else "backlog")
        created += new
        for key, title, start, end in tasks:
            title = titles.get(key, title)
            a, b = date.fromisoformat(start), date.fromisoformat(end)
            pct = progress(a, b, today, lead)
            column = "done" if pct == 100 else "in-progress" if pct else "todo" if (a - today).days <= 45 else "backlog"
            tid, new = card(title, column)
            task_ids[key] = tid
            if not new:
                continue
            created += 1
            body = {"start_date": start, "due_date": end, "mode": "manual", "parent_id": summary_id, "percent_complete": pct, "milestone": ms_key}
            if pct:
                body["actual_start"] = start
            if pct == 100:
                body["actual_finish"] = end
            c.call("PUT", f"/api/v1/work-items/{tid}/schedule", body)
    for first, then in FOLLOWS:
        if first in task_ids and then in task_ids:
            c.call("PUT", f"/api/v1/work-items/{task_ids[then]}/relationships/{task_ids[first]}", {"type": "FS", "direction": "driven_by"})
    print(f"{p.name}: plan has {created} new tasks")
    return task_ids


def load(api, token):
    from sync import Client  # scripts/openv/sync.py

    c = Client(api, token=token)
    live = {pr["name"]: pr for pr in (c.call("GET", "/api/v1/projects") or [])}
    today = date.today()

    ids = {}  # CODE.KEY -> artifact id
    pids = {}
    for p in PROJECTS:  # a parent comes before its children in the seed
        if p.name not in live:
            live[p.name] = c.call("POST", "/api/v1/projects", {"name": p.name, "description": p.description})
            print(f"{p.name}: project created")
        pid = pids[p.code] = live[p.name]["id"]
        want = {"name": p.name, "description": p.description, "parent_project_id": pids[p.parent] if p.parent else ""}
        have = live[p.name]
        if any((have.get(k) or "") != v for k, v in want.items()):
            c.call("PUT", f"/api/v1/projects/{pid}", want)
        by_title = {a["title"]: a for a in (c.call("GET", f"/api/v1/artifacts?project_id={pid}") or [])}
        created, sort = 0, 0
        for a in p.artifacts:
            sort += 10
            full = qualify(p, a["key"])
            if a["title"] in by_title:
                ids[full] = by_title[a["title"]]["id"]
                continue
            body = {"project_id": pid, "type": a["type"], "title": a["title"], "body": a["body"], "sort_order": sort}
            if a.get("parent"):
                body["parent_id"] = ids[qualify(p, a["parent"])]
            if a.get("attributes"):
                body["attributes"] = a["attributes"]
            ids[full] = c.call("POST", "/api/v1/artifacts", body)["id"]
            created += 1
        print(f"{p.name}: {created} artifacts created, {len(p.artifacts) - created} already there")

    for p in PROJECTS:
        have = {(l["from_id"], l["to_id"], l["type"]) for l in (c.call("GET", f"/api/v1/links?project_id={pids[p.code]}") or [])}
        made = 0
        for link in p.links:
            key = (ids[qualify(p, link["from"])], ids[qualify(p, link["to"])], link["type"])
            if key not in have:
                c.call("POST", "/api/v1/links", {"from_id": key[0], "to_id": key[1], "type": key[2]})
                made += 1
        print(f"{p.name}: {made} links created, {len(p.links) - made} already there")

    tasks = {p.code: load_plan(c, p, pids[p.code], today) for p in PROJECTS}
    # A parent's integration task starts after each child has handed over.
    for p in PROJECTS:
        if not p.parent:
            continue
        waits = WAITS_FOR_CHILDREN[PLANS[p.parent][0]]
        c.call("PUT", f"/api/v1/work-items/{tasks[p.parent][waits]}/external-links/{tasks[p.code]['L']}", {"type": "FS"})
    print("plans linked across projects")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check", action="store_true", help="validate the seed and print its size")
    ap.add_argument("--dump", action="store_true", help="print the seed as JSON")
    ap.add_argument("--api", default=os.environ.get("OPENV_API_URL", ""))
    ap.add_argument("--token", default=os.environ.get("OPENV_API_TOKEN", ""))
    args = ap.parse_args()

    problems = check()
    if problems:
        raise SystemExit("the seed has problems:\n  " + "\n  ".join(problems))
    if args.dump:
        json.dump([vars(p) | {"_heading": None} for p in PROJECTS], sys.stdout, indent=1, ensure_ascii=False)
        return
    summary()
    if args.check:
        return
    if not args.api or not args.token:
        raise SystemExit("set OPENV_API_URL and OPENV_API_TOKEN")
    load(args.api, args.token)


if __name__ == "__main__":
    main()
