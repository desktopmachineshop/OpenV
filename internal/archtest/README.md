# internal/archtest

Architecture tests for the Go backend, step S1 of the codebase refactor
plan (`docs/plans/codebase-refactor.md`, whose rule and step names, such as
K7, R6 or M6, this README uses). They freeze the import graph, check the
layering, cap file and function sizes, ban a few constructs, and hold
count ratchets on legacy idioms, so each can shrink but never spread. The
package has only `_test.go` files, this README, `ratchets.json` and the
goldens of step S8 under `testdata/`; it imports nothing from the module.
The architecture rules build nothing; the
[env var inventory (S8)](#env-var-inventory-s8) type-checks the module.

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
  The env var inventory, which is not a rule of `TestArchitecture`, is the
  one exception.
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
| `helper_homes` | [K3 helper homes](#k3-helper-homes) | grandfathered `package:Func` or `package:Receiver.Method` helpers declared in an area file and used from another file |
| `api_spec_undocumented_routes` | [API spec drift](#api-spec-drift) | a ceiling on the routes `docs/api-spec.md` does not document |

Any PR may lower or remove an entry. A refactor PR never raises or adds
one (the Refactor guard job, S14b, refuses it), with the
exceptions S14b lists. A class D PR may add entries for the package it
creates: `import_edges` into it, and from it to packages the moved
declarations' old home already imports (the moved code's own dependencies:
for P1, `snapshot` to `artifacts`, `attachments`, `attributes`, `links` and
`products`, which `exports` imports today), and, for a new domain package,
its name in the `client_domain_deps` of a binary that linked the old
package (P2b's `tokens` for `cmd/agentd`). The edges into it include the
first edge of a package that imported no package of this module before,
which adds that package's key, listing only packages the PR creates (P2b's
`users` and `interviews`, each listing `tokens`). The new package must still
pass K7. A class T commit that adds a rule may add that rule's key, holding
only what the tree has (M5's K3 allowlist). A class E commit may add an
`import_edges` edge listed in `X14B_IMPORT_EDGES`
(`scripts/refactor/refactor_guard.py`, read from the PR), into a package
the base already has, as a new target in an importer's list or a new
importer's key: a named exception (plan §8.3) that X14b fills in its own
class E commit for the `internal/api` → `internal/domain/snapshot` edge its
loader adds. Everything else in the file may only shrink, in that PR too.
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
it; the Refactor guard job (S14b) requires refactor PRs to
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
types P3 creates, whose `FinishRequest` and `LogEntry` `agentruns` then
aliases. A leaf is
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
(`TestClientBinaryDiscovery` proves this on a fixture). `cmd/agentd` links 7
(agentruns, agents, events, providers, repoconns, runnersessions, users;
artifacts went with P4b), `cmd/openv-mcp` links artifacts, and
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
lines. The 13 files over it at `d11dee8` were grandfathered in `file_lines`
with a ceiling of their size plus R6 headroom: 10%, at most 150 lines
(`internal/api/handlers.go`, then 3,369 lines, could reach 3,519). Nine
have since gone under the budget, `handlers.go` among them; four remain:
`exports/reqif.go` and `reports`' `report.go`, `pdf_report.go` and
`docx_report.go`.

**Why.** K14: small files are findable and conflict less. Headroom means a
feature PR touching a giant is never blocked. The ceiling never rises; it
falls when the regenerate command is run after a file shrinks, and the
Refactor guard job (S14b) requires that of refactor PRs, so a
split file cannot grow back.

**Fix.** Split the file by concern within its package (a class A move). A
new file over 800 lines is never grandfathered.

**Regenerate.** When a grandfathered file shrinks, the regenerate command
lowers its ceiling to the new size plus headroom, and removes the entry
once the file is within 800 lines.

## K14 function size

**Enforces.** A function or method in production code spans at most 100
lines, from its `func` line to its closing brace. The 34 over it at
`d11dee8` were grandfathered in `func_lines`, keyed `package:Func` or
`package:Receiver.Method`, with the same headroom (`cmd/server:main`, then
853 lines, could reach 938, until M4 cut it to about 50); 30 remain. The
key names no file, so a function keeps its
ceiling when it moves within its package; a renamed function is a new one.
A method that moves to another receiver in its package keeps its ceiling
under the new key: the Refactor guard accepts the new key when the old one
goes in the same commit and the value does not rise, as M15b's sign-in
methods moved from `Worker` to `loginBroker`.

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
to a type of this module), and methods on a value a pure standard-library
function (`pureFuncs`, `purePackages`) just built. A method on a
conversion's or a builtin's result is not pure: that value may be of this
module's type, whose methods are its own code (`someType(nil).load()`,
`(*someType)(nil).load()`, `new(someType).load()`). A function literal's
body is not inspected while the literal is only kept, as a value, through a
conversion (`http.HandlerFunc`) or by the `sync.Once` wrappers
(`funcStorers`), or by a builtin such as `append`, since it runs only when
called. An immediately invoked literal is impure, and a literal passed to a
pure call that may run it, such as an iterator `maps.Collect` runs, has its
body held to the same rule, calls of its own parameters (the iterator's
`yield`) counting as pure. Anything else, such as a call to a function of
the module, a third-party constructor, `time.Now`, `os.Getenv` or
`log.New`, makes the variable side-effecting. `TestPackageVarRules` proves
the rule on a fixture, the two forms #379's bug 97 found slipping past it
among them. The 5 at `d11dee8` are grandfathered in `side_effect_vars`,
keyed `package:var`: `quality`'s `vagueQuantifierRe`, `placeholderRe` and
`conventionPatterns` (built by its own `wordListRegexp`),
`reports:linkTypeLabels` (`buildLinkTypeLabels()`) and `reports/doc:parser`
(`goldmark.New`). All five are pure in fact; the rule cannot see inside the
functions they call.

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
(`buildHTTPHandler`, `http.go`, since M4). In production code a second `mux.NewRouter()`, in
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
`counts.handle_func_outside_registrars` was 53 at `d11dee8`: the 52 routes
`RegisterRoutes` registered inline, `/health` included, and `/metrics` in
`cmd/server/main.go`. M6 moved the 52 into registrars, so it is 1:
`/metrics`, now in `cmd/server/http.go`.

**Why.** K1: an area's routes live in its registrar, and `RegisterRoutes`
(`routes.go`) is only the ordered list of registrar calls.

**Fix.** Register the route in the area's `register<Area>Routes`.

**Regenerate.** Lower the ceiling as the count falls.

## Handler literals in tests

**Enforces.** Counts `api.Handler` composite literals (`&Handler{...}` or
`Handler{...}`, or `api.Handler` from another package) in test files other
than `testkit_test.go`. The ceiling `counts.handler_literals_in_tests` is
0 since M13a–M13d (137, in 60 files, at `d11dee8`). `NewHandler`'s own
literal is production code and not counted.

**Why.** K6: tests build handlers with `newTestHandler` (M13), so a new
dependency is one option, not sixty edits.

**Fix.** Build the handler with `newTestHandler(t, opts...)` from
`testkit_test.go`, setting each field in an option:
`newTestHandler(t, func(h *Handler) { h.exportService = fake })`.

**Regenerate.** Nothing to regenerate: the ceiling is 0.

## Raw JSON encodes

**Enforces.** Counts `json.NewEncoder(...)` calls in `internal/api` and its
subpackages outside `respond.go`. The ceiling `counts.raw_json_encodes` is
249 at `d11dee8`, every one `json.NewEncoder(w).Encode(...)`.

**Why.** K4: responses are written through the helpers in `respond.go`
(X1 adds `writeJSON` and `writeJSONBare`), so headers are decided in one
place.

**Fix.** Write through `respondJSON` (`respond.go`, where M5 moved it) or
the X1 writers. Encodes inside `respond.go`
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
`counts.require_outside_authz` was 10 at `d11dee8`; M5 moved 8 of them, so
it is 2: `requireWritable` (`limits.go`) and
`requireAttributeDefinitionWrite` (`attribute_definition_handlers.go`).

**Why.** K3: every guard has one home, so the checks a handler runs can be
read in one file.

**Fix.** Declare the guard in `authz.go`.

**Regenerate.** Lower the ceiling as the count falls.

## K3 helper homes

**Enforces.** An unexported function or method declared in a
`*_handlers.go` file (an area file, K1) of `internal/api`, or of a package
below it, may be used only in that file. One referenced from another
production file of the package fails, unless `helper_homes` lists it,
keyed `package:Func` or `package:Receiver.Method` as `func_lines` keys it,
so a helper keeps its entry when it moves between area files. `register<Area>Routes` is K1's,
not a helper, and is left out; so are exported names, `handlers.go`, which
is not an area file, and test files. The allowlist is the 33 helpers the
tree had when M5 added the rule, which M5 shrinks by the ones it moves.

The rule reads syntax only: a function counts as used where its bare name
appears (a call or a function value), and a method where a selector names
it (`h.helper`, `h.helper(...)`), whatever the receiver, so a local, a
field or another type's method spelled like a helper counts too. Rename
the look-alike if that ever bites.

**Why.** K3: a helper that several areas use has one home, so it is found
where its kind lives, not in whichever area happened to need it first, and
an area file can move (M6) without dragging another area's helpers along.

**Fix.** Move the helper to its home: `respond.go` (JSON in and out),
`httperr.go` (error writers), `errmap.go` (per-area error tables),
`authz.go` (every `require*`), `publish.go`, `cookies.go` or a
`middleware_*.go` file, or another file that is not an area file;
otherwise keep it in the one area file that uses it.

**Regenerate.** Remove an entry once its helper has moved or is used by one
file; `UPDATE_RATCHETS=1` does it.

## K5 raw HandlerDeps reads

**Enforces.** In `internal/api`, production and test files alike, only
`handlers.go` names the raw `HandlerDeps` settings that `NewHandler` derives
private values from: `FrontendURL`, `SecureCookies` and `CrossSiteCookies`
(`rawDeps` in `handler_deps_test.go`). Since M14 `Handler` embeds
`HandlerDeps`, so `h.FrontendURL` compiles; anywhere else a selector naming
one fails, a write as much as a read: `h.FrontendURL`, `fx.h.SecureCookies`,
`h.HandlerDeps.CrossSiteCookies`. There is none today. The rule reads syntax
only, so it cannot tell a `Handler` from another type: a selector of a
dependency the handler holds (`h.GoogleOAuth.FrontendURL`,
`h.OIDC.FrontendURL`: one whose operand is a selector naming a field of
`Handler` or `HandlerDeps` other than the embedded `HandlerDeps`) reads that
dependency's own field and passes, and any other look-alike, such as a
local `cfg.FrontendURL`, fails; rename the look-alike or read it through
the handler's field if that ever bites. A `HandlerDeps{...}` composite
literal key is not a selector, so a test still sets the settings there. The
rule also fails if `HandlerDeps` no longer declares one of the three, so a
rename cannot switch it off unseen. `TestRawHandlerDeps` proves it on a
fixture.

**Why.** K5: a dependency is declared once, as a `HandlerDeps` field, and
the values `NewHandler` derives stay private. Read raw, the frontend URL
keeps a trailing slash that the share links, previews and billing return
URL built from `h.frontendURL` do not have, and the cookie flags miss
`CrossSiteCookies` forcing `Secure` on with `SameSite=None` (the S4 cookie
profiles), so a handler that read them would behave differently from its
neighbours.

**Fix.** Read the derived value: `h.frontendURL` for `FrontendURL`,
`h.secureCookies` for `SecureCookies`, and `h.cookieSameSite` with
`h.secureCookies` for `CrossSiteCookies`; or the cookie helpers in
`cookies.go`. A new derived value is computed in `NewHandler`, in
`handlers.go`. There is no allowlist.

**Regenerate.** Nothing to regenerate.

## Direct env reads

**Enforces.** Counts references to `os.Getenv`, `os.LookupEnv`,
`os.ExpandEnv`, `os.Environ`, `syscall.Getenv` and `syscall.Environ` in
production code under `internal/`, per package, against `env_reads`; a
package not listed may make none. A reference is a call or a function value
(`var getenv = os.Getenv`, `os.Expand(s, os.Getenv)`); a call counts once.
At `d11dee8` there are 44, all calls, in 8 packages: 39
`Getenv`/`LookupEnv` calls on 38 lines (`runner/geminicli.go:187` has two)
and 5 `os.Environ` calls in `internal/runner`. `TestEnvReadForms` proves
the forms on a fixture.

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
instead `decodeErrorSources`, also in `decode_test.go`, maps a function or
method whose returned error carries such a decode error to the alias type,
and an error assigned from a matching call counts as the decode's. A key is
a bare name, which matches a call of any function or method by that name,
or `<package>.<Func>` with the module-relative package, as on the alias
list, which matches only a call of that package's function through an
import of it, under whatever name the file gives the import. The alias
list names `ProjectExport` under
`internal/domain/exports` and `internal/domain/snapshot`, which P1 added in
a class T commit before moving it, and `FinishRequest` and `LogEntry` under
`internal/domain/agentruns` and `internal/workerproto`, which P3 added the
same way. The sources are `Handler.projectExport` (`project_snapshot.go`); the report
service's `GenerateProjectReport` and `GenerateProjectReportDOCX` (through
`loadReportExport`) and `GenerateVVReport`; and the download service's
`Options` (through `reports.LoadReportExport`); and
`internal/domain/snapshot.Load`, the loader X14b writes, which X14a added
ahead of it, keyed by its package because `Load` is a common name
(`runnersessions.PoolCounts.Load`, `sync.Map`'s, the planned
`config.Load`). Four paths that decode a `ProjectExport` are not on it. The export
service's `ImportProject` and `ImportProjectWithOverrides`, and the
template service's `CreateProjectFromTemplate`, which calls the latter,
return `exports.ErrMalformedImport` with a description of the refusal in
the terms of the file, not the decode error, since #508 (#379 bug 184;
`TestImportRefusalsNameNoGoType` in `internal/domain/exports` pins it), so
their error names no Go type. The download service's `Download` decodes as
`Options` does, but `serveDownload` writes `err.Error()` for
`ErrUnsupportedFormat` in its `if err != nil` branch, which the rule would
flag, and answers every other error with a fixed message, kept by review
(R8). A `FinishRequest` is decoded only by `Handler.FinishAgentRun`, in
`internal/api`, which the rule reads itself; a `LogEntry`, in the worker's
log push, by the helper `decodeRunLogBody`, a source, whose caller
`Handler.AppendAgentRunLogs` answers a fixed message. The scan follows an
error only through the
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

