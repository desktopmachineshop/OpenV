package postgres

import "strings"

// The helpers that turn an empty Go value into a driver NULL for a
// statement's argument (refactor plan X13), gathered from the repositories
// that declared them; each is as it was.

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}

// nullableString maps a *string to a driver NULL when nil/empty.
func nullableString(s *string) interface{} {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// nullUUID keeps an empty id out of a UUID column.
func nullUUID(id string) interface{} {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}
