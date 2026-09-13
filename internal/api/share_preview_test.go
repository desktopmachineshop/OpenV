package api

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func TestRenderPreviewIsACard(t *testing.T) {
	data, err := renderPreview(previewCard{
		Eyebrow:  "Shared specification · view only",
		Title:    "A project whose name runs on for long enough that it has to wrap onto a second line and then some",
		Subtitle: "Desktop Machine Shop",
		Lines:    []string{"33 requirements · 10 needs", strings.Repeat("description ", 40)},
		Footer:   "Requirements, traceability and V&V evidence",
	})
	if err != nil {
		t.Fatalf("renderPreview: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != previewW || b.Dy() != previewH {
		t.Fatalf("card is %dx%d, want %dx%d", b.Dx(), b.Dy(), previewW, previewH)
	}
}

func TestWrapLinesStopsAtMaxWithEllipsis(t *testing.T) {
	face, err := previewFace(goregular.TTF, 30)
	if err != nil {
		t.Fatal(err)
	}
	lines := wrapLines(face, strings.Repeat("word ", 200), 400, 2)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !strings.HasSuffix(lines[1], "…") {
		t.Errorf("last line %q is not ellipsised", lines[1])
	}
	if got := wrapLines(face, "short", 400, 2); len(got) != 1 || got[0] != "short" {
		t.Errorf("short text wrapped to %v", got)
	}
}

func TestPreviewPageEscapesAndRedirects(t *testing.T) {
	page := previewPage(`Bad <name> & "quotes"`, "desc", "https://app/share/t", "https://api/preview.png", "https://app/s/t")
	if strings.Contains(page, "<name>") {
		t.Error("title was not escaped")
	}
	for _, want := range []string{
		`property="og:title" content="Bad &lt;name&gt; &amp; &#34;quotes&#34;"`,
		`property="og:image" content="https://api/preview.png"`,
		`name="twitter:card" content="summary_large_image"`,
		`http-equiv="refresh" content="0; url=https://app/s/t"`,
		`name="robots" content="noindex"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestCountsLine(t *testing.T) {
	if got := countsLine(map[string]int{"requirement": 1, "test-case": 3, "heading": 9}); got != "1 requirement · 3 test cases" {
		t.Errorf("countsLine = %q", got)
	}
	if got := countsLine(nil); got != "" {
		t.Errorf("empty counts = %q", got)
	}
}

func TestTruncateWords(t *testing.T) {
	if got := truncateWords("one  two\nthree", 100); got != "one two three" {
		t.Errorf("whitespace: %q", got)
	}
	got := truncateWords("the quick brown fox jumps over the lazy dog", 20)
	if len(got) > 24 || !strings.HasSuffix(got, "…") || strings.Contains(got, "fox ju") {
		t.Errorf("truncated = %q", got)
	}
}
