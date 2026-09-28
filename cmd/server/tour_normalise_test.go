//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The tour's normaliser (refactor plan §6.4 S5a–S5e). A tour golden stores
// each answer as bytes, so that key order, the trailing newline, HTML
// escaping and null against [] all survive; only the values that change from
// run to run or machine to machine are replaced by tokens, on the raw text,
// and nothing else is touched:
//
//   - names the tour registered (tour.register, tourResult.capture): an id,
//     a session token, a workspace, written <name>;
//   - literals the tour keeps as they are (tour.keep): the zero time
//     0001-01-01T00:00:00Z, which links_snapshot entries carry, and any
//     timestamp the tour itself sent;
//   - the server's port after 127.0.0.1: or localhost:, written <port>, and
//     the test's temporary directory, <tmp>;
//   - the running release, <release-version> (and <stable-version>), so a
//     promotion never changes a tour golden;
//   - then the generic patterns, in this order: a 60-hex multipart boundary
//     (<boundary>); a 64-hex token (<hex64:N>); a version-4 UUID
//     (<uuid:N>); an RFC 3339 timestamp with or without a fraction
//     (<time@...> or <time>, below); an HTTP-date (<http-date>); a PDF date (<pdf-date>); a
//     wall-clock "YYYY-MM-DD hh:mm[:ss]" (<datetime>); a filename stamp
//     _YYYYMMDD_hhmmss (_<stamp>); a date (<date>); then the area's own
//     (tour.pattern).
//
// <uuid:N> and <hex64:N> are numbered by first appearance in the golden, so a
// reference from one answer to another stays visible. Timestamps are not
// numbered by value (created_at and updated_at differ by nanoseconds on
// create and may round to the same microsecond when read back); a time the
// server minted during the tour is written instead with the exchange during
// which it was minted (tourClock): <time@step 12>, <time@before step 12> (a
// setup or probe after step 11), <time@events after step 12>, <time@boot>.
// The tour sends its requests one at a time and the server stamps a time
// before it answers, so a time falls between the end of the exchange before
// and the end of its own; the database rounds to the microsecond, which no
// exchange is short enough to cross. So which moment a field holds is pinned,
// and created_at, updated_at, valid_from and valid_to swapped or taken from
// the wrong row change the golden. A time before the tour began (one the tour
// sent) or after its last exchange (an expiry to come) stays <time>, and so
// does a time on a route the area declared as writing whole seconds
// (tour.wholeSeconds: a truncated time falls in an earlier exchange by
// chance). Every other token is flat. Version-5 UUIDs (the file template's
// id) and the nil UUID are not random and stay as they are.
//
// normalise also returns the band of raw lengths the normalised text stands
// for: a timestamp's fraction has 0 to 9 digits, a port 4 or 5, and the
// temporary directory depends on the machine. The gzip check uses it to fail
// on an answer that could land on either side of the compressor's 1,400-byte
// floor on another run (tourStraddles).

// tourLiteral is a value replaced by a token wherever it appears.
type tourLiteral struct {
	value  string
	token  string // the replacement; the value itself for a kept literal
	minLen int    // the band of raw lengths the value stands for
	maxLen int
}

// tourPattern is a generic token. The regexp's first group, when it has
// one, is the part replaced; otherwise the whole match is.
type tourPattern struct {
	class    string
	re       *regexp.Regexp
	numbered bool   // <class:N> by first appearance
	flat     string // the token when not numbered
	fraction bool   // a timestamp: its fraction may have 0 to 9 digits
}

// tourNormaliser holds the registry's literals, the patterns and the
// numbering of one golden.
type tourNormaliser struct {
	literals []tourLiteral
	patterns []tourPattern
	numbers  map[string]map[string]int // class -> value -> N
	counts   map[string]int
	flatOnly bool       // numbered classes written without their number (the upload list's first sort)
	flatNew  bool       // a sort key: a value not numbered yet written without a number, a known one with it
	clock    *tourClock // where a minted time falls; nil: every time is <time>
	whole    bool       // the text comes from a route that writes whole seconds (tour.wholeSeconds)
}

