# manual: the OpenV user manual

The user manual as an [OpenD](https://github.com/desktopmachineshop/OpenD)
DITA project. OpenD publishes it at docs.openv.app, one release line per
OpenV minor version, and serves the topics the app's help opens by context
key. `docs/plans/opend-help.md` is the plan that moves the app's help onto
it, and `docs/docs-maintenance.md` the rules for keeping it current.
`go run ./internal/tools/areas which <path>` names a file's area.

## Areas

| Area (`docs/areas.json`) | Files here |
|---|---|
| frontend-shell | `**` |

## Map

| Glob | What it holds |
|---|---|
| `opend.config.yaml` | the project: title, map, `release:` (the major.minor line this branch documents, stamped by Promote to release), the `product` values (OpenV plan families) |
| `help-contexts.yaml` | the `openv-web` app's routes to context keys, and its default key `start` |
| `preview-contexts.yaml` | sample readers for `opend preview manual --as <name>`, one per plan family |
| `filters/publish.ditaval` | the build-time filter: `audience="internal"` never publishes |
| `maps/manual.ditamap` | the navigation: one chapter topic per chapter, its sections beneath it (generated) |
| `topics/<chapter>/index.dita` | a chapter's introduction (generated) |
| `topics/<chapter>/<section>.dita` | one topic per `##` section of a chapter (generated) |
| `redirects.json` | each old `/manual/<slug>` and `/manual/<slug>#<heading>` link and the topic page it becomes (generated) |

## Invariants

- **One source.** Until the help drawer reads from OpenD, the app serves the
  markdown chapters in `frontend/src/manual/chapters`, and the generated
  files here are `scripts/manual/docs.py sync`'s output from them. Edit
  the chapter, then sync; never edit a generated file by hand. Once the
  drawer ships, the chapters go and the topics here become the source.
- **Context keys are stable.** A key in `resourceid` or
  `help-contexts.yaml` is what the app sends; renaming one breaks help
  for every release that sends it. Keys are dotted from general to specific
  (`vv.runs`), and fall back to their parents.
- **Docs describe the code beside them.** The manual on master documents
  master; each released line is a branch, `docs/<major.minor>`.

## Recipes

**Change the manual.** Edit the chapter in `frontend/src/manual/chapters`,
then `python3 scripts/manual/docs.py sync` and
`scripts/manual/validate.sh`.

**Give a section a context key.** Add it to `CHAPTER_KEYS` or
`SECTION_KEYS` in `scripts/manual/docs.py`, sync, and (for a page) map the
page's route in `help-contexts.yaml`.

**Read it as a reader would.** `opend preview manual --as business`, with
the OpenD CLI that `scripts/manual/validate.sh` builds into `.cache/opend`:
`node .cache/opend/packages/cli/dist/bin.js preview manual`.

## Guards

- **Generated files match the chapters.**
  `python3 scripts/manual/docs.py sync --check` (also in `make check-fast`)
- **OpenD validates the project**, 0 errors and 0 warnings.
  `scripts/manual/validate.sh`
- **A pull request that changes what users see changes the manual.**
  `python3 scripts/manual/docs.py check-pr --base <merge base>`, or the
  `no-docs-change` label
