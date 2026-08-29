package artifacts

import "testing"

// TestRefPrefix: catalog types use the fixed table; unknown types derive a
// deterministic prefix (initials of hyphenated words, else first three
// letters); degenerate input falls back to ART.
func TestRefPrefix(t *testing.T) {
	cases := map[string]string{
		TypeHeading:     "HDG",
		TypeDescription: "DSC",
		TypePersona:     "PER",
		TypeUserNeed:    "NEED",
		TypeRequirement: "REQ",
		TypeDesignItem:  "DES",
		TypeTestCase:    "TC",
		TypeHazard:      "HAZ",
		TypeOther:       "ART",
		// Legacy/unknown types.
		"epic":         "EPI",
		"user-story":   "US",
		"risk-control": "RC",
		"x":            "X",
		"123":          "ART",
		"":             "ART",
	}
	for typ, want := range cases {
		if got := RefPrefix(typ); got != want {
			t.Errorf("RefPrefix(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestParseRef: PREFIX-NUM round-trips; proposal-style temp tokens and other
// junk are rejected.
func TestParseRef(t *testing.T) {
	prefix, num, ok := ParseRef("REQ-42")
	if !ok || prefix != "REQ" || num != 42 {
		t.Fatalf("ParseRef(REQ-42) = %q,%d,%v", prefix, num, ok)
	}
	if FormatRef(prefix, num) != "REQ-42" {
		t.Fatalf("FormatRef did not round-trip: %q", FormatRef(prefix, num))
	}

	for _, bad := range []string{"t1", "req-42", "REQ-", "-42", "REQ-0", "REQ--3", "REQ", "", "REQ-4.2"} {
		if _, _, ok := ParseRef(bad); ok {
			t.Errorf("ParseRef(%q) unexpectedly ok", bad)
		}
	}
}