// tourClock places the times the server minted during the tour: the
// client's clock when the tour began and when it had read each exchange's
// answer, with what the exchange was. It is shared by a normaliser and its
// clones.
type tourClock struct {
	start time.Time
	marks []tourMark
}

type tourMark struct {
	end   time.Time
	label string
}

// window names the exchange during which a time was minted: the first whose
// answer was read at or after it. "" for a time before the tour began or
// after its last exchange.
func (c *tourClock) window(tm time.Time) string {
	if tm.Before(c.start) {
		return ""
	}
	i := sort.Search(len(c.marks), func(i int) bool { return !c.marks[i].end.Before(tm) })
	switch {
	case i == len(c.marks):
		return ""
	case i == 0:
		return "boot"
	}
	return c.marks[i].label
}

// The generic patterns. A hex token is delimited by anything but a hex
// digit (a UUID in a file name is followed by "_"); the delimiters are not
// part of what is replaced (group 1). The v4 UUID pattern requires the
// version and variant nibbles, so v5 and nil UUIDs are left alone, and it
// also matches a UUID written with underscores (DOCX bookmark names), which
// is numbered as the same UUID.
var (
	tourBoundaryRE = regexp.MustCompile(`(?:^|[^0-9a-f])([0-9a-f]{60})(?:$|[^0-9a-f])`)
	tourHex64RE    = regexp.MustCompile(`(?:^|[^0-9a-f])([0-9a-f]{64})(?:$|[^0-9a-f])`)
	tourUUIDRE     = regexp.MustCompile(`(?:^|[^0-9a-f])([0-9a-f]{8}([-_])[0-9a-f]{4}[-_]4[0-9a-f]{3}[-_][89ab][0-9a-f]{3}[-_][0-9a-f]{12})(?:$|[^0-9a-f])`)
	tourTimeRE     = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})`)
	tourHTTPDateRE = regexp.MustCompile(`\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d{2} (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} \d{2}:\d{2}:\d{2} GMT`)
	tourPDFDateRE  = regexp.MustCompile(`D:\d{14}(?:Z|[+-]\d{2}'\d{2}'?)?`)
	tourDateTimeRE = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2}(?:\.\d{1,9})?)?`)
	tourStampRE    = regexp.MustCompile(`_(\d{8}_\d{6})\b`)
	tourDateRE     = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	tourFractionRE = regexp.MustCompile(`\.\d{1,9}`)
)

func newTourNormaliser() *tourNormaliser {
	return &tourNormaliser{
		patterns: []tourPattern{
			{class: "boundary", re: tourBoundaryRE, flat: "<boundary>"},
			{class: "hex64", re: tourHex64RE, numbered: true},
			{class: "uuid", re: tourUUIDRE, numbered: true},
			{class: "time", re: tourTimeRE, flat: "<time>", fraction: true},
			{class: "http-date", re: tourHTTPDateRE, flat: "<http-date>"},
			{class: "pdf-date", re: tourPDFDateRE, flat: "<pdf-date>"},
			{class: "datetime", re: tourDateTimeRE, flat: "<datetime>", fraction: true},
			{class: "stamp", re: tourStampRE, flat: "<stamp>"},
			{class: "date", re: tourDateRE, flat: "<date>"},
		},
		numbers: map[string]map[string]int{},
		counts:  map[string]int{},
	}
}

// addLiteral registers a value; a longer value is replaced first. A UUID
// is registered in its underscore form too.
func (n *tourNormaliser) addLiteral(l tourLiteral) {
	if l.minLen == 0 && l.maxLen == 0 {
		l.minLen, l.maxLen = len(l.value), len(l.value)
	}
	n.literals = append(n.literals, l)
	if uuidRE.MatchString(l.value) && len(l.value) == 36 && l.token != l.value {
		u := l
		u.value = strings.ReplaceAll(l.value, "-", "_")
		n.literals = append(n.literals, u)
	}
	sort.SliceStable(n.literals, func(i, j int) bool { return len(n.literals[i].value) > len(n.literals[j].value) })
}

// addPattern appends an area's own pattern after the generic ones.
func (n *tourNormaliser) addPattern(p tourPattern) { n.patterns = append(n.patterns, p) }

