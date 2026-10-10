package api

import "net/http"

// Case is one request TestFixtureAnswersAlike sends to the fixture before
// and after the rewrite.
type Case struct {
	Name    string
	Handler http.HandlerFunc
	Method  string
	Target  string
	Body    string
}

// Cases sends every handler each body that takes another path through it.
var Cases = []Case{
	{"create", createItem, "POST", "/", `{"title":"<x> & y","tags":["t"]}`},
	{"create bad body", createItem, "POST", "/", `{`},
	{"create empty body", createItem, "POST", "/", ``},
	{"get", getItem, "GET", "/?id=1", ``},
	{"get missing", getItem, "GET", "/?id=9", ``},
	{"accept later", acceptItem, "POST", "/?mode=later", ``},
	{"accept now", acceptItem, "POST", "/", ``},
	{"rename", renameItem, "POST", "/", `{"title":"t"}`},
	{"rename bad body", renameItem, "POST", "/", `[`},
	{"filter", filterItems, "POST", "/", `{"id":"1","meta":{"k":"<v>"}}`},
	{"filter bad body", filterItems, "POST", "/", `nope`},
	{"optional", optionalBody, "POST", "/", `{"id":"3"}`},
	{"optional empty", optionalBody, "POST", "/", ``},
	{"optional bad body", optionalBody, "POST", "/", `{"id":`},
	{"raw", rawBody, "POST", "/", `abc`},
	{"raw empty", rawBody, "POST", "/", ``},
	{"bare", bareList, "GET", "/", ``},
	{"bare empty", bareList, "GET", "/?empty=1", ``},
	{"set early", setEarly, "GET", "/?x=1", ``},
	{"set early other", setEarly, "GET", "/", ``},
	{"retry", withRetry, "GET", "/", ``},
	{"charset", withCharset, "GET", "/", ``},
	{"late type", lateType, "GET", "/", ``},
	{"commented", commented, "GET", "/", ``},
	{"reads writer", readsWriter, "GET", "/", ``},
	{"calls closure", callsClosure, "GET", "/", ``},
	{"checked", checked, "GET", "/", ``},
	{"indented", indented, "GET", "/", ``},
	{"to buffer", toBuffer, "GET", "/", ``},
	{"local writer", localWriter, "GET", "/", ``},
	{"stream", stream, "GET", "/", ``},
	{"logged decode", loggedDecode, "POST", "/", `{`},
	{"pass through", passThrough, "POST", "/", `{`},
}
