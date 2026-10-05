package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The emitter and its marshal self-check, run over fixtures rather than the
// domain types: one struct with encoding/json's harder field rules, which
// the self-check must accept and the emitter must type as below, and three
// MarshalJSON methods it must refuse.

type fixtureBase struct {
	Shared string `json:"shared"`
	Depth  int
}

type FixtureInner struct {
	Tie   string
	Pick  string `json:"Pick"`
	Inner bool   `json:"inner"`
}

type FixtureOther struct {
	Tie  string
	Pick string
	Won  string
}

type FixturePtr struct {
	Via string `json:"via"`
}

type fixture struct {
	fixtureBase  // unexported, but a struct: its exported fields are promoted
	FixtureInner // Tie ties with FixtureOther's at the same depth, so both are hidden; its tagged Pick wins
	FixtureOther
	*FixturePtr                         // promoted through a pointer: absent while it is nil
	Named       FixtureInner            `json:"named,omitempty"` // omitempty never leaves a struct out
	Won         string                  // shallower than FixtureOther's, so it wins
	Skipped     string                  `json:"-"`
	Dash        string                  `json:"-,"`
	Fallback    string                  `json:"a\\b"`
	Quoted      int                     `json:"quoted,string"`
	QuotedPtr   *int                    `json:"quoted_ptr,string"`
	Zero        time.Time               `json:"zero,omitzero"`
	Maybe       *string                 `json:"maybe"`
	MaybeNot    *string                 `json:"maybe_not,omitempty"`
	List        []string                `json:"list"`
	Ptrs        []*int                  `json:"ptrs,omitempty"`
	Bytes       []byte                  `json:"bytes"`
	Raw         json.RawMessage         `json:"raw"`
	Any         any                     `json:"any"`
	ByID        map[string]*FixturePtr  `json:"by_id"`
	Fixed       [2]int                  `json:"fixed,omitempty"`
	Nested      map[string][]FixturePtr `json:"nested,omitempty"`
	unexported  string
}

const fixtureTS = `
/** contract.fixture (internal/contract). */
export interface WireFixture {
  shared: string;
  Depth: number;
  Pick: string;
  inner: boolean;
  via?: string;
  named: WireFixtureInner;
  Won: string;
  '-': string;
  Fallback: string;
  quoted: string;
  quoted_ptr: string | null;
  zero?: string;
  maybe: string | null;
  maybe_not?: string;
  list: string[] | null;
  ptrs?: (number | null)[];
  bytes: string | null;
  raw: unknown;
  any: unknown;
  by_id: Record<string, WireFixturePtr | null> | null;
  fixed: number[];
  nested?: Record<string, WireFixturePtr[] | null>;
}

/** contract.FixtureInner (internal/contract), as contract.fixture carries it. */
export interface WireFixtureInner {
  Tie: string;
  Pick: string;
  inner: boolean;
}

/** contract.FixturePtr (internal/contract), as contract.fixture carries it. */
export interface WireFixturePtr {
  via: string;
}
`

func TestWireEmitterFollowsEncodingJSON(t *testing.T) {
	_ = fixture{}.unexported
	w := buildWire(t, []wireRoot{{"WireFixture", reflect.TypeFor[fixture]()}}, nil)
	got := strings.TrimPrefix(string(w.typescript()), wireHeader)
	if got != fixtureTS {
		t.Errorf("the emitter writes the fixture as (- want, + got):\n%s",
			strings.Join(lineDiff(fixtureTS, got, 40), "\n"))
	}
}

// addsKey writes its field and a key no field gives, as
// Attachment.MarshalJSON does.
type addsKey struct {
	ID string `json:"id"`
}

func (a addsKey) MarshalJSON() ([]byte, error) {
	type plain addsKey
	return json.Marshal(struct {
		plain
		Extra string `json:"extra"`
	}{plain(a), "x"})
}

// dropsKey leaves out one of its fields.
type dropsKey struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

func (d *dropsKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"id": d.ID})
}

// TestWireSelfCheckRefuses runs the builder over types whose JSON is not
// what their fields give, and over a wireAdded entry no MarshalJSON writes:
// each must fail, naming the key.
func TestWireSelfCheckRefuses(t *testing.T) {
	for _, c := range []struct {
		name  string
		typ   reflect.Type
		added []addedKey
		want  []string
	}{
		{"a key a MarshalJSON adds, undeclared", reflect.TypeFor[addsKey](), nil,
			[]string{"writes extra, which no field of the struct gives", "wireAdded"}},
		{"a key a pointer-receiver MarshalJSON drops", reflect.TypeFor[dropsKey](), nil,
			[]string{"the struct's fields give secret, which json.Marshal of a sample does not write"}},
		{"a declared key nothing writes", reflect.TypeFor[FixturePtr](),
			[]addedKey{{name: "kind", typ: reflect.TypeFor[string](), method: "FixturePtr.MarshalJSON"}},
			[]string{"wireAdded lists kind, which json.Marshal of a sample does not write"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{}
			r.run(func() {
				buildWire(r, []wireRoot{{"WireX", c.typ}}, map[reflect.Type][]addedKey{c.typ: c.added})
			})
			msgs := strings.Join(r.msgs, "\n")
			if len(r.msgs) == 0 {
				t.Fatal("the builder accepted it")
			}
			for _, w := range c.want {
				if !strings.Contains(msgs, w) {
					t.Errorf("no message says %q; the builder said:\n%s", w, msgs)
				}
			}
		})
	}

	// Declared, the added key is emitted with its Go type.
	r := &recorder{}
	var w wire
	r.run(func() {
		w = buildWire(r, []wireRoot{{"WireX", reflect.TypeFor[addsKey]()}}, map[reflect.Type][]addedKey{
			reflect.TypeFor[addsKey](): {{name: "extra", typ: reflect.TypeFor[string](), method: "addsKey.MarshalJSON"}},
		})
	})
	if len(r.msgs) > 0 {
		t.Fatalf("the builder refused a declared key: %s", strings.Join(r.msgs, "\n"))
	}
	if got := string(w.typescript()); !strings.Contains(got, "  id: string;\n  extra: string;\n}") {
		t.Errorf("the declared key is not emitted after the fields:\n%s", got)
	}
	if doc := w.interfaces[0].doc; !strings.HasSuffix(doc, ", with extra, which addsKey.MarshalJSON adds.") {
		t.Errorf("the doc comment does not name the added key and its method: %s", doc)
	}
}

// recorder collects what the builder reports; Fatalf stops the run, as it
// does a test.
type recorder struct{ msgs []string }

type fatal struct{}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(fatal{})
}

func (r *recorder) run(f func()) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(fatal); !ok {
				panic(p)
			}
		}
	}()
	f()
}
