package attachments

import "testing"

// A figure reference is a citation: it appears in a report, a review comment
// or a conversation months later. Formatting and parsing have to round-trip,
// and parsing must refuse anything that is not one — otherwise a filename that
// merely looks like a figure gets treated as one.
func TestFigureRefRoundTrips(t *testing.T) {
	cases := []struct {
		artifactRef string
		num         int
		want        string
	}{
		{"REQ-17", 1, "REQ-17-FIG-1"},
		{"REQ-17", 12, "REQ-17-FIG-12"},
		{"HAZ-3", 2, "HAZ-3-FIG-2"},
	}
	for _, tc := range cases {
		got := FormatFigureRef(tc.artifactRef, tc.num)
		if got != tc.want {
			t.Errorf("FormatFigureRef(%q, %d) = %q, want %q", tc.artifactRef, tc.num, got, tc.want)
		}
		ref, num, ok := ParseFigureRef(got)
		if !ok || ref != tc.artifactRef || num != tc.num {
			t.Errorf("ParseFigureRef(%q) = %q, %d, %v; want %q, %d, true",
				got, ref, num, ok, tc.artifactRef, tc.num)
		}
	}
}

func TestParseFigureRefRejectsNonFigures(t *testing.T) {
	for _, in := range []string{
		"REQ-17",           // an artifact reference, not a figure
		"REQ-17-FIG-",      // no number
		"REQ-17-FIG-0",     // figures start at one
		"REQ-17-FIG-x",     // not a number
		"-FIG-1",           // no artifact reference
		"REQ-17-FIG-1.png", // a filename, not a reference
		"",
	} {
		if _, _, ok := ParseFigureRef(in); ok {
			t.Errorf("ParseFigureRef(%q) accepted a non-figure", in)
		}
	}
}

// The stored name is the figure's, so a downloaded file is named for what the
// document calls it rather than for whatever the uploader's camera did.
func TestFigureFilenameUsesTheFigureName(t *testing.T) {
	cases := []struct {
		original string
		want     string
	}{
		{"Screenshot 2026-09-03 at 14.22.31.png", "REQ-17-FIG-1.png"},
		{"diagram.SVG", "REQ-17-FIG-1.svg"},
		{"photo.jpeg", "REQ-17-FIG-1.jpeg"},
		// No extension to carry over, so the name simply has none.
		{"scan", "REQ-17-FIG-1"},
		{"", "REQ-17-FIG-1"},
		// A trailing dot is not an extension.
		{"weird.", "REQ-17-FIG-1"},
	}
	for _, tc := range cases {
		if got := FigureFilename("REQ-17-FIG-1", tc.original); got != tc.want {
			t.Errorf("FigureFilename(%q) = %q, want %q", tc.original, got, tc.want)
		}
	}
}

// The extension comes from an uploaded filename, which is attacker-controlled
// text that ends up in a Content-Disposition header and possibly on a disk.
// Anything that is not a short, plain extension is dropped rather than
// carried through.
func TestFigureFilenameRefusesHostileExtensions(t *testing.T) {
	for _, original := range []string{
		`evil.p"ng`,
		"evil.pn\\ng",
		"evil.reallylongextension",
		"evil.png/../../etc/passwd",
		"evil.%2e%2e",
	} {
		got := FigureFilename("REQ-17-FIG-1", original)
		for _, bad := range []string{`"`, "\\", "/", "..", "%"} {
			if contains(got, bad) {
				t.Errorf("FigureFilename(%q) = %q, which carries %q through", original, got, bad)
			}
		}
		if len(got) > len("REQ-17-FIG-1")+8 {
			t.Errorf("FigureFilename(%q) = %q, longer than a reference plus a short extension", original, got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// NewAttachment starts a figure at version 1; the repository assigns the
// number, so a fresh attachment carries none yet.
func TestNewAttachmentStartsAtVersionOne(t *testing.T) {
	a := NewAttachment(CreateAttachmentRequest{
		ArtifactID:       "art-1",
		Filename:         "photo.png",
		OriginalFilename: "photo.png",
		MimeType:         "image/png",
		FilePath:         "/tmp/x",
		FileSize:         10,
	})
	if a.Version != 1 {
		t.Errorf("version = %d, want 1", a.Version)
	}
	if a.FigureRef != "" || a.FigureNum != 0 {
		t.Errorf("a new attachment should carry no figure yet, got %q/%d", a.FigureRef, a.FigureNum)
	}
	if a.OriginalFilename != "photo.png" {
		t.Errorf("original filename = %q, want the uploaded name", a.OriginalFilename)
	}
}
