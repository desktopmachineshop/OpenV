// Package ref stands in for a package of the module that an entry uses.
package ref

// Normalize marks a value as normalised.
func Normalize(s string) string { return "#" + s }
