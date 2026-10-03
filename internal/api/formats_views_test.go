package api

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"
)

// How the S9 format goldens show an answer (refactor plan step S9). Text
// formats (JSON, CSV, ReqIF's XML) are kept byte for byte, with the clock
// reads replaced. The binary formats are shown by what they say, since their
// bytes carry compressed streams and font subsets a Go or library upgrade
// may encode differently:
//
//   - xlsx: the parts in order, each sheet's XML with its cells listed one
//     per line (reference, style id, type, value with shared strings
//     resolved, formula), and every style id the cells use, as excelize
//     reads it back;
//   - pdf: the page count and each page's text (the strings its content
//     streams show, whitespace collapsed), and how many images, links and
//     outline entries it holds;
//   - docx: the parts in order and word/document.xml, one tag per line;
//   - zip: the entries in order, a document inside shown as above, a file's
//     size otherwise.

const (
	s9GoldenDir   = "testdata/formats"
	s9GoldenShown = "internal/api/testdata/formats"
	s9UpdateEnv   = "UPDATE_GOLDEN"
	s9Command     = "UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^(TestFormatsGolden|TestProposalPayloadsGolden)$'"
)

// s9Clock replaces the times the server reads its own clock for, in the
// layout it wrote them, with a token naming the layout: a time that falls
// within [from, to] (a whole unit either side, so a stamp truncated to the
// second, minute or day still matches). Any other time, the fixture's
// among them, stays as it is.
type s9Clock struct {
	from, to time.Time
	uploads  string
}

type s9ClockLayout struct {
	re     *regexp.Regexp
	layout string
	name   string
	unit   time.Duration
	utc    bool
}

