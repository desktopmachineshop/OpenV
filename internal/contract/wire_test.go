package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Refactor plan X5: frontend/src/generated/wire.ts states, for the Go types
// the API sends most, the JSON encoding/json writes for each, one TypeScript
// interface per type; frontend/src/api/wireCompat.ts holds the frontend's
// hand-written interfaces to them, by type only.

const wireFile = "frontend/src/generated/wire.ts"

// wireTypes are the Go types wire.ts states, in the order it writes them,
// each with its TypeScript name. A struct one of them carries that is not
// listed here is written after them as Wire<its Go name>.
var wireTypes = []wireRoot{
	{"WireArtifact", reflect.TypeFor[artifacts.Artifact]()},
	{"WireLink", reflect.TypeFor[links.Link]()},
	{"WireProject", reflect.TypeFor[projects.Project]()},
	{"WireOrg", reflect.TypeFor[orgs.Org]()},
	{"WireUser", reflect.TypeFor[users.User]()},
	{"WireRun", reflect.TypeFor[agentruns.Run]()},
	{"WireAgent", reflect.TypeFor[agents.Agent]()},
	{"WireAttachment", reflect.TypeFor[attachments.Attachment]()},
	{"WireBaseline", reflect.TypeFor[baselines.Baseline]()},
	{"WireNotification", reflect.TypeFor[notifications.Notification]()},
	{"WireEvent", reflect.TypeFor[events.Event]()},
}

// addedKey is a key a type's own MarshalJSON writes beside its fields', with
// the Go type of its value, which the emitter types as it would a field's,
// and the method that adds it.
type addedKey struct {
	name   string
	typ    reflect.Type
	method string
}

// wireAdded lists, per type, the keys its MarshalJSON adds. The marshal
// self-check refuses a key a sample's JSON carries that is neither a field's
// nor listed here, and an entry here the JSON does not carry, so a
// MarshalJSON that adds, renames or drops a key fails before wire.ts can be
// regenerated without it.
var wireAdded = map[reflect.Type][]addedKey{
	reflect.TypeFor[attachments.Attachment](): {
		{name: "kind", typ: reflect.TypeFor[attachments.Kind](), method: "Attachment.MarshalJSON"},
	},
}

func TestWire(t *testing.T) {
	src := loadSources(t)
	w := buildWire(t, wireTypes, wireAdded)
	if t.Failed() {
		return // a self-check failure: wire.ts is neither compared nor written
	}
	syncGenerated(t, src, []generated{{
		path: wireFile, data: w.typescript(), from: "the JSON the Go types write",
		why: "frontend/src/api/wireCompat.ts holds the frontend's hand-written interfaces to it, and a refactor " +
			"never changes what the API writes (the file is on the refactor guard's golden list).",
		after: "then run cd frontend && npx tsc --noEmit -p . and update the hand-written interface, or its " +
			"COMPAT_EXCEPTIONS entry in wireCompat.ts, that it names.",
	}})
}

// wireRoot is a Go type wire.ts states and the name it gives it.
type wireRoot struct {
	name string
	typ  reflect.Type
}

// reporter is the part of *testing.T the builder reports through, so a test
// can run it over a type that must fail and read what it says.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// wireKey is one key of an emitted interface.
type wireKey struct {
	name     string
	ts       string
	optional bool
}

// wireInterface is one emitted interface: its name, its doc comment, and
// its keys in the order encoding/json writes them.
type wireInterface struct {
	name  string
	doc   string
	keys  []wireKey
	added []addedKey
}

type wire struct {
	interfaces []wireInterface
}

// wireBuilder names every struct it meets: the roots by their given names,
// any other by Wire<Go name>, queued to be emitted after the roots.
type wireBuilder struct {
	t       reporter
	names   map[reflect.Type]string
	taken   map[string]reflect.Type
	queue   []reflect.Type
	usedBy  map[reflect.Type]string
	added   map[reflect.Type][]addedKey
	current string
}