// clone copies the numbering, so that a comparison or a sort key can be
// normalised without numbering anything new in the golden.
func (n *tourNormaliser) clone(flatOnly bool) *tourNormaliser {
	c := &tourNormaliser{literals: n.literals, patterns: n.patterns, numbers: map[string]map[string]int{},
		counts: map[string]int{}, flatOnly: flatOnly, clock: n.clock, whole: n.whole}
	for class, m := range n.numbers {
		c.numbers[class] = map[string]int{}
		for v, k := range m {
			c.numbers[class][v] = k
		}
	}
	for class, k := range n.counts {
		c.counts[class] = k
	}
	return c
}

// sortKey is a clone for sorting: a value the golden has numbered keeps its
// number, so elements that differ only in such values are told apart, and a
// value not numbered yet is written without one, so the key does not depend
// on which random id came first. It never numbers anything, so one key sorts
// any number of bodies the same way.
func (n *tourNormaliser) sortKey() *tourNormaliser {
	c := n.clone(false)
	c.flatNew = true
	return c
}

func (n *tourNormaliser) number(class, value string) string {
	if n.flatOnly {
		return "<" + class + ">"
	}
	m := n.numbers[class]
	if m == nil {
		m = map[string]int{}
		n.numbers[class] = m
	}
	k, ok := m[value]
	if !ok && n.flatNew {
		return "<" + class + ">"
	}
	if !ok {
		n.counts[class]++
		k = n.counts[class]
		m[value] = k
	}
	return fmt.Sprintf("<%s:%d>", class, k)
}

// sentinel marks a replaced span until every pattern has run, so that no
// pattern matches inside another's token. Private-use runes and capital
// letters, which no pattern matches.
func sentinel(i int) string {
	s := ""
	for {
		s = string(rune('A'+i%26)) + s
		i /= 26
		if i == 0 {
			break
		}
	}
	return "\ue000" + s + "\ue001"
}

var sentinelRE = regexp.MustCompile("\ue000([A-Z]+)\ue001")

func sentinelIndex(s string) int {
	i := 0
	for _, r := range s {
		i = i*26 + int(r-'A')
	}
	return i
}

// normalise replaces the varying values of raw by tokens and returns the
// band of raw lengths the result stands for.
func (n *tourNormaliser) normalise(raw string) (text string, minLen, maxLen int) {
	minLen, maxLen = len(raw), len(raw)
	var slots []string
	text = raw
	for _, l := range n.literals {
		if l.value == "" || !strings.Contains(text, l.value) {
			continue
		}
		c := strings.Count(text, l.value)
		minLen += c * (l.minLen - len(l.value))
		maxLen += c * (l.maxLen - len(l.value))
		s := sentinel(len(slots))
		slots = append(slots, l.token)
		text = strings.ReplaceAll(text, l.value, s)
	}
	for _, p := range n.patterns {
		// A delimiter a match consumed may be the next match's; so repeat
		// until nothing matches (a replaced span is a sentinel, which no
		// pattern matches).
		for pass := 0; pass < 4 && p.re.MatchString(text); pass++ {
			text = n.replace(p, text, &slots, &minLen, &maxLen)
		}
	}
	text = sentinelRE.ReplaceAllStringFunc(text, func(s string) string {
		return slots[sentinelIndex(sentinelRE.FindStringSubmatch(s)[1])]
	})
	return text, minLen, maxLen
}

// replace replaces every match of one pattern by a sentinel for its token.
func (n *tourNormaliser) replace(p tourPattern, text string, slots *[]string, minLen, maxLen *int) string {
	var b strings.Builder
	last := 0
	for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[0], m[1]
		if len(m) >= 4 && m[2] >= 0 {
			start, end = m[2], m[3]
		}
		value := text[start:end]
		if p.fraction && strings.Count(value, ":") >= 2 {
			// A time to the second may carry a fraction of 0 to 9 digits,
			// with its dot (RFC3339Nano drops trailing zeros).
			frac := len(tourFractionRE.FindString(value))
			*minLen -= frac
			*maxLen += 10 - frac
		}
		token := p.flat
		if p.class == "time" {
			token = n.timeToken(value)
		}
		if p.numbered {
			token = n.number(p.class, strings.ReplaceAll(value, "_", "-"))
		}
		b.WriteString(text[last:start])
		b.WriteString(sentinel(len(*slots)))
		*slots = append(*slots, token)
		last = end
	}
	b.WriteString(text[last:])
	return b.String()
}

