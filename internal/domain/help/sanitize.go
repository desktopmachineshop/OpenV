package help

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// allowed is every element a manual topic may keep, with the attributes it
// may keep. Anything else is dropped with its content (script, style,
// iframe, form, object) or unwrapped to its children (the rest).
var allowed = map[atom.Atom][]string{
	atom.Article: {"id", "class"}, atom.Section: {"id", "class"}, atom.Div: {"id", "class"},
	atom.Nav: {"class"}, atom.Aside: {"class"}, atom.Figure: {"id"}, atom.Figcaption: nil,
	atom.H1: {"id"}, atom.H2: {"id"}, atom.H3: {"id"}, atom.H4: {"id"}, atom.H5: {"id"}, atom.H6: {"id"},
	atom.P: {"id", "class"}, atom.Ul: nil, atom.Ol: {"start"}, atom.Li: {"id"},
	atom.Dl: nil, atom.Dt: nil, atom.Dd: nil,
	atom.Table: {"class"}, atom.Caption: nil, atom.Thead: nil, atom.Tbody: nil, atom.Tfoot: nil,
	atom.Tr: nil, atom.Th: {"colspan", "rowspan", "scope"}, atom.Td: {"colspan", "rowspan"},
	atom.Strong: nil, atom.B: nil, atom.Em: nil, atom.I: nil, atom.U: nil, atom.S: nil,
	atom.Code: nil, atom.Pre: nil, atom.Kbd: nil, atom.Samp: nil, atom.Var: nil,
	atom.Sup: nil, atom.Sub: nil, atom.Span: {"class"}, atom.Q: nil, atom.Cite: nil,
	atom.Blockquote: nil, atom.Br: nil, atom.Hr: nil,
	atom.A: {"href"}, atom.Img: {"src", "alt", "width", "height"},
}

// dropped elements go with everything inside them.
var dropped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true, atom.Object: true, atom.Embed: true,
	atom.Form: true, atom.Input: true, atom.Button: true, atom.Select: true, atom.Textarea: true,
	atom.Template: true, atom.Noscript: true, atom.Svg: true, atom.Math: true, atom.Link: true, atom.Meta: true,
}

// Sanitize keeps only the allowed markup of an OpenD topic. Links to topics
// on the docs site gain data-help-topic with the topic's page id, which the
// drawer opens in place; other links open in a new tab; only https and
// same-page links survive, and images only from the docs site.
func Sanitize(fragment, docsURL string) string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, n := range nodes {
		clean(n, docsURL, &b)
	}
	return b.String()
}

func clean(n *html.Node, docsURL string, b *strings.Builder) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(html.EscapeString(n.Data))
		return
	case html.ElementNode:
	default:
		return
	}
	if dropped[n.DataAtom] {
		return
	}
	attrs, ok := allowed[n.DataAtom]
	if !ok || n.DataAtom == 0 {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			clean(c, docsURL, b)
		}
		return
	}
	kept := keepAttrs(n, attrs, docsURL)
	if kept == nil {
		// An image from anywhere else: dropped.
		return
	}
	b.WriteString("<" + n.Data)
	for _, a := range kept {
		b.WriteString(" " + a.Key + `="` + html.EscapeString(a.Val) + `"`)
	}
	b.WriteString(">")
	if n.DataAtom == atom.Br || n.DataAtom == atom.Hr || n.DataAtom == atom.Img {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		clean(c, docsURL, b)
	}
	b.WriteString("</" + n.Data + ">")
}

// keepAttrs returns the attributes n keeps, or nil when n must go.
func keepAttrs(n *html.Node, names []string, docsURL string) []html.Attribute {
	kept := []html.Attribute{}
	for _, a := range n.Attr {
		if a.Namespace != "" || !contains(names, a.Key) {
			continue
		}
		switch a.Key {
		case "href":
			link, ok := cleanLink(a.Val, docsURL)
			if !ok {
				continue
			}
			kept = append(kept, link...)
			continue
		case "src":
			if !strings.HasPrefix(a.Val, docsURL+"/") {
				return nil
			}
		case "id", "class":
			if strings.ContainsAny(a.Val, "<>\"'`") {
				continue
			}
		}
		kept = append(kept, a)
	}
	if n.DataAtom == atom.Img && !hasAttr(kept, "src") {
		return nil
	}
	return kept
}

// cleanLink returns the attributes a link keeps: in-page anchors as they
// are, a docs-site topic as its page with data-help-topic, any other https
// link with a new-tab target.
func cleanLink(href, docsURL string) ([]html.Attribute, bool) {
	if strings.HasPrefix(href, "#") {
		return []html.Attribute{{Key: "href", Val: href}}, true
	}
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "https" && !strings.HasPrefix(href, docsURL+"/") {
		return nil, false
	}
	attrs := []html.Attribute{{Key: "href", Val: href}, {Key: "target", Val: "_blank"}, {Key: "rel", Val: "noopener noreferrer"}}
	if rest, ok := strings.CutPrefix(href, docsURL+"/"); ok {
		page, fragment, _ := strings.Cut(rest, "#")
		if id, ok := strings.CutSuffix(page, ".html"); ok && validTopicID(id) {
			attrs = append(attrs, html.Attribute{Key: "data-help-topic", Val: id})
			if fragment != "" {
				attrs = append(attrs, html.Attribute{Key: "data-help-fragment", Val: fragment})
			}
		}
	}
	return attrs, true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func hasAttr(attrs []html.Attribute, key string) bool {
	for _, a := range attrs {
		if a.Key == key {
			return true
		}
	}
	return false
}
