# internal/archtest

Architecture tests for the Go backend, step S1 of the codebase refactor
plan (`docs/plans/codebase-refactor.md`, whose rule and step names, such as
K7, R6 or M6, this README uses). They freeze the import graph, check the
layering, cap file and function sizes, ban a few constructs, and hold
count ratchets on legacy idioms, so each can shrink but never spread. The
package has only `_test.go` files, this README and `ratchets.json`; it
builds nothing and imports nothing from the module.

## Running

```sh
go test ./internal/archtest                             # every rule, in well under a second
go test ./internal/archtest -v                          # also logs each rule's baseline
go test ./internal/archtest -run 'TestArchitecture/K7'  # one rule; subtest names use _ for spaces
```

CI runs it with the rest of `go test . ./cmd/... ./internal/...`.

How the code is read:

- **Scope.** The module root package and every package under `cmd/` and
  `internal/`, never `./...` (`frontend/node_modules` holds a Go package).
  `testdata` and directories starting with `.` or `_` are skipped, as the go
  command skips them.
- **Syntax only.** Files are parsed with `go/parser`; nothing is built or
  type-checked, so a run is fast and gives the same answer on every OS.
  Files count whatever their build constraints (a `_windows.go` file counts
  on Linux), except files no build selects (`//go:build ignore`).
- **Production code** means files not named `*_test.go`. Packages are
  written as module-relative directories (`internal/api`); the root package
  is `(root)`. A qualified name is resolved through the file's imports, so
  `json.NewEncoder` means `encoding/json` under any import name.

## ratchets.json

Every number and list the rules compare against lives in this one file.
Each entry is a ceiling that may only fall or an allowlist that may only
shrink.