// timeToken is the token of an RFC 3339 time: the exchange during which the
// server minted it (tourClock.window), or <time>.
func (n *tourNormaliser) timeToken(value string) string {
	c := n.clock
	if c == nil || (n.whole && !strings.Contains(value, ".")) {
		return "<time>"
	}
	tm, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "<time>"
	}
	if label := c.window(tm); label != "" {
		return "<time@" + label + ">"
	}
	return "<time>"
}

// text is normalise without the band.
func (n *tourNormaliser) text(raw string) string {
	t, _, _ := n.normalise(raw)
	return t
}

// compressFloor is the compression middleware's floor (internal/api
// compression.go, compressMinBytes): an answer of at least this many bytes
// is gzipped when the client accepts it.
const compressFloor = 1400

// tourStraddles reports whether a raw length band holds lengths on both
// sides of the compressor's floor.
func tourStraddles(minLen, maxLen int) bool { return minLen < compressFloor && maxLen >= compressFloor }

// ----------------------------------------------------------------------------
// JSON, read without losing its bytes

// jsonSpan is a value's [start, end) in a JSON text.
type jsonSpan struct{ start, end int }

// jsonScanner walks a JSON text for the byte spans of its values. It never
// re-encodes anything: the tour reorders or captures by span, and the bytes
// stay the server's.
type jsonScanner struct {
	data []byte
	i    int
}

func (s *jsonScanner) ws() {
	for s.i < len(s.data) && strings.IndexByte(" \t\r\n", s.data[s.i]) >= 0 {
		s.i++
	}
}

// value skips one value and returns its span.
func (s *jsonScanner) value() (jsonSpan, error) {
	s.ws()
	start := s.i
	if s.i >= len(s.data) {
		return jsonSpan{}, fmt.Errorf("unexpected end of JSON")
	}
	switch c := s.data[s.i]; c {
	case '{', '[':
		closer := byte('}')
		if c == '[' {
			closer = ']'
		}
		s.i++
		s.ws()
		if s.i < len(s.data) && s.data[s.i] == closer {
			s.i++
			return jsonSpan{start, s.i}, nil
		}
		for {
			if c == '{' {
				if _, err := s.str(); err != nil {
					return jsonSpan{}, err
				}
				s.ws()
				if s.i >= len(s.data) || s.data[s.i] != ':' {
					return jsonSpan{}, fmt.Errorf("expected ':' at byte %d", s.i)
				}
				s.i++
			}
			if _, err := s.value(); err != nil {
				return jsonSpan{}, err
			}
			s.ws()
			if s.i >= len(s.data) {
				return jsonSpan{}, fmt.Errorf("unexpected end of JSON")
			}
			if s.data[s.i] == ',' {
				s.i++
				s.ws()
				continue
			}
			if s.data[s.i] == closer {
				s.i++
				return jsonSpan{start, s.i}, nil
			}
			return jsonSpan{}, fmt.Errorf("unexpected %q at byte %d", s.data[s.i], s.i)
		}
	case '"':
		_, err := s.str()
		return jsonSpan{start, s.i}, err
	default:
		for s.i < len(s.data) && strings.IndexByte(",}] \t\r\n", s.data[s.i]) < 0 {
			s.i++
		}
		return jsonSpan{start, s.i}, nil
	}
}

// str skips a string and returns it decoded.
func (s *jsonScanner) str() (string, error) {
	s.ws()
	start := s.i
	if s.i >= len(s.data) || s.data[s.i] != '"' {
		return "", fmt.Errorf("expected a string at byte %d", s.i)
	}
	s.i++
	for s.i < len(s.data) {
		switch s.data[s.i] {
		case '\\':
			s.i += 2
		case '"':
			s.i++
			var out string
			err := json.Unmarshal(s.data[start:s.i], &out)
			return out, err
		default:
			s.i++
		}
	}
	return "", fmt.Errorf("unterminated string")
}