// s9ClockLayouts are tried in order; the first a text matches wins.
var s9ClockLayouts = []s9ClockLayout{
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`), time.RFC3339Nano, "RFC 3339", time.Second, false},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC`), "2006-01-02 15:04 UTC", "2006-01-02 15:04 UTC", time.Minute, true},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}`), "2006-01-02 15:04", "2006-01-02 15:04", time.Minute, false},
	{regexp.MustCompile(`\d{8}_\d{6}`), "20060102_150405", "20060102_150405", time.Second, false},
	{regexp.MustCompile(`\d{8}`), "20060102", "20060102", 24 * time.Hour, false},
}

func (c s9Clock) within(ts time.Time, unit time.Duration) bool {
	return !ts.Before(c.from.Add(-unit)) && !ts.After(c.to.Add(unit))
}

// text replaces the clock reads in s, and the fixture's uploads directory.
func (c s9Clock) text(s string) string {
	if c.uploads != "" {
		s = strings.ReplaceAll(s, c.uploads, "<uploads>")
	}
	for _, l := range s9ClockLayouts {
		s = l.re.ReplaceAllStringFunc(s, func(m string) string {
			loc := time.Local
			if l.utc {
				loc = time.UTC
			}
			ts, err := time.ParseInLocation(l.layout, m, loc)
			if err != nil || !c.within(ts, l.unit) {
				return m
			}
			if l.name == "RFC 3339" && strings.Contains(m, ".") {
				// The zone is left out: a time.Now() not put in UTC is written
				// at the machine's offset, which would make the golden
				// depend on where it runs.
				return "<now RFC 3339 with fraction>"
			}
			return "<now " + l.name + ">"
		})
	}
	return s
}

// s9View writes the golden of one answer: the request, the status, the
// headers that name the file, and the body by its kind.
func s9View(t *testing.T, c s9Clock, method, path string, status int, header map[string]string, body []byte) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\nstatus: %d\n", method, c.text(path), status)
	for _, k := range s9SortedKeys(header) {
		if header[k] != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, c.text(header[k]))
		}
	}
	b.WriteString(s9Body(t, c, header["Content-Type"], body))
	return b.String()
}

// s9Body shows a body by its media type.
func s9Body(t *testing.T, c s9Clock, contentType string, body []byte) string {
	t.Helper()
	mt, _, _ := strings.Cut(contentType, ";")
	switch strings.TrimSpace(mt) {
	case "application/pdf":
		return s9PDF(t, c, body)
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return s9XLSX(t, c, body)
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return s9DOCX(t, c, body)
	case "application/zip":
		return s9Zip(t, c, body)
	}
	return s9Text(c, body)
}

// s9Text keeps a text body byte for byte, apart from the clock, and says
// whether it ended in a newline (the file it is written to always does).
func s9Text(c s9Clock, body []byte) string {
	text := c.text(string(body))
	ends := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	return fmt.Sprintf("body: text, %d lines, ends with a newline: %t\n%s\n", strings.Count(text, "\n")+1, ends, text)
}

// s9XMLLines splits XML after each tag another tag follows.
func s9XMLLines(text string) string {
	return strings.ReplaceAll(text, "><", ">\n<")
}

func s9ReadZip(t *testing.T, body []byte) *zip.Reader {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip archive: %v", err)
	}
	return zr
}

func s9ZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open %s: %v", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", f.Name, err)
	}
	return data
}

// s9DOCX lists a Word document's parts and its word/document.xml.
func s9DOCX(t *testing.T, c s9Clock, body []byte) string {
	t.Helper()
	zr := s9ReadZip(t, body)
	var b strings.Builder
	b.WriteString("body: docx\nparts:\n")
	var document []byte
	for _, f := range zr.File {
		fmt.Fprintf(&b, "  %s\n", f.Name)
		if f.Name == "word/document.xml" {
			document = s9ZipFile(t, f)
		}
	}
	if document == nil {
		t.Fatal("the Word document has no word/document.xml")
	}
	fmt.Fprintf(&b, "word/document.xml:\n%s\n", s9XMLLines(c.text(string(document))))
	return b.String()
}

// s9XLSX lists a workbook's parts, each sheet's cells with their style ids,
// and the styles those ids name.
func s9XLSX(t *testing.T, c s9Clock, body []byte) string {
	t.Helper()
	zr := s9ReadZip(t, body)
	var b strings.Builder
	b.WriteString("body: xlsx\nparts:\n")
	var strs []string
	for _, f := range zr.File {
		fmt.Fprintf(&b, "  %s\n", f.Name)
		if f.Name == "xl/sharedStrings.xml" {
			var table struct {
				SI []struct {
					T string `xml:"t"`
					R []struct {
						T string `xml:"t"`
					} `xml:"r"`
				} `xml:"si"`
			}
			if err := xml.Unmarshal(s9ZipFile(t, f), &table); err != nil {
				t.Fatalf("sharedStrings.xml: %v", err)
			}
			for _, si := range table.SI {
				s := si.T
				for _, r := range si.R {
					s += r.T
				}
				strs = append(strs, s)
			}
		}
	}
	wb, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open the workbook: %v", err)
	}
	defer wb.Close()
	styles := map[int]bool{}
	for _, sh := range s9Sheets(t, zr) {
		fmt.Fprintf(&b, "sheet %q (%s):\n", sh.name, sh.part.Name)
		raw := s9ZipFile(t, sh.part)
		var sheet struct {
			Rows []struct {
				C []struct {
					R  string `xml:"r,attr"`
					S  string `xml:"s,attr"`
					T  string `xml:"t,attr"`
					V  string `xml:"v"`
					F  string `xml:"f"`
					Is struct {
						T string `xml:"t"`
					} `xml:"is"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err := xml.Unmarshal(raw, &sheet); err != nil {
			t.Fatalf("%s: %v", sh.part.Name, err)
		}
		text := string(raw)
		if i, j := strings.Index(text, "<sheetData"), strings.Index(text, "</sheetData>"); i >= 0 && j > i {
			text = text[:i] + "<sheetData>(the cells below)" + text[j:]
		}
		fmt.Fprintf(&b, "%s\n", s9XMLLines(c.text(text)))
		for _, row := range sheet.Rows {
			for _, cell := range row.C {
				v := cell.V
				switch cell.T {
				case "s":
					i, err := strconv.Atoi(cell.V)
					if err != nil || i < 0 || i >= len(strs) {
						t.Fatalf("cell %s names shared string %q of %d", cell.R, cell.V, len(strs))
					}
					v = strs[i]
				case "inlineStr":
					v = cell.Is.T
				}
				line := fmt.Sprintf("  %s s=%s t=%s: %s", cell.R, cell.S, cell.T, strconv.Quote(c.text(v)))
				if cell.F != "" {
					line += " f=" + strconv.Quote(cell.F)
				}
				b.WriteString(line + "\n")
				if n, err := strconv.Atoi(cell.S); err == nil {
					styles[n] = true
				}
			}
		}
	}
	ids := make([]int, 0, len(styles))
	for id := range styles {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	b.WriteString("styles:\n")
	for _, id := range ids {
		st, err := wb.GetStyle(id)
		if err != nil {
			t.Fatalf("style %d: %v", id, err)
		}
		enc, _ := json.Marshal(st)
		fmt.Fprintf(&b, "  s=%d: %s\n", id, enc)
	}
	return b.String()
}