| Key | Rule | Holds |
|---|---|---|
| `import_edges` | [Import edges](#import-edges) | importer → imported packages, for every production edge in the module |
| `layering_exceptions` | [K7 layering](#k7-layering) | edges that break K7 (none) |
| `client_domain_deps` | [Client binaries](#client-binaries) | per binary other than `cmd/server`, the domain packages it links |
| `domain_reflect` | [Domain reflection](#domain-reflection) | domain packages that import `reflect` |
| `file_lines` | [K14 file size](#k14-file-size) | a ceiling per grandfathered file |
| `func_lines` | [K14 function size](#k14-function-size) | a ceiling per grandfathered function |
| `side_effect_vars` | [Side-effecting package variables](#side-effecting-package-variables) | grandfathered `package:var` |
| `counts` | the five count ratchets below | one ceiling each |
| `env_reads` | [Direct env reads](#direct-env-reads) | a ceiling per package |

Any PR may lower or remove an entry. A refactor PR never raises or adds
one (the plan's Refactor guard job, S14b, is to refuse it), with one
exception: a class D PR may add `import_edges` entries into the package it
creates, and from it to packages the moved declarations' old home already
imports (the moved code's own dependencies: for P1, `snapshot` to
`artifacts`, `attachments`, `attributes`, `links` and `products`, which
`exports` imports today). The new package must still pass K7. Everything
else in the file may only shrink, in that PR too.
Outside a refactor, adding an entry by hand is an architecture decision
made in review; the usual cases are an import edge to a new package, which
must still pass K7, and the entry of a new client binary. A ceiling is
never raised to make a PR pass: change the code instead.

## Regenerating

```sh
UPDATE_RATCHETS=1 go test ./internal/archtest
```

rewrites `ratchets.json` with every entry tightened to the tree: counts
lowered to today's value, size ceilings lowered to the current size plus
headroom (never above the old ceiling), and entries no longer needed
removed. It never raises or adds anything: when any rule fails it writes
nothing and fails. Commit the result with the change that caused it.

Run it when an entry "can be tightened". `go test` hides a passing test's
log, so that note shows only with `go test -v ./internal/archtest`. An
untightened file still passes, so a loose entry lets code grow back up to
it; the plan's Refactor guard job (S14b) is to require refactor PRs to
commit the tightened file.

Tightening can race with a parallel PR. If one PR removes the last use of
an edge, or shrinks a count or a file, and tightens, while another PR adds
that edge or uses the old headroom, each PR is green alone, `ratchets.json`
merges without a conflict, and master goes red. So merge the latest master
into a tightening PR and re-run this test just before merging it. If master
does go red this way, restore the removed entry, or the previous ceiling,
by hand in a non-refactor PR: that restores master's prior state and is not
a raise.

`UPDATE_RATCHETS=bootstrap` creates the file from the tree, with R6
headroom on the size ceilings, and only when the file is missing. It was
used once, to create the file.

Every failure names the rule, the offending items, the allowlist, this
command and the README section:

```text
archtest rule "K7 layering" failed: 1 violation(s)
  - internal/domain/projects -> internal/api: a domain package may import only other internal/domain packages (...)
Allowlist: "layering_exceptions" in internal/archtest/ratchets.json (entries may be removed, never added)
Regenerate: UPDATE_RATCHETS=1 go test ./internal/archtest (it only lowers or removes entries; it refuses to raise or add one)
README: internal/archtest/README.md#k7-layering
```

## Import edges

**Enforces.** The production import graph between the module's packages is
frozen: every edge, importer to imported, from non-test files, must be
listed in `import_edges`. At `d11dee8` there are 227 edges between 63
packages. Imports made only by test files are not edges.

**Why.** K7: the layering can only improve if nothing new appears
unnoticed, and a class D move adds no edge except into the package it
creates and from it to its moved code's existing dependencies (see
[ratchets.json](#ratchetsjson)).

**Fix.** Drop the new import: declare the interface you need on the
consumer side, or move the code to a package that may import it. A
deliberate new dependency in a feature PR (a new domain package that
`internal/api` and `cmd/server` import, say) is added to `import_edges` by
hand, and K7 still applies to it.

**Regenerate.** When an edge disappears, `go test -v` logs it; the
regenerate command removes it. Until then the stale entry lets the edge
come back.

## K7 layering

**Enforces.** Over the production edges: a package under `internal/domain`
imports only other `internal/domain` packages and the types-only leaves
(besides the standard library and third-party modules);
`internal/persistence` imports only `internal/domain` and itself;
`internal/api` never imports `internal/persistence`; a types-only leaf
imports no package of this module. The types-only leaves are `typesLeaves`
in `graph_test.go`: `internal/workerproto` only, the package of worker wire
types P3 creates, whose `FinishRequest` `agentruns` then aliases. A leaf is
not a domain package, so it counts in no client binary's list. A new leaf
joins `typesLeaves` in a class T commit, in review. Nothing breaks this
today, so `layering_exceptions` is empty. `TestLayeringRule` pins the
rule.

**Why.** Dependencies point inward, and the domain stays testable without a
database or HTTP. The layering violations the analysis found are all closed
at runtime (setters, callbacks, raw SQL, reflection), none by an import;
this keeps it so.

**Fix.** Declare the port in the domain package (as `artifacts` does with
`LinkSuspector`) and let `cmd/server` wire the implementation.

**Regenerate.** Nothing to do: the list stays empty.

## Client binaries

**Enforces.** For each binary under `cmd/` but `cmd/server`, the set of
`internal/domain` packages it links, directly or not, may only shrink from
its entry in `client_domain_deps`. A new binary fails until its entry, with
the domain packages it links, is added by hand in review
(`TestClientBinaryDiscovery` proves this on a fixture). `cmd/agentd` links 8
(agentruns, agents, artifacts, events, providers, repoconns,
runnersessions, users), `cmd/openv-mcp` links artifacts, and
`cmd/openv-connector` and `cmd/openv-vapid` link none. The walk is the
same as `go list -deps`, over every build variant.

**Why.** These binaries run on operators' machines and are not rebuilt with
the server, so every server type they link is wire contract. P3 and P4b
shrink `cmd/agentd`'s list.

**Fix.** The message shows the import chain that pulls the package in. Cut
it, usually by moving the shared type to a leaf package with no domain
imports (`internal/workerproto`, `internal/mcp/toolnames`).

**Regenerate.** When a package drops out, run the regenerate command;
until then the stale entry lets it come back.

## Domain reflection

**Enforces.** The domain packages that import `reflect` may only shrink.
Today that is `internal/domain/links`, which calls `artifacts` through an
`interface{}` field and reflection.

**Why.** Reflection hides a dependency from the compiler and from the edge
list. X9 replaces it with a typed port and removes the entry.

**Fix.** Declare a typed interface on the consumer side.

**Regenerate.** After X9, run the regenerate command.

## K14 file size

**Enforces.** A production Go file that is not generated has at most 800
lines. The 13 files over it at `d11dee8` are grandfathered in `file_lines`
with a ceiling of their size plus R6 headroom: 10%, at most 150 lines.
`internal/api/handlers.go`, 3,369 lines, may reach 3,519.

**Why.** K14: small files are findable and conflict less. Headroom means a
feature PR touching a giant is never blocked. The ceiling never rises; it
falls when the regenerate command is run after a file shrinks, and the
plan's Refactor guard job (S14b) is to require that of refactor PRs, so a
split file cannot grow back.

**Fix.** Split the file by concern within its package (a class A move). A
new file over 800 lines is never grandfathered.

**Regenerate.** When a grandfathered file shrinks, the regenerate command
lowers its ceiling to the new size plus headroom, and removes the entry
once the file is within 800 lines.

## K14 function size

**Enforces.** A function or method in production code spans at most 100
lines, from its `func` line to its closing brace. The 34 over it at
`d11dee8` are grandfathered in `func_lines`, keyed `package:Func` or
`package:Receiver.Method`, with the same headroom: `cmd/server:main`, 853
lines, may reach 938. The key names no file, so a function keeps its
ceiling when it moves within its package; a renamed function is a new one.

**Why.** K14, as for files.

**Fix.** Extract named helpers, called at the same point.

**Regenerate.** As for files.

## No init functions

**Enforces.** No `func init()` in production code. There is none today.

**Why.** K9: an `init` runs at import time, in an order nobody reads, in
every binary and test that links its package. Registries (migrations, MCP
tools, routes) are explicit ordered lists.

**Fix.** Call the setup from `main` or a constructor. There is no
allowlist.

**Regenerate.** Nothing to regenerate.

## Side-effecting package variables

**Enforces.** A package-level `var` in production code is initialised by
pure calls only: the functions and conversions in `pureFuncs`
(`errors.New`, `fmt.Errorf`, `regexp.MustCompile`, `template.Must`,
`sync.OnceValue` and others), any function of a standard package in
`purePackages` (`strings`, `strconv`, `bytes` and others), the builtins in
`pureBuiltins` (`append`, `make` and others), type conversions (including
to a type of this module), and methods on a value one of those just built.
A function literal's body is not inspected, since it runs only when called;
an immediately invoked one is. Anything else, such as a call to a function
of the module, a third-party constructor, `time.Now`, `os.Getenv` or
`log.New`, makes the variable side-effecting. The 5 at `d11dee8` are
grandfathered in `side_effect_vars`, keyed `package:var`: `quality`'s
`vagueQuantifierRe`, `placeholderRe` and `conventionPatterns` (built by its
own `wordListRegexp`), `reports:linkTypeLabels` (`buildLinkTypeLabels()`)
and `reports/doc:parser` (`goldmark.New`). All five are pure in fact; the
rule cannot see inside the functions they call.

**Why.** Package initialisation runs before `main` and in every test
binary. An environment read, file or network access, clock read or global
registration there cannot be ordered, configured or faked.

**Fix.** Build the value in the function that needs it, or wrap the call
in `sync.OnceValue` so it runs on first use. A pure standard-library
function can join `pureFuncs` in `bans_test.go`, in a class T change.

**Regenerate.** When a grandfathered variable goes or turns pure, run the
regenerate command.

## One router

**Enforces.** One router: the one `mux.NewRouter()` call in `cmd/server`
(`main.go` today). In production code a second `mux.NewRouter()`, in
`cmd/server` or anywhere else, and any `http.NewServeMux()` fail, as do
`.Subrouter(...)`, `.PathPrefix(...)`, `NotFoundHandler` and
`MethodNotAllowedHandler` (set on a value or in a composite literal). There
is none today. `TestRouterRules` proves the rule on a fixture.

**Why.** I2 and K2: there is one gorilla/mux router, every route is a full
template registered on it in an order the route goldens pin, and 404 and
405 answers are gorilla's defaults (I6).

**Fix.** Register the full path template in the area's registrar. There is
no allowlist.

**Regenerate.** Nothing to regenerate.

## HandleFunc outside registrars

**Enforces.** Counts route registrations in production code outside a
registrar, a function named `register<Area>Routes`. A registration is a
two-argument `.HandleFunc(...)` or `.Handle(...)` call, or a one-argument
`.HandlerFunc(...)` or `.Handler(...)` that ends a route-builder chain
(`r.Path(p).Methods(m).HandlerFunc(h)`, `r.NewRoute().Handler(h)`); a
conversion such as `http.HandlerFunc(fn)` is not. The ceiling
`counts.handle_func_outside_registrars` is 53 at `d11dee8`: the 52 routes
`RegisterRoutes` registers inline, `/health` included, and `/metrics` in
`cmd/server/main.go`.

**Why.** K1: an area's routes live in its registrar, and `RegisterRoutes`
is only the ordered list of registrar calls. M6 moves the inline routes.

**Fix.** Register the route in the area's `register<Area>Routes`.

**Regenerate.** Lower the ceiling as the count falls.

## Handler literals in tests

**Enforces.** Counts `api.Handler` composite literals (`&Handler{...}` or
`Handler{...}`, or `api.Handler` from another package) in test files other
than `testkit_test.go`. The ceiling `counts.handler_literals_in_tests` is
137, in 60 files, at `d11dee8`. `NewHandler`'s own literal is production
code and not counted.

**Why.** K6: tests build handlers with `newTestHandler` (M13), so a new
dependency is one option, not sixty edits.

**Fix.** Build the handler with `newTestHandler` from `testkit_test.go`
(added by M13a).

**Regenerate.** M13a–M13d migrate the literals; lower the ceiling as they
go.

## Raw JSON encodes

**Enforces.** Counts `json.NewEncoder(...)` calls in `internal/api` and its
subpackages outside `respond.go`. The ceiling `counts.raw_json_encodes` is
249 at `d11dee8`, every one `json.NewEncoder(w).Encode(...)`.

**Why.** K4: responses are written through the helpers in `respond.go`
(X1 adds `writeJSON` and `writeJSONBare`), so headers are decided in one
place.

**Fix.** Write through `respondJSON` (in `evidence_handlers.go` until M5
moves it to `respond.go`) or the X1 writers. Encodes inside `respond.go`
are not counted.

**Regenerate.** X1 takes the count to 0; lower the ceiling as it falls.

## Invalid request body literals

**Enforces.** Counts the string literal `"invalid request body"` in
`internal/api`. The ceiling `counts.invalid_request_body_literals` is 106
at `d11dee8`.

**Why.** K4: bodies are read through X1's `decodeJSON`, which holds the one
literal that remains (106 to 1).

**Fix.** Decode through the shared helper rather than writing the literal.
A new decode needed before X1 lands introduces `decodeJSON` in
`respond.go` and converts at least one existing call site in the same PR,
so the count does not rise.

**Regenerate.** Lower the ceiling as the count falls.

## require helpers outside authz

**Enforces.** Counts functions and methods named `require<Something>`
declared in `internal/api` outside `authz.go`. The ceiling
`counts.require_outside_authz` is 10 at `d11dee8`; M5 moves 8 of them,
leaving `requireWritable` (`limits.go`) and
`requireAttributeDefinitionWrite`.

**Why.** K3: every guard has one home, so the checks a handler runs can be
read in one file.

**Fix.** Declare the guard in `authz.go`.

**Regenerate.** Lower the ceiling after M5.

## Direct env reads

**Enforces.** Counts references to `os.Getenv`, `os.LookupEnv`,
`os.ExpandEnv`, `os.Environ`, `syscall.Getenv` and `syscall.Environ` in
production code under `internal/`, per package, against `env_reads`; a
package not listed may make none. A reference is a call or a function value
(`var getenv = os.Getenv`, `os.Expand(s, os.Getenv)`); a call counts once.
At `d11dee8` there are 44, all calls, in 8 packages: 39
`Getenv`/`LookupEnv` calls on 38 lines (`runner/geminicli.go:187` has two)
and 5 `os.Environ` calls in `internal/runner`. (The plan's figures, 38 and
43, count lines.) `TestEnvReadForms` proves the forms on a fixture.

**Why.** K8: configuration is read in `internal/config` and
`cmd/*/config.go`, apart from S8's reasoned exemptions (per-request reads,
`OPENV_MCP_TOOLS`, the runner's per-run reads). The per-package ceilings
stop reads spreading while X10 moves them out.

**Fix.** Take the value as a parameter or config field wired from the
composition root. A read that must happen at run time is argued as an S8
exemption, in review.

**Regenerate.** Lower the ceilings as X10 moves reads out.

## R8 decode errors

**Enforces.** In `internal/api`, a handler may not write to the response
the error of a JSON decode (`json.Unmarshal`, or `Decode` on a
`json.NewDecoder`) into a type that is, or contains, a type on the alias
list: `decodeAliasTypes` in `decode_test.go`. Writing means a call in the
`if err != nil` branch that takes the `http.ResponseWriter` and formats the
error (`err.Error()`, or `err` passed to `fmt.Sprint*` or `fmt.Errorf`).
Passing `err` itself to `respondError`, which logs it and answers a fixed
message, is fine. The check follows local variables, struct fields and the
module's named types. It does not follow a value through a helper function;
instead `decodeErrorSources`, also in `decode_test.go`, maps the name of a
function or method whose returned error carries such a decode error to the
alias type, and an error assigned from a call by that name counts as the
decode's. The sites known today are `Handler.projectExport`
(`suite_handlers.go`); the export service's `ImportProject`
(`handlers.go:1892`) and `ImportProjectWithOverrides` (`handlers.go:2168`),
which decode in `internal/domain/exports`, outside `internal/api`; and the
report service's `GenerateProjectReport` and `GenerateProjectReportDOCX`
(through `loadReportExport`) and `GenerateVVReport`. Both lists are empty
today; P1 adds `ProjectExport` with those six names, after checking for
others, and P3 `FinishRequest`, each in a class T commit before its move,
and X14 adds `snapshot`'s `Load`. The scan follows an error only through the
later statements of the list it was assigned in, so it cannot follow
`Handler.GenerateReport`, which assigns the error inside a `switch` case
and tests it after the switch; that site answers with a fixed message
(`respondError` or `respondInternal`) today and must keep doing so. `TestDecodeAliasRule` proves the check on a
fixture.

**Why.** R8: encoding/json's errors name the Go type, package qualified, so
moving a type behind an alias would change response bytes.

**Fix.** Answer with a fixed message and log the error with
`respondError`.

**Regenerate.** Nothing in `ratchets.json`; the alias list only grows.

## Build context

**Enforces.** For each Go image built from the repository root,
`Dockerfile.api` and `Dockerfile.worker`, the rule reads the `COPY` and
`ADD` sources (not `--from` copies) that land at their own path in the
directory where `go build` runs, and the `go build` targets of its `RUN`
lines. Every non-test file of every package a target links, every
`//go:embed` pattern in those packages, and `go.mod` and `go.sum` must lie
inside those paths. For `Dockerfile.api` so must every `//go:embed` pattern
in the module, and its targets must include `cmd/server`. A linked package
outside the root package, `cmd/` and `internal/` fails whatever the `COPY`
list says, because archtest does not read its files. Today
`Dockerfile.api` copies `go.mod`, `go.sum`, `cmd`, `internal`, `examples`,
`release_notes.go` and `RELEASE_NOTES.md` and builds `cmd/server`,
`cmd/agentd`, `cmd/openv-mcp` and `cmd/openv-connector`;
`Dockerfile.worker` copies `go.mod`, `go.sum`, `cmd` and `internal` and
builds `cmd/agentd` and `cmd/openv-mcp`. `TestDockerfileParsing` pins the
parser.

**Why.** I26: the Dockerfiles copy an allowlist of paths and CI's Go jobs
never build the images, so a new top-level package, root file or embedded
asset would break only the Docker build.

**Fix.** Keep Go code under `cmd/` or `internal/`. For a root file or an
embedded asset, add its path to the Dockerfile's `COPY` list.

**Regenerate.** Nothing to regenerate: the `COPY` list is the allowlist.