// children returns the members of the object or elements of the array at
// span: names ("" for elements) and value spans.
func (s *jsonScanner) children(span jsonSpan) ([]string, []jsonSpan, error) {
	s.i = span.start
	s.ws()
	open := s.data[s.i]
	if open != '{' && open != '[' {
		return nil, nil, nil
	}
	s.i++
	s.ws()
	var names []string
	var spans []jsonSpan
	if s.data[s.i] == '}' || s.data[s.i] == ']' {
		return nil, nil, nil
	}
	for {
		name := ""
		if open == '{' {
			var err error
			if name, err = s.str(); err != nil {
				return nil, nil, err
			}
			s.ws()
			s.i++ // ':'
		}
		v, err := s.value()
		if err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		spans = append(spans, v)
		s.ws()
		if s.data[s.i] == ',' {
			s.i++
			continue
		}
		return names, spans, nil
	}
}

// jsonFind returns the spans of the values a pointer names (RFC 6901, with
// "*" standing for every member or element), in document order.
func jsonFind(data []byte, pointer string) ([]jsonSpan, error) {
	s := &jsonScanner{data: data}
	root, err := s.value()
	if err != nil {
		return nil, err
	}
	spans := []jsonSpan{root}
	if pointer == "" {
		return spans, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("a JSON pointer starts with /: %q", pointer)
	}
	for _, seg := range strings.Split(pointer[1:], "/") {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		var next []jsonSpan
		for _, sp := range spans {
			names, kids, err := s.children(sp)
			if err != nil {
				return nil, err
			}
			isArray := data[sp.start] == '['
			for k, kid := range kids {
				switch {
				case seg == "*":
					next = append(next, kid)
				case isArray && seg == strconv.Itoa(k):
					next = append(next, kid)
				case !isArray && names[k] == seg:
					next = append(next, kid)
				}
			}
		}
		spans = next
	}
	return spans, nil
}

// members returns the spans of an object's members, each from its name's
// opening quote to the end of its value.
func (s *jsonScanner) members(span jsonSpan) ([]jsonSpan, error) {
	s.i = span.start + 1 // past '{'
	s.ws()
	if s.i < len(s.data) && s.data[s.i] == '}' {
		return nil, nil
	}
	var out []jsonSpan
	for {
		s.ws()
		start := s.i
		if _, err := s.str(); err != nil {
			return nil, err
		}
		s.ws()
		if s.i >= len(s.data) || s.data[s.i] != ':' {
			return nil, fmt.Errorf("expected ':' at byte %d", s.i)
		}
		s.i++
		v, err := s.value()
		if err != nil {
			return nil, err
		}
		out = append(out, jsonSpan{start, v.end})
		s.ws()
		if s.i < len(s.data) && s.data[s.i] == ',' {
			s.i++
			continue
		}
		return out, nil
	}
}

// jsonReorder sorts the elements of every array a pointer names by key,
// keeping each element's bytes and the separators between them; for an
// object it sorts the members, each keyed by its whole "name":value text.
// The sort is stable, so elements with equal keys keep the server's order.
func jsonReorder(data []byte, pointer string, key func([]byte) string) ([]byte, error) {
	spans, err := jsonFind(data, pointer)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), data...)
	for _, sp := range spans {
		s := &jsonScanner{data: data}
		var kids []jsonSpan
		switch data[sp.start] {
		case '[':
			_, kids, err = s.children(sp)
		case '{':
			kids, err = s.members(sp)
		default:
			return nil, fmt.Errorf("%s names a value that is neither an array nor an object", pointer)
		}
		if err != nil {
			return nil, err
		}
		if len(kids) < 2 {
			continue
		}
		order := make([]int, len(kids))
		keys := make([]string, len(kids))
		for i, k := range kids {
			order[i] = i
			keys[i] = key(data[k.start:k.end])
		}
		sort.SliceStable(order, func(a, b int) bool { return keys[order[a]] < keys[order[b]] })
		var b bytes.Buffer
		for pos, k := range kids {
			src := kids[order[pos]]
			b.Write(data[src.start:src.end])
			if pos+1 < len(kids) {
				b.Write(data[k.end:kids[pos+1].start])
			}
		}
		copy(out[kids[0].start:kids[len(kids)-1].end], b.Bytes())
	}
	return out, nil
}

