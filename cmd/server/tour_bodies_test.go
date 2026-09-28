//go:build unix

package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// How a tour golden shows an answer's body (refactor plan §6.4 S5a–S5e).
// Text is kept as bytes; formats whose bytes cannot repeat from run to run
// (Go's flate output, embedded dates, font subsets) are shown by structure:
//
//   - empty: no body;
//   - json: the body's lines, exactly as sent once normalised, with
//     trailing_newline saying whether it ended in one; a one-line body also
//     gets parsed, the same bytes indented, which keeps key order, number
//     text, HTML escapes and null against [] (never decoded into a map);
//   - text (text/plain, HTML, Markdown, XML, ...): the lines, the same way;
//   - csv: the lines, and parsed as rows of fields;
//   - png: width, height, colour model and a hash of the decoded pixels
//     (not of the file, which depends on Go's encoder), plus the bytes in
//     base64 when they are 4 KiB or less;
//   - pdf: the header, the Info dictionary, the page count, each page's
//     text (the Tj/TJ strings of its inflated content streams, whitespace
//     collapsed), and the count of images, links and outline entries;
//   - zip, xlsx, docx: the entries in order with their method and modified
//     time; a text entry's lines (XML split after each tag that another tag
//     follows), a binary entry's size and hash, and for xlsx each sheet's
//     cells (ref, style, type, value with shared strings resolved) instead
//     of sharedStrings.xml, whose indices shift with deduplication;
//   - multipart (Range requests with several ranges): each part's headers
//     and bytes;
//   - bytes: anything else, base64 when 4 KiB or less, otherwise its size
//     and hash.
//
// Every text inside goes through the golden's normaliser.

// tourBodyView is what a step records of a body.
type tourBodyView struct {
	Kind            string          `json:"kind"`
	Lines           []string        `json:"body,omitempty"`
	TrailingNewline *bool           `json:"trailing_newline,omitempty"`
	Parsed          json.RawMessage `json:"parsed,omitempty"`
	Rows            [][]string      `json:"rows,omitempty"`
	Summary         any             `json:"summary,omitempty"`
	Base64          string          `json:"body_base64,omitempty"`
}

// tourSmallBinary is the size up to which a binary body is also recorded
// byte for byte, in base64.
const tourSmallBinary = 4 << 10

// viewBody builds a body's view. contentType is the answer's (possibly
// absent); raw is the body as received, decoded when it was gzipped.
func viewBody(n *tourNormaliser, contentType string, raw []byte) (tourBodyView, error) {
	if len(raw) == 0 {
		return tourBodyView{Kind: "empty"}, nil
	}
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "multipart/") {
		parts, err := summariseMultipart(n, raw, params["boundary"])
		return tourBodyView{Kind: "multipart", Summary: parts}, err
	}
	if isText(raw) {
		lines, nl := textLines(n.text(string(raw)))
		v := tourBodyView{Kind: "text", Lines: lines, TrailingNewline: &nl}
		switch {
		case json.Valid(raw):
			v.Kind = "json"
			if len(lines) == 1 {
				if norm := []byte(lines[0]); json.Valid(norm) {
					v.Parsed = json.RawMessage(norm)
				}
			}
		case mediaType == "text/csv":
			v.Kind = "csv"
			r := csv.NewReader(strings.NewReader(n.text(string(raw))))
			r.FieldsPerRecord = -1
			rows, err := r.ReadAll()
			if err != nil {
				return v, fmt.Errorf("a text/csv body that does not parse as CSV: %v", err)
			}
			v.Rows = rows
		}
		return v, nil
	}
	v := tourBodyView{Kind: "bytes"}
	switch {
	case bytes.HasPrefix(raw, pngSignature):
		v.Kind = "png"
		s, err := summarisePNG(raw)
		if err != nil {
			return v, err
		}
		v.Summary = s
	case bytes.HasPrefix(raw, []byte("%PDF")):
		v.Kind = "pdf"
		s, err := summarisePDF(n, raw)
		if err != nil {
			return v, err
		}
		return tourBodyView{Kind: "pdf", Summary: s}, nil
	case bytes.HasPrefix(raw, []byte("PK\x03\x04")):
		kind, s, err := summariseZip(n, raw)
		return tourBodyView{Kind: kind, Summary: s}, err
	default:
		if len(raw) > tourSmallBinary {
			v.Summary = digest(raw)
		}
	}
	if len(raw) <= tourSmallBinary {
		v.Base64 = base64.StdEncoding.EncodeToString(raw)
	}
	return v, nil
}

