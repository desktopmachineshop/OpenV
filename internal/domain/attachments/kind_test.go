package attachments

import (
	"encoding/json"
	"strings"
	"testing"
)

// A browser names an image and guesses at everything else, so the extension
// has to be what decides for the formats it does not know.
func TestAcceptUploadTakesTheTypeFromTheExtension(t *testing.T) {
	cases := []struct {
		name     string
		declared string
		filename string
		wantMime string
		wantKind Kind
	}{
		{"png as declared", "image/png", "pump.png", "image/png", KindImage},
		{"pdf", "application/pdf", "datasheet.pdf", "application/pdf", KindDocument},
		{"pdf the browser could not name", "application/octet-stream", "datasheet.pdf", "application/pdf", KindDocument},
		{"step", "application/octet-stream", "manifold.STEP", "model/step", KindModel},
		{"stp", "", "manifold.stp", "model/step", KindModel},
		{"stl", "application/octet-stream", "bracket.stl", "model/stl", KindModel},
		{"dxf is a drawing, not a picture", "image/vnd.dxf", "panel.dxf", "image/vnd.dxf", KindModel},
		{"solidworks part", "application/octet-stream", "housing.SLDPRT", "application/octet-stream", KindModel},
		{"extension wins over a wrong declaration", "text/plain", "assembly.step", "model/step", KindModel},
		{"image with no extension, declared", "image/jpeg", "scan", "image/jpeg", KindImage},
		{"declared type with a charset", "image/svg+xml; charset=utf-8", "schematic", "image/svg+xml", KindImage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mime, kind, ok := AcceptUpload(tc.declared, tc.filename)
			if !ok {
				t.Fatalf("AcceptUpload(%q, %q) refused it", tc.declared, tc.filename)
			}
			if mime != tc.wantMime || kind != tc.wantKind {
				t.Fatalf("got (%q, %q), want (%q, %q)", mime, kind, tc.wantMime, tc.wantKind)
			}
		})
	}
}

// The catalogue is a decision about what other members will be handed, not a
// hole that takes whatever arrives.
func TestAcceptUploadRefusesWhatTheCatalogueDoesNotName(t *testing.T) {
	for _, tc := range []struct{ declared, filename string }{
		{"application/x-msdownload", "installer.exe"},
		{"text/html", "page.html"},
		{"application/zip", "everything.zip"},
		{"application/octet-stream", "mystery"},
		{"application/javascript", "payload.js"},
		{"", ""},
	} {
		if _, _, ok := AcceptUpload(tc.declared, tc.filename); ok {
			t.Errorf("AcceptUpload(%q, %q) accepted it; want refused", tc.declared, tc.filename)
		}
	}
}

func TestKindForMime(t *testing.T) {
	cases := map[string]Kind{
		"image/png":        KindImage,
		"image/svg+xml":    KindImage,
		"IMAGE/JPEG":       KindImage,
		"application/pdf":  KindDocument,
		"model/step":       KindModel,
		"model/stl":        KindModel,
		"image/vnd.dxf":    KindModel,
		"image/vnd.dwg":    KindModel,
		"text/csv":         KindOther,
		"":                 KindOther,
		"image/png; q=0.9": KindImage,
	}
	for mime, want := range cases {
		if got := KindForMime(mime); got != want {
			t.Errorf("KindForMime(%q) = %q, want %q", mime, got, want)
		}
	}
}

// IsImage is what the exports and the report renderer ask before treating an
// attachment as something they can draw. A DXF is the trap: its registered
// type starts with "image/" and nothing can render it.
func TestIsImageExcludesDrawingFormats(t *testing.T) {
	if !IsImage("image/png") {
		t.Error("a PNG should be an image")
	}
	for _, mime := range []string{"image/vnd.dxf", "image/vnd.dwg", "application/pdf", "model/step"} {
		if IsImage(mime) {
			t.Errorf("IsImage(%q) = true; a report would try to embed it", mime)
		}
	}
}

func TestContentMatchesFormat(t *testing.T) {
	t.Run("a signature is enforced where the format has one", func(t *testing.T) {
		if !ContentMatchesFormat("datasheet.pdf", []byte("%PDF-1.7\n...")) {
			t.Error("a real PDF was refused")
		}
		if ContentMatchesFormat("datasheet.pdf", []byte("not a pdf at all")) {
			t.Error("a mis-named PDF was accepted")
		}
		if !ContentMatchesFormat("part.stp", []byte("ISO-10303-21;\nHEADER;")) {
			t.Error("a real STEP file was refused")
		}
		if ContentMatchesFormat("part.stp", []byte("%PDF-1.7")) {
			t.Error("a PDF renamed to .stp was accepted")
		}
	})

	t.Run("a format with no dependable signature passes on its extension", func(t *testing.T) {
		// Binary STL opens with 80 bytes of free-form header; refusing it for
		// having no magic number would refuse most real STL files.
		if !ContentMatchesFormat("bracket.stl", []byte(strings.Repeat("\x00", 200))) {
			t.Error("a binary STL was refused")
		}
		if !ContentMatchesFormat("housing.sldprt", []byte{0xd0, 0xcf, 0x11, 0xe0}) {
			t.Error("a SolidWorks part was refused")
		}
	})

	t.Run("an empty file is never a figure", func(t *testing.T) {
		if ContentMatchesFormat("bracket.stl", nil) {
			t.Error("an empty upload was accepted")
		}
	})
}

// A client asks for the kind rather than pattern-matching MIME types of its
// own, so every response has to carry it.
func TestAttachmentJSONCarriesItsKind(t *testing.T) {
	raw, err := json.Marshal(Attachment{FigureRef: "REQ-17-FIG-2", MimeType: "application/pdf"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["kind"] != string(KindDocument) {
		t.Errorf("kind = %v, want %q", got["kind"], KindDocument)
	}
	if got["figure_ref"] != "REQ-17-FIG-2" {
		t.Errorf("the rest of the attachment did not survive: %v", got)
	}

	vraw, err := json.Marshal(Version{MimeType: "model/stl", Version: 3})
	if err != nil {
		t.Fatalf("marshal version: %v", err)
	}
	if !strings.Contains(string(vraw), `"kind":"model"`) {
		t.Errorf("a version should say what format it was: %s", vraw)
	}
}

// Figures are one numbering sequence whatever the format, and the stored name
// keeps the extension a download needs to open in the right tool.
func TestFigureFilenameKeepsCADAndDocumentExtensions(t *testing.T) {
	cases := map[string]string{
		"pump drawing.PDF":    "REQ-17-FIG-3.pdf",
		"manifold-rev-c.step": "REQ-17-FIG-3.step",
		"bracket.stl":         "REQ-17-FIG-3.stl",
		"housing.SLDPRT":      "REQ-17-FIG-3.sldprt",
	}
	for original, want := range cases {
		if got := FigureFilename("REQ-17-FIG-3", original); got != want {
			t.Errorf("FigureFilename(%q) = %q, want %q", original, got, want)
		}
	}
}
