// Package fixture is splittools's test input: a table in the shape of
// internal/mcp's Tools(), with comments above, inside and after entries, a
// blank line between two, a named import, a module import, a blank import,
// and a local variable that shares an import's name.
package fixture

import (
	_ "embed"
	"fmt"
	"net/url"
	"sort"
	str "strconv"
	"strings"

	"example.com/fixture/ref"
)

// Tool is one entry of the table.
type Tool struct {
	Name    string
	Handler func(args map[string]string) (string, error)
}

// prefix is used by an entry and stays here.
const prefix = "tool:"

// Tools returns the table.
func Tools() []Tool {
	return []Tool{
		{
			Name: "alpha",
			Handler: func(args map[string]string) (string, error) {
				return prefix + strings.ToUpper(args["x"]), nil
			},
		},
		// beta's comment travels with it.
		{
			Name: "beta",
			Handler: func(args map[string]string) (string, error) {
				q := url.Values{"id": {args["id"]}}
				return q.Encode(), nil // the query as sent
			},
		}, // beta ends here

		{
			Name: "gamma",
			Handler: func(args map[string]string) (string, error) {
				n, err := str.Atoi(args["n"])
				if err != nil {
					return "", fmt.Errorf("gamma: %w", err)
				}
				return ref.Normalize(fmt.Sprint(n)), nil
			},
		},
		{
			Name: "delta",
			Handler: func(args map[string]string) (string, error) {
				sort := struct{ Keys []string }{Keys: []string{args["k"]}}
				return strings.Join(sort.Keys, ","), nil
			},
		},
		{
			Name: "epsilon",
			Handler: func(args map[string]string) (string, error) {
				keys := make([]string, 0, len(args))
				for k := range args {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				return strings.Join(keys, ","), nil
			},
		},
	}
}

// Names lists the table's names; it stays here and keeps using fmt.
func Names() string {
	var out []string
	for _, t := range Tools() {
		out = append(out, t.Name)
	}
	return fmt.Sprint(out)
}
