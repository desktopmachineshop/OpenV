package exports

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// goTypeText is what encoding/json's own messages name of the program: a Go
// value or struct field, a method such as Time.UnmarshalJSON, or a package
// qualified type such as exports.ProjectExport or artifacts.Artifact.
var goTypeText = regexp.MustCompile(`Go (value|struct)|UnmarshalJSON|\b[a-z][a-z0-9]*\.[A-Z][A-Za-z0-9]*`)

// A JSON import that does not fit the export is refused with
// ErrMalformedImport and a reason in the terms of the file, whatever field
// holds the wrong value (#379 bug 184). The test puts a value of each wrong
// kind at every JSON path an export has, as encoding/json reaches it, so a
// field added to ProjectExport or to a type it holds is covered with no
// edit here. The decode error is not wrapped: a caller that prints the
// error, such as the import handler's 400, cannot print encoding/json's
// text, which names Go types.
func TestImportRefusalsNameNoGoType(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	bodies := wrongValueBodies(reflect.TypeOf(ProjectExport{}), nil, 0)
	bodies = append(bodies, `[]`, `"x"`, `5`, `true`, `{`, `x`, `{"a" 1}`, "\xff")
	refused := 0
	for _, body := range bodies {
		var probe ProjectExport
		if json.Unmarshal([]byte(body), &probe) == nil {
			continue // a value encoding/json accepts (-1 for an int); the import would go on to write
		}
		refused++
		_, err := s.ImportProject([]byte(body), "org-1")
		if !errors.Is(err, ErrMalformedImport) {
			t.Errorf("import of %s: %v, want ErrMalformedImport", body, err)
			continue
		}
		if m := goTypeText.FindString(err.Error()); m != "" {
			t.Errorf("import of %s names %q of the program: %v", body, m, err)
		}
		var mistyped *json.UnmarshalTypeError
		var syntax *json.SyntaxError
		var badTime *time.ParseError
		for _, wrapped := range []any{&mistyped, &syntax, &badTime} {
			if errors.As(err, wrapped) {
				t.Errorf("import of %s wraps the decode error (%T), whose text can name Go types", body, reflect.ValueOf(wrapped).Elem().Interface())
			}
		}
	}
	if refused < 150 {
		t.Fatalf("only %d of %d bodies were refused; the walk over ProjectExport no longer reaches its fields", refused, len(bodies))
	}
}

// An error encoding/json passes through from a type's own UnmarshalJSON
// carries whatever that method wrote, which can name the program. None but
// time.Time's reaches the import today; any other is described without its
// text.
func TestAnUnmarshalersOtherRefusalIsDescribedWithoutItsText(t *testing.T) {
	err := malformedImport(errors.New("profile.Settings.UnmarshalJSON: want an object"))
	if !errors.Is(err, ErrMalformedImport) {
		t.Fatalf("%v: want ErrMalformedImport", err)
	}
	if want := "malformed JSON: json: a value does not fit the export format"; err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

// wrongValueBodies returns, for t at path and every JSON path below it, a
// document holding a value of each kind t cannot take there.
func wrongValueBodies(t reflect.Type, path []string, depth int) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var out []string
	for _, v := range wrongValues(t) {
		out = append(out, nestAt(path, v))
	}
	if depth > 6 || t == reflect.TypeOf(time.Time{}) {
		return out
	}
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			switch {
			case f.Anonymous && name == "":
				out = append(out, wrongValueBodies(f.Type, path, depth+1)...)
			case !f.IsExported() || name == "-":
			default:
				if name == "" {
					name = f.Name
				}
				out = append(out, wrongValueBodies(f.Type, append(append([]string{}, path...), name), depth+1)...)
			}
		}
	case reflect.Slice:
		out = append(out, wrongValueBodies(t.Elem(), append(append([]string{}, path...), "[]"), depth+1)...)
	case reflect.Map:
		out = append(out, wrongValueBodies(t.Elem(), append(append([]string{}, path...), "{}"), depth+1)...)
	}
	return out
}

// wrongValues are JSON values of kinds a Go type cannot take.
func wrongValues(t reflect.Type) []string {
	if t == reflect.TypeOf(time.Time{}) {
		return []string{`5`, `"not a time"`, `[]`}
	}
	switch t.Kind() {
	case reflect.String:
		return []string{`5`, `{}`}
	case reflect.Bool:
		return []string{`"x"`}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return []string{`"x"`, `1.5`, `-1`}
	case reflect.Struct, reflect.Slice:
		return []string{`5`, `"x"`, `{}`, `[]`}
	case reflect.Map:
		return []string{`5`, `[]`}
	case reflect.Interface:
		return nil
	}
	return []string{`5`}
}

// nestAt puts v at path in a document: a key, "[]" for an array element or
// "{}" for a map value.
func nestAt(path []string, v string) string {
	if len(path) == 0 {
		return v
	}
	inner := nestAt(path[1:], v)
	switch path[0] {
	case "[]":
		return "[" + inner + "]"
	case "{}":
		return `{"k":` + inner + "}"
	}
	key, _ := json.Marshal(path[0])
	return "{" + string(key) + ":" + inner + "}"
}
