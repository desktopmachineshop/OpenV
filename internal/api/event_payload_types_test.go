package api

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// Domain event payload Go types (refactor plan step S6, invariant I10),
// pinned in testdata/event_payload_types.txt. The test swaps a recording bus
// into a handler, drives every path that publishes (event_payload_drives_test.go),
// and writes one row per event type, payload key and publisher with the
// value's Go type (%T).
//
// The JSON an API client reads (S5d's events golden) cannot see these types,
// but in-process subscribers can: they type-assert payload values
// (notify/membership.go payloadBool, orchestration/hooks.go onEvent) and the
// automation trigger matcher compares them through fmt.Sprintf("%v"), so a
// string that becomes a *string, or an int that becomes an int64, changes
// what those subscribers do while the JSON stays the same.
//
// TestEventPayloadDrivesReachEveryPublisher keeps the drive list complete:
// every call in the module that passes an event type constant is reached by
// a drive that publishes that type.

const payloadTypesGolden = "testdata/event_payload_types.txt"

// payloadBus is the recording bus: every event, the drive it came from, and
// the functions on the stack when it was published.
type payloadBus struct {
	events.Bus
	mu        sync.Mutex
	via       string
	published []publishedEvent
}

type publishedEvent struct {
	event events.Event
	via   string
	stack map[string]bool // function names as the runtime prints them, closures folded
}

var closureSuffix = regexp.MustCompile(`\.(func|gowrap|deferwrap)\d+(\.\d+)*$`)

func (b *payloadBus) Publish(e events.Event) {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	stack := map[string]bool{}
	for {
		f, more := frames.Next()
		stack[closureSuffix.ReplaceAllString(f.Function, "")] = true
		if !more {
			break
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, publishedEvent{event: e, via: b.via, stack: stack})
}

func (b *payloadBus) Subscribe(func(events.Event)) {}

func (b *payloadBus) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.published)
}

// runPayloadDrives runs every drive against a fresh fixture and returns what
// was published. A drive that publishes nothing fails: its fixture no longer
// reaches the publish.
func runPayloadDrives(t *testing.T) []publishedEvent {
	t.Helper()
	fx := newPayloadFixture(t)
	seen := map[string]bool{}
	for _, d := range payloadDrives() {
		if seen[d.via] {
			t.Fatalf("two drives are labelled %q", d.via)
		}
		seen[d.via] = true
		before := fx.bus.count()
		fx.bus.mu.Lock()
		fx.bus.via = d.via
		fx.bus.mu.Unlock()
		d.run(t, fx)
		if fx.bus.count() == before {
			t.Fatalf("drive %q published no event: its fixture no longer reaches the publish", d.via)
		}
	}
	fx.bus.mu.Lock()
	defer fx.bus.mu.Unlock()
	return append([]publishedEvent(nil), fx.bus.published...)
}

