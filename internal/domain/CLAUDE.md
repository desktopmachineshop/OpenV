@README.md

Before you finish a change here, run:
- `go test -count=1 ./internal/domain/... ./internal/vocabparity ./internal/contract ./internal/archtest`
- after changing an event type, an exported type or a vocabulary: the regenerate command its recipe in README.md names

Don't:
- import `internal/api`, `internal/persistence` or an app service; declare a port and let `cmd/server` wire it (K7)
- rename a moved type, or turn an event payload map into a struct (R8, I10)
- change an event type string, a stored JSON key or `release/features.go` in a refactor (I10, I16, I25)
- fix a pinned Go/TS drift without a release note (I24)
