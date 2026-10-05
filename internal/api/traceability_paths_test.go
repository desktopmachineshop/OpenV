package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Refactor step X11a (plan §6.7, class C; quirks Q3 and Q4): the four paths
// that write links today, characterized side by side so that X11b and X11c,
// which route them through internal/domain/traceability with a Policy of
// their own each, can re-run this file unchanged.
//
//   - POST /api/v1/links (CreateLink, link_handlers.go), with its removal
//     counterpart DELETE /api/v1/links/{id};
//   - the managed link edits of PUT /api/v1/artifacts/{id}
//     (pendingLinkAdds and pendingLinkRemoves: UpdateArtifact,
//     processManagedLinkChanges and autoVersionLinkedArtifacts);
//   - the proposal appliers (ProposalAppliers, proposal_appliers.go), which
//     POST /api/v1/proposals/{id}/approve runs for a proposal a
//     proposal-mode run filed through the two link routes;
//   - the guided drafts, POST /api/v1/guided-sessions/{id}/drafts
//     (guided.DefaultService.MaterializeDrafts).
//
// Each case drives the real handler through the router, over the real
// artifact, link, proposal and guided services, whose repositories here keep
// rows as Postgres does (JSON round trips like jsonb, a link's ends are UUID
// columns, a deleted link is closed, not removed, lists are newest first),
// and records in one transcript, in the order they happen: the response, the
// links created, deleted or turned suspect, each new artifact version with
// the links_snapshot it carries (Q4), the chatter notes (Q3), the domain
// events with their payload's Go types (I10) and actor, and the proposals.
// Ids are written as names (artifacts by role, links L1, L2, … in the order
// they are made) and times as T.
//
// What the paths do differently today is what Policy will name:
//
//	               POST /links           managed edit          applier               guided draft
//	OnInvalid      400, nothing made     skipped, 200; the     apply_failed, 500,    made unchecked; one
//	                                     note lists it (Q3)    nothing made          the store refuses
//	                                                                                 is skipped
//	FlowDown gate  on refines, for a     none                  none (a run's         none
//	               member; a run passes                        propose passes it)
//	TargetRole     editor, viewer for    editor on both ends'  none at apply; the    none
//	               refines; a project    projects, any type,   run reaches its own
//	               not reached is 400;   add or removal        project only
//	               delete: editor on
//	               the source alone
//	Snapshots      both ends, always     the edited artifact   both ends, always     none; nothing is
//	               written, [] too       while a link remains  written, [] too       versioned
//	                                     (Q4); the other end,
//	                                     always (of a removed
//	                                     link between two
//	                                     others, its target)
//	Events         link.created,         artifact.updated      link.created,         none
//	               link.deleted          only (Q3)             link.deleted;
//	                                                           proposal.created
//	                                                           when proposed
//	Actor          the caller            the caller            system                none; the session
//	                                                                                 keeps created_by, and
//	                                                                                 a proposal-mode run's
//	                                                                                 drafts are not proposed
//
// Chatter: every auto-version writes "Auto-updated to version N due to link
// changes" (link-change, no author), N the version read before it plus 1
// (Q4); the managed edit writes the edited artifact's summary
// (version-change, no author), which lists the links requested, made or not
// (Q3). Side effects are pinned in the order they happen: the bus's
// subscribers run as the event is published.
//
// The managed edit collects the other ends it auto-versions in a Go map, so
// with two or more their order changes from run to run: no case here has
// more than one. Duplicates are made on every path: the links table has no
// unique key.
//
// The repositories implement exactly the methods today's paths reach; any
// other panics through the embedded interface, so a path that starts to
// reach a new one fails here loudly rather than answering wrongly.

const (
	tpOrg      = "org-1"
	tpOtherOrg = "org-2"
	tpP        = "proj-p" // the parent project
	tpC        = "proj-c" // a child project of the same workspace
	tpX        = "proj-x" // a project of another workspace
	tpEditor   = "u-editor"
	tpSupplier = "u-supplier"
	tpPhantom  = "00000000-0000-4000-8000-0000000000ff"
)

// tpSeed is every artifact a fixture starts with, each at version 1.
var tpSeed = []struct{ name, id, project, typ, title string }{
	{"req", "00000000-0000-4000-8000-000000000001", tpP, "requirement", "Req one"},
	{"req2", "00000000-0000-4000-8000-000000000002", tpP, "requirement", "Req two"},
	{"need", "00000000-0000-4000-8000-000000000003", tpP, "user-need", "Need one"},
	{"tc", "00000000-0000-4000-8000-000000000004", tpP, "test-case", "Test one"},
	{"di", "00000000-0000-4000-8000-000000000005", tpP, "design-item", "Design one"},
	{"creq", "00000000-0000-4000-8000-000000000006", tpC, "requirement", "Supplier req"},
	{"xreq", "00000000-0000-4000-8000-000000000007", tpX, "requirement", "Other workspace req"},
}

// tpCaller is who sends a request, and the name the transcript gives it.
type tpCaller struct {
	name string
	set  func(*http.Request) *http.Request
}

// tpUser is a signed-in member: u-editor edits P and C; u-supplier edits C
// and only views P. Neither is a member of X's workspace.
func tpUser(id string) tpCaller {
	user := &users.User{ID: id, Email: id + "@example.com", Name: id}
	return tpCaller{name: id, set: func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}}
}

// tpRun is an agent run's token in project P: run-proposal's agent writes
// in proposal mode, run-direct's directly.
func tpRun(id, agentID string) tpCaller {
	project := tpP
	run := &agentruns.Run{ID: id, OrgID: tpOrg, AgentID: agentID, ProjectID: &project, Status: agentruns.StatusRunning}
	return tpCaller{name: id, set: func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), ctxRun, run))
	}}
}

var (
	tpAsEditor      = tpUser(tpEditor)
	tpAsSupplier    = tpUser(tpSupplier)
	tpAsProposalRun = tpRun("run-proposal", "agent-proposal")
	tpAsDirectRun   = tpRun("run-direct", "agent-direct")
)

// tpFixture is one handler over one in-memory store, and the transcript of
// what its requests did.
type tpFixture struct {
	t         *testing.T
	router    *mux.Router
	proposals *proposals.DefaultService
	arts      *tpArtifactRepo
	links     *tpLinkRepo
	ids       map[string]string // name → id
	names     map[string]string // id → name
	seq       map[string]int    // name prefix → last number given
	lines     []string
	quiet     bool
}

// newTPFixture builds the fixture. With flowDown false the workspace is on
// the stable channel with no stable release turned on, so the flow-down
// feature is closed to its members.
func newTPFixture(t *testing.T, flowDown bool) *tpFixture {
	t.Helper()
	fx := &tpFixture{t: t, ids: map[string]string{}, names: map[string]string{}, seq: map[string]int{}}
	fx.arts = &tpArtifactRepo{fx: fx, rows: map[string][]byte{}}
	fx.links = &tpLinkRepo{fx: fx}
	artifactService := artifacts.NewDefaultService(fx.arts)
	linkService := links.NewDefaultService(fx.links)
	artifactService.SetLinkSuspector(linkService)
	notes := &tpChatter{fx: fx}
	bus := &tpBus{fx: fx}
	fx.proposals = proposals.NewDefaultService(&tpProposalRepo{fx: fx, rows: map[string][]byte{}}, proposals.Appliers{})
	guidedService := guided.NewDefaultService(&tpGuidedRepo{fx: fx, rows: map[string][]byte{}},
		artifactService, linkService, notes, nil, bus)

	plan := orgs.PlanFree
	if !flowDown {
		plan = orgs.PlanBusiness
	}
	h := newTestHandler(t, func(h *Handler) {
		h.ArtifactService = artifactService
		h.LinkService = linkService
		h.ChatterService = notes
		h.Bus = bus
		h.ProposalService = fx.proposals
		h.GuidedService = guidedService
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
			tpP: {ID: tpP, OrgID: tpOrg, Name: "Parent"},
			tpC: {ID: tpC, OrgID: tpOrg, Name: "Child"},
			tpX: {ID: tpX, OrgID: tpOtherOrg, Name: "Elsewhere"},
		}}
		h.OrgService = &fakeOrgService{plan: plan, roles: map[string]map[string]string{
			tpOrg:      {tpEditor: orgs.RoleMember, tpSupplier: orgs.RoleMember},
			tpOtherOrg: {},
		}}
		h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
			tpP: {tpEditor: members.RoleEditor, tpSupplier: members.RoleViewer},
			tpC: {tpEditor: members.RoleEditor, tpSupplier: members.RoleEditor},
			tpX: {},
		}}
		h.AgentService = &fakeAgentService{byID: map[string]*agents.Agent{
			"agent-proposal": {ID: "agent-proposal", OrgID: tpOrg, WriteMode: agents.WriteModeProposal},
			"agent-direct":   {ID: "agent-direct", OrgID: tpOrg, WriteMode: agents.WriteModeDirect},
		}}
	})
	// As the composition root does once the handler exists.
	fx.proposals.SetAppliers(h.ProposalAppliers())
	fx.router = mux.NewRouter()
	h.RegisterRoutes(fx.router)

	fx.nameID("phantom", tpPhantom)
	for _, s := range tpSeed {
		fx.nameID(s.name, s.id)
		a := &artifacts.Artifact{ID: s.id, ProjectID: s.project, Type: s.typ, Title: s.title,
			Body: "The " + s.name + ".", Status: artifacts.StatusDraft,
			Attributes: map[string]interface{}{"status": artifacts.StatusDraft}, Version: 1}
		fx.arts.rows[s.id] = tpMarshal(t, a)
	}
	return fx
}

