@README.md

Before you finish a change here, run:
- `cd e2e && npm ci && npx playwright test --list` (every spec loads; no stack needed)
- against a running stack (README.md, "Run the suite"): `cd e2e && npx playwright test`
- after a layout change, with a production build served: `cd e2e && npm run audit:phone`

Don't:
- hard-code a user or rely on data a spec did not make: register per run (`makeRunId`, `makeUser`)
- add a `data-testid` or a CSS-path selector where a role, label, placeholder, title or text will do
- change a selector I21 freezes (`#type`, `#title`, `#body`, `.measure`, `.card`, `Search...`) in a refactor
- raise `workers` or make a journey parallel: each shares one page, serially
