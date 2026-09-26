// Package fixture is the package declmove's tests move declarations in.
package fixture

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strings"
)

// Mode says how a Server answers.
type Mode int

// The modes use iota, so they move as one group.
const (
	ModeOff Mode = iota
	ModeOn
	ModeLoud // shouts
)

// Server answers requests.
type Server struct {
	name string
	mode Mode
}

var errEmpty = errors.New("empty request")

var _ fmt.Stringer = (*Server)(nil)

func init() { registry = append(registry, "server") }

// String names the server.
func (s *Server) String() string { return "server " + s.name }

// Handle answers one request.
func (s *Server) Handle(req string) (string, error) {
	if req == "" {
		return "", errEmpty
	}
	out := render(req) // escaped
	if s.mode == ModeLoud {
		out = strings.ToUpper(out)
	}
	return out, nil
}

// render escapes a request for HTML.
func render(s string) string {
	return htmltemplate.HTMLEscapeString(s)
}

func helper() string { return fmt.Sprintf("%d", len(registry)) }