## Build by package path

**Enforces.** No README, doc, script, `Makefile`, Dockerfile, compose file
or workflow builds or runs a single `.go` file under `cmd/` (step M1): a
`go build`, `go run` or `go install` whose arguments name a `.go` file
there, with or without `./`, fails, naming the file, the line and the
package command to use instead. The rule reads, besides Go code, every
`*.md`, every `README*` file, `.txt` files under `docs/`, `*.sh`, `*.bash`,
`*.ps1`, every file under `scripts/`, `Makefile`s and `*.mk`, Dockerfiles,
compose files and the YAML under `.github/`. It joins lines ending in `\`,
cuts a line into commands at Markdown code-span ticks and at the shell's
unquoted separators, and splits a command into words as a shell would,
reading a quoted span that holds a blank as a line of its own; so a fenced
block, an inline code span, a `RUN` continued over lines, a command inside
`sh -c '...'` and a Python argument list (`["go", "build", ...]`) all
count. A `go run` stops at its package, since what follows is the
program's arguments. It skips test files (`*_test.*`, `*.test.*`,
`*.spec.*`), which quote commands as fixtures; the directories the go
command skips (`testdata`, names starting with `.` or `_`), except
`.github`; `node_modules`; and the records in `buildPathRecords`
(`buildpath_test.go`), which quote the commands M1 replaced: the refactor
plan and the dated assessments under `docs/assessments/`.
`TestBuildByPackagePath` proves the rule on a fixture.

**Why.** Given files, `go build` and `go run` compile those files alone,
not their package. `cmd/server` is several files from M1 on, so a build
of `cmd/server/main.go` alone stops compiling, and it fails only where it
runs: in the API image, which CI's Go jobs never build, or in a README
step nobody runs in CI.

**Fix.** Name the package: `go build ./cmd/server`, `go run ./cmd/server`.
There is no allowlist; `buildPathRecords` holds only records of the past,
and grows only for another one, in a class T commit.

**Regenerate.** Nothing to regenerate.

## API spec drift

**Enforces.** Counts the routes S2's route inventory,
`internal/api/testdata/routes.txt`, lists that `docs/api-spec.md` does not
document, against `api_spec_undocumented_routes` (step D1). A route is
documented by a row of one of the spec's tables whose first cell holds its
method, alone or with others (`GET/HEAD`), and whose second cell is a code
span holding its path exactly, variable names included (`/api/v1/orgs/{id}`);
a brace group with a comma stands for each of its alternatives
(`/download/{json,csv}`). Prose does not count. The ceiling was 36 of 341
when D1 added the rule: the 15 deprecated `/api/v1/teams*` and
`/api/v1/team-*` aliases of the crew routes, the four attribute-definition
routes, the OIDC sign-in pair and 15 others.
`TestAPISpecRule` proves the parse on a fixture.

**Why.** tooling-8 and api-core-14: the spec is written by hand, and the
route inventory is the only machine-checked list of what the router
serves, so without a count nothing tells a pull request that adds a route
it left the spec behind.

**Fix.** Add a row for the new route in the spec's route inventory, in the
table of its area: method, path as the router registers it, purpose and
who may call it.

**Regenerate.** Lower the ceiling as routes are documented or removed; the
regenerate command does it.

## Env var inventory (S8)

**Enforces.** `TestEnvInventory` lists every environment variable the
production code of the module root, `cmd/` and `internal/` reads, and
freezes the list in `testdata/env_vars.txt`: one row per variable, the way
it is read and its default. It is a test of its own, not a rule of
`TestArchitecture`, and it never reads `UPDATE_RATCHETS`. Unlike the rules
above it type-checks: it runs `go list -export -deps` once and checks every
module package with `go/types` (about 1.5 s warm; a cold build cache
compiles export data first).

A getter is found by data flow, not by its name: any function whose string
parameter reaches `os.Getenv` or `os.LookupEnv`, directly or through another
getter, and any function-typed parameter that a call binds to `os.Getenv`
(`billing.ConfigFromEnv(os.Getenv)`, `resolveToken(os.Getenv)`). A function
that assigns to its name parameter or takes its address (an alias table, a
default) is no getter: its read does not resolve. At each call site the name
must resolve: a constant or a constant expression, a concatenation of those,
a local assigned exactly once and never address-taken, or the element of a
range over a package-level table of string constants that nothing but range
statements uses (the Gemini auth probe's). The read column is `os.Getenv`,
`os.LookupEnv` or `<package>:<getter>(<param>)`, followed by the comparison
when the value is only compared with a string constant
(`os.Getenv !=""`); the default is the argument that pairs with the
name, the i-th name parameter with the i-th other parameter, printed when it
is a constant. The rate-limit variables and their defaults are included.
Where each read sits (file and line, function, the binaries that link it) is
not frozen: `go test -v -run '^TestEnvInventory$' ./internal/archtest` logs
the report, and `ENV_INVENTORY_REPORT=<file>` writes it there.

The test fails on a name that does not resolve and no exemption covers; on
an env reader, a getter or an interface method a getter implements used as a
value, which the scan cannot follow; on a call through such an interface
method, whose target the scan cannot tell; on a getter that no call the scan
follows reaches (called only through an interface or a value, or not at
all); on a read outside a function declaration; on a read in a production
file this build leaves out (a `_windows.go` file, a build tag), found in
S1's syntax trees; and on a stale exemption. The exemptions are
`envExemptions` in `env_inventory_test.go`, each with its reason, and the
golden lists them by id: `unresolved` (the run's provider API key in
`Worker.execute`) and `environ` (child processes that inherit the whole
environment) must match exactly one read in each function they name;
`placement` names the named reads K8 keeps where they are when X10 moves
configuration to `internal/config` (`OPENV_MCP_TOOLS`, the per-request
reads, the runner's probes), and fails when one of its names is no longer
read there. The functions an exemption covers live only in the test, so
moving a read edits the test, not the golden.

`testdata/env_parse.txt` says what each read column returns for the same
list of inputs (unset, empty, blank, `TRUE`, invalid and valid values), for
X10a to rerun against `internal/config`. Each package with a getter has an
`env_parse_test.go` whose `TestEnvParse` calls its real, unexported getters
and writes its `<package>:` sections, and an `env_parse_helpers_test.go`, the
same in every package apart from the package clause; `TestEnvInventory`
writes the `os.*` and comparison sections and fails when a read column has
no section (or, where the parse depends on the variable, one per variable),
when a section is stale or tries other inputs, when a getter's package has
no `TestEnvParse` that calls it, or when a copy of the helpers differs.
`TestEnvScanForms` proves the scan on a fixture: the forms it resolves, and
a local assigned twice, a package variable, a getter fed a non-constant, a
getter that reassigns its name parameter or takes its address, a function
literal's parameter, a mutable table, values, calls through an interface
method a getter implements, a getter no followed call reaches, reads at
package initialisation, stale exemptions and a Windows-only read, which it
refuses.

**Why.** I13 and K8: the platform's configuration surface is its
environment, which no other guard sees whole. A refactor that renames a
variable, drops one, changes a default or a parse, or adds a read no one can
name fails here. X10 moves the reads into `internal/config` and reruns the
parse table against it; D1 generates `docs/env-vars.md` from the inventory.

**Fix.** Name the variable with a constant where it is read, or pass the
name through a getter whose parameter each call fills with a constant. A
new getter needs a `TestEnvParse` section: add `env_parse_test.go` to its
package, with a copy of `env_parse_helpers_test.go`. A read no constant can
name, or one that must stay where it is, is an exemption with its reason,
argued in review.

**Regenerate.** Only for a deliberate behavior change:

```sh
UPDATE_GOLDEN=1 go test ./internal/archtest -count=1 -run '^TestEnvInventory$'
UPDATE_GOLDEN=1 go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest \
  ./cmd/agentd ./cmd/openv-mcp ./cmd/server ./internal/api ./internal/billing \
  ./internal/domain/users ./internal/hosting ./internal/notify
```

The first rewrites `env_vars.txt` (it refuses while the scan fails); the
second also rewrites every section of `env_parse.txt`, whose writers take a
lock, so they may run in parallel. Only `UPDATE_GOLDEN=1` regenerates; any
other value compares.