func (fx *tpFixture) nameID(name, id string) {
	fx.ids[name] = id
	fx.names[id] = name
}

// mint names a new row: prefix plus the next number.
func (fx *tpFixture) mint(prefix, id string) string {
	fx.seq[prefix]++
	name := fmt.Sprintf("%s%d", prefix, fx.seq[prefix])
	fx.nameID(name, id)
	return name
}

// name is the transcript's name for an id, or the id itself.
func (fx *tpFixture) name(id string) string {
	if n, ok := fx.names[id]; ok {
		return n
	}
	return id
}

func (fx *tpFixture) logf(format string, args ...interface{}) {
	if !fx.quiet {
		fx.lines = append(fx.lines, fmt.Sprintf(format, args...))
	}
}

var tpPlaceholder = regexp.MustCompile(`\{\{([A-Za-z0-9]+)\}\}`)

// fill puts the id of each {{name}} into s; tpShown puts the name.
func (fx *tpFixture) fill(s string) string {
	return tpPlaceholder.ReplaceAllStringFunc(s, func(m string) string {
		name := tpPlaceholder.FindStringSubmatch(m)[1]
		id, ok := fx.ids[name]
		if !ok {
			fx.t.Fatalf("no row is named %q", name)
		}
		return id
	})
}

func tpShown(s string) string { return tpPlaceholder.ReplaceAllString(s, "$1") }

// send serves one request through the router and returns its answer.
func (fx *tpFixture) send(as tpCaller, method, path, body string) *httptest.ResponseRecorder {
	fx.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, fx.fill(path), nil)
	} else {
		r = httptest.NewRequest(method, fx.fill(path), strings.NewReader(fx.fill(body)))
		r.Header.Set("Content-Type", "application/json")
	}
	r = as.set(r)
	w := httptest.NewRecorder()
	fx.router.ServeHTTP(w, r)
	return w
}

// step sends a request and records it, its answer and what it wrote.
func (fx *tpFixture) step(as tpCaller, method, path, body string) {
	fx.t.Helper()
	line := "> " + method + " " + tpShown(path) + " as " + as.name
	if body != "" {
		line += " " + tpShown(body)
	}
	fx.lines = append(fx.lines, line)
	mark := len(fx.lines)
	w := fx.send(as, method, path, body)
	effects := append([]string(nil), fx.lines[mark:]...)
	fx.lines = append(fx.lines[:mark], fmt.Sprintf("< %d %s", w.Code, fx.answer(w.Body.Bytes())))
	for _, e := range effects {
		fx.lines = append(fx.lines, "  "+e)
	}
}

// setup sends a request whose writes the case does not record, and wants a
// 2xx; it returns the answer.
func (fx *tpFixture) setup(as tpCaller, method, path, body string) *httptest.ResponseRecorder {
	fx.t.Helper()
	fx.quiet = true
	defer func() { fx.quiet = false }()
	w := fx.send(as, method, path, body)
	if w.Code < 200 || w.Code > 299 {
		fx.t.Fatalf("setup %s %s answered %d: %s", method, path, w.Code, w.Body.String())
	}
	return w
}

// propose files run-proposal's create_link proposal in P straight through
// the proposal service, with a payload no route would file, unrecorded.
func (fx *tpFixture) propose(payload map[string]interface{}) {
	fx.t.Helper()
	fx.quiet = true
	defer func() { fx.quiet = false }()
	if _, err := fx.proposals.Propose("run-proposal", tpP, proposals.OpCreateLink, nil, payload); err != nil {
		fx.t.Fatal(err)
	}
}

// note records a line of the transcript's own.
func (fx *tpFixture) note(s string) { fx.lines = append(fx.lines, "# "+s) }

// expect compares the transcript so far with want, and starts a new one.
func (fx *tpFixture) expect(want string) {
	fx.t.Helper()
	got := strings.Join(fx.lines, "\n")
	fx.lines = nil
	if want = strings.TrimSpace(tpDedent(want)); got != want {
		fx.t.Errorf("transcript differs.\n--- got:\n%s\n--- want:\n%s\n--- first difference: %s", got, want, tpFirstDiff(got, want))
	}
}

// tpDedent drops the tab indentation of a raw string written inside a
// function.
func tpDedent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimLeft(l, "\t")
	}
	return strings.Join(lines, "\n")
}

func tpFirstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d\n  got:  %s\n  want: %s", i+1, gl, wl)
		}
	}
	return "none"
}

// ---- rendering ----

var tpTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

// norm walks decoded JSON, naming ids and blanking times.
func (fx *tpFixture) norm(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, e := range x {
			out[k] = fx.norm(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = fx.norm(e)
		}
		return out
	case string:
		if tpTime.MatchString(x) {
			return "T"
		}
		return fx.nameAll(x)
	}
	return v
}

// nameAll replaces every id the fixture knows inside s by its name.
func (fx *tpFixture) nameAll(s string) string {
	for id, name := range fx.names {
		s = strings.ReplaceAll(s, id, name)
	}
	return s
}

// render is v as normalised JSON, with sorted keys.
func (fx *tpFixture) render(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		fx.t.Fatal(err)
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		fx.t.Fatal(err)
	}
	out, err := json.Marshal(fx.norm(decoded))
	if err != nil {
		fx.t.Fatal(err)
	}
	return string(out)
}

// answer renders a response body: a link, an artifact or a proposal in its
// short form, anything else as normalised JSON.
func (fx *tpFixture) answer(body []byte) string {
	if len(strings.TrimSpace(string(body))) == 0 {
		return "(no body)"
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return strings.TrimSpace(string(body))
	}
	_, hasFrom := m["from_id"]
	_, hasOp := m["op"]
	_, hasTitle := m["title"]
	switch {
	case hasOp:
		return "proposal " + fx.proposalLine(m)
	case hasFrom:
		return "link " + fx.linkLine(m)
	case hasTitle:
		attrs, _ := m["attributes"].(map[string]interface{})
		return fmt.Sprintf("artifact %s v%v %s", fx.name(m["id"].(string)), m["version"], fx.snapshotOf(attrs))
	}
	return fx.render(m)
}

// linkLine is a link (decoded JSON) in short form.
func (fx *tpFixture) linkLine(m map[string]interface{}) string {
	str := func(k string) string { s, _ := m[k].(string); return s }
	return fmt.Sprintf("%s %s-%s->%s suspect=%v attributes=%s", fx.name(str("id")), fx.name(str("from_id")),
		str("type"), fx.name(str("to_id")), m["suspect"], fx.render(m["attributes"]))
}

// snapshotOf is an attributes map's links_snapshot in short form.
func (fx *tpFixture) snapshotOf(attrs map[string]interface{}) string {
	snap, ok := attrs["links_snapshot"]
	if !ok {
		return "links_snapshot absent"
	}
	list, ok := snap.([]interface{})
	if !ok {
		return "links_snapshot=" + fx.render(snap)
	}
	parts := make([]string, len(list))
	for i, e := range list {
		m, _ := e.(map[string]interface{})
		parts[i] = fx.linkLine(m)
	}
	return "links_snapshot=[" + strings.Join(parts, ", ") + "]"
}

func (fx *tpFixture) proposalLine(m map[string]interface{}) string {
	str := func(k string) string { s, _ := m[k].(string); return s }
	line := fmt.Sprintf("%s %s %s", fx.name(str("id")), str("op"), str("status"))
	if e := str("applied_entity_id"); e != "" {
		line += " entity=" + fx.name(e)
	}
	if by := str("reviewed_by"); by != "" {
		line += " reviewed_by=" + by
	}
	if str("status") != proposals.StatusPending {
		line += fmt.Sprintf(" note=%q", fx.nameAll(str("review_note")))
	}
	return line
}

func tpMarshal(t *testing.T, v interface{}) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// ---- the store: repositories that keep rows as Postgres does ----

// tpArtifactRepo keeps each artifact's current version as JSON, as the
// artifacts table keeps attributes as jsonb, so every read is a fresh copy.
type tpArtifactRepo struct {
	artifacts.Repository
	fx   *tpFixture
	rows map[string][]byte
}

