package mentions

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/members"
)

func TestTokens(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    []string
	}{
		{"no at sign costs nothing", "please check the seal spec", nil},
		{"one name", "@dave can you check this", []string{"dave"}},
		{"dots and dashes are part of a handle", "@dave.smith-jones ping", []string{"dave.smith-jones"}},
		{"case folds", "@Dave and @DAVE are one person", []string{"dave"}},
		{"several", "@dave @priya over to you", []string{"dave", "priya"}},
		{"an email address is not two mentions", "mail dave@example.com", []string{"example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Tokens(tc.message)
			if len(got) != len(tc.want) {
				t.Fatalf("Tokens(%q) = %v, want %v", tc.message, got, tc.want)
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Errorf("Tokens(%q) missing %q, got %v", tc.message, w, got)
				}
			}
		})
	}
}

func TestHandles(t *testing.T) {
	got := Handles("Dave Smith", "dsmith@example.com")
	want := map[string]bool{"davesmith": true, "dave": true, "dsmith": true}
	if len(got) != len(want) {
		t.Fatalf("Handles = %v, want %d entries", got, len(want))
	}
	for _, h := range got {
		if !want[h] {
			t.Errorf("unexpected handle %q in %v", h, got)
		}
	}
}

// A member with no name set is still addressable by the local part of their
// email — which is the only handle a freshly invited person has.
func TestHandlesWithoutAName(t *testing.T) {
	got := Handles("   ", "priya@example.com")
	if len(got) != 1 || got[0] != "priya" {
		t.Errorf("Handles = %v, want [priya]", got)
	}
}

// TestHandlesOrderMatchesTheComposer pins the exact list, in order, that the
// note composer mirrors in frontend/src/components/noteMentions.ts. The
// composer picks the handle it writes into a note from this sequence, so a
// change here that is not made there produces mentions that name nobody —
// silently, because an unmatched @token is indistinguishable from prose.
//
// Change one side and this test fails; change both and it passes. That is the
// whole point of it.
func TestHandlesOrderMatchesTheComposer(t *testing.T) {
	cases := []struct {
		name, email string
		want        []string
	}{
		{"Dana Okoro", "dana.okoro@example.com", []string{"danaokoro", "dana", "dana.okoro"}},
		{"", "jo@example.com", []string{"jo"}},
		{"Dana Okoro", "not-an-email", []string{"danaokoro", "dana"}},
		// An apostrophe survives here but the @([\w.-]+) pattern stops at it,
		// so the composer skips this handle and writes the next one instead.
		{"Ciara O'Brien", "ciara.obrien@example.com", []string{"ciarao'brien", "ciara", "ciara.obrien"}},
	}
	for _, c := range cases {
		got := Handles(c.name, c.email)
		if len(got) != len(c.want) {
			t.Errorf("Handles(%q, %q) = %v, want %v", c.name, c.email, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Handles(%q, %q)[%d] = %q, want %q", c.name, c.email, i, got[i], c.want[i])
			}
		}
	}
}

func member(id, name, email string) *members.Member {
	return &members.Member{UserID: id, UserName: name, UserEmail: email}
}

func TestResolve(t *testing.T) {
	list := []*members.Member{
		member("u1", "Dave Smith", "dsmith@example.com"),
		member("u2", "Priya Patel", "priya@example.com"),
		member("u3", "Sam Okonkwo", "sam@example.com"),
	}

	cases := []struct {
		name    string
		message string
		want    []string
	}{
		{"first name", "@dave please look", []string{"u1"}},
		{"full name without spaces", "@davesmith please look", []string{"u1"}},
		{"email local part", "@dsmith please look", []string{"u1"}},
		{"two people, in list order", "@priya and @dave", []string{"u1", "u2"}},
		{"named twice is returned once", "@dave @dave @davesmith", []string{"u1"}},
		{"nobody matches", "@nobody here", nil},
		{"no mention at all", "just a note", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.message, list)
			if len(got) != len(tc.want) {
				t.Fatalf("Resolve(%q) returned %d members, want %d", tc.message, len(got), len(tc.want))
			}
			for i, id := range tc.want {
				if got[i].UserID != id {
					t.Errorf("Resolve(%q)[%d] = %s, want %s", tc.message, i, got[i].UserID, id)
				}
			}
		})
	}
}

// The notifier and the notes panel must agree about who a note addresses:
// one telling Dave he was mentioned while the other offers a to-do for
// nobody is the drift this package exists to prevent.
func TestMatchesAgreesWithResolve(t *testing.T) {
	list := []*members.Member{
		member("u1", "Dave Smith", "dsmith@example.com"),
		member("u2", "Priya Patel", "priya@example.com"),
	}
	message := "@priya over to you"
	tokens := Tokens(message)

	resolved := map[string]bool{}
	for _, m := range Resolve(message, list) {
		resolved[m.UserID] = true
	}
	for _, m := range list {
		if Matches(tokens, m) != resolved[m.UserID] {
			t.Errorf("member %s: Matches=%v but Resolve=%v", m.UserID, Matches(tokens, m), resolved[m.UserID])
		}
	}
}

func TestMatchesHandlesNothing(t *testing.T) {
	if Matches(nil, member("u1", "Dave", "d@example.com")) {
		t.Error("no tokens must match nobody")
	}
	if Matches(map[string]bool{"dave": true}, nil) {
		t.Error("a nil member must not match")
	}
}
