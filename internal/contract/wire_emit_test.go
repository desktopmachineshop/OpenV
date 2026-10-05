package contract

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// jsonField is one key encoding/json writes for a struct.
type jsonField struct {
	name      string
	typ       reflect.Type // the field's declared type
	index     []int
	tagged    bool
	omitEmpty bool
	omitZero  bool
	quoted    bool // the ,string option on a scalar: the value is written as a JSON string
	viaPtr    bool // promoted through an embedded pointer, so absent while it is nil
}

// jsonFields lists the keys encoding/json writes for struct type t, by the
// rules of its typeFields: the json tag's name, else the Go name; "-"
// skipped, and unexported fields; an embedded struct with no tag name
// flattened into its parent; for a name found more than once, the
// shallowest wins, then the one tagged, and two that still tie hide each
// other. The result is in field order.
func jsonFields(t reflect.Type) []jsonField {
	type level struct {
		typ    reflect.Type
		index  []int
		viaPtr bool
	}
	var fields []jsonField
	var current []level
	next := []level{{typ: t}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	for len(next) > 0 {
		current, next = next, nil
		count, nextCount = nextCount, map[reflect.Type]int{}
		for _, l := range current {
			if visited[l.typ] {
				continue
			}
			visited[l.typ] = true
			for i := range l.typ.NumField() {
				sf := l.typ.Field(i)
				if sf.Anonymous {
					et := sf.Type
					if et.Kind() == reflect.Pointer {
						et = et.Elem()
					}
					if !sf.IsExported() && et.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				if !validJSONTagName(name) {
					name = ""
				}
				index := append(slices.Clone(l.index), i)
				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					f := jsonField{name: name, typ: sf.Type, index: index, tagged: name != "",
						omitEmpty: hasOption(opts, "omitempty"), omitZero: hasOption(opts, "omitzero"),
						viaPtr: l.viaPtr}
					if name == "" {
						f.name = sf.Name
					}
					if hasOption(opts, "string") {
						switch ft.Kind() {
						case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
							reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
							reflect.Float32, reflect.Float64, reflect.String:
							f.quoted = true
						}
					}
					fields = append(fields, f)
					if count[l.typ] > 1 {
						// Embedded twice at this depth: the copy makes the
						// name tie with itself, as encoding/json does.
						fields = append(fields, f)
					}
					continue
				}
				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, level{typ: ft, index: index,
						viaPtr: l.viaPtr || sf.Type.Kind() == reflect.Pointer})
				}
			}
		}
	}

	sort.SliceStable(fields, func(i, j int) bool {
		a, b := fields[i], fields[j]
		if a.name != b.name {
			return a.name < b.name
		}
		if len(a.index) != len(b.index) {
			return len(a.index) < len(b.index)
		}
		if a.tagged != b.tagged {
			return a.tagged
		}
		return slices.Compare(a.index, b.index) < 0
	})
	var out []jsonField
	for i := 0; i < len(fields); {
		j := i + 1
		for j < len(fields) && fields[j].name == fields[i].name {
			j++
		}
		group := fields[i:j]
		if len(group) == 1 || len(group[0].index) != len(group[1].index) || group[0].tagged != group[1].tagged {
			out = append(out, group[0])
		}
		i = j
	}
	sort.Slice(out, func(i, j int) bool { return slices.Compare(out[i].index, out[j].index) < 0 })
	return out
}

func hasOption(opts, name string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == name {
			return true
		}
	}
	return false
}

// validJSONTagName is encoding/json's isValidTag: a name of letters,
// digits and the punctuation it allows; any other name falls back to the
// Go field name.
func validJSONTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// optional reports whether encoding/json can leave the key out: omitempty
// on a type it can find empty (any but a struct; an array only of length
// 0), omitzero, or a field promoted through an embedded pointer.
func (f jsonField) optional() bool {
	if f.viaPtr || f.omitZero {
		return true
	}
	if !f.omitEmpty {
		return false
	}
	switch f.typ.Kind() {
	case reflect.Struct:
		return false
	case reflect.Array:
		return f.typ.Len() == 0
	}
	return true
}

// tsType is a TypeScript type, apart from a null the Go value can write.
type tsType struct {
	expr     string
	nullable bool
}

func (x tsType) String() string {
	if x.nullable && x.expr != "unknown" {
		return x.expr + " | null"
	}
	return x.expr
}

// element is x as the element of an array type.
func (x tsType) element() string {
	if s := x.String(); strings.Contains(s, " | ") {
		return "(" + s + ")"
	}
	return x.String()
}