func (r *tpArtifactRepo) load(id string) (*artifacts.Artifact, error) {
	raw, ok := r.rows[id]
	if !ok {
		return nil, artifacts.ErrNotFound
	}
	a := &artifacts.Artifact{}
	if err := json.Unmarshal(raw, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *tpArtifactRepo) Save(a *artifacts.Artifact) error {
	r.rows[a.ID] = tpMarshal(r.fx.t, a)
	name := r.fx.mint("new", a.ID)
	r.fx.logf("artifact %s created in %s: %s %q v%d status=%s", name, a.ProjectID, a.Type, a.Title, a.Version, a.Status)
	return nil
}

func (r *tpArtifactRepo) FindByID(id string) (*artifacts.Artifact, error) { return r.load(id) }

func (r *tpArtifactRepo) Update(a *artifacts.Artifact) error {
	before, err := r.load(a.ID)
	if err != nil {
		return err
	}
	r.rows[a.ID] = tpMarshal(r.fx.t, a)
	stored, _ := r.load(a.ID)
	var changed []string
	if before.Type != stored.Type {
		changed = append(changed, "type="+stored.Type)
	}
	if before.Title != stored.Title {
		changed = append(changed, fmt.Sprintf("title=%q", stored.Title))
	}
	if before.Body != stored.Body {
		changed = append(changed, fmt.Sprintf("body=%q", stored.Body))
	}
	if before.Status != stored.Status {
		changed = append(changed, "status="+stored.Status)
	}
	extra := ""
	if len(changed) > 0 {
		extra = " " + strings.Join(changed, " ")
	}
	r.fx.logf("version %s %d->%d%s %s", r.fx.name(a.ID), before.Version, stored.Version, extra, r.fx.snapshotOf(stored.Attributes))
	return nil
}

func (r *tpArtifactRepo) NextSortOrder(projectID string, parentID *string) (int, error) {
	return len(r.rows) + 1, nil
}

// tpLinkRow is one row of the links table; rows are kept in creation
// order, which stands in for created_at.
type tpLinkRow struct {
	raw     []byte // the link as JSON, attributes as the jsonb column holds them
	deleted bool   // valid_to is set
}

// tpLinkRepo keeps links as the links table does: from_id and to_id are
// UUID columns, a delete closes the row, and the lists read live rows
// newest first.
type tpLinkRepo struct {
	links.Repository
	fx   *tpFixture
	rows []*tpLinkRow
	byID map[string]*tpLinkRow
}

func (r *tpLinkRepo) Save(l *links.Link) error {
	for _, id := range []string{l.ID, l.FromID, l.ToID} {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("pq: invalid input syntax for type uuid: %q", id)
		}
	}
	if r.byID == nil {
		r.byID = map[string]*tpLinkRow{}
	}
	row := &tpLinkRow{raw: tpMarshal(r.fx.t, l)}
	r.rows = append(r.rows, row)
	r.byID[l.ID] = row
	r.fx.mint("L", l.ID)
	r.fx.logf("link created: %s", r.fx.linkLine(r.decode(row)))
	return nil
}

func (r *tpLinkRepo) decode(row *tpLinkRow) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal(row.raw, &m); err != nil {
		r.fx.t.Fatal(err)
	}
	return m
}

func (r *tpLinkRepo) link(row *tpLinkRow) *links.Link {
	l := &links.Link{}
	if err := json.Unmarshal(row.raw, l); err != nil {
		r.fx.t.Fatal(err)
	}
	return l
}

func (r *tpLinkRepo) FindByID(id string) (*links.Link, error) {
	if row, ok := r.byID[id]; ok && !row.deleted {
		return r.link(row), nil
	}
	return nil, errors.New("link not found")
}

func (r *tpLinkRepo) live(match func(*links.Link) bool) []*links.Link {
	var out []*links.Link
	for i := len(r.rows) - 1; i >= 0; i-- {
		if row := r.rows[i]; !row.deleted {
			if l := r.link(row); match(l) {
				out = append(out, l)
			}
		}
	}
	return out
}

func (r *tpLinkRepo) FindByFromID(id string) ([]*links.Link, error) {
	return r.live(func(l *links.Link) bool { return l.FromID == id }), nil
}

func (r *tpLinkRepo) FindByToID(id string) ([]*links.Link, error) {
	return r.live(func(l *links.Link) bool { return l.ToID == id }), nil
}

func (r *tpLinkRepo) Delete(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("pq: invalid input syntax for type uuid: %q", id)
	}
	if row, ok := r.byID[id]; ok && !row.deleted {
		row.deleted = true
		r.fx.logf("link deleted: %s", r.fx.name(id))
	}
	return nil
}

func (r *tpLinkRepo) setSuspect(row *tpLinkRow, suspect bool) {
	l := r.link(row)
	l.Suspect = suspect
	row.raw = tpMarshal(r.fx.t, l)
	r.fx.logf("link suspect=%v: %s", suspect, r.fx.name(l.ID))
}

func (r *tpLinkRepo) SetSuspectByArtifact(artifactID string, suspect bool) error {
	for _, row := range r.rows {
		if l := r.link(row); !row.deleted && (l.FromID == artifactID || l.ToID == artifactID) && l.Suspect != suspect {
			r.setSuspect(row, suspect)
		}
	}
	return nil
}

func (r *tpLinkRepo) RecordLinkForArtifactVersion(linkID, artifactID string, version int) error {
	return nil
}

// tpProposalRepo keeps proposals as JSON rows, newest first.
type tpProposalRepo struct {
	proposals.Repository
	fx    *tpFixture
	rows  map[string][]byte
	order []string
}

func (r *tpProposalRepo) Save(p *proposals.Proposal) error {
	r.rows[p.ID] = tpMarshal(r.fx.t, p)
	r.order = append(r.order, p.ID)
	name := r.fx.mint("prop", p.ID)
	target := "-"
	if p.TargetID != nil {
		target = r.fx.name(*p.TargetID)
	}
	r.fx.logf("proposal created: %s %s %s by %s in %s target=%s payload=%s", name, p.Op, p.Status, p.RunID,
		p.ProjectID, target, r.fx.render(p.Payload))
	return nil
}

func (r *tpProposalRepo) Update(p *proposals.Proposal) error {
	r.rows[p.ID] = tpMarshal(r.fx.t, p)
	var m map[string]interface{}
	_ = json.Unmarshal(r.rows[p.ID], &m)
	r.fx.logf("proposal resolved: %s", r.fx.proposalLine(m))
	return nil
}

func (r *tpProposalRepo) FindByID(id string) (*proposals.Proposal, error) {
	raw, ok := r.rows[id]
	if !ok {
		return nil, proposals.ErrNotFound
	}
	p := &proposals.Proposal{}
	return p, json.Unmarshal(raw, p)
}

func (r *tpProposalRepo) List(orgID, projectID, status, runID string) ([]*proposals.Proposal, error) {
	var out []*proposals.Proposal
	for i := len(r.order) - 1; i >= 0; i-- {
		p, _ := r.FindByID(r.order[i])
		if (projectID == "" || p.ProjectID == projectID) && (status == "" || p.Status == status) &&
			(runID == "" || p.RunID == runID) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *tpProposalRepo) CountByRun(runID string) (int, error) {
	list, _ := r.List("", "", "", runID)
	return len(list), nil
}

// tpGuidedRepo keeps guided sessions as JSON rows.
type tpGuidedRepo struct {
	guided.Repository
	fx   *tpFixture
	rows map[string][]byte
}

func (r *tpGuidedRepo) Save(s *guided.Session) error {
	r.rows[s.ID] = tpMarshal(r.fx.t, s)
	name := r.fx.mint("S", s.ID)
	by := "-"
	if s.CreatedBy != nil {
		by = *s.CreatedBy
	}
	r.fx.logf("guided session created: %s in %s by %s", name, s.ProjectID, by)
	return nil
}

func (r *tpGuidedRepo) Update(s *guided.Session) error {
	r.rows[s.ID] = tpMarshal(r.fx.t, s)
	r.fx.logf("guided session updated: %s draft_artifact_ids=%s", r.fx.name(s.ID), r.fx.render(s.DraftArtifactIDs))
	return nil
}

func (r *tpGuidedRepo) FindByID(id string) (*guided.Session, error) {
	raw, ok := r.rows[id]
	if !ok {
		return nil, guided.ErrSessionNotFound
	}
	s := &guided.Session{}
	return s, json.Unmarshal(raw, s)
}

// tpChatter records each note an artifact's feed gets.
type tpChatter struct {
	chatter.Service
	fx *tpFixture
}

func (c *tpChatter) CreateEntry(e *chatter.ChatterEntry) error {
	by := "-"
	if e.CreatedBy != nil {
		by = *e.CreatedBy
	}
	c.fx.logf("chatter %s %s auto=%v by=%s author=%q %q", c.fx.name(e.ArtifactID), e.EntryType, e.IsAutoEntry, by,
		e.AuthorName, c.fx.nameAll(e.Message))
	return nil
}

// tpBus records each published event with its payload's Go types.
type tpBus struct {
	events.Bus
	fx *tpFixture
}

func (b *tpBus) Publish(e events.Event) {
	keys := make([]string, 0, len(e.Payload))
	for k := range e.Payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		v := e.Payload[k]
		shown := fmt.Sprintf("%v", v)
		if s, ok := v.(string); ok {
			shown = b.fx.nameAll(s)
		}
		parts[i] = fmt.Sprintf("%s %T %s", k, v, shown)
	}
	b.fx.logf("event %s %s project=%s org=%s actor=%s {%s}", e.EventType, b.fx.name(e.EntityID), e.ProjectID,
		e.OrgID, e.Actor, strings.Join(parts, ", "))
}

