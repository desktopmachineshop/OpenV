@README.md

Before you finish a change here, run:
- `cd frontend && npx tsc --noEmit && npm run lint && npm test`
- after a route, stylesheet import or lazy view change: `cd frontend && npm run build && node scripts/bundle-check.mjs`
- the repository's quick gate, from the root: `make check-fast`

Don't:
- import from outside `frontend/` (the image copies only `frontend/`), or an `api/` module other than the four entry points (K12)
- make a lazy view eager or reorder stylesheet imports (I20); `ProjectList.css` stays eager
- derive the routes of `App.tsx` from data; its route JSX stays hand-written (K13)
- add an inline snapshot, an `EventSource` outside the shared hook, or an `err.response?.data?.error` read (Q20, Q21)
- change an id, class, label or role the e2e specs select on (I21)
- update a snapshot with `-u` in a refactor (R3)