// bodyBand is the band of raw lengths a body stands for, for the gzip
// check: from the normaliser for text; a compressed format's length depends
// on what it compresses, so it gets a tenth either way.
func bodyBand(n *tourNormaliser, raw []byte) (int, int) {
	if isText(raw) {
		_, lo, hi := n.clone(true).normalise(string(raw))
		return lo, hi
	}
	if bytes.HasPrefix(raw, []byte("%PDF")) || bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		return len(raw) - len(raw)/10, len(raw) + len(raw)/10
	}
	return len(raw), len(raw)
}

// isText reports whether a body is UTF-8 text with no NUL byte.
func isText(raw []byte) bool { return utf8.Valid(raw) && bytes.IndexByte(raw, 0) < 0 }

// isPrintable reports whether bytes are UTF-8 text with no control
// character but tab and newline, which a JSON string shows as they are.
func isPrintable(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	for _, r := range string(raw) {
		if r < 0x20 && r != '\t' && r != '\n' {
			return false
		}
	}
	return true
}

// viewPartial shows a Range answer's bytes: a slice of a file is not a
// file of its own kind, so it is never summarised as one.
func viewPartial(raw []byte) tourBodyView {
	v := tourBodyView{Kind: "bytes"}
	if len(raw) > tourSmallBinary {
		v.Summary = digest(raw)
	} else {
		v.Base64 = base64.StdEncoding.EncodeToString(raw)
	}
	return v
}

// textLines splits text into its lines and says whether it ended in a
// newline; joining the lines with "\n" (and the newline) gives it back.
func textLines(text string) ([]string, bool) {
	nl := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n"), nl
}

// xmlLines splits an XML text after each tag that another tag follows, so a
// one-line document reads a tag per line; joining the lines gives it back.
func xmlLines(text string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '>' && text[i+1] == '<' {
			out = append(out, text[start:i+1])
			start = i + 1
		}
	}
	return append(out, text[start:])
}

type tourDigest struct {
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

func digest(raw []byte) tourDigest {
	sum := sha256.Sum256(raw)
	return tourDigest{Size: len(raw), SHA256: hex.EncodeToString(sum[:])}
}

// ----------------------------------------------------------------------------
// PNG

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

type tourPNGSummary struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ColorModel   string `json:"color_model"`
	PixelsSHA256 string `json:"pixels_sha256"`
}