func (b *tpBus) Subscribe(func(events.Event)) {}

// ---- path 1: POST /api/v1/links (and DELETE /api/v1/links/{id}) ----

// TestTraceabilityPathPostLinks: POST /api/v1/links validates the type
// against the link rules (400), gates refines on the flow-down feature for a
// member, wants editor rights on the target's project (viewer for refines),
// auto-versions both ends with a links_snapshot and a note each, and
// publishes link.created as the caller. DELETE /api/v1/links/{id} wants
// editor rights on the source's project alone.
func TestTraceabilityPathPostLinks(t *testing.T) {
	t.Run("a valid new link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links",
			`{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies","attributes":{"rationale":"traced"}}`)
		fx.expect(`
			> POST /api/v1/links as u-editor {"from_id":"tc","to_id":"req","type":"verifies"}
			< 201 link L1 tc-verifies->req suspect=false attributes=null
			  link created: L1 tc-verifies->req suspect=false attributes=null
			  version tc 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=user:u-editor {from_id string tc, link_type string verifies, to_id string req}
			> POST /api/v1/links as u-editor {"from_id":"di","to_id":"req","type":"satisfies","attributes":{"rationale":"traced"}}
			< 201 link L2 di-satisfies->req suspect=false attributes={"rationale":"traced"}
			  link created: L2 di-satisfies->req suspect=false attributes={"rationale":"traced"}
			  version di 1->2 links_snapshot=[L2 di-satisfies->req suspect=false attributes={"rationale":"traced"}]
			  chatter di link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 2->3 links_snapshot=[L2 di-satisfies->req suspect=false attributes={"rationale":"traced"}, L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.created L2 project=proj-p org=org-1 actor=user:u-editor {from_id string di, link_type string satisfies, to_id string req}
		`)
	})

	t.Run("a duplicate link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.expect(`
			> POST /api/v1/links as u-editor {"from_id":"tc","to_id":"req","type":"verifies"}
			< 201 link L2 tc-verifies->req suspect=false attributes=null
			  link created: L2 tc-verifies->req suspect=false attributes=null
			  version tc 2->3 links_snapshot=[L2 tc-verifies->req suspect=false attributes=null, L1 tc-verifies->req suspect=false attributes=null]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  version req 2->3 links_snapshot=[L2 tc-verifies->req suspect=false attributes=null, L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.created L2 project=proj-p org=org-1 actor=user:u-editor {from_id string tc, link_type string verifies, to_id string req}
		`)
	})

	t.Run("OnInvalid: a type the link rules refuse, or an end no artifact has", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{req}}","to_id":"{{tc}}","type":"verifies"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{need}}","type":"verifies"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"bogus"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{phantom}}","type":"verifies"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{phantom}}","to_id":"{{req}}","type":"verifies"}`)
		fx.expect(`
			> POST /api/v1/links as u-editor {"from_id":"req","to_id":"tc","type":"verifies"}
			< 400 {"error":"link type 'verifies' cannot originate from artifact type 'requirement' (allowed: [test-case])"}
			> POST /api/v1/links as u-editor {"from_id":"tc","to_id":"need","type":"verifies"}
			< 400 {"error":"link type 'verifies' cannot target artifact type 'user-need' (allowed: [requirement])"}
			> POST /api/v1/links as u-editor {"from_id":"tc","to_id":"req","type":"bogus"}
			< 400 {"error":"invalid link type: bogus"}
			> POST /api/v1/links as u-editor {"from_id":"tc","to_id":"phantom","type":"verifies"}
			< 400 {"error":"target artifact not found"}
			> POST /api/v1/links as u-editor {"from_id":"phantom","to_id":"req","type":"verifies"}
			< 400 {"error":"source artifact not found"}
		`)
	})

	t.Run("TargetRole: another workspace's artifact, a project the caller only views", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{xreq}}","type":"verifies"}`)
		fx.step(tpAsSupplier, "POST", "/api/v1/links", `{"from_id":"{{creq}}","to_id":"{{need}}","type":"derives-from"}`)
		fx.step(tpAsSupplier, "POST", "/api/v1/links", `{"from_id":"{{req}}","to_id":"{{creq}}","type":"refines"}`)
		fx.step(tpAsSupplier, "POST", "/api/v1/links", `{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}`)
		fx.expect(`
			> POST /api/v1/links as u-editor {"from_id":"tc","to_id":"xreq","type":"verifies"}
			< 400 {"error":"target artifact not found"}
			> POST /api/v1/links as u-supplier {"from_id":"creq","to_id":"need","type":"derives-from"}
			< 403 {"error":"you do not have access to this project"}
			> POST /api/v1/links as u-supplier {"from_id":"req","to_id":"creq","type":"refines"}
			< 403 {"error":"you do not have access to this project"}
			> POST /api/v1/links as u-supplier {"from_id":"creq","to_id":"req","type":"refines"}
			< 201 link L1 creq-refines->req suspect=false attributes=null
			  link created: L1 creq-refines->req suspect=false attributes=null
			  version creq 1->2 links_snapshot=[L1 creq-refines->req suspect=false attributes=null]
			  chatter creq link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 creq-refines->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-c org=org-1 actor=user:u-supplier {from_id string creq, link_type string refines, to_id string req}
		`)
	})

	t.Run("RequireFlowDownFeature: refines while the feature is closed", func(t *testing.T) {
		fx := newTPFixture(t, false)
		fx.step(tpAsSupplier, "POST", "/api/v1/links", `{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{req2}}","to_id":"{{req}}","type":"refines"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{req2}}","to_id":"{{req}}","type":"decomposes-to"}`)
		fx.step(tpAsDirectRun, "POST", "/api/v1/links", `{"from_id":"{{req2}}","to_id":"{{req}}","type":"refines"}`)
		fx.note("a refines link's delete asks no feature")
		fx.step(tpAsEditor, "DELETE", "/api/v1/links/{{L2}}", "")
		fx.expect(`
			> POST /api/v1/links as u-supplier {"from_id":"creq","to_id":"req","type":"refines"}
			< 403 {"error":"this feature reaches stable-channel workspaces at their next stable release; switch the workspace to nightly, or preview the next release, in workspace settings"}
			> POST /api/v1/links as u-editor {"from_id":"req2","to_id":"req","type":"refines"}
			< 403 {"error":"this feature reaches stable-channel workspaces at their next stable release; switch the workspace to nightly, or preview the next release, in workspace settings"}
			> POST /api/v1/links as u-editor {"from_id":"req2","to_id":"req","type":"decomposes-to"}
			< 201 link L1 req2-decomposes-to->req suspect=false attributes=null
			  link created: L1 req2-decomposes-to->req suspect=false attributes=null
			  version req2 1->2 links_snapshot=[L1 req2-decomposes-to->req suspect=false attributes=null]
			  chatter req2 link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 req2-decomposes-to->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=user:u-editor {from_id string req2, link_type string decomposes-to, to_id string req}
			> POST /api/v1/links as run-direct {"from_id":"req2","to_id":"req","type":"refines"}
			< 201 link L2 req2-refines->req suspect=false attributes=null
			  link created: L2 req2-refines->req suspect=false attributes=null
			  version req2 2->3 links_snapshot=[L2 req2-refines->req suspect=false attributes=null, L1 req2-decomposes-to->req suspect=false attributes=null]
			  chatter req2 link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  version req 2->3 links_snapshot=[L2 req2-refines->req suspect=false attributes=null, L1 req2-decomposes-to->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.created L2 project=proj-p org=org-1 actor=agent:run-direct {from_id string req2, link_type string refines, to_id string req}
			# a refines link's delete asks no feature
			> DELETE /api/v1/links/L2 as u-editor
			< 204 (no body)
			  link deleted: L2
			  version req2 3->4 links_snapshot=[L1 req2-decomposes-to->req suspect=false attributes=null]
			  chatter req2 link-change auto=true by=- author="" "Auto-updated to version 4 due to link changes"
			  version req 3->4 links_snapshot=[L1 req2-decomposes-to->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 4 due to link changes"
			  event link.deleted L2 project=proj-p org=org-1 actor=user:u-editor {from_id string req2, link_type string refines, to_id string req}
		`)
	})

	t.Run("Actor: a direct-mode run, and a proposal-mode run's link becomes a proposal", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsDirectRun, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}`)
		fx.expect(`
			> POST /api/v1/links as run-direct {"from_id":"tc","to_id":"req","type":"verifies"}
			< 201 link L1 tc-verifies->req suspect=false attributes=null
			  link created: L1 tc-verifies->req suspect=false attributes=null
			  version tc 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=agent:run-direct {from_id string tc, link_type string verifies, to_id string req}
			> POST /api/v1/links as run-proposal {"from_id":"di","to_id":"req","type":"satisfies"}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop1","proposed":true}
			  proposal created: prop1 create_link pending by run-proposal in proj-p target=- payload={"attributes":null,"from_id":"di","to_id":"req","type":"satisfies"}
			  event proposal.created prop1 project=proj-p org=org-1 actor=agent:run-proposal {op string create_link, run_id string run-proposal}
		`)
	})

	t.Run("a removal: DELETE /api/v1/links/{id}", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{creq}}","to_id":"{{need}}","type":"impacts"}`)
		fx.step(tpAsEditor, "DELETE", "/api/v1/links/{{L1}}", "")
		fx.note("the supplier, an editor of C and a viewer of P, deletes a link from C into P it could not have made")
		fx.step(tpAsSupplier, "DELETE", "/api/v1/links/{{L2}}", "")
		fx.expect(`
			> DELETE /api/v1/links/L1 as u-editor
			< 204 (no body)
			  link deleted: L1
			  version tc 2->3 links_snapshot=[]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  version req 2->3 links_snapshot=[]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.deleted L1 project=proj-p org=org-1 actor=user:u-editor {from_id string tc, link_type string verifies, to_id string req}
			# the supplier, an editor of C and a viewer of P, deletes a link from C into P it could not have made
			> DELETE /api/v1/links/L2 as u-supplier
			< 204 (no body)
			  link deleted: L2
			  version creq 2->3 links_snapshot=[]
			  chatter creq link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  version need 2->3 links_snapshot=[]
			  chatter need link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.deleted L2 project=proj-c org=org-1 actor=user:u-supplier {from_id string creq, link_type string impacts, to_id string need}
		`)
	})
}

