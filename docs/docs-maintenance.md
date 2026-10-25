# Maintaining the user manual

The user manual is an OpenD project in `manual/`. OpenD publishes it at
docs.openv.app and serves the app's context help from it, one release line
per OpenV minor version, so every workspace reads the manual for the
version it is on (`docs/plans/opend-help.md`). These are the rules that
keep it true. CI enforces the ones marked **(enforced)**.

## The docs travel with the code

1. **A change users can see changes the manual in the same pull request.**
   **(enforced)** The *Docs* job (`.github/workflows/docs.yml`) fails a pull
   request that does any of the following without touching the manual:
   - adds a `### New features` bullet to `RELEASE_NOTES.md`;
   - changes `frontend/src/pages.ts`;
   - changes the feature registry, `internal/domain/release/features.go`.

   The manual is `manual/` or, until the drawer moves, the chapters in
   `frontend/src/manual/chapters`. The `no-docs-change` label exempts a pull
   request whose change the manual already describes correctly, for example
   a fix that makes the product do what the manual says. Locally:
   `make check` (or `NO_DOCS_CHANGE=1` for a labelled pull request).

   The rule also covers what CI cannot see: a changed default, a renamed
   button, a new error message a user will search for, a removed option.
   Write the docs change with the code, not after it.

2. **The manual always validates.** **(enforced)**
   `scripts/manual/validate.sh` runs OpenD's own checks: broken links,
   unknown context keys, undeclared attribute values, duplicate contexts.
   It must report 0 errors and 0 warnings, in CI on every pull request and
   on master.

3. **Generated files match their source.** **(enforced)** Until the help
   drawer reads from OpenD, edit the chapter, then run
   `python3 scripts/manual/docs.py sync`. `sync --check` runs in
   `make check-fast`, `make check` and CI.

4. **The manual on master describes master.** Never write "coming soon" or
   "from 0.25": the release lines do the versioning. A change that only
   applies to some plans is marked with `product=` (the plan families in
   `opend.config.yaml`), not with prose.

5. **Context keys are an interface.** The app sends a key per page
   (`frontend/src/pages.ts`) and per help link. Renaming or dropping a
   key breaks help in every release that sends it, so:
   - add keys freely;
   - retire one by keeping it on the topic that replaced it.

   A new page gets a key, and a route in `manual/help-contexts.yaml`, in
   the pull request that adds the page.

## Before a release

Promote to release (`.github/workflows/promote-release.yml`) carries the
docs with the code. It changes nothing about who decides to promote: the
maintainer still asks for every release, and the docs steps run inside the
promotion, never on their own.

6. **Docs gate.** **(enforced)** Promotion refuses a master head whose
   checks are not all green, and the *Docs* job is one of them. A release
   whose manual would not build cannot ship.

7. **Stamp the line.** **(enforced)** The release commit sets
   `release:` in `manual/opend.config.yaml` to the new version's
   major.minor (`docs.py stamp`), so the manual on master names the line it
   documents. A patch release leaves the line as it is.

8. **Publish the line.** **(enforced)** After the release is pushed and
   tagged, the shipped commit is pushed to `docs/<major.minor>`. The docs
   site builds each `docs/*` branch as its own release line. A patch moves
   its line's branch on, so 0.24.2's docs replace 0.24.1's.
   The docs site follows on its own: its version menu lists every live line
   and its public default is the newest, so a new line becomes what
   visitors read as soon as its build is live. No credentials for the docs
   site are kept here.

9. **Before asking for a release, read the manual for what is going out.**
   The checks prove the manual builds; only a person can say it is right.
   For each `### New features` bullet under Unreleased:
   - open its topic;
   - check that the steps match the screens;
   - check that the help button on its page opens it.

   Run `opend preview manual --as <plan>` to read the manual as each plan's
   readers do.

## After a release

10. **Never push to a `docs/*` branch by hand.** Such a push is a fork of
    published docs that the next release would overwrite. A correction to a
    released line:
    - lands on master first;
    - then is cherry-picked onto each `docs/<line>` still served to a
      workspace (the stable line and the latest), with a pull request into
      that branch.

    The next release of the line moves past the cherry-pick, which master
    already carries.

11. **Keep every line still served.** A line stays published while any
    workspace, dedicated instance or supported self-hosted version is on
    it. The stable line and the latest line always are.

12. **The version menu lists the live lines.** OpenD's version picker
    offers every line still live, newest first; the public default is the
    latest release. Retiring a line (rule 11) means deleting its
    `docs/<line>` branch, which takes it off the menu.

## Where this lives

| What | Where |
|---|---|
| The manual | `manual/` (its `README.md` is the area guide) |
| The rules' code | `scripts/manual/docs.py`, `scripts/manual/validate.sh` |
| The CI job | `.github/workflows/docs.yml` |
| Release steps | `.github/workflows/promote-release.yml` ("Stamp the manual's release line", "Publish the manual's release line") |
| The plan | `docs/plans/opend-help.md` |