// summarisePNG decodes a PNG and hashes its pixels as non-premultiplied
// RGBA, row by row, which no encoder setting changes.
func summarisePNG(raw []byte) (tourPNGSummary, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return tourPNGSummary{}, fmt.Errorf("a PNG that does not decode: %v", err)
	}
	b := img.Bounds()
	h := sha256.New()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			h.Write([]byte{c.R, c.G, c.B, c.A})
		}
	}
	return tourPNGSummary{Width: b.Dx(), Height: b.Dy(), ColorModel: colorModelName(img),
		PixelsSHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func colorModelName(img image.Image) string {
	switch img.(type) {
	case *image.Paletted:
		return "paletted"
	case *image.Gray, *image.Gray16:
		return "gray"
	case *image.NRGBA, *image.NRGBA64:
		return "nrgba"
	case *image.RGBA, *image.RGBA64:
		return "rgba"
	}
	return fmt.Sprintf("%T", img)
}

// ----------------------------------------------------------------------------
// Multipart byte ranges

// tourPart is one part of a multipart body, a request's form or a
// multipart/byteranges answer: its headers, and its bytes as text, as
// base64, or, past tourSmallBinary, by their size and digest (an upload
// made to reach a size limit).
type tourPart struct {
	Headers []string    `json:"headers"`
	Text    *string     `json:"body,omitempty"`
	Base64  string      `json:"body_base64,omitempty"`
	Digest  *tourDigest `json:"digest,omitempty"`
}

func summariseMultipart(n *tourNormaliser, raw []byte, boundary string) ([]tourPart, error) {
	if boundary == "" {
		return nil, fmt.Errorf("a multipart answer with no boundary")
	}
	mr := multipart.NewReader(bytes.NewReader(raw), boundary)
	var parts []tourPart
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return parts, nil
		}
		if err != nil {
			return nil, fmt.Errorf("a multipart answer that does not parse: %v", err)
		}
		data, err := io.ReadAll(p)
		if err != nil {
			return nil, err
		}
		var part tourPart
		names := make([]string, 0, len(p.Header))
		for k := range p.Header {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			for _, v := range p.Header[k] {
				part.Headers = append(part.Headers, k+": "+n.text(v))
			}
		}
		if len(data) > tourSmallBinary {
			d := digest(data)
			part.Digest = &d
		} else if isPrintable(data) {
			text := n.text(string(data))
			part.Text = &text
		} else {
			part.Base64 = base64.StdEncoding.EncodeToString(data)
		}
		parts = append(parts, part)
	}
}

// ----------------------------------------------------------------------------
// Zip: bundles, xlsx, docx

type tourZipEntry struct {
	Name     string   `json:"name"`
	Method   string   `json:"method"`
	Modified string   `json:"modified"`
	Lines    []string `json:"text,omitempty"`
	Cells    []string `json:"cells,omitempty"`
	Nested   any      `json:"nested,omitempty"`
	Digest   any      `json:"digest,omitempty"`
}

type tourZipSummary struct {
	Entries []tourZipEntry `json:"entries"`
}

// summariseZip lists a zip's entries. An xlsx workbook (xl/workbook.xml)
// gets a cell dump per sheet instead of its shared strings; a nested
// document is summarised by its own kind.
func summariseZip(n *tourNormaliser, raw []byte) (string, tourZipSummary, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "zip", tourZipSummary{}, fmt.Errorf("a zip that does not open: %v", err)
	}
	kind := "zip"
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	switch {
	case files["xl/workbook.xml"] != nil:
		kind = "xlsx"
	case files["word/document.xml"] != nil:
		kind = "docx"
	}
	var out tourZipSummary
	for _, f := range zr.File {
		e := tourZipEntry{Name: n.text(f.Name), Method: zipMethod(f.Method), Modified: f.Modified.UTC().Format("2006-01-02T15:04:05Z")}
		data, err := readZipFile(f)
		if err != nil {
			return kind, out, err
		}
		switch {
		case kind == "xlsx" && f.Name == "xl/sharedStrings.xml":
			e.Lines = []string{"(read into the sheets' cells below)"}
		case kind == "xlsx" && strings.HasPrefix(f.Name, "xl/worksheets/") && strings.HasSuffix(f.Name, ".xml"):
			frame, cells, err := xlsxSheet(n, data, files["xl/sharedStrings.xml"])
			if err != nil {
				return kind, out, fmt.Errorf("%s: %v", f.Name, err)
			}
			e.Lines, e.Cells = frame, cells
		case isText(data) && isXMLName(f.Name):
			e.Lines = xmlLines(n.text(string(data)))
		case isText(data) && !bytes.HasPrefix(data, []byte("%PDF")):
			e.Lines, _ = textLines(n.text(string(data)))
		default:
			v, err := viewBody(n, "", data)
			if err != nil {
				return kind, out, fmt.Errorf("%s: %v", f.Name, err)
			}
			switch v.Kind {
			case "pdf", "zip", "xlsx", "docx", "png":
				e.Nested = v
			default:
				e.Digest = digest(data)
			}
		}
		out.Entries = append(out.Entries, e)
	}
	return kind, out, nil
}