// ---- path 2: the managed link edits of PUT /api/v1/artifacts/{id} ----

// TestTraceabilityPathManagedEdits: pendingLinkAdds and pendingLinkRemoves
// in PUT /api/v1/artifacts/{id} (Q3, Q4). An add or a removal the rules or
// the caller's roles refuse is skipped without a word and the update still
// answers 200; there is no flow-down gate; editor rights are wanted on both
// ends' projects whatever the type; the edited artifact gets one version
// whose links_snapshot is written only while a link remains (Q4), and a note
// listing the links requested (Q3); the other end of each link made or
// removed is auto-versioned; only artifact.updated is published.
func TestTraceabilityPathManagedEdits(t *testing.T) {
	t.Run("a valid new link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}",
			`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}]}`)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{tc}}",
			`{"pendingLinkAdds":[{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies","attributes":{"method":"test"}}]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkAdds":[{"from_id":"di","to_id":"req","type":"satisfies"}]}
			< 200 artifact di v2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  link created: L1 di-satisfies->req suspect=false attributes={}
			  version di 1->2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  chatter di version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - satisfies: Req one (added)\n"
			  version req 1->2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 2}
			> PUT /api/v1/artifacts/tc as u-editor {"pendingLinkAdds":[{"from_id":"tc","to_id":"req","type":"verifies","attributes":{"method":"test"}}]}
			< 200 artifact tc v2 links_snapshot=[L2 tc-verifies->req suspect=false attributes={"method":"test"}]
			  link created: L2 tc-verifies->req suspect=false attributes={"method":"test"}
			  version tc 1->2 links_snapshot=[L2 tc-verifies->req suspect=false attributes={"method":"test"}]
			  chatter tc version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - verifies: Req one (added)\n"
			  version req 2->3 links_snapshot=[L2 tc-verifies->req suspect=false attributes={"method":"test"}, L1 di-satisfies->req suspect=false attributes={}]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event artifact.updated tc project=proj-p org=org-1 actor=user:u-editor {artifact_type string test-case, title string Test one, version int 2}
		`)
	})

	t.Run("a duplicate link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}",
			`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"},`+
				`{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkAdds":[{"from_id":"di","to_id":"req","type":"satisfies"},{"from_id":"di","to_id":"req","type":"satisfies"}]}
			< 200 artifact di v2 links_snapshot=[L2 di-satisfies->req suspect=false attributes={}, L1 di-satisfies->req suspect=false attributes={}]
			  link created: L1 di-satisfies->req suspect=false attributes={}
			  link created: L2 di-satisfies->req suspect=false attributes={}
			  version di 1->2 links_snapshot=[L2 di-satisfies->req suspect=false attributes={}, L1 di-satisfies->req suspect=false attributes={}]
			  chatter di version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - satisfies: Req one (added)\n    - satisfies: Req one (added)\n"
			  version req 1->2 links_snapshot=[L2 di-satisfies->req suspect=false attributes={}, L1 di-satisfies->req suspect=false attributes={}]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 2}
		`)
	})

	t.Run("OnInvalid: a type the link rules refuse, an end no artifact has, an add with no type", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}",
			`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"verifies"},`+
				`{"from_id":"{{di}}","to_id":"{{phantom}}","type":"satisfies"},`+
				`{"from_id":"{{di}}","to_id":"{{req}}"}]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkAdds":[{"from_id":"di","to_id":"req","type":"verifies"},{"from_id":"di","to_id":"phantom","type":"satisfies"},{"from_id":"di","to_id":"req"}]}
			< 200 artifact di v2 links_snapshot absent
			  version di 1->2 links_snapshot absent
			  chatter di version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - verifies: Req one (added)\n    - satisfies: phantom (added)\n"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 2}
		`)
	})

	t.Run("TargetRole: another workspace's artifact, a project the caller only views", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}",
			`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{xreq}}","type":"satisfies"}]}`)
		fx.step(tpAsSupplier, "PUT", "/api/v1/artifacts/{{creq}}",
			`{"pendingLinkAdds":[{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}]}`)
		fx.note("the supplier made the same refines link through POST /api/v1/links, and cannot remove it here")
		fx.setup(tpAsSupplier, "POST", "/api/v1/links", `{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}`)
		fx.step(tpAsSupplier, "PUT", "/api/v1/artifacts/{{creq}}", `{"pendingLinkRemoves":["{{L1}}"]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkAdds":[{"from_id":"di","to_id":"xreq","type":"satisfies"}]}
			< 200 artifact di v2 links_snapshot absent
			  version di 1->2 links_snapshot absent
			  chatter di version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - satisfies: Other workspace req (added)\n"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 2}
			> PUT /api/v1/artifacts/creq as u-supplier {"pendingLinkAdds":[{"from_id":"creq","to_id":"req","type":"refines"}]}
			< 200 artifact creq v2 links_snapshot absent
			  version creq 1->2 links_snapshot absent
			  chatter creq version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - refines: Req one (added)\n"
			  event artifact.updated creq project=proj-c org=org-1 actor=user:u-supplier {artifact_type string requirement, title string Supplier req, version int 2}
			# the supplier made the same refines link through POST /api/v1/links, and cannot remove it here
			> PUT /api/v1/artifacts/creq as u-supplier {"pendingLinkRemoves":["L1"]}
			< 200 artifact creq v4 links_snapshot=[L1 creq-refines->req suspect=false attributes=null]
			  version creq 3->4 links_snapshot=[L1 creq-refines->req suspect=false attributes=null]
			  chatter creq version-change auto=true by=- author="" "Updated to version 4\n\nChanges:\n- Links:\n    - refines: Req one (removed)\n"
			  event artifact.updated creq project=proj-c org=org-1 actor=user:u-supplier {artifact_type string requirement, title string Supplier req, version int 4}
		`)
	})

	t.Run("RequireFlowDownFeature: refines while the feature is closed", func(t *testing.T) {
		fx := newTPFixture(t, false)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{creq}}",
			`{"pendingLinkAdds":[{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/creq as u-editor {"pendingLinkAdds":[{"from_id":"creq","to_id":"req","type":"refines"}]}
			< 200 artifact creq v2 links_snapshot=[L1 creq-refines->req suspect=false attributes={}]
			  link created: L1 creq-refines->req suspect=false attributes={}
			  version creq 1->2 links_snapshot=[L1 creq-refines->req suspect=false attributes={}]
			  chatter creq version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - refines: Req one (added)\n"
			  version req 1->2 links_snapshot=[L1 creq-refines->req suspect=false attributes={}]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event artifact.updated creq project=proj-c org=org-1 actor=user:u-editor {artifact_type string requirement, title string Supplier req, version int 2}
		`)
	})

	t.Run("a removal", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}`)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{di}}","to_id":"{{need}}","type":"impacts"}`)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.note("one of the edited artifact's two links: the other remains, so the snapshot is written")
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}", `{"pendingLinkRemoves":["{{L1}}"]}`)
		fx.note("its last link: none remains, so the version carries the previous snapshot forward (Q4)")
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}", `{"pendingLinkRemoves":["{{L2}}"]}`)
		fx.note("a link between two other artifacts: only its target is auto-versioned")
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}", `{"pendingLinkRemoves":["{{L3}}"]}`)
		fx.note("an id no link has")
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}", `{"pendingLinkRemoves":["{{phantom}}"]}`)
		fx.expect(`
			# one of the edited artifact's two links: the other remains, so the snapshot is written
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkRemoves":["L1"]}
			< 200 artifact di v4 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  link deleted: L1
			  version di 3->4 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  chatter di version-change auto=true by=- author="" "Updated to version 4\n\nChanges:\n- Links:\n    - satisfies: Req one (removed)\n"
			  version req 3->4 links_snapshot=[L3 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 4 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 4}
			# its last link: none remains, so the version carries the previous snapshot forward (Q4)
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkRemoves":["L2"]}
			< 200 artifact di v5 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  link deleted: L2
			  version di 4->5 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  chatter di version-change auto=true by=- author="" "Updated to version 5\n\nChanges:\n- Links:\n    - impacts: Need one (removed)\n"
			  version need 2->3 links_snapshot=[]
			  chatter need link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 5}
			# a link between two other artifacts: only its target is auto-versioned
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkRemoves":["L3"]}
			< 200 artifact di v6 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  link deleted: L3
			  version di 5->6 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  chatter di version-change auto=true by=- author="" "Updated to version 6\n\nChanges:\n- Links:\n    - verifies: Test one (removed)\n"
			  version req 4->5 links_snapshot=[]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 5 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 6}
			# an id no link has
			> PUT /api/v1/artifacts/di as u-editor {"pendingLinkRemoves":["phantom"]}
			< 200 artifact di v7 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  version di 6->7 links_snapshot=[L2 di-impacts->need suspect=false attributes=null]
			  chatter di version-change auto=true by=- author="" "Updated to version 7"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 7}
		`)
	})

	t.Run("a content change with the add: the new link is suspect at once, the snapshot says not", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}",
			`{"body":"The di, revised.","pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/di as u-editor {"body":"The di, revised.","pendingLinkAdds":[{"from_id":"di","to_id":"req","type":"satisfies"}]}
			< 200 artifact di v2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  link created: L1 di-satisfies->req suspect=false attributes={}
			  version di 1->2 body="The di, revised." links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  link suspect=true: L1
			  chatter di version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Body: \"The di.\" → \"The di, revised.\"\n- Links:\n    - satisfies: Req one (added)\n"
			  version req 1->2 links_snapshot=[L1 di-satisfies->req suspect=true attributes={}]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=user:u-editor {artifact_type string design-item, title string Design one, version int 2}
		`)
	})

	t.Run("Actor: a direct-mode run, and a proposal-mode run's edit becomes a proposal", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsDirectRun, "PUT", "/api/v1/artifacts/{{di}}",
			`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}]}`)
		fx.step(tpAsProposalRun, "PUT", "/api/v1/artifacts/{{tc}}",
			`{"pendingLinkAdds":[{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}]}`)
		fx.expect(`
			> PUT /api/v1/artifacts/di as run-direct {"pendingLinkAdds":[{"from_id":"di","to_id":"req","type":"satisfies"}]}
			< 200 artifact di v2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  link created: L1 di-satisfies->req suspect=false attributes={}
			  version di 1->2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  chatter di version-change auto=true by=- author="" "Updated to version 2\n\nChanges:\n- Links:\n    - satisfies: Req one (added)\n"
			  version req 1->2 links_snapshot=[L1 di-satisfies->req suspect=false attributes={}]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event artifact.updated di project=proj-p org=org-1 actor=agent:run-direct {artifact_type string design-item, title string Design one, version int 2}
			> PUT /api/v1/artifacts/tc as run-proposal {"pendingLinkAdds":[{"from_id":"tc","to_id":"req","type":"verifies"}]}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop1","proposed":true}
			  proposal created: prop1 update_artifact pending by run-proposal in proj-p target=tc payload={"attributes":null,"pendingLinkAdds":[{"from_id":"tc","to_id":"req","type":"verifies"}]}
			  event proposal.created prop1 project=proj-p org=org-1 actor=agent:run-proposal {op string update_artifact, run_id string run-proposal}
		`)
	})
}