// TestEventPayloadTypes renders the rows and compares them with the golden.
func TestEventPayloadTypes(t *testing.T) {
	published := runPayloadDrives(t)
	rows := map[string]bool{}
	for _, p := range published {
		if len(p.event.Payload) == 0 {
			rows[payloadRow(p.event.EventType, "(empty)", "-", p.via)] = true
		}
		for key, value := range p.event.Payload {
			rows[payloadRow(p.event.EventType, key, fmt.Sprintf("%T", value), p.via)] = true
		}
	}
	driven := map[string]bool{}
	for _, p := range published {
		driven[p.event.EventType] = true
	}
	var unpublished []string
	for _, name := range declaredEventTypes(t) {
		if !driven[name] {
			unpublished = append(unpublished, payloadRow(name, "(no publisher)", "-", "-"))
		}
	}
	lines := sortedKeys(rows)
	var b strings.Builder
	for _, h := range []string{
		"Domain event payloads (refactor plan S6, invariant I10): for every path that publishes an event, each",
		"payload key and the Go type of its value (%T). In-process subscribers type-assert these values",
		"(notify/membership.go, orchestration/hooks.go) and the trigger matcher compares them through %v, so a",
		"changed Go type changes behavior even where the JSON (S5d) does not. Written by TestEventPayloadTypes.",
		"Columns: event type, payload key, Go type, published by (the route driven, or the in-process path).",
		"Regenerate: UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^TestEventPayloadTypes$'",
	} {
		b.WriteString("# " + h + "\n")
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	if len(unpublished) > 0 {
		b.WriteString("# Event types nothing publishes today:\n")
		for _, l := range unpublished {
			b.WriteString(l + "\n")
		}
	}
	checkS6Golden(t, filepath.FromSlash(payloadTypesGolden), "internal/api/"+payloadTypesGolden, []byte(b.String()),
		"TestEventPayloadTypes",
		"Subscribers read these payloads in process; a changed key or Go type changes what they do.")
}

func payloadRow(eventType, key, goType, via string) string {
	return fmt.Sprintf("%-29s %-18s %-24s %s", eventType, key, goType, via)
}

// TestEventPayloadDrivesReachEveryPublisher finds every call in the module's
// production code that passes a domain event type constant (h.publish(r,
// events.ArtifactCreated, ...), events.New(events.RunFinished, ...)) and
// requires a drive to have published that type with the call's function on
// the stack. A new publisher without a drive fails here.
func TestEventPayloadDrivesReachEveryPublisher(t *testing.T) {
	published := runPayloadDrives(t)
	sites := eventTypeCallSites(t)
	if len(sites) < 20 {
		t.Fatalf("found only %d publish sites: the scan has gone blind", len(sites))
	}
	var missed []string
	for _, site := range sites {
		reached := false
		for _, p := range published {
			if p.event.EventType == site.eventType && p.stack[site.function] {
				reached = true
				break
			}
		}
		if !reached {
			missed = append(missed, fmt.Sprintf("%s publishes %s in %s", site.pos, site.eventType, site.function))
		}
	}
	if len(missed) > 0 {
		t.Fatalf("no drive reaches these publishers:\n  %s\n"+
			"Add a drive in internal/api/event_payload_drives_test.go that publishes the event through this path, "+
			"then regenerate the golden:\n  %s",
			strings.Join(missed, "\n  "), s6Regenerate("TestEventPayloadTypes"))
	}
}

// eventSite is one call that passes an event type constant.
type eventSite struct {
	eventType string
	function  string // the enclosing function, as the runtime names it
	pos       string
}

const eventsImportSuffix = "/internal/domain/events"

// eventTypeCallSites lists every call in production code with a direct
// argument naming an event type constant of internal/domain/events.
func eventTypeCallSites(t *testing.T) []eventSite {
	t.Helper()
	mod := loadModuleSources(t)
	types := map[string]string{}
	for _, c := range eventTypeConsts(t, mod) {
		types[c[0]] = c[1]
	}
	var sites []eventSite
	for _, f := range mod.files {
		local := importName(f.file, mod.path+eventsImportSuffix)
		if local == "" {
			continue
		}
		for _, d := range f.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn := runtimeFuncName(mod.path, f.dir, fd)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, arg := range call.Args {
					sel, ok := arg.(*ast.SelectorExpr)
					if !ok || !isIdent(sel.X, local) {
						continue
					}
					if value, ok := types[sel.Sel.Name]; ok {
						p := mod.fset.Position(call.Pos())
						sites = append(sites, eventSite{eventType: value, function: fn, pos: fmt.Sprintf("%s:%d", p.Filename, p.Line)})
					}
				}
				return true
			})
		}
	}
	return sites
}

// eventTypeConsts reads the event type constants (name, value) from
// internal/domain/events: every string constant but the Actor ones.
func eventTypeConsts(t *testing.T, mod *moduleSources) [][2]string {
	t.Helper()
	var out [][2]string
	for _, f := range mod.files {
		if !strings.HasSuffix(mod.path+"/"+f.dir, eventsImportSuffix) {
			continue
		}
		for _, d := range f.file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if strings.HasPrefix(name.Name, "Actor") || i >= len(vs.Values) {
						continue
					}
					if v, ok := stringLit(vs.Values[i]); ok {
						out = append(out, [2]string{name.Name, v})
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("found no event type constants in internal/domain/events")
	}
	return out
}

func declaredEventTypes(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range eventTypeConsts(t, loadModuleSources(t)) {
		out = append(out, c[1])
	}
	sort.Strings(out)
	return out
}

// importName is the name a file uses for an import path, "" if it has none.
func importName(f *ast.File, importPath string) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return p[strings.LastIndex(p, "/")+1:]
	}
	return ""
}

// runtimeFuncName names a declaration the way runtime.Frame.Function does:
// "<pkg path>.Func", "<pkg path>.(*T).Method" or "<pkg path>.T.Method".
func runtimeFuncName(modPath, dir string, fd *ast.FuncDecl) string {
	pkg := modPath
	if dir != "." {
		pkg += "/" + dir
	}
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return pkg + "." + fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	if idx, ok := recv.(*ast.IndexExpr); ok {
		recv = idx.X
	}
	if star, ok := recv.(*ast.StarExpr); ok {
		if id, ok := star.X.(*ast.Ident); ok {
			return pkg + ".(*" + id.Name + ")." + fd.Name.Name
		}
	}
	if id, ok := recv.(*ast.Ident); ok {
		return pkg + "." + id.Name + "." + fd.Name.Name
	}
	return pkg + ".?." + fd.Name.Name
}
