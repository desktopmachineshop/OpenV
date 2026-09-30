package reports

import (
	"os"
	"path/filepath"
	"testing"
)

// A report finds a figure stored under the uploads directory by the name
// the server keeps for it. The server reads UPLOADS_DIR trimmed, like every
// setting (#379, question 15), so the report must too, or spaces round the
// value would leave the two looking in different directories and the figure
// out of the report.
func TestResolveAttachmentPathReadsUploadsDirTrimmed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "figure.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir()) // so the relative name resolves only through UPLOADS_DIR
	t.Setenv("UPLOADS_DIR", " "+dir+" ")
	got, ok := resolveAttachmentPath("figure.png")
	if !ok || got != filepath.Join(dir, "figure.png") {
		t.Fatalf("resolveAttachmentPath with UPLOADS_DIR=%q = %q, %v; want %q", " "+dir+" ", got, ok, filepath.Join(dir, "figure.png"))
	}
}