// ---- path 3: the proposal appliers ----

// TestTraceabilityPathProposalAppliers: a proposal-mode run's link writes
// become proposals (POST /api/v1/links checks the rules, the run's own
// project and the flow-down gate, which a run always passes, before it files
// one), and POST /api/v1/proposals/{id}/approve runs the applier: it checks
// the rules again and fails the proposal (apply_failed, 500) on a refusal,
// asks no role and no feature, auto-versions both ends as POST /links does
// and publishes link.created or link.deleted as the system.
func TestTraceabilityPathProposalAppliers(t *testing.T) {
	approve := func(fx *tpFixture, proposal string) {
		fx.step(tpAsEditor, "POST", "/api/v1/proposals/{{"+proposal+"}}/approve", `{"note":"ok"}`)
	}

	t.Run("a valid new link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		approve(fx, "prop1")
		fx.expect(`
			> POST /api/v1/links as run-proposal {"from_id":"tc","to_id":"req","type":"verifies"}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop1","proposed":true}
			  proposal created: prop1 create_link pending by run-proposal in proj-p target=- payload={"attributes":null,"from_id":"tc","to_id":"req","type":"verifies"}
			  event proposal.created prop1 project=proj-p org=org-1 actor=agent:run-proposal {op string create_link, run_id string run-proposal}
			> POST /api/v1/proposals/prop1/approve as u-editor {"note":"ok"}
			< 200 proposal prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
			  link created: L1 tc-verifies->req suspect=false attributes=null
			  version tc 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=system {from_id string tc, link_type string verifies, to_id string req}
			  proposal resolved: prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
		`)
	})

	t.Run("a duplicate link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.setup(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		approve(fx, "prop1")
		approve(fx, "prop2")
		fx.expect(`
			> POST /api/v1/proposals/prop1/approve as u-editor {"note":"ok"}
			< 200 proposal prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
			  link created: L1 tc-verifies->req suspect=false attributes=null
			  version tc 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=system {from_id string tc, link_type string verifies, to_id string req}
			  proposal resolved: prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
			> POST /api/v1/proposals/prop2/approve as u-editor {"note":"ok"}
			< 200 proposal prop2 create_link applied entity=L2 reviewed_by=u-editor note="ok"
			  link created: L2 tc-verifies->req suspect=false attributes=null
			  version tc 2->3 links_snapshot=[L2 tc-verifies->req suspect=false attributes=null, L1 tc-verifies->req suspect=false attributes=null]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  version req 2->3 links_snapshot=[L2 tc-verifies->req suspect=false attributes=null, L1 tc-verifies->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.created L2 project=proj-p org=org-1 actor=system {from_id string tc, link_type string verifies, to_id string req}
			  proposal resolved: prop2 create_link applied entity=L2 reviewed_by=u-editor note="ok"
		`)
	})

	t.Run("OnInvalid: refused at propose time, failed at apply time", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{req}}","to_id":"{{tc}}","type":"verifies"}`)
		fx.note("a link valid when proposed, whose target is retyped before the approval")
		fx.setup(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.setup(tpAsEditor, "PUT", "/api/v1/artifacts/{{req}}", `{"type":"design-item"}`)
		approve(fx, "prop1")
		fx.note("a create_link proposal naming an artifact no row has")
		fx.propose(map[string]interface{}{"from_id": fx.ids["tc"], "to_id": tpPhantom, "type": "verifies"})
		approve(fx, "prop2")
		fx.note("a managed link edit, which the update_artifact applier refuses whole (#176)")
		fx.step(tpAsProposalRun, "PUT", "/api/v1/artifacts/{{di}}",
			`{"title":"Design one, linked","pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req2}}","type":"satisfies"}]}`)
		approve(fx, "prop3")
		fx.note("a link from a pending artifact proposal's ref, checked at apply time alone, approved in bulk")
		fx.step(tpAsProposalRun, "POST", "/api/v1/artifacts",
			`{"project_id":"`+tpP+`","type":"requirement","title":"Proposed req","ref":"new-req"}`)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"new-req","to_id":"{{req}}","type":"verifies"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/proposals/bulk", `{"action":"approve","ids":["{{prop5}}","{{prop4}}"]}`)
		fx.expect(`
			> POST /api/v1/links as run-proposal {"from_id":"req","to_id":"tc","type":"verifies"}
			< 400 {"error":"link type 'verifies' cannot originate from artifact type 'requirement' (allowed: [test-case])"}
			# a link valid when proposed, whose target is retyped before the approval
			> POST /api/v1/proposals/prop1/approve as u-editor {"note":"ok"}
			< 500 {"error":"failed to apply approved proposal"}
			  proposal resolved: prop1 create_link apply_failed reviewed_by=u-editor note="ok | apply failed: cannot apply create_link: link type 'verifies' cannot target artifact type 'design-item' (allowed: [requirement])"
			# a create_link proposal naming an artifact no row has
			> POST /api/v1/proposals/prop2/approve as u-editor {"note":"ok"}
			< 500 {"error":"failed to apply approved proposal"}
			  proposal resolved: prop2 create_link apply_failed reviewed_by=u-editor note="ok | apply failed: cannot apply create_link: target artifact \"phantom\" not found: artifact not found"
			# a managed link edit, which the update_artifact applier refuses whole (#176)
			> PUT /api/v1/artifacts/di as run-proposal {"title":"Design one, linked","pendingLinkAdds":[{"from_id":"di","to_id":"req2","type":"satisfies"}]}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop3","proposed":true}
			  proposal created: prop3 update_artifact pending by run-proposal in proj-p target=di payload={"attributes":null,"pendingLinkAdds":[{"from_id":"di","to_id":"req2","type":"satisfies"}],"title":"Design one, linked"}
			  event proposal.created prop3 project=proj-p org=org-1 actor=agent:run-proposal {op string update_artifact, run_id string run-proposal}
			> POST /api/v1/proposals/prop3/approve as u-editor {"note":"ok"}
			< 500 {"error":"failed to apply approved proposal"}
			  proposal resolved: prop3 update_artifact apply_failed reviewed_by=u-editor note="ok | apply failed: proposal carries managed link edits (pendingLinkAdds/pendingLinkRemoves) that cannot be applied here; propose the link changes as separate create_link/delete_link operations"
			# a link from a pending artifact proposal's ref, checked at apply time alone, approved in bulk
			> POST /api/v1/artifacts as run-proposal {"project_id":"proj-p","type":"requirement","title":"Proposed req","ref":"new-req"}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop4","proposed":true}
			  proposal created: prop4 create_artifact pending by run-proposal in proj-p target=- payload={"attributes":null,"body":"","project_id":"proj-p","title":"Proposed req","type":"requirement"}
			  event proposal.created prop4 project=proj-p org=org-1 actor=agent:run-proposal {op string create_artifact, run_id string run-proposal}
			> POST /api/v1/links as run-proposal {"from_id":"new-req","to_id":"req","type":"verifies"}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop5","proposed":true}
			  proposal created: prop5 create_link pending by run-proposal in proj-p target=- payload={"attributes":null,"from_id":"new-req","to_id":"req","type":"verifies"}
			  event proposal.created prop5 project=proj-p org=org-1 actor=agent:run-proposal {op string create_link, run_id string run-proposal}
			> POST /api/v1/proposals/bulk as u-editor {"action":"approve","ids":["prop5","prop4"]}
			< 200 {"results":[{"id":"prop4","ok":true},{"error":"failed to apply approved proposal","id":"prop5","ok":false}]}
			  artifact new1 created in proj-p: requirement "Proposed req" v1 status=draft
			  event artifact.created new1 project=proj-p org=org-1 actor=system {artifact_type string requirement, title string Proposed req, version int 1}
			  proposal resolved: prop4 create_artifact applied entity=new1 reviewed_by=u-editor note=""
			  proposal resolved: prop5 create_link apply_failed reviewed_by=u-editor note="apply failed: cannot apply create_link: link type 'verifies' cannot originate from artifact type 'requirement' (allowed: [test-case])"
		`)
	})

	t.Run("TargetRole: the run reaches its own project only; the applier asks no role", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{req}}","to_id":"{{creq}}","type":"refines"}`)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{req}}","to_id":"{{xreq}}","type":"refines"}`)
		fx.note("a create_link proposal naming another workspace's artifact, filed without the route")
		fx.propose(map[string]interface{}{"from_id": fx.ids["req"], "to_id": fx.ids["xreq"], "type": "refines"})
		approve(fx, "prop1")
		fx.expect(`
			> POST /api/v1/links as run-proposal {"from_id":"req","to_id":"creq","type":"refines"}
			< 400 {"error":"target artifact not found"}
			> POST /api/v1/links as run-proposal {"from_id":"req","to_id":"xreq","type":"refines"}
			< 400 {"error":"target artifact not found"}
			# a create_link proposal naming another workspace's artifact, filed without the route
			> POST /api/v1/proposals/prop1/approve as u-editor {"note":"ok"}
			< 200 proposal prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
			  link created: L1 req-refines->xreq suspect=false attributes=null
			  version req 1->2 links_snapshot=[L1 req-refines->xreq suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version xreq 1->2 links_snapshot=[L1 req-refines->xreq suspect=false attributes=null]
			  chatter xreq link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=system {from_id string req, link_type string refines, to_id string xreq}
			  proposal resolved: prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
		`)
	})

	t.Run("RequireFlowDownFeature: a run's refines while the feature is closed to the reviewer", func(t *testing.T) {
		fx := newTPFixture(t, false)
		fx.step(tpAsProposalRun, "POST", "/api/v1/links", `{"from_id":"{{req2}}","to_id":"{{req}}","type":"refines"}`)
		approve(fx, "prop1")
		fx.expect(`
			> POST /api/v1/links as run-proposal {"from_id":"req2","to_id":"req","type":"refines"}
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop1","proposed":true}
			  proposal created: prop1 create_link pending by run-proposal in proj-p target=- payload={"attributes":null,"from_id":"req2","to_id":"req","type":"refines"}
			  event proposal.created prop1 project=proj-p org=org-1 actor=agent:run-proposal {op string create_link, run_id string run-proposal}
			> POST /api/v1/proposals/prop1/approve as u-editor {"note":"ok"}
			< 200 proposal prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
			  link created: L1 req2-refines->req suspect=false attributes=null
			  version req2 1->2 links_snapshot=[L1 req2-refines->req suspect=false attributes=null]
			  chatter req2 link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  version req 1->2 links_snapshot=[L1 req2-refines->req suspect=false attributes=null]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 2 due to link changes"
			  event link.created L1 project=proj-p org=org-1 actor=system {from_id string req2, link_type string refines, to_id string req}
			  proposal resolved: prop1 create_link applied entity=L1 reviewed_by=u-editor note="ok"
		`)
	})

	t.Run("a removal", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)
		fx.step(tpAsProposalRun, "DELETE", "/api/v1/links/{{L1}}", "")
		approve(fx, "prop1")
		fx.note("a delete_link proposal whose link is gone by the approval")
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}`)
		fx.setup(tpAsProposalRun, "DELETE", "/api/v1/links/{{L2}}", "")
		fx.setup(tpAsEditor, "DELETE", "/api/v1/links/{{L2}}", "")
		approve(fx, "prop2")
		fx.expect(`
			> DELETE /api/v1/links/L1 as run-proposal
			< 202 {"note":"This write is pending human review and has not been applied yet.","proposal_id":"prop1","proposed":true}
			  proposal created: prop1 delete_link pending by run-proposal in proj-p target=L1 payload={}
			  event proposal.created prop1 project=proj-p org=org-1 actor=agent:run-proposal {op string delete_link, run_id string run-proposal}
			> POST /api/v1/proposals/prop1/approve as u-editor {"note":"ok"}
			< 200 proposal prop1 delete_link applied entity=L1 reviewed_by=u-editor note="ok"
			  link deleted: L1
			  version tc 2->3 links_snapshot=[]
			  chatter tc link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  version req 2->3 links_snapshot=[]
			  chatter req link-change auto=true by=- author="" "Auto-updated to version 3 due to link changes"
			  event link.deleted L1 project=proj-p org=org-1 actor=system {from_id string tc, link_type string verifies, to_id string req}
			  proposal resolved: prop1 delete_link applied entity=L1 reviewed_by=u-editor note="ok"
			# a delete_link proposal whose link is gone by the approval
			> POST /api/v1/proposals/prop2/approve as u-editor {"note":"ok"}
			< 200 proposal prop2 delete_link applied entity=L2 reviewed_by=u-editor note="ok"
			  proposal resolved: prop2 delete_link applied entity=L2 reviewed_by=u-editor note="ok"
		`)
	})
}

