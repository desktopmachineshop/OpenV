package fixture

import "sort"

var registry []string

// Sorted returns the registry, sorted.
func Sorted() []string {
	out := append([]string(nil), registry...)
	sort.Strings(out)
	return helperSorted(out)
}

func init() { registry = append(registry, "small") }