var (
	timeType          = reflect.TypeFor[time.Time]()
	rawMessageType    = reflect.TypeFor[json.RawMessage]()
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// fieldTS is a field's TypeScript type. Where omitempty or omitzero leaves a
// nil pointer, slice or map out, the key is optional and never null, so the
// null goes; a pointer keeps whatever null its element can write.
func (b *wireBuilder) fieldTS(f jsonField) string {
	t := f.typ
	if f.quoted {
		if t.Kind() == reflect.Pointer {
			return tsType{expr: "string", nullable: !f.omitEmpty && !f.omitZero}.String()
		}
		return "string"
	}
	if (f.omitEmpty || f.omitZero) && t != rawMessageType {
		switch t.Kind() {
		case reflect.Pointer:
			return b.ts(t.Elem()).String()
		case reflect.Slice, reflect.Map:
			x := b.ts(t)
			x.nullable = false
			return x.String()
		}
	}
	return b.ts(t).String()
}

// ts is the TypeScript type of the JSON encoding/json writes for a value of
// Go type t: time.Time a string, json.RawMessage and interfaces unknown,
// named string types string, numbers number, a pointer its element or null,
// a slice an array or null ([]byte a base64 string or null), a map a Record
// with string keys or null, a struct the interface wire.ts names for it. A
// type with its own MarshalJSON or MarshalText fails: reflect cannot see the
// shape it writes.
func (b *wireBuilder) ts(t reflect.Type) tsType {
	switch t {
	case timeType:
		return tsType{expr: "string"}
	case rawMessageType:
		return tsType{expr: "unknown"}
	}
	if hasMarshaler(t) {
		b.t.Fatalf("%s carries a %s, which has its own MarshalJSON or MarshalText, so reflect cannot see what it "+
			"writes; teach the emitter its shape", b.current, t)
	}
	switch t.Kind() {
	case reflect.String:
		return tsType{expr: "string"}
	case reflect.Bool:
		return tsType{expr: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return tsType{expr: "number"}
	case reflect.Interface:
		return tsType{expr: "unknown"}
	case reflect.Pointer:
		x := b.ts(t.Elem())
		x.nullable = true
		return x
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !hasMarshaler(t.Elem()) {
			return tsType{expr: "string", nullable: true}
		}
		return tsType{expr: b.ts(t.Elem()).element() + "[]", nullable: true}
	case reflect.Array:
		return tsType{expr: b.ts(t.Elem()).element() + "[]"}
	case reflect.Map:
		switch t.Key().Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		default:
			b.t.Fatalf("%s carries %s, whose key type the emitter does not write", b.current, t)
		}
		return tsType{expr: "Record<string, " + b.ts(t.Elem()).String() + ">", nullable: true}
	case reflect.Struct:
		return tsType{expr: b.ref(t)}
	}
	b.t.Fatalf("%s carries %s, which encoding/json cannot write", b.current, t)
	return tsType{}
}

// hasMarshaler reports whether a value of non-pointer type t writes itself,
// through a method on either receiver; a pointer is judged by its element,
// which ts reaches next.
func hasMarshaler(t reflect.Type) bool {
	if t == timeType || t == rawMessageType || t.Kind() == reflect.Pointer {
		return false
	}
	for _, m := range []reflect.Type{jsonMarshalerType, textMarshalerType} {
		if t.Implements(m) || reflect.PointerTo(t).Implements(m) {
			return true
		}
	}
	return false
}

const wireHeader = `// Code generated by go test ./internal/contract (refactor plan X5). DO NOT EDIT.
//
// The JSON the API writes for the Go types it sends most, one interface per
// type, as encoding/json writes it: a key per field's json name (and per key
// a MarshalJSON adds), optional where omitempty can leave it out, and
// ` + "`| null`" + ` where a nil pointer, slice or map writes null. The generator
// marshals a sample of each type and requires the keys it writes to be the
// keys below. A stale copy fails ` + "`go test ./internal/contract/...`" + `; after a
// deliberate change to one of these types in Go, regenerate with:
//
//   ` + regenerate + `
//
// frontend/src/api/wireCompat.ts holds the hand-written interfaces of
// frontend/src/api/types to these, by type only; nothing imports this file
// at run time.
`

var tsIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// typescript is frontend/src/generated/wire.ts.
func (w wire) typescript() []byte {
	var b strings.Builder
	b.WriteString(wireHeader)
	for _, it := range w.interfaces {
		b.WriteString("\n")
		lines := wrap(it.doc, 76)
		if len(lines) == 1 {
			fmt.Fprintf(&b, "/** %s */\n", lines[0])
		} else {
			b.WriteString("/**\n")
			for _, l := range lines {
				fmt.Fprintf(&b, " * %s\n", l)
			}
			b.WriteString(" */\n")
		}
		fmt.Fprintf(&b, "export interface %s {\n", it.name)
		for _, k := range it.keys {
			name := k.name
			if !tsIdentifier.MatchString(name) {
				name = tsString(name)
			}
			opt := ""
			if k.optional {
				opt = "?"
			}
			fmt.Fprintf(&b, "  %s%s: %s;\n", name, opt, k.ts)
		}
		b.WriteString("}\n")
	}
	return []byte(b.String())
}
