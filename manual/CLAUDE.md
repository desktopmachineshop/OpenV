@README.md

Before you finish a change here, run:
- `python3 scripts/manual/docs.py sync --check` (the generated files match the chapters)
- `scripts/manual/validate.sh` (OpenD's checks: 0 errors, 0 warnings)
- `python3 -m unittest scripts/manual/docs_test.py` after a change to the generator

Don't:
- edit a generated topic, the map or `redirects.json` by hand: edit the chapter and sync
- rename or drop a context key the app sends, or point one at a different screen
- write "coming in 0.x" or other future tense: the manual on master describes master
- push to a `docs/*` branch except through Promote to release or a backport
