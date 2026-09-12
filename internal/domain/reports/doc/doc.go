// Package doc is the document model every rendered report is built from.
//
// An artifact body is markdown. The editor renders it as GitHub-flavoured
// markdown (tables, task lists, strikethrough) and turns "#REQ-12" citations
// into links; a PDF or Word rendering of the same body has to show the same
// structure, or the two are different documents. This package parses a body
// once into blocks and inline runs, so that each renderer walks one tree
// rather than re-deriving structure from text with regular expressions.
//
// The model is deliberately small: paragraphs, headings, lists, tables, code,
// quotes and rules, with inline runs carrying emphasis, code, strikethrough,
// an external URL or an artifact/figure reference. Anything markdown can say
// that a printed document cannot (raw HTML, footnotes) is flattened to text.
package doc

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Inline is one run of text with uniform styling. Break is a hard line break
// (its Text is empty).
type Inline struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
	Strike bool
	// URL is set for an external link; the run's Text is the link text.
	URL string
	// Ref is set for an artifact or figure citation ("REQ-12",
	// "REQ-12-FIG-1"); the run's Text is the citation as written ("#REQ-12").
	Ref   string
	Break bool
}

// Block is one block-level element of a body.
type Block interface{ block() }

// Paragraph is a run of inlines.
type Paragraph struct{ Inlines []Inline }

// Heading is a markdown heading inside a body (not an artifact heading).
type Heading struct {
	Level   int
	Inlines []Inline
}

// List is an ordered or bulleted list; items may nest further blocks.
type List struct {
	Ordered bool
	Start   int
	Items   []ListItem
}

// ListItem is one entry. Task is nil for an ordinary item, otherwise the
// checkbox state of a task-list item.
type ListItem struct {
	Blocks []Block
	Task   *bool
}

// Table is a GFM table: one header row and zero or more body rows.
type Table struct {
	Header []Cell
	Rows   [][]Cell
	// Align holds "left", "center", "right" or "" per column.
	Align []string
}

// Cell is one table cell.
type Cell struct{ Inlines []Inline }

// CodeBlock is fenced or indented code, verbatim.
type CodeBlock struct {
	Language string
	Text     string
}

// Quote is a block quote.
type Quote struct{ Blocks []Block }

// Rule is a thematic break.
type Rule struct{}

func (Paragraph) block() {}
func (Heading) block()   {}
func (List) block()      {}
func (Table) block()     {}
func (CodeBlock) block() {}
func (Quote) block()     {}
func (Rule) block()      {}

// referencePattern mirrors the editor's inlineReferencePattern
// (frontend/src/components/artifactReferences.ts): a "#" at the start or
// after whitespace, followed by a stable reference, optionally a figure.
var referencePattern = regexp.MustCompile(`(^|\s)#([A-Za-z][A-Za-z0-9]*-\d+(?:-FIG-\d+)?)`)

var parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList))

// Parse turns a markdown body into blocks. It never fails: text goldmark
// cannot parse is still text.
func Parse(markdown string) []Block {
	src := []byte(strings.ReplaceAll(markdown, "\r\n", "\n"))
	root := parser.Parser().Parse(text.NewReader(src))
	w := &walker{src: src}
	return w.blocks(root)
}

// PlainText flattens blocks to text: a paragraph per line, list markers and
// table cells separated by spaces. It is what a spreadsheet cell or a
// truncated title shows.
func PlainText(blocks []Block) string {
	var b strings.Builder
	for i, blk := range blocks {
		if i > 0 {
			b.WriteString("\n")
		}
		writePlain(&b, blk, "")
	}
	return strings.TrimSpace(b.String())
}

// InlineText joins inline runs to plain text.
func InlineText(inlines []Inline) string {
	var b strings.Builder
	for _, in := range inlines {
		if in.Break {
			b.WriteString("\n")
			continue
		}
		b.WriteString(in.Text)
	}
	return b.String()
}