// jsonValue decodes the value a pointer names (no wildcard), numbers kept
// as json.Number.
func jsonValue(data []byte, pointer string) (any, error) {
	spans, err := jsonFind(data, pointer)
	if err != nil {
		return nil, err
	}
	if len(spans) != 1 {
		return nil, fmt.Errorf("%q names %d values, not one", pointer, len(spans))
	}
	d := json.NewDecoder(bytes.NewReader(data[spans[0].start:spans[0].end]))
	d.UseNumber()
	var v any
	err = d.Decode(&v)
	return v, err
}

// jsonType describes a decoded value's JSON type: string, number, boolean,
// null, array[<element types>] or object{<key>: <type>, ...}, keys sorted.
func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case []any:
		seen := map[string]bool{}
		var kinds []string
		for _, e := range x {
			if k := jsonType(e); !seen[k] {
				seen[k] = true
				kinds = append(kinds, k)
			}
		}
		sort.Strings(kinds)
		return "array[" + strings.Join(kinds, "|") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ": " + jsonType(x[k])
		}
		return "object{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprintf("%T", v)
}

// ----------------------------------------------------------------------------
// A check of the normaliser, with no server

// TestTourNormaliser pins what the tour's normaliser does to a few answers,
// with no database: the tokens and their numbering, the kept literals, the
// raw-length band, and reordering an unordered array without touching its
// bytes.
func TestTourNormaliser(t *testing.T) {
	n := newTourNormaliser()
	n.addLiteral(tourLiteral{value: "0001-01-01T00:00:00Z", token: "0001-01-01T00:00:00Z"})
	n.addLiteral(tourLiteral{value: "11111111-2222-4333-8444-555555555555", token: "<p1>"})
	n.addLiteral(tourLiteral{value: "/tmp/TestX123/001", token: "<tmp>", minLen: 8, maxLen: 160})
	n.addLiteral(tourLiteral{value: "127.0.0.1:40123", token: "127.0.0.1:<port>", minLen: 14, maxLen: 15})
	raw := `{"id":"11111111-2222-4333-8444-555555555555","org":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",` +
		`"again":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","v5":"1e241c0f-c902-5f43-8907-21ec0b1b2414",` +
		`"at":"2026-09-27T15:14:00.1234Z","zero":"0001-01-01T00:00:00Z","path":"/tmp/TestX123/001/a_b.png",` +
		`"url":"http://127.0.0.1:40123/x","name":"Baseline 2026-09-27 15:14","file":"p_20260927_151400.json",` +
		`"html":"\u003cb\u003e","file":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee_x.png","mark":"a_11111111_2222_4333_8444_555555555555",` +
		`"pair":"bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee,cccccccc-bbbb-4ccc-8ddd-eeeeeeeeeeee"}` + "\n"
	got, lo, hi := n.normalise(raw)
	want := `{"id":"<p1>","org":"<uuid:1>","again":"<uuid:1>","v5":"1e241c0f-c902-5f43-8907-21ec0b1b2414",` +
		`"at":"<time>","zero":"0001-01-01T00:00:00Z","path":"<tmp>/a_b.png","url":"http://127.0.0.1:<port>/x",` +
		`"name":"Baseline <datetime>","file":"p_<stamp>.json","html":"\u003cb\u003e","file":"<uuid:1>_x.png","mark":"a_<p1>",` +
		`"pair":"<uuid:2>,<uuid:3>"}` + "\n"
	if got != want {
		t.Errorf("normalise:\n got %s\nwant %s", got, want)
	}
	// The fraction .1234 may have 0 to 9 digits, the directory 8 to 160
	// bytes, the port 4 or 5 digits.
	wantLo := len(raw) - 5 + (8 - len("/tmp/TestX123/001")) + (14 - 15)
	wantHi := len(raw) + 5 + (160 - len("/tmp/TestX123/001"))
	if lo != wantLo || hi != wantHi {
		t.Errorf("band: got [%d, %d], want [%d, %d]", lo, hi, wantLo, wantHi)
	}
	if !tourStraddles(1399, 1400) || tourStraddles(1400, 1500) || tourStraddles(10, 1399) {
		t.Error("tourStraddles does not bracket the 1,400-byte floor")
	}
	list := []byte(`[{"n":"b","id":"x"},{"n":"a","id":"y"},{"n":"c","id":"z"}]`)
	sorted, err := jsonReorder(list, "", func(b []byte) string { return string(b) })
	if err != nil || string(sorted) != `[{"n":"a","id":"y"},{"n":"b","id":"x"},{"n":"c","id":"z"}]` {
		t.Errorf("jsonReorder: %s %v", sorted, err)
	}
	indented := []byte("{\n  \"l\": [\n    2,\n    1\n  ]\n}")
	sorted, err = jsonReorder(indented, "/l", func(b []byte) string { return string(b) })
	if err != nil || string(sorted) != "{\n  \"l\": [\n    1,\n    2\n  ]\n}" {
		t.Errorf("jsonReorder (indented): %q %v", sorted, err)
	}
	// An object's members, each keyed by its "name":value text, the bytes
	// and separators kept; an empty object and a scalar.
	object := []byte(`{"m": {"b": [1, 2], "a" :"x\"}", "c":{}}, "e":{}}`)
	sorted, err = jsonReorder(object, "/m", func(b []byte) string { return string(b) })
	if err != nil || string(sorted) != `{"m": {"a" :"x\"}", "b": [1, 2], "c":{}}, "e":{}}` {
		t.Errorf("jsonReorder (object): %s %v", sorted, err)
	}
	if sorted, err = jsonReorder(object, "/e", func(b []byte) string { return string(b) }); err != nil || !bytes.Equal(sorted, object) {
		t.Errorf("jsonReorder (empty object): %s %v", sorted, err)
	}
	if _, err = jsonReorder(object, "/m/a", func(b []byte) string { return string(b) }); err == nil {
		t.Error("jsonReorder sorts a string")
	}
	v, err := jsonValue([]byte(`{"a":[{"k":"x","id":7}]}`), "/a/0/id")
	if err != nil || jsonType(v) != "number" || fmt.Sprint(v) != "7" {
		t.Errorf("jsonValue: %v %v", v, err)
	}
	if got := jsonType(map[string]any{"b": []any{"x", json.Number("1")}, "a": nil}); got != "object{a: null, b: array[number|string]}" {
		t.Errorf("jsonType: %s", got)
	}

	// The clock: a time is written with the exchange whose answer was read
	// first at or after it; one before the tour or after its last exchange,
	// and a whole second on a route that writes whole seconds, is <time>.
	t0 := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	n = newTourNormaliser()
	n.clock = &tourClock{start: t0, marks: []tourMark{{at(400), "before step 1"}, {at(900), "before step 1"},
		{at(1500), "step 1"}, {at(1600), "events after step 1"}, {at(2500), "step 2"}}}
	for _, c := range []struct{ raw, want string }{
		{"2026-09-27T11:59:59.999Z", "<time>"},                  // before the tour began
		{"2026-09-27T12:00:00.2Z", "<time@boot>"},               // before the first answer was read
		{"2026-09-27T12:00:00.4Z", "<time@boot>"},               // at it
		{"2026-09-27T12:00:00.400001Z", "<time@before step 1>"}, // a microsecond after it
		{"2026-09-27T14:00:01.2+02:00", "<time@step 1>"},        // another zone, the same moment
		{"2026-09-27T12:00:01Z", "<time@step 1>"},               // a whole second
		{"2026-09-27T12:00:02.5Z", "<time@step 2>"},
		{"2026-09-27T12:00:02.500000001Z", "<time>"}, // after the last exchange: an expiry to come
	} {
		if got := n.text(c.raw); got != c.want {
			t.Errorf("the clock: %s is %s, want %s", c.raw, got, c.want)
		}
	}
	n.whole = true
	if got := n.text("2026-09-27T12:00:01Z 2026-09-27T12:00:01.2Z"); got != "<time> <time@step 1>" {
		t.Errorf("the clock on a route that writes whole seconds: %s", got)
	}
	if c := n.clone(false); c.clock != n.clock || !c.whole {
		t.Error("a clone does not share the clock, or does not keep whole")
	}
}
