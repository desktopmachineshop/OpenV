package doc

import (
	"strings"
	"testing"
)

const sample = "This has **bold**, *italic*, `code`, ~~gone~~ and a [link](https://example.com/x#y).\n" +
	"Second line cites #REQ-6 and #REQ-9-FIG-1, but not PR#12 or a #hashtag.\n\n" +
	"| A | B |\n|---|:-:|\n| 1 | **2** |\n\n" +
	"1. one\n2. two\n   - nested\n\n" +
	"- [x] done\n- [ ] open\n\n" +
	"```go\nfunc main() {\n    ok()\n}\n```\n\n> quoted\n\n## Inner heading\n\n---\n"

func TestParseStructure(t *testing.T) {
	blocks := Parse(sample)
	kinds := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.(type) {
		case Paragraph:
			kinds = append(kinds, "p")
		case Table:
			kinds = append(kinds, "table")
		case List:
			kinds = append(kinds, "list")
		case CodeBlock:
			kinds = append(kinds, "code")
		case Quote:
			kinds = append(kinds, "quote")
		case Heading:
			kinds = append(kinds, "h")
		case Rule:
			kinds = append(kinds, "hr")
		}
	}
	want := "p table list list code quote h hr"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("blocks = %q, want %q", got, want)
	}

	p := blocks[0].(Paragraph)
	var bold, italic, code, strike, link, refs int
	var breaks int
	for _, in := range p.Inlines {
		if in.Break {
			breaks++
		}
		if in.Bold {
			bold++
		}
		if in.Italic {
			italic++
		}
		if in.Code {
			code++
		}
		if in.Strike {
			strike++
		}
		if in.URL != "" {
			link++
			if in.URL != "https://example.com/x#y" {
				t.Errorf("url = %q", in.URL)
			}
		}
		if in.Ref != "" {
			refs++
		}
	}
	if bold != 1 || italic != 1 || code != 1 || strike != 1 || link != 1 {
		t.Errorf("styles: bold=%d italic=%d code=%d strike=%d link=%d", bold, italic, code, strike, link)
	}
	if refs != 2 {
		t.Errorf("refs = %d, want 2 (PR#12 and #hashtag are not references)", refs)
	}
	if breaks != 1 {
		t.Errorf("soft breaks = %d, want 1 (the writer's line break is kept)", breaks)
	}

	tbl := blocks[1].(Table)
	if len(tbl.Header) != 2 || len(tbl.Rows) != 1 || tbl.Align[1] != "center" {
		t.Errorf("table = %+v", tbl)
	}
	if !tbl.Rows[0][1].Inlines[0].Bold {
		t.Error("table cell styling lost")
	}

	ol := blocks[2].(List)
	if !ol.Ordered || ol.Start != 1 || len(ol.Items) != 2 {
		t.Errorf("ordered list = %+v", ol)
	}
	if len(ol.Items[1].Blocks) != 2 {
		t.Errorf("nested list not attached to item two: %+v", ol.Items[1])
	}

	tasks := blocks[3].(List)
	if tasks.Items[0].Task == nil || !*tasks.Items[0].Task || tasks.Items[1].Task == nil || *tasks.Items[1].Task {
		t.Errorf("task states = %+v", tasks.Items)
	}
	if got := InlineText(tasks.Items[0].Blocks[0].(Paragraph).Inlines); strings.TrimSpace(got) != "done" {
		t.Errorf("task text = %q", got)
	}

	cb := blocks[4].(CodeBlock)
	if cb.Language != "go" || !strings.Contains(cb.Text, "    ok()") {
		t.Errorf("code block lost its indentation: %+v", cb)
	}
	if h := blocks[6].(Heading); h.Level != 2 {
		t.Errorf("heading = %+v", h)
	}
}

func TestPlainText(t *testing.T) {
	got := PlainText(Parse("1. one\n2. two\n\n| A | B |\n|---|---|\n| 1 | 2 |"))
	want := "1. one\n2. two\nA | B\n1 | 2"
	if got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

func TestParseEmptyAndPlain(t *testing.T) {
	if blocks := Parse(""); len(blocks) != 0 {
		t.Errorf("empty body = %+v", blocks)
	}
	blocks := Parse("just words")
	if len(blocks) != 1 || InlineText(blocks[0].(Paragraph).Inlines) != "just words" {
		t.Errorf("plain = %+v", blocks)
	}
}