func isXMLName(name string) bool {
	switch path.Ext(name) {
	case ".xml", ".rels", ".vml":
		return true
	}
	return false
}

func zipMethod(m uint16) string {
	switch m {
	case zip.Store:
		return "store"
	case zip.Deflate:
		return "deflate"
	}
	return strconv.Itoa(int(m))
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %v", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// xlsxSheet returns a worksheet's XML with its sheetData left out (the
// frame: columns, views, merges) and its cells, one line each:
// "A1 s=1 t=s: value", the value resolved from the shared strings.
func xlsxSheet(n *tourNormaliser, data []byte, sst *zip.File) ([]string, []string, error) {
	var strs []string
	if sst != nil {
		raw, err := readZipFile(sst)
		if err != nil {
			return nil, nil, err
		}
		var table struct {
			SI []struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if err := xml.Unmarshal(raw, &table); err != nil {
			return nil, nil, fmt.Errorf("sharedStrings.xml: %v", err)
		}
		for _, si := range table.SI {
			s := si.T
			for _, r := range si.R {
				s += r.T
			}
			strs = append(strs, s)
		}
	}
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
	if err := xml.Unmarshal(data, &sheet); err != nil {
		return nil, nil, err
	}
	var cells []string
	for _, row := range sheet.Rows {
		for _, c := range row.C {
			v := c.V
			switch c.T {
			case "s":
				i, err := strconv.Atoi(c.V)
				if err != nil || i < 0 || i >= len(strs) {
					return nil, nil, fmt.Errorf("cell %s names shared string %q of %d", c.R, c.V, len(strs))
				}
				v = strs[i]
			case "inlineStr":
				v = c.Is.T
			}
			line := fmt.Sprintf("%s s=%s t=%s: %s", c.R, c.S, c.T, strconv.Quote(n.text(v)))
			if c.F != "" {
				line += " f=" + strconv.Quote(c.F)
			}
			cells = append(cells, line)
		}
	}
	text := string(data)
	if i, j := strings.Index(text, "<sheetData"), strings.Index(text, "</sheetData>"); i >= 0 && j > i {
		text = text[:i] + "<sheetData>(the cells below)" + text[j:]
	} else if i := strings.Index(text, "<sheetData/>"); i >= 0 {
		text = text[:i] + "<sheetData>(no cells)</sheetData>" + text[i+len("<sheetData/>"):]
	}
	return xmlLines(n.text(text)), cells, nil
}

// ----------------------------------------------------------------------------
// PDF

type tourPDFPage struct {
	Text string `json:"text"`
}

type tourPDFSummary struct {
	Header        string        `json:"header"`
	Info          []string      `json:"info"`
	Pages         int           `json:"pages"`
	PageText      []tourPDFPage `json:"page_text"`
	Images        int           `json:"images"`
	Links         int           `json:"links"`
	OutlineItems  int           `json:"outline_items"`
	UnreadStreams int           `json:"unread_streams,omitempty"`
}

// pdfObject is one "N 0 obj ... endobj": its dictionary text and its
// stream, if any, still encoded.
type pdfObject struct {
	dict   string
	stream []byte
}

var (
	pdfObjRE      = regexp.MustCompile(`(\d+) 0 obj\s*`)
	pdfLengthRE   = regexp.MustCompile(`/Length (\d+)(?: 0 R)?`)
	pdfRefRE      = regexp.MustCompile(`(\d+) 0 R`)
	pdfContentsRE = regexp.MustCompile(`/Contents\s*(\[[^\]]*\]|\d+ 0 R)`)
	pdfKidsRE     = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
	pdfInfoRE     = regexp.MustCompile(`/Info (\d+) 0 R`)
	pdfPageTypeRE = regexp.MustCompile(`/Type\s*/Page\b`)
	pdfPagesRE    = regexp.MustCompile(`/Type\s*/Pages\b`)
	pdfWSRE       = regexp.MustCompile(`\s+`)
)

// parsePDFObjects reads a PDF's objects in file order. A stream's length is
// taken from /Length, directly or through the object it names.
func parsePDFObjects(raw []byte) (map[int]*pdfObject, []int, error) {
	objs := map[int]*pdfObject{}
	var order []int
	type pending struct {
		num    int
		start  int
		length string
	}
	i := 0
	var deferred []pending
	for {
		loc := pdfObjRE.FindSubmatchIndex(raw[i:])
		if loc == nil {
			break
		}
		num, _ := strconv.Atoi(string(raw[i+loc[2] : i+loc[3]]))
		body := i + loc[1]
		streamAt := bytes.Index(raw[body:], []byte("stream"))
		endAt := bytes.Index(raw[body:], []byte("endobj"))
		if endAt < 0 {
			return nil, nil, fmt.Errorf("object %d has no endobj", num)
		}
		o := &pdfObject{}
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
			m := pdfLengthRE.FindStringSubmatch(o.dict)
			if m == nil {
				return nil, nil, fmt.Errorf("object %d's stream has no /Length", num)
			}
			if strings.HasSuffix(m[0], " 0 R") {
				// An indirect length: resolved below, after its object.
				end := bytes.Index(raw[start:], []byte("endstream"))
				if end < 0 {
					return nil, nil, fmt.Errorf("object %d's stream has no endstream", num)
				}
				deferred = append(deferred, pending{num: num, start: start, length: m[1]})
				i = start + end + len("endstream")
				continue
			}
			n, _ := strconv.Atoi(m[1])
			if start+n > len(raw) {
				return nil, nil, fmt.Errorf("object %d's stream runs past the file", num)
			}
			o.stream = raw[start : start+n]
			i = start + n
			continue
		}
		o.dict = string(raw[body : body+endAt])
		i = body + endAt + len("endobj")
	}
	for _, p := range deferred {
		ref, _ := strconv.Atoi(p.length)
		lo := objs[ref]
		if lo == nil {
			return nil, nil, fmt.Errorf("object %d's /Length names missing object %d", p.num, ref)
		}
		n, err := strconv.Atoi(strings.TrimSpace(lo.dict))
		if err != nil || p.start+n > len(raw) {
			return nil, nil, fmt.Errorf("object %d's /Length object %d is not a length", p.num, ref)
		}
		objs[p.num].stream = raw[p.start : p.start+n]
	}
	return objs, order, nil
}

// summarisePDF reads a PDF's structure and text.
func summarisePDF(n *tourNormaliser, raw []byte) (tourPDFSummary, error) {
	var out tourPDFSummary
	out.Header = string(raw[:bytes.IndexByte(raw, '\n')])
	objs, order, err := parsePDFObjects(raw)
	if err != nil {
		return out, fmt.Errorf("a PDF that does not parse: %v", err)
	}
	var pages []int
	var walk func(num int, depth int)
	walk = func(num int, depth int) {
		o := objs[num]
		if o == nil || depth > 16 {
			return
		}
		if pdfPageTypeRE.MatchString(o.dict) && !pdfPagesRE.MatchString(o.dict) {
			pages = append(pages, num)
			return
		}
		if m := pdfKidsRE.FindStringSubmatch(o.dict); m != nil {
			for _, r := range pdfRefRE.FindAllStringSubmatch(m[1], -1) {
				k, _ := strconv.Atoi(r[1])
				walk(k, depth+1)
			}
		}
	}
	for _, num := range order {
		if pdfPagesRE.MatchString(objs[num].dict) && !strings.Contains(objs[num].dict, "/Parent") {
			walk(num, 0)
		}
	}
	for _, num := range order {
		d := objs[num].dict
		switch {
		case strings.Contains(d, "/Subtype /Image"):
			out.Images++
		case strings.Contains(d, "/Subtype /Link"):
			out.Links++
		}
		if strings.Contains(d, "/Title") && strings.Contains(d, "/Parent") && !pdfPageTypeRE.MatchString(d) {
			out.OutlineItems++
		}
	}
	out.Pages = len(pages)
	for _, p := range pages {
		m := pdfContentsRE.FindStringSubmatch(objs[p].dict)
		var texts []string
		if m != nil {
			for _, r := range pdfRefRE.FindAllStringSubmatch(m[1], -1) {
				k, _ := strconv.Atoi(r[1])
				co := objs[k]
				if co == nil {
					continue
				}
				data := co.stream
				if strings.Contains(co.dict, "/FlateDecode") {
					zr, err := zlib.NewReader(bytes.NewReader(data))
					if err != nil {
						out.UnreadStreams++
						continue
					}
					data, err = io.ReadAll(zr)
					if err != nil {
						out.UnreadStreams++
						continue
					}
				}
				texts = append(texts, pdfTextStrings(data)...)
			}
		}
		text := strings.TrimSpace(pdfWSRE.ReplaceAllString(strings.Join(texts, " "), " "))
		out.PageText = append(out.PageText, tourPDFPage{Text: n.text(text)})
	}
	if m := pdfInfoRE.FindSubmatch(raw); m != nil {
		k, _ := strconv.Atoi(string(m[1]))
		if o := objs[k]; o != nil {
			out.Info = pdfInfo(n, o.dict)
		}
	}
	return out, nil
}

var pdfInfoKeyRE = regexp.MustCompile(`/(\w+)\s*(\((?:[^()\\]|\\.)*\)|<[0-9A-Fa-f]*>)`)

// pdfInfo lists an Info dictionary's string entries, decoded, sorted.
func pdfInfo(n *tourNormaliser, dict string) []string {
	var out []string
	for _, m := range pdfInfoKeyRE.FindAllStringSubmatch(dict, -1) {
		out = append(out, m[1]+": "+n.text(decodePDFString(pdfStringBytes(m[2]))))
	}
	sort.Strings(out)
	return out
}

// pdfTextStrings extracts the text a content stream shows: the string
// operands of each Tj, TJ, ' and " operator, in order, a TJ array's strings
// joined. Operands of any other operator are dropped.
func pdfTextStrings(data []byte) []string {
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
			pending = append(pending, decodePDFString(pdfStringBytes(string(data[i:j]))))
			i = j
		case c == '<' && i+1 < len(data) && data[i+1] == '<', c == '>' && i+1 < len(data) && data[i+1] == '>':
			i += 2
		case c == '<':
			j := bytes.IndexByte(data[i:], '>')
			if j < 0 {
				return out
			}
			pending = append(pending, decodePDFString(pdfStringBytes(string(data[i:i+j+1]))))
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
				continue // a number operand
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

// pdfStringBytes decodes a literal (...) or hex <...> PDF string.
func pdfStringBytes(s string) []byte {
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
			// A line continuation.
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

// decodePDFString reads a PDF string's bytes as text: UTF-16BE when it
// carries the byte-order mark or is plainly two bytes per character (the
// code points gofpdf's UTF-8 fonts write), otherwise one byte per
// character (Latin-1, which the core fonts' WinAnsi matches for ASCII).
func decodePDFString(b []byte) string {
	if bytes.HasPrefix(b, []byte{0xfe, 0xff}) {
		return utf16BE(b[2:])
	}
	if len(b) >= 2 && len(b)%2 == 0 {
		zeros := 0
		for i := 0; i < len(b); i += 2 {
			if b[i] == 0 {
				zeros++
			}
		}
		if zeros*2 >= len(b)/2 {
			return utf16BE(b)
		}
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func utf16BE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(u))
}