// ---- path 4: the guided drafts ----

// TestTraceabilityPathGuidedDrafts: POST /api/v1/guided-sessions/{id}/drafts
// creates each draft's links with no check at all (no link rules, no role on
// the target, no feature gate), skips without a word a link the store
// refuses, versions neither end, writes no links_snapshot and no note, and
// publishes nothing. The drafts only add links.
func TestTraceabilityPathGuidedDrafts(t *testing.T) {
	start := func(fx *tpFixture, as tpCaller, project string) {
		fx.setup(as, "POST", "/api/v1/guided-sessions", `{"project_id":"`+project+`"}`)
	}

	t.Run("a valid new link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.step(tpAsEditor, "POST", "/api/v1/guided-sessions", `{"project_id":"`+tpP+`"}`)
		fx.step(tpAsEditor, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Draft req","body":"The system shall draft.",`+
				`"links":[{"type":"derives-from","to_id":"{{need}}"}]}]}`)
		fx.expect(`
			> POST /api/v1/guided-sessions as u-editor {"project_id":"proj-p"}
			< 201 {"answers":{},"created_at":"T","created_by":"u-editor","current_step":0,"draft_artifact_ids":[],"id":"S1","project_id":"proj-p","status":"in-progress","updated_at":"T"}
			  guided session created: S1 in proj-p by u-editor
			> POST /api/v1/guided-sessions/S1/drafts as u-editor {"drafts":[{"type":"requirement","title":"Draft req","body":"The system shall draft.","links":[{"type":"derives-from","to_id":"need"}]}]}
			< 200 {"artifact_ids":["new1"]}
			  artifact new1 created in proj-p: requirement "Draft req" v1 status=draft
			  link created: L1 new1-derives-from->need suspect=false attributes=null
			  guided session updated: S1 draft_artifact_ids=["new1"]
		`)
	})

	t.Run("a duplicate link", func(t *testing.T) {
		fx := newTPFixture(t, true)
		start(fx, tpAsEditor, tpP)
		fx.step(tpAsEditor, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Draft req",`+
				`"links":[{"type":"derives-from","to_id":"{{need}}"},{"type":"derives-from","to_id":"{{need}}"}]}]}`)
		fx.expect(`
			> POST /api/v1/guided-sessions/S1/drafts as u-editor {"drafts":[{"type":"requirement","title":"Draft req","links":[{"type":"derives-from","to_id":"need"},{"type":"derives-from","to_id":"need"}]}]}
			< 200 {"artifact_ids":["new1"]}
			  artifact new1 created in proj-p: requirement "Draft req" v1 status=draft
			  link created: L1 new1-derives-from->need suspect=false attributes=null
			  link created: L2 new1-derives-from->need suspect=false attributes=null
			  guided session updated: S1 draft_artifact_ids=["new1"]
		`)
	})

	t.Run("OnInvalid: types the link rules refuse, an end no artifact has, an id the store refuses", func(t *testing.T) {
		fx := newTPFixture(t, true)
		start(fx, tpAsEditor, tpP)
		fx.step(tpAsEditor, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Draft req","links":[`+
				`{"type":"verifies","to_id":"{{need}}"},{"type":"bogus","to_id":"{{need}}"},`+
				`{"type":"derives-from","to_id":"{{phantom}}"},{"type":"derives-from","to_id":"not-a-uuid"}]}]}`)
		fx.expect(`
			> POST /api/v1/guided-sessions/S1/drafts as u-editor {"drafts":[{"type":"requirement","title":"Draft req","links":[{"type":"verifies","to_id":"need"},{"type":"bogus","to_id":"need"},{"type":"derives-from","to_id":"phantom"},{"type":"derives-from","to_id":"not-a-uuid"}]}]}
			< 200 {"artifact_ids":["new1"]}
			  artifact new1 created in proj-p: requirement "Draft req" v1 status=draft
			  link created: L1 new1-verifies->need suspect=false attributes=null
			  link created: L2 new1-bogus->need suspect=false attributes=null
			  link created: L3 new1-derives-from->phantom suspect=false attributes=null
			  guided session updated: S1 draft_artifact_ids=["new1"]
		`)
	})

	t.Run("TargetRole: a project the caller only views, another workspace's artifact", func(t *testing.T) {
		fx := newTPFixture(t, true)
		start(fx, tpAsSupplier, tpC)
		fx.step(tpAsSupplier, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Draft supplier req","links":[`+
				`{"type":"derives-from","to_id":"{{need}}"},{"type":"relates-to","to_id":"{{xreq}}"}]}]}`)
		fx.expect(`
			> POST /api/v1/guided-sessions/S1/drafts as u-supplier {"drafts":[{"type":"requirement","title":"Draft supplier req","links":[{"type":"derives-from","to_id":"need"},{"type":"relates-to","to_id":"xreq"}]}]}
			< 200 {"artifact_ids":["new1"]}
			  artifact new1 created in proj-c: requirement "Draft supplier req" v1 status=draft
			  link created: L1 new1-derives-from->need suspect=false attributes=null
			  link created: L2 new1-relates-to->xreq suspect=false attributes=null
			  guided session updated: S1 draft_artifact_ids=["new1"]
		`)
	})

	t.Run("RequireFlowDownFeature: refines while the feature is closed", func(t *testing.T) {
		fx := newTPFixture(t, false)
		start(fx, tpAsEditor, tpC)
		fx.step(tpAsEditor, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Draft refinement","links":[{"type":"refines","to_id":"{{req}}"}]}]}`)
		fx.expect(`
			> POST /api/v1/guided-sessions/S1/drafts as u-editor {"drafts":[{"type":"requirement","title":"Draft refinement","links":[{"type":"refines","to_id":"req"}]}]}
			< 200 {"artifact_ids":["new1"]}
			  artifact new1 created in proj-c: requirement "Draft refinement" v1 status=draft
			  link created: L1 new1-refines->req suspect=false attributes=null
			  guided session updated: S1 draft_artifact_ids=["new1"]
		`)
	})

	t.Run("Actor: a proposal-mode run's drafts and their links are written, not proposed", func(t *testing.T) {
		fx := newTPFixture(t, true)
		start(fx, tpAsEditor, tpP)
		fx.step(tpAsProposalRun, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Run's draft","links":[{"type":"derives-from","to_id":"{{need}}"}]}]}`)
		fx.expect(`
			> POST /api/v1/guided-sessions/S1/drafts as run-proposal {"drafts":[{"type":"requirement","title":"Run's draft","links":[{"type":"derives-from","to_id":"need"}]}]}
			< 200 {"artifact_ids":["new1"]}
			  artifact new1 created in proj-p: requirement "Run's draft" v1 status=draft
			  link created: L1 new1-derives-from->need suspect=false attributes=null
			  guided session updated: S1 draft_artifact_ids=["new1"]
		`)
	})
}

// TestTraceabilityPathsSnapshotContent pins the whole of each links_snapshot
// entry the writing paths store (I16, Q4): every key of the link as the
// links table reads it back, in jsonb's order (sorted), times as T. The
// managed edit's answer carries the handler's own list, the same content.
func TestTraceabilityPathsSnapshotContent(t *testing.T) {
	stored := func(fx *tpFixture, name string) string {
		t.Helper()
		a, err := fx.arts.load(fx.ids[name])
		if err != nil {
			t.Fatal(err)
		}
		snap, ok := a.Attributes["links_snapshot"]
		if !ok {
			return name + ": absent"
		}
		return name + ": " + fx.render(snap)
	}

	t.Run("POST /api/v1/links", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/links",
			`{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies","attributes":{"rationale":"traced","weight":2}}`)
		fx.lines = append(fx.lines, stored(fx, "di"), stored(fx, "req"))
		fx.expect(`
			di: [{"attributes":{"rationale":"traced","weight":2},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
			req: [{"attributes":{"rationale":"traced","weight":2},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
		`)
	})

	t.Run("a managed edit", func(t *testing.T) {
		fx := newTPFixture(t, true)
		w := fx.setup(tpAsEditor, "PUT", "/api/v1/artifacts/{{di}}",
			`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies","attributes":{"rationale":"traced"}}]}`)
		var answer struct {
			Attributes map[string]interface{} `json:"attributes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		fx.lines = append(fx.lines, "answer: "+fx.render(answer.Attributes["links_snapshot"]), stored(fx, "di"), stored(fx, "req"))
		fx.expect(`
			answer: [{"attributes":{"rationale":"traced"},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
			di: [{"attributes":{"rationale":"traced"},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
			req: [{"attributes":{"rationale":"traced"},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
		`)
	})

	t.Run("a proposal applier", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsProposalRun, "POST", "/api/v1/links",
			`{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies","attributes":{"rationale":"traced"}}`)
		fx.setup(tpAsEditor, "POST", "/api/v1/proposals/{{prop1}}/approve", "")
		fx.lines = append(fx.lines, stored(fx, "di"), stored(fx, "req"))
		fx.expect(`
			di: [{"attributes":{"rationale":"traced"},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
			req: [{"attributes":{"rationale":"traced"},"created_at":"T","from_id":"di","id":"L1","suspect":false,"to_id":"req","type":"satisfies","updated_at":"T","valid_from":"T","valid_to":null,"version":1}]
		`)
	})

	t.Run("a removal leaves []", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/links", `{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}`)
		fx.setup(tpAsEditor, "DELETE", "/api/v1/links/{{L1}}", "")
		fx.lines = append(fx.lines, stored(fx, "di"), stored(fx, "req"))
		fx.expect(`
			di: []
			req: []
		`)
	})

	t.Run("a guided draft writes none", func(t *testing.T) {
		fx := newTPFixture(t, true)
		fx.setup(tpAsEditor, "POST", "/api/v1/guided-sessions", `{"project_id":"`+tpP+`"}`)
		fx.setup(tpAsEditor, "POST", "/api/v1/guided-sessions/{{S1}}/drafts",
			`{"drafts":[{"type":"requirement","title":"Draft req","links":[{"type":"derives-from","to_id":"{{need}}"}]}]}`)
		fx.lines = append(fx.lines, stored(fx, "new1"), stored(fx, "need"))
		fx.expect(`
			new1: absent
			need: absent
		`)
	})
}