// buildWire walks each root with encoding/json's field rules, marshals a
// sample of it and of every struct it carries, and requires the two to name
// the same keys (the marshal self-check); the emitted keys are the sample's,
// in its order, typed by the field each came from or by its wireAdded entry.
func buildWire(t reporter, roots []wireRoot, added map[reflect.Type][]addedKey) wire {
	t.Helper()
	b := &wireBuilder{t: t, names: map[reflect.Type]string{}, taken: map[string]reflect.Type{},
		usedBy: map[reflect.Type]string{}, added: added}
	for _, r := range roots {
		if prev, dup := b.names[r.typ]; dup {
			t.Fatalf("%s is listed twice, as %s and %s", r.typ, prev, r.name)
		}
		b.claim(r.typ, r.name)
		b.queue = append(b.queue, r.typ)
	}
	for typ := range added {
		if _, ok := b.names[typ]; !ok {
			t.Errorf("wireAdded lists %s, which is not one of the types wire.ts states", typ)
		}
	}
	var w wire
	for i := 0; i < len(b.queue); i++ {
		w.interfaces = append(w.interfaces, b.build(b.queue[i], i < len(roots)))
	}
	return w
}

func (b *wireBuilder) claim(typ reflect.Type, name string) {
	if other, ok := b.taken[name]; ok && other != typ {
		b.t.Fatalf("wire.ts would name both %s and %s %s", other, typ, name)
	}
	b.names[typ] = name
	b.taken[name] = typ
}

// ref is the TypeScript name of a struct a field carries.
func (b *wireBuilder) ref(typ reflect.Type) string {
	if name, ok := b.names[typ]; ok {
		return name
	}
	if typ.Name() == "" {
		b.t.Fatalf("%s carries an anonymous struct (%s): give it a named type, which wire.ts then states",
			b.current, typ)
	}
	b.claim(typ, "Wire"+typ.Name())
	b.queue = append(b.queue, typ)
	b.usedBy[typ] = b.current
	return b.names[typ]
}

func (b *wireBuilder) build(typ reflect.Type, root bool) wireInterface {
	t := b.t
	t.Helper()
	b.current = typ.String()
	w := wireInterface{name: b.names[typ], added: b.added[typ]}
	w.doc = fmt.Sprintf("%s (%s).", typ, goPackageDir(typ))
	if !root {
		w.doc = fmt.Sprintf("%s (%s), as %s carries it.", typ, goPackageDir(typ), b.usedBy[typ])
	}
	if len(w.added) > 0 {
		var names, methods []string
		for _, a := range w.added {
			names = append(names, a.name)
			methods = appendOnce(methods, a.method)
		}
		w.doc = strings.TrimSuffix(w.doc, ".") + fmt.Sprintf(", with %s, which %s adds.",
			strings.Join(names, ", "), strings.Join(methods, " and "))
	}

	fields := map[string]jsonField{}
	var fieldKeys []string
	for _, f := range jsonFields(typ) {
		fields[f.name] = f
		fieldKeys = append(fieldKeys, f.name)
	}
	addedByName := map[string]addedKey{}
	for _, a := range w.added {
		if _, ok := fields[a.name]; ok {
			t.Errorf("%s: wireAdded lists %s, which is a field's key already; remove the entry", typ, a.name)
		}
		addedByName[a.name] = a
	}

	keys, values := marshalledKeys(t, typ)
	written := map[string]bool{}
	var extra, unwritten, undeclared []string
	for _, k := range keys {
		written[k] = true
		f, isField := fields[k]
		a, isAdded := addedByName[k]
		var key wireKey
		switch {
		case isField:
			key = wireKey{name: k, ts: b.fieldTS(f), optional: f.optional()}
		case isAdded:
			key = wireKey{name: k, ts: b.ts(a.typ).String()}
		default:
			extra = append(extra, k)
			continue
		}
		if msg := jsonKindMismatch(key.ts, values[k]); msg != "" {
			t.Errorf("%s: the sample writes %s as %s, but wire.ts would type it %s (%s): the emitter's type "+
				"rules disagree with encoding/json here", typ, k, values[k], key.ts, msg)
		}
		w.keys = append(w.keys, key)
	}
	for _, k := range fieldKeys {
		if !written[k] {
			unwritten = append(unwritten, k)
		}
	}
	for _, a := range w.added {
		if !written[a.name] {
			undeclared = append(undeclared, a.name)
		}
	}
	if len(extra) > 0 {
		t.Errorf("%s: json.Marshal of a sample writes %s, which no field of the struct gives: a MarshalJSON "+
			"method adds it. Declare each such key in wireAdded (internal/contract/wire_test.go) with the Go type "+
			"of its value and the method, then regenerate with:\n  %s", typ, strings.Join(extra, ", "), regenerate)
	}
	if len(unwritten) > 0 {
		t.Errorf("%s: the struct's fields give %s, which json.Marshal of a sample does not write: a MarshalJSON "+
			"method leaves it out, or the emitter's field rules disagree with encoding/json",
			typ, strings.Join(unwritten, ", "))
	}
	if len(undeclared) > 0 {
		t.Errorf("%s: wireAdded lists %s, which json.Marshal of a sample does not write; remove the entry",
			typ, strings.Join(undeclared, ", "))
	}
	return w
}