// s9Sheet is a workbook's sheet: its name and its part.
type s9Sheet struct {
	name string
	part *zip.File
}

// s9Sheets lists a workbook's sheets in its order, each with the part
// xl/workbook.xml's relationship names for it.
func s9Sheets(t *testing.T, zr *zip.Reader) []s9Sheet {
	t.Helper()
	parts := map[string]*zip.File{}
	for _, f := range zr.File {
		parts[f.Name] = f
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	for name, v := range map[string]interface{}{"xl/workbook.xml": &book, "xl/_rels/workbook.xml.rels": &rels} {
		f := parts[name]
		if f == nil {
			t.Fatalf("the workbook has no %s", name)
		}
		if err := xml.Unmarshal(s9ZipFile(t, f), v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	target := map[string]string{}
	for _, r := range rels.Rels {
		target[r.ID] = "xl/" + strings.TrimPrefix(r.Target, "/xl/")
	}
	var out []s9Sheet
	for _, sh := range book.Sheets {
		part := parts[target[sh.RID]]
		if part == nil {
			t.Fatalf("sheet %q names no part (%s)", sh.Name, target[sh.RID])
		}
		out = append(out, s9Sheet{sh.Name, part})
	}
	return out
}

// s9Zip lists an archive's entries; a document inside is shown as itself.
func s9Zip(t *testing.T, c s9Clock, body []byte) string {
	t.Helper()
	zr := s9ReadZip(t, body)
	var b strings.Builder
	b.WriteString("body: zip\n")
	for _, f := range zr.File {
		data := s9ZipFile(t, f)
		fmt.Fprintf(&b, "entry %s\n", c.text(f.Name))
		switch strings.ToLower(filepath.Ext(f.Name)) {
		case ".json", ".csv", ".xml", ".reqif", ".txt":
			b.WriteString(s9Text(c, data))
		case ".pdf":
			b.WriteString(s9PDF(t, c, data))
		case ".docx":
			b.WriteString(s9DOCX(t, c, data))
		case ".xlsx":
			b.WriteString(s9XLSX(t, c, data))
		default:
			// A file as uploaded: its bytes are the fixture's.
			fmt.Fprintf(&b, "body: %d bytes\n", len(data))
		}
	}
	return b.String()
}

// --- PDF ------------------------------------------------------------------
//
// The reader below is the S5 tour's (cmd/server/tour_bodies_test.go), which
// a package main test cannot share: the objects in file order, each page's
// content streams inflated, and the strings the text operators show.

type s9PDFObject struct {
	dict   string
	stream []byte
}

var (
	s9PDFObjRE      = regexp.MustCompile(`(\d+) 0 obj\s*`)
	s9PDFLengthRE   = regexp.MustCompile(`/Length (\d+)(?: 0 R)?`)
	s9PDFRefRE      = regexp.MustCompile(`(\d+) 0 R`)
	s9PDFContentsRE = regexp.MustCompile(`/Contents\s*(\[[^\]]*\]|\d+ 0 R)`)
	s9PDFKidsRE     = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
	s9PDFPageTypeRE = regexp.MustCompile(`/Type\s*/Page\b`)
	s9PDFPagesRE    = regexp.MustCompile(`/Type\s*/Pages\b`)
	s9PDFWSRE       = regexp.MustCompile(`\s+`)
)

func s9PDFObjects(t *testing.T, raw []byte) (map[int]*s9PDFObject, []int) {
	t.Helper()
	objs := map[int]*s9PDFObject{}
	var order []int
	type pending struct {
		num, start int
		length     string
	}
	var deferred []pending
	for i := 0; ; {
		loc := s9PDFObjRE.FindSubmatchIndex(raw[i:])
		if loc == nil {
			break
		}
		num, _ := strconv.Atoi(string(raw[i+loc[2] : i+loc[3]]))
		body := i + loc[1]
		streamAt := bytes.Index(raw[body:], []byte("stream"))
		endAt := bytes.Index(raw[body:], []byte("endobj"))
		if endAt < 0 {
			t.Fatalf("PDF object %d has no endobj", num)
		}
		o := &s9PDFObject{}
		objs[num] = o
		order = append(order, num)
		if streamAt >= 0 && streamAt < endAt {
			o.dict = string(raw[body : body+streamAt])
			start := body + streamAt + len("stream")
			if raw[start] == '\r' {
				start++
			}
			if raw[start] == '\n' {
				start++
			}
			m := s9PDFLengthRE.FindStringSubmatch(o.dict)
			if m == nil {
				t.Fatalf("PDF object %d's stream has no /Length", num)
			}
			if strings.HasSuffix(m[0], " 0 R") {
				end := bytes.Index(raw[start:], []byte("endstream"))
				deferred = append(deferred, pending{num, start, m[1]})
				i = start + end + len("endstream")
				continue
			}
			n, _ := strconv.Atoi(m[1])
			o.stream = raw[start : start+n]
			i = start + n
			continue
		}
		o.dict = string(raw[body : body+endAt])
		i = body + endAt + len("endobj")
	}
	for _, p := range deferred {
		ref, _ := strconv.Atoi(p.length)
		n, err := strconv.Atoi(strings.TrimSpace(objs[ref].dict))
		if err != nil {
			t.Fatalf("PDF object %d's /Length object %d is not a length", p.num, ref)
		}
		objs[p.num].stream = raw[p.start : p.start+n]
	}
	return objs, order
}

// s9PDF shows a PDF by its page count and the text on each page.
func s9PDF(t *testing.T, c s9Clock, raw []byte) string {
	t.Helper()
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", raw[:min(len(raw), 16)])
	}
	objs, order := s9PDFObjects(t, raw)
	var pages []int
	var walk func(num, depth int)
	walk = func(num, depth int) {
		o := objs[num]
		if o == nil || depth > 16 {
			return
		}
		if s9PDFPageTypeRE.MatchString(o.dict) && !s9PDFPagesRE.MatchString(o.dict) {
			pages = append(pages, num)
			return
		}
		if m := s9PDFKidsRE.FindStringSubmatch(o.dict); m != nil {
			for _, r := range s9PDFRefRE.FindAllStringSubmatch(m[1], -1) {
				k, _ := strconv.Atoi(r[1])
				walk(k, depth+1)
			}
		}
	}
	images, links, outline := 0, 0, 0
	for _, num := range order {
		d := objs[num].dict
		if s9PDFPagesRE.MatchString(d) && !strings.Contains(d, "/Parent") {
			walk(num, 0)
		}
		switch {
		case strings.Contains(d, "/Subtype /Image"):
			images++
		case strings.Contains(d, "/Subtype /Link"):
			links++
		}
		if strings.Contains(d, "/Title") && strings.Contains(d, "/Parent") && !s9PDFPageTypeRE.MatchString(d) {
			outline++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "body: pdf, %s, %d pages, %d images, %d links, %d outline entries\n",
		string(raw[:bytes.IndexByte(raw, '\n')]), len(pages), images, links, outline)
	for i, p := range pages {
		var texts []string
		if m := s9PDFContentsRE.FindStringSubmatch(objs[p].dict); m != nil {
			for _, r := range s9PDFRefRE.FindAllStringSubmatch(m[1], -1) {
				k, _ := strconv.Atoi(r[1])
				co := objs[k]
				if co == nil {
					continue
				}
				data := co.stream
				if strings.Contains(co.dict, "/FlateDecode") {
					zr, err := zlib.NewReader(bytes.NewReader(data))
					if err != nil {
						t.Fatalf("page %d: %v", i+1, err)
					}
					if data, err = io.ReadAll(zr); err != nil {
						t.Fatalf("page %d: %v", i+1, err)
					}
				}
				texts = append(texts, s9PDFStrings(data)...)
			}
		}
		text := strings.TrimSpace(s9PDFWSRE.ReplaceAllString(strings.Join(texts, " "), " "))
		fmt.Fprintf(&b, "page %d: %s\n", i+1, c.text(text))
	}
	return b.String()
}

// s9PDFStrings extracts the string operands of each Tj, TJ, ' and "
// operator of a content stream, in order, a TJ array's strings joined.
func s9PDFStrings(data []byte) []string {
	var out, pending []string
	isDelim := func(c byte) bool { return strings.IndexByte(" \t\r\n\f\x00()<>[]{}/%", c) >= 0 }
	for i := 0; i < len(data); {
		c := data[i]
		switch {
		case strings.IndexByte(" \t\r\n\f\x00[]{}", c) >= 0:
			i++
		case c == '%':
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
		case c == '(':
			depth, j := 1, i+1
			for ; j < len(data) && depth > 0; j++ {
				switch data[j] {
				case '\\':
					j++
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			pending = append(pending, s9PDFDecode(s9PDFStringBytes(string(data[i:j]))))
			i = j
		case c == '<' && i+1 < len(data) && data[i+1] == '<', c == '>' && i+1 < len(data) && data[i+1] == '>':
			i += 2
		case c == '<':
			j := bytes.IndexByte(data[i:], '>')
			if j < 0 {
				return out
			}
			pending = append(pending, s9PDFDecode(s9PDFStringBytes(string(data[i:i+j+1]))))
			i += j + 1
		case c == '/':
			for i++; i < len(data) && !isDelim(data[i]); i++ {
			}
		default:
			j := i + 1
			for j < len(data) && !isDelim(data[j]) {
				j++
			}
			word := string(data[i:j])
			i = j
			if word == "" || strings.IndexByte("+-.0123456789", word[0]) >= 0 {
				continue
			}
			switch word {
			case "Tj", "TJ", "'", "\"":
				out = append(out, strings.Join(pending, ""))
			}
			pending = nil
		}
	}
	return out
}

// s9PDFStringBytes decodes a literal (...) or hex <...> PDF string.
func s9PDFStringBytes(s string) []byte {
	if strings.HasPrefix(s, "<") {
		h := strings.Map(func(r rune) rune {
			if strings.ContainsRune(" \r\n\t", r) {
				return -1
			}
			return r
		}, s[1:len(s)-1])
		if len(h)%2 == 1 {
			h += "0"
		}
		b, _ := hex.DecodeString(h)
		return b
	}
	s = s[1 : len(s)-1]
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b = append(b, c)
			continue
		}
		i++
		switch e := s[i]; e {
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'b':
			b = append(b, '\b')
		case 'f':
			b = append(b, '\f')
		case '\r', '\n':
		default:
			if e >= '0' && e <= '7' {
				j := i
				for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
					j++
				}
				v, _ := strconv.ParseUint(s[i:j], 8, 8)
				b = append(b, byte(v))
				i = j - 1
			} else {
				b = append(b, e)
			}
		}
	}
	return b
}

// s9PDFDecode reads a PDF string's bytes as text: UTF-16BE when it carries
// the byte-order mark or is plainly two bytes per character (the code
// points gofpdf's UTF-8 fonts write), otherwise one byte per character.
func s9PDFDecode(b []byte) string {
	if bytes.HasPrefix(b, []byte{0xfe, 0xff}) {
		b = b[2:]
	} else if len(b) < 2 || len(b)%2 != 0 {
		return s9Latin1(b)
	} else {
		zeros := 0
		for i := 0; i < len(b); i += 2 {
			if b[i] == 0 {
				zeros++
			}
		}
		if zeros*2 < len(b)/2 {
			return s9Latin1(b)
		}
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(u))
}

func s9Latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// --- goldens ----------------------------------------------------------------

// checkS9Golden compares got with the golden at testdata/formats/<name>, or
// rewrites it under UPDATE_GOLDEN=1 (any other value compares).
func checkS9Golden(t *testing.T, dir, name, got string) {
	t.Helper()
	path := filepath.Join(dir, name)
	shown := strings.Replace(filepath.ToSlash(path), "testdata/", "internal/api/testdata/", 1)
	if os.Getenv(s9UpdateEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with:\n  %s", shown, err, s9Command)
	}
	if string(want) == got {
		return
	}
	t.Errorf("%s does not match what the code answers:\n%s\n"+
		"A refactor never changes a golden. If this is a deliberate behavior change, regenerate it with:\n  %s\n"+
		"(only %s=1 regenerates; any other value compares)",
		shown, s9Diff(string(want), got), s9Command, s9UpdateEnv)
}

// checkS9Orphans fails on a golden under dir, <name>.txt, whose name is
// not one of names: its case is gone, so it pins nothing and, unchecked,
// would let a dropped case pass. Regenerating deletes no file, so it holds
// under UPDATE_GOLDEN=1 as well. what names the case it lacks.
func checkS9Orphans(t *testing.T, dir string, names map[string]bool, what string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if name := strings.TrimSuffix(filepath.Base(file), ".txt"); !names[name] {
			t.Errorf("%s pins no %s: delete it with its case",
				strings.Replace(filepath.ToSlash(file), "testdata/", "internal/api/testdata/", 1), what)
		}
	}
}

// s9Diff is a line diff of the golden and the answer, the changed lines
// with two lines of context, capped at 60 lines. The lines both share at
// the start and at the end are set aside before the longest common
// subsequence is computed, so a change to one of the Word goldens (5,000
// lines and more) costs a table of the changed stretch only.
func s9Diff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	type op struct {
		kind byte
		text string
		line int
	}
	ops := make([]op, 0, len(a)+len(b)-pre-suf)
	for i := 0; i < pre; i++ {
		ops = append(ops, op{' ', a[i], i + 1})
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	n, m := len(ma), len(mb)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case ma[i] == mb[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && ma[i] == mb[j]:
			ops = append(ops, op{' ', ma[i], pre + i + 1})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', ma[i], pre + i + 1})
			i++
		default:
			ops = append(ops, op{'+', mb[j], pre + i + 1})
			j++
		}
	}
	for i := len(a) - suf; i < len(a); i++ {
		ops = append(ops, op{' ', a[i], i + 1})
	}
	show := make([]bool, len(ops))
	for i, o := range ops {
		if o.kind != ' ' {
			for k := max(0, i-2); k <= min(len(ops)-1, i+2); k++ {
				show[k] = true
			}
		}
	}
	var out strings.Builder
	printed := 0
	for i, o := range ops {
		if !show[i] {
			continue
		}
		if printed == 60 {
			out.WriteString("... (diff truncated)\n")
			break
		}
		if i == 0 || !show[i-1] {
			fmt.Fprintf(&out, "@@ golden line %d @@\n", o.line)
		}
		fmt.Fprintf(&out, "%c %s\n", o.kind, o.text)
		printed++
	}
	return out.String()
}
