@README.md

Before you finish a change here, run:
- `cd frontend && npx tsc --noEmit && npm run lint`
- `cd frontend && npx vitest run src/arch src/api src/test`
- after adding a route on the server too: `go test -count=1 ./internal/api` from the repository root

Don't:
- import `http.ts`, an area module or `types/` from outside `api/`; import `api/client` (K12)
- call a path the server does not register, or change one's method or shape without the server side (I23)
- reorder the barrel's `export` lines or load anything before `http.ts` (load order)
- rename or drop an export of the barrel in a refactor; the surface snapshot pins it (I23)
- read `err.response?.data?.error` inline; use `apiErrorMessage` from `errors.ts` (Q20)
