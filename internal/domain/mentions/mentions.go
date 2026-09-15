// Package mentions resolves the @name tokens people write in notes to the
// project members they address.
//
// The rules live here rather than in one caller because two things now act
// on the same text and must agree: the notifier, which tells someone they
// were mentioned, and the notes panel, which offers to raise a to-do for
// whoever a note names. A note that notifies Dave but offers a to-do for
// nobody would read as a bug, and matching rules that drift apart are how
// that happens.
package mentions

import (
	"regexp"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/members"
)

// pattern captures the token after an "@": word characters plus the
// dot/dash/underscore that commonly appear in handles.
var pattern = regexp.MustCompile(`@([\w.-]+)`)

// Tokens extracts the distinct lowercased @tokens from a message. A message
// with no "@" in it costs one byte scan and nothing else.
func Tokens(message string) map[string]bool {
	if !strings.Contains(message, "@") {
		return nil
	}
	found := pattern.FindAllStringSubmatch(message, -1)
	if len(found) == 0 {
		return nil
	}
	tokens := make(map[string]bool, len(found))
	for _, m := range found {
		tokens[strings.ToLower(m[1])] = true
	}
	return tokens
}

// Handles lists the tokens a member answers to: their full name with the
// spaces removed, their first name, and the local part of their email.
// Matching is intentionally cheap and local — there are no stored handles
// in this product, so a mention is matched against what is already known
// about the person.
func Handles(name, email string) []string {
	var handles []string
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		lower := strings.ToLower(trimmed)
		handles = append(handles, strings.ReplaceAll(lower, " ", ""))
		if fields := strings.Fields(lower); len(fields) > 0 {
			handles = append(handles, fields[0])
		}
	}
	if at := strings.IndexByte(email, '@'); at > 0 {
		handles = append(handles, strings.ToLower(email[:at]))
	}
	return handles
}

// Matches reports whether any token addresses the member.
func Matches(tokens map[string]bool, member *members.Member) bool {
	if len(tokens) == 0 || member == nil {
		return false
	}
	for _, h := range Handles(member.UserName, member.UserEmail) {
		if tokens[h] {
			return true
		}
	}
	return false
}

// Resolve returns the members a message mentions, in the order they appear
// in list. A member is returned once however many times they are named.
func Resolve(message string, list []*members.Member) []*members.Member {
	tokens := Tokens(message)
	if len(tokens) == 0 {
		return nil
	}
	var out []*members.Member
	for _, m := range list {
		if Matches(tokens, m) {
			out = append(out, m)
		}
	}
	return out
}
