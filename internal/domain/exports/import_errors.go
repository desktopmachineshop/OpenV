package exports

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// malformedImport is ErrMalformedImport with the reason a JSON import does
// not parse, in the terms of the file (#379 bug 184). encoding/json's own
// text names the Go types the export is read into ("Go value of type
// exports.ProjectExport", "Go struct field ProjectExport.linked_artifacts of
// type []*exports.LinkedArtifact"), which is the program's layout, not the
// file's, and the import's 400 shows the reason. So a syntax error, and a
// time that does not parse, keep their text, which names no Go type; a
// value of the wrong kind names its field as the file spells it, or the
// whole document; time.Time's refusal of a value that is not a string
// ("Time.UnmarshalJSON: input is not a JSON string") is said in words; and
// anything else is described without its text. The decode error is not
// wrapped, so no caller can print it.
func malformedImport(err error) error {
	var syntax *json.SyntaxError
	var badTime *time.ParseError
	var mistyped *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax), errors.As(err, &badTime):
		return fmt.Errorf("%w: %s", ErrMalformedImport, err.Error())
	case errors.As(err, &mistyped) && mistyped.Field != "":
		return fmt.Errorf("%w: json: cannot unmarshal %s into field %q", ErrMalformedImport, mistyped.Value, mistyped.Field)
	case errors.As(err, &mistyped):
		return fmt.Errorf("%w: json: cannot unmarshal %s into a project export", ErrMalformedImport, mistyped.Value)
	case strings.HasPrefix(err.Error(), "Time.UnmarshalJSON: "):
		return fmt.Errorf("%w: json: a date and time must be a JSON string", ErrMalformedImport)
	default:
		return fmt.Errorf("%w: json: a value does not fit the export format", ErrMalformedImport)
	}
}