func writePlain(b *strings.Builder, blk Block, indent string) {
	switch v := blk.(type) {
	case Paragraph:
		b.WriteString(indent + InlineText(v.Inlines))
	case Heading:
		b.WriteString(indent + InlineText(v.Inlines))
	case List:
		for i, item := range v.Items {
			marker := "- "
			if v.Ordered {
				marker = itoa(v.Start+i) + ". "
			}
			for j, sub := range item.Blocks {
				if j == 0 {
					b.WriteString(indent + marker)
					writePlain(b, sub, "")
				} else {
					b.WriteString("\n")
					writePlain(b, sub, indent+"  ")
				}
			}
			if i < len(v.Items)-1 {
				b.WriteString("\n")
			}
		}
	case Table:
		row := func(cells []Cell) {
			parts := make([]string, len(cells))
			for i, c := range cells {
				parts[i] = InlineText(c.Inlines)
			}
			b.WriteString(indent + strings.Join(parts, " | "))
		}
		row(v.Header)
		for _, r := range v.Rows {
			b.WriteString("\n")
			row(r)
		}
	case CodeBlock:
		b.WriteString(indent + strings.TrimRight(v.Text, "\n"))
	case Quote:
		for i, sub := range v.Blocks {
			if i > 0 {
				b.WriteString("\n")
			}
			writePlain(b, sub, indent)
		}
	case Rule:
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

type walker struct{ src []byte }

func (w *walker) blocks(parent gast.Node) []Block {
	var out []Block
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if blk, ok := w.block(n); ok {
			out = append(out, blk)
		}
	}
	return out
}

func (w *walker) block(n gast.Node) (Block, bool) {
	switch v := n.(type) {
	case *gast.Paragraph, *gast.TextBlock:
		return Paragraph{Inlines: w.inlines(n, style{})}, true
	case *gast.Heading:
		return Heading{Level: v.Level, Inlines: w.inlines(n, style{})}, true
	case *gast.List:
		l := List{Ordered: v.IsOrdered(), Start: v.Start}
		if l.Ordered && l.Start == 0 {
			l.Start = 1
		}
		for item := v.FirstChild(); item != nil; item = item.NextSibling() {
			li := ListItem{}
			li.Blocks = w.blocks(item)
			if t := taskState(item); t != nil {
				li.Task = t
			}
			l.Items = append(l.Items, li)
		}
		return l, true
	case *east.Table:
		t := Table{}
		for _, a := range v.Alignments {
			switch a {
			case east.AlignLeft:
				t.Align = append(t.Align, "left")
			case east.AlignCenter:
				t.Align = append(t.Align, "center")
			case east.AlignRight:
				t.Align = append(t.Align, "right")
			default:
				t.Align = append(t.Align, "")
			}
		}
		for row := v.FirstChild(); row != nil; row = row.NextSibling() {
			var cells []Cell
			for c := row.FirstChild(); c != nil; c = c.NextSibling() {
				cells = append(cells, Cell{Inlines: w.inlines(c, style{})})
			}
			if _, isHeader := row.(*east.TableHeader); isHeader {
				t.Header = cells
			} else {
				t.Rows = append(t.Rows, cells)
			}
		}
		return t, true
	case *gast.FencedCodeBlock:
		return CodeBlock{Language: string(v.Language(w.src)), Text: w.lines(v)}, true
	case *gast.CodeBlock:
		return CodeBlock{Text: w.lines(v)}, true
	case *gast.Blockquote:
		return Quote{Blocks: w.blocks(v)}, true
	case *gast.ThematicBreak:
		return Rule{}, true
	case *gast.HTMLBlock:
		// Raw HTML has no printed form; keep its text so nothing is lost.
		txt := strings.TrimSpace(stripTags(w.lines(v)))
		if txt == "" {
			return nil, false
		}
		return Paragraph{Inlines: []Inline{{Text: txt}}}, true
	}
	return nil, false
}

// taskState reads the checkbox of a task-list item, which goldmark places as
// the first inline of the item's first paragraph.
func taskState(item gast.Node) *bool {
	first := item.FirstChild()
	if first == nil {
		return nil
	}
	if cb, ok := first.FirstChild().(*east.TaskCheckBox); ok {
		v := cb.IsChecked
		return &v
	}
	return nil
}

func (w *walker) lines(n gast.Node) string {
	var b strings.Builder
	segs := n.Lines()
	for i := 0; i < segs.Len(); i++ {
		s := segs.At(i)
		b.Write(s.Value(w.src))
	}
	return b.String()
}

type style struct {
	bold, italic, strike bool
	url                  string
}

func (w *walker) inlines(parent gast.Node, st style) []Inline {
	var out []Inline
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		out = append(out, w.inline(n, st)...)
	}
	return out
}