// marshalledKeys marshals a pointer to a sample of typ, as a handler writes
// one (so a MarshalJSON on either receiver runs), and returns the top-level
// keys of the JSON object in the order written, with each key's value.
func marshalledKeys(t reporter, typ reflect.Type) ([]string, map[string]json.RawMessage) {
	t.Helper()
	sample := reflect.New(typ)
	fillSample(sample.Elem(), 0)
	data, err := json.Marshal(sample.Interface())
	if err != nil {
		t.Fatalf("%s: json.Marshal of a sample: %v", typ, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("%s: json.Marshal of a sample writes %s, not an object", typ, data)
	}
	var keys []string
	values := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("%s: %v in %s", typ, err, data)
		}
		k := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("%s: %v in %s", typ, err, data)
		}
		if _, dup := values[k]; dup {
			t.Fatalf("%s: json.Marshal of a sample writes %s twice", typ, k)
		}
		keys = append(keys, k)
		values[k] = v
	}
	return keys, values
}

var sampleTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// fillSample gives every settable field a value encoding/json writes even
// under omitempty: a non-empty string, 1, true, a non-nil pointer, a slice or
// map of one filled element, a fixed time; json.RawMessage gets {} and an
// empty interface a string. Depth bounds a type that contains itself.
func fillSample(v reflect.Value, depth int) {
	if depth > 8 {
		return
	}
	switch v.Type() {
	case timeType:
		v.Set(reflect.ValueOf(sampleTime))
		return
	case rawMessageType:
		v.Set(reflect.ValueOf(json.RawMessage(`{}`)))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fillSample(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillSample(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Array:
		for i := range v.Len() {
			fillSample(v.Index(i), depth+1)
		}
	case reflect.Map:
		m := reflect.MakeMapWithSize(v.Type(), 1)
		k := reflect.New(v.Type().Key()).Elem()
		fillSample(k, depth+1)
		e := reflect.New(v.Type().Elem()).Elem()
		fillSample(e, depth+1)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf("x"))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				fillSample(f, depth+1)
			}
		}
	}
}

// jsonKindMismatch says why a sample's value cannot be of the TypeScript
// type the emitter gave its key, or "" when it can. Every pointer, slice and
// map of a sample is filled, so null fits only unknown.
func jsonKindMismatch(ts string, value json.RawMessage) string {
	if ts == "unknown" {
		return ""
	}
	ts = strings.TrimSuffix(ts, " | null")
	first := byte(0)
	if len(value) > 0 {
		first = value[0]
	}
	var ok bool
	switch {
	case ts == "string":
		ok = first == '"'
	case ts == "number":
		ok = first == '-' || (first >= '0' && first <= '9')
	case ts == "boolean":
		ok = first == 't' || first == 'f'
	case strings.HasSuffix(ts, "[]"):
		ok = first == '['
	case strings.HasPrefix(ts, "Record<"), strings.HasPrefix(ts, "Wire"):
		ok = first == '{'
	default:
		return "no JSON kind for it"
	}
	if !ok {
		return "another JSON kind"
	}
	return ""
}

func goPackageDir(typ reflect.Type) string {
	return strings.TrimPrefix(typ.PkgPath(), modulePath+"/")
}

const modulePath = "github.com/openv/requirements-platform"

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