func (w *walker) inline(n gast.Node, st style) []Inline {
	switch v := n.(type) {
	case *gast.Text:
		var out []Inline
		out = append(out, w.text(string(v.Segment.Value(w.src)), st)...)
		if v.HardLineBreak() || v.SoftLineBreak() {
			// The editor honours the line breaks a writer typed (REQ-52), so
			// a soft break is a break here too.
			out = append(out, Inline{Break: true})
		}
		return out
	case *gast.String:
		return w.text(string(v.Value), st)
	case *gast.CodeSpan:
		var b strings.Builder
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*gast.Text); ok {
				b.Write(t.Segment.Value(w.src))
			}
		}
		return []Inline{{Text: b.String(), Code: true, Bold: st.bold, Italic: st.italic, Strike: st.strike, URL: st.url}}
	case *gast.Emphasis:
		next := st
		if v.Level >= 2 {
			next.bold = true
		} else {
			next.italic = true
		}
		return w.inlines(v, next)
	case *east.Strikethrough:
		next := st
		next.strike = true
		return w.inlines(v, next)
	case *gast.Link:
		next := st
		next.url = string(v.Destination)
		runs := w.inlines(v, next)
		if len(runs) == 0 {
			runs = []Inline{{Text: next.url, URL: next.url}}
		}
		return runs
	case *gast.AutoLink:
		u := string(v.URL(w.src))
		return []Inline{{Text: string(v.Label(w.src)), URL: u, Bold: st.bold, Italic: st.italic, Strike: st.strike}}
	case *gast.Image:
		// A markdown image has no file in the project; keep its alt text.
		alt := string(v.Text(w.src))
		if alt == "" {
			alt = string(v.Destination)
		}
		return w.text(alt, st)
	case *gast.RawHTML:
		return nil
	case *east.TaskCheckBox:
		return nil
	}
	// Anything else: descend.
	return w.inlines(n, st)
}

// text splits a run at "#REF" citations, which become Ref inlines.
func (w *walker) text(s string, st style) []Inline {
	if s == "" {
		return nil
	}
	mk := func(t string) Inline {
		return Inline{Text: t, Bold: st.bold, Italic: st.italic, Strike: st.strike, URL: st.url}
	}
	if st.url != "" {
		return []Inline{mk(s)}
	}
	var out []Inline
	pos := 0
	for _, m := range referencePattern.FindAllStringSubmatchIndex(s, -1) {
		// m[2],m[3] = leading boundary; m[4],m[5] = the reference.
		hashStart := m[4] - 1
		if hashStart > pos {
			out = append(out, mk(s[pos:hashStart]))
		}
		ref := s[m[4]:m[5]]
		r := mk("#" + ref)
		r.Ref = ref
		out = append(out, r)
		pos = m[5]
	}
	if pos < len(s) {
		out = append(out, mk(s[pos:]))
	}
	return out
}

var tagPattern = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string { return tagPattern.ReplaceAllString(s, "") }
