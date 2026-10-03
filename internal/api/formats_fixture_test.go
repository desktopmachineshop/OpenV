package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// The fixture behind the S9 format goldens (refactor plan step S9,
// invariant I15; formats_golden_test.go). It is a workspace with two
// projects, served by in-memory stand-ins for the leaf services the export
// path reads, under the real export, report and download services and the
// real handlers, so a golden covers the route, its headers, the snapshot
// load (live and from a baseline) and the renderer.
//
// Project P, "Formats \"fixture\" / Ü & co" (a name each filename
// sanitiser treats its own way), holds two headings with a description, a
// user need, two requirements, two test cases, a hazard and a design item
// under them, and a loose note at the root; every link type the rules allow
// between them, one of them suspect; a PNG figure, a datasheet whose file is
// missing and an SVG drawing; a product profile; the attribute definitions
// in effect (an enum, a boolean, a number); test evidence (a pass, a fail);
// and two baselines, one that kept the definitions and one taken before
// baselines kept them. Project C, "Formats supplier", refines P's first
// requirement, so P's export lists a linked artifact and its V&V status
// flows down from C's passing test (REQ-145, REQ-146). A crew of two agents
// and a person is the crew export's fixture.
//
// The clock is pinned: every time the fixture holds is a fixed time in June
// 2025, and every time the server reads its own clock for (a filename's
// stamp, the export's exported_at, a cover's "Generated", the V&V report's
// date) is replaced in the golden by a token naming its layout, after the
// test has checked that it lies within the request (s9Clock).

// s9ID is the fixture's id n, UUID-shaped as the database's are.
func s9ID(n int) string { return fmt.Sprintf("5e9a0000-0000-4000-8000-%012d", n) }

// s9At is the fixture's minute m: June 2025, far from any clock a test
// reads.
func s9At(m int) time.Time {
	return time.Date(2025, 6, 2, 8, 0, 0, 0, time.UTC).Add(time.Duration(m) * time.Minute)
}

func s9Ptr[T any](v T) *T { return &v }

// The fixture's ids.
var (
	s9Org      = s9ID(1)
	s9P        = s9ID(11)
	s9C        = s9ID(12)
	s9B1       = s9ID(601) // the baseline that kept the definitions
	s9B0       = s9ID(602) // a baseline taken before baselines kept them
	s9Crew     = s9ID(801)
	s9Admin    = s9ID(901)
	s9Heading1 = s9ID(101)
	s9Req1     = s9ID(104)
	s9Req2     = s9ID(105)
)

// s9PNG is a 4x3 RGB PNG, the figure and the workspace logo, as bytes so
// that no encoder version changes it.
var s9PNG = []byte("\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52\x00\x00\x00\x04\x00\x00\x00\x03" +
	"\x08\x02\x00\x00\x00\x3b\x96\x39\x91\x00\x00\x00\x2a\x49\x44\x41\x54\x78\xda\x0d\xc7\x31\x01\x00\x30\x0c" +
	"\xc3\x30\x03\x2b\x88\xc0\x29\x9c\x80\x28\x08\xc3\xda\xf4\x09\x70\x70\xf1\x10\xe2\xc4\x8d\x97\x9f\x3a\x75" +
	"\xeb\xd5\x07\x22\x36\x11\x59\x6d\x3a\x79\x55\x00\x00\x00\x00\x49\x45\x4e\x44\xae\x42\x60\x82")

const s9SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect width="4" height="4"/></svg>`

// s9Req1Body is a requirement in every Markdown form the document renderers
// lay out: emphasis, code, a link, a list, a table, a code block, a
// citation of another requirement and of a figure, and text that XML and
// CSV must escape.
const s9Req1Body = "The system shall answer within **2 s** of a request, measured at the _client_.\n" +
	"\n" +
	"- Logged with `answer_ms`\n" +
	"- Checked against [the budget](https://example.com/budget)\n" +
	"\n" +
	"| Load | Limit |\n" +
	"| --- | --- |\n" +
	"| idle | 1 s |\n" +
	"| peak | 2 s |\n" +
	"\n" +
	"See REQ-2 and REQ-1-FIG-1; quote \"as is\", a < b & c > d, 5 µs, Ü.\n" +
	"\n" +
	"```\n" +
	"answer(ctx)\n" +
	"```"

// s9Data is what the stand-ins serve.
type s9Data struct {
	uploads     string
	projects    map[string]*projects.Project
	artifacts   map[string][]*artifacts.Artifact // by project, in the repository's order
	links       map[string][]*links.Link         // by project, newest first as the repository lists them
	attachments map[string][]*attachments.Attachment
	profiles    map[string]*products.ProductProfile
	defs        map[string][]*attributes.Definition
	latest      map[string]map[string]*vv.TestResult
	runs        map[string][]*vv.TestRun
	baselines   map[string]*baselines.Baseline
	crews       map[string]*teams.TeamGraph
	agents      map[string]*agents.Agent
}

// s9Artifact is one fixture artifact; its times follow from its number.
func s9Artifact(n int, project string, parent int, typ, ref, title, body string, sort, version int,
	attrs map[string]interface{}) *artifacts.Artifact {
	a := &artifacts.Artifact{ID: s9ID(n), ProjectID: project, Type: typ, Ref: ref, Title: title, Body: body,
		SortOrder: sort, Version: version, Attributes: attrs, Status: "draft",
		CreatedAt: s9At(n - 100), UpdatedAt: s9At(n - 70), ValidFrom: s9At(n - 70)}
	if parent != 0 {
		a.ParentID = s9Ptr(s9ID(parent))
	}
	if s, ok := attrs["status"].(string); ok {
		a.Status = s
	}
	return a
}

func s9Link(n int, from, to int, typ string, suspect bool) *links.Link {
	return &links.Link{ID: s9ID(n), FromID: s9ID(from), ToID: s9ID(to), Type: typ, Suspect: suspect,
		Version: 1, ValidFrom: s9At(n - 150), CreatedAt: s9At(n - 150), UpdatedAt: s9At(n - 150)}
}

// newS9Data builds the fixture and writes the figure files it names into a
// directory of its own.
func newS9Data(t *testing.T) *s9Data {
	t.Helper()
	uploads := t.TempDir()
	for name, content := range map[string][]byte{"timing.png": s9PNG, "drawing.svg": []byte(s9SVG)} {
		if err := os.WriteFile(filepath.Join(uploads, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d := &s9Data{
		uploads: uploads,
		projects: map[string]*projects.Project{
			s9P: {ID: s9P, OrgID: s9Org, Name: "Formats \"fixture\" / Ü & co", Description: "The S9 format fixture: <every> renderer & its escaping.",
				AgentAuth: projects.AgentAuthUserAccount, CreatedAt: s9At(0), UpdatedAt: s9At(1)},
			s9C: {ID: s9C, OrgID: s9Org, Name: "Formats supplier", Description: "Refines P.", ParentProjectID: s9P,
				AgentAuth: projects.AgentAuthUserAccount, CreatedAt: s9At(2), UpdatedAt: s9At(3)},
		},
		profiles: map[string]*products.ProductProfile{
			s9P: {ProjectID: s9P, Vision: "Answers before anyone asks.", ProblemStatement: "Operators wait & guess.",
				TargetUsers: "Operators and engineers",
				Constraints: []map[string]interface{}{{"text": "Runs offline"}, {"description": "Fits on one rack"}},
				SuccessMetrics: []map[string]interface{}{
					{"name": "Answer time", "target": "2 s", "current": "1.4 s"}, {"title": "Uptime"}},
				Settings:  map[string]interface{}{"units": "metric"},
				CreatedAt: s9At(4), UpdatedAt: s9At(5)},
		},
	}

	snapshotEntry := map[string]interface{}{"id": s9ID(202), "from_id": s9ID(107), "to_id": s9Req1, "type": "verifies",
		"suspect": false, "version": 1, "valid_from": s9At(52).Format(time.RFC3339)}
	req1 := s9Artifact(104, s9P, 101, "requirement", "REQ-1", "Answer in time", s9Req1Body, 3, 3,
		map[string]interface{}{"status": "approved", "priority": "must", "owner": "alice", "risk": "high",
			"verification_method": "test", "weight": 3, "safety": true, "tags": []interface{}{"timing", "core"},
			"links_snapshot": []interface{}{snapshotEntry}})
	req1.UpdatedAt = req1.UpdatedAt.Add(123456789 * time.Nanosecond)
	d.artifacts = map[string][]*artifacts.Artifact{
		s9P: {
			// Roots first, then each parent's children: the repository's
			// order (parent_id NULLS FIRST, sort_order, created_at).
			s9Artifact(101, s9P, 0, "heading", "HDG-1", "Scope", "What the fixture covers; see REQ-1.", 1, 2,
				map[string]interface{}{"status": "approved"}),
			s9Artifact(106, s9P, 0, "heading", "HDG-2", "Safety", "", 2, 1, map[string]interface{}{"status": "draft"}),
			s9Artifact(111, s9P, 0, "other", "ART-1", "Loose note", "A note no section owns.", 3, 1,
				map[string]interface{}{"status": "draft", "owner": "carol"}),
			s9Artifact(102, s9P, 101, "description", "DSC-1", "Purpose", "Why the fixture exists, in *one* line.", 1, 1,
				map[string]interface{}{"status": "draft"}),
			s9Artifact(103, s9P, 101, "user-need", "NEED-1", "Operators need answers", "Operators wait for answers.", 2, 1,
				map[string]interface{}{"status": "approved", "owner": "alice"}),
			req1,
			s9Artifact(105, s9P, 101, "requirement", "REQ-2", "Log each answer & error <code>",
				"The system shall log each answer.\nThe log shall survive a restart.", 4, 2,
				map[string]interface{}{"status": "draft", "priority": "should", "owner": "bob", "risk": "low",
					"verification_method": "inspection", "verification_status": "verified", "safety": false}),
			s9Artifact(107, s9P, 101, "test-case", "TC-1", "Time the answer", "Measure the answer time under load.", 5, 1,
				map[string]interface{}{"status": "approved", "execution_method": "manual"}),
			s9Artifact(108, s9P, 101, "test-case", "TC-2", "Check the log", "Restart, then read the log.", 6, 1,
				map[string]interface{}{"status": "draft", "execution_method": "agent"}),
			s9Artifact(109, s9P, 106, "hazard", "HAZ-1", "Lost record", "An answer goes unlogged.", 1, 1,
				map[string]interface{}{"status": "draft", "severity": "major"}),
			s9Artifact(110, s9P, 106, "design-item", "DES-1", "Write-ahead log", "Append before answering.", 2, 1,
				map[string]interface{}{"status": "draft", "owner": "bob"}),
		},
		s9C: {
			s9Artifact(121, s9C, 0, "requirement", "REQ-1", "Cache hits", "The cache shall answer within 1 s.", 1, 1,
				map[string]interface{}{"status": "approved", "verification_method": "test"}),
			s9Artifact(122, s9C, 0, "test-case", "TC-1", "Measure hits", "Count the cache hits.", 2, 1,
				map[string]interface{}{"status": "approved"}),
		},
	}

	refines := s9Link(206, 121, 104, "refines", false)
	d.links = map[string][]*links.Link{
		// Newest first, as GetAllLinks lists them (created_at DESC).
		s9P: {s9Link(207, 111, 104, "relates-to", false), refines, s9Link(205, 108, 105, "verifies", false),
			s9Link(204, 110, 109, "mitigates", false), s9Link(203, 110, 105, "satisfies", true),
			s9Link(202, 107, 104, "verifies", false), s9Link(201, 104, 103, "derives-from", false)},
		s9C: {s9Link(208, 122, 121, "verifies", false), refines},
	}

	d.attachments = map[string][]*attachments.Attachment{
		s9Req1: {
			{ID: s9ID(301), ArtifactID: s9Req1, Filename: "REQ-1-FIG-1.png", OriginalFilename: "timing.png", Title: "Timing plot",
				MimeType: "image/png", FilePath: filepath.Join(uploads, "timing.png"), FileSize: len(s9PNG),
				FigureRef: "REQ-1-FIG-1", FigureNum: 1, Version: 1, CreatedAt: s9At(70)},
			{ID: s9ID(302), ArtifactID: s9Req1, Filename: "REQ-1-FIG-2.pdf", OriginalFilename: "datasheet.pdf",
				MimeType: "application/pdf", FilePath: filepath.Join(uploads, "datasheet-not-on-disk.pdf"), FileSize: 1234,
				FigureRef: "REQ-1-FIG-2", FigureNum: 2, Version: 2, CreatedAt: s9At(71)},
		},
		s9ID(110): {
			{ID: s9ID(303), ArtifactID: s9ID(110), Filename: "DES-1-FIG-1.svg", OriginalFilename: "drawing.svg",
				Title: "Log layout", MimeType: "image/svg+xml", FilePath: filepath.Join(uploads, "drawing.svg"),
				FileSize: len(s9SVG), FigureRef: "DES-1-FIG-1", FigureNum: 1, Version: 1, CreatedAt: s9At(72)},
		},
	}

	org := s9Org
	d.defs = map[string][]*attributes.Definition{
		s9P: {
			{ID: s9ID(701), OrgID: &org, Key: "risk", Label: "Risk", DataType: attributes.DataTypeEnum,
				EnumValues: []string{"low", "high"}, AppliesToType: "requirement", SortOrder: 1, CreatedAt: s9At(6)},
			{ID: s9ID(702), ProjectID: s9Ptr(s9P), Key: "safety", Label: "Safety related", DataType: attributes.DataTypeBoolean,
				AppliesToType: "requirement", SortOrder: 2, CreatedAt: s9At(7)},
			{ID: s9ID(703), ProjectID: s9Ptr(s9P), Key: "weight", Label: "Weight", DataType: attributes.DataTypeNumber,
				SortOrder: 3, CreatedAt: s9At(8)},
		},
	}

	d.runs = map[string][]*vv.TestRun{
		s9P: {
			{ID: s9ID(402), ProjectID: s9P, Name: "Regression", Description: "After the log change.", Status: "aborted",
				StartedAt: s9At(90), CreatedAt: s9At(90), UpdatedAt: s9At(95)},
			{ID: s9ID(401), ProjectID: s9P, Name: "Release check", Description: "Before 1.0.", Status: "completed",
				StartedAt: s9At(80), CompletedAt: s9Ptr(s9At(85)), CreatedAt: s9At(80), UpdatedAt: s9At(85)},
		},
		s9C: {
			{ID: s9ID(403), ProjectID: s9C, Name: "Supplier run", Status: "completed", StartedAt: s9At(81),
				CompletedAt: s9Ptr(s9At(82)), CreatedAt: s9At(81), UpdatedAt: s9At(82)},
		},
	}
	d.latest = map[string]map[string]*vv.TestResult{
		s9P: {
			s9ID(107): {ID: s9ID(501), RunID: s9ID(401), TestCaseID: s9ID(107), TestCaseVersion: 1, Status: "pass",
				Notes: "1.4 s at peak", ExecutedAt: s9Ptr(s9At(84)), ExecutedBy: s9Ptr(s9Admin),
				CreatedAt: s9At(84), UpdatedAt: s9At(84)},
			s9ID(108): {ID: s9ID(502), RunID: s9ID(402), TestCaseID: s9ID(108), TestCaseVersion: 1, Status: "fail",
				Notes: "The log lost its last line.", ExecutedAt: s9Ptr(s9At(94)), CreatedAt: s9At(94), UpdatedAt: s9At(94)},
		},
		s9C: {
			s9ID(122): {ID: s9ID(503), RunID: s9ID(403), TestCaseID: s9ID(122), TestCaseVersion: 1, Status: "pass",
				ExecutedAt: s9Ptr(s9At(82)), CreatedAt: s9At(82), UpdatedAt: s9At(82)},
		},
	}

	d.agents = map[string]*agents.Agent{
		s9ID(811): {ID: s9ID(811), OrgID: s9Org, Slug: "lead-reviewer", Name: "Lead reviewer"},
		s9ID(812): {ID: s9ID(812), OrgID: s9Org, Slug: "checker", Name: "Checker"},
	}
	d.crews = map[string]*teams.TeamGraph{
		s9Crew: {
			Team: &teams.Team{ID: s9Crew, OrgID: s9Org, Name: "Review crew: \"S9\"", Description: "Reads, then checks.",
				EntryNodeID: s9Ptr(s9ID(821)), CreatedAt: s9At(100), UpdatedAt: s9At(101)},
			Nodes: []*teams.Node{
				{ID: s9ID(821), TeamID: s9Crew, NodeType: teams.NodeAgent, AgentID: s9ID(811), Label: "Lead",
					Department: "Review", Position: map[string]interface{}{"org": map[string]interface{}{"x": 10, "y": 20}}},
				{ID: s9ID(822), TeamID: s9Crew, NodeType: teams.NodeAgent, AgentID: s9ID(812), Label: "Lead",
					Position: map[string]interface{}{}},
				{ID: s9ID(823), TeamID: s9Crew, NodeType: teams.NodeHuman, UserID: s9Ptr(s9Admin), Label: "Approver"},
			},
			Edges: []*teams.Edge{
				{ID: s9ID(831), TeamID: s9Crew, FromNodeID: s9ID(821), ToNodeID: s9ID(822), EdgeType: "delegates",
					Config: map[string]interface{}{"max": 2}},
				{ID: s9ID(832), TeamID: s9Crew, FromNodeID: s9ID(822), ToNodeID: s9ID(823), EdgeType: "reports_to"},
			},
		},
	}
	return d
}

// --- stand-ins for the leaf services ----------------------------------------

type s9Artifacts struct {
	artifacts.Service
	d *s9Data
}

func (s *s9Artifacts) ListArtifacts(projectID, artifactType string) ([]*artifacts.Artifact, error) {
	var out []*artifacts.Artifact
	for _, a := range s.d.artifacts[projectID] {
		if artifactType == "" || a.Type == artifactType {
			c := *a
			out = append(out, &c)
		}
	}
	return out, nil
}

func (s *s9Artifacts) GetArtifact(id string) (*artifacts.Artifact, error) {
	for _, list := range s.d.artifacts {
		for _, a := range list {
			if a.ID == id {
				c := *a
				return &c, nil
			}
		}
	}
	return nil, artifacts.ErrNotFound
}

type s9Links struct {
	links.Service
	d *s9Data
}

func (s *s9Links) GetAllLinks(projectID string) ([]*links.Link, error) {
	out := make([]*links.Link, 0, len(s.d.links[projectID]))
	for _, l := range s.d.links[projectID] {
		c := *l
		out = append(out, &c)
	}
	return out, nil
}

type s9Attachments struct {
	attachments.Service
	d *s9Data
}

func (s *s9Attachments) GetAttachmentsByArtifacts(ids []string) (map[string][]*attachments.Attachment, error) {
	out := map[string][]*attachments.Attachment{}
	for _, id := range ids {
		for _, a := range s.d.attachments[id] {
			c := *a
			out[id] = append(out[id], &c)
		}
	}
	return out, nil
}

type s9ProjectInfo struct{ d *s9Data }

func (s *s9ProjectInfo) FindByID(id string) (*exports.ProjectInfo, error) {
	p, ok := s.d.projects[id]
	if !ok {
		return nil, fmt.Errorf("project %s not found", id)
	}
	return &exports.ProjectInfo{ID: p.ID, Name: p.Name, Description: p.Description}, nil
}

type s9Projects struct {
	projects.Service
	d *s9Data
}

func (s *s9Projects) GetProject(id string) (*projects.Project, error) {
	p, ok := s.d.projects[id]
	if !ok {
		return nil, fmt.Errorf("project %s not found", id)
	}
	c := *p
	return &c, nil
}

type s9Products struct {
	products.Service
	d *s9Data
}

func (s *s9Products) GetProfile(projectID string) (*products.ProductProfile, error) {
	if p, ok := s.d.profiles[projectID]; ok {
		c := *p
		return &c, nil
	}
	// The real service creates an empty profile on first read.
	return &products.ProductProfile{ProjectID: projectID, CreatedAt: s9At(9), UpdatedAt: s9At(9)}, nil
}

type s9Attributes struct {
	attributes.Service
	d *s9Data
}

func (s *s9Attributes) EffectiveForProject(_, projectID string) ([]*attributes.Definition, error) {
	return s.d.defs[projectID], nil
}

type s9Baselines struct {
	baselines.Repository
	d *s9Data
}

func (s *s9Baselines) GetByID(id string) (*baselines.Baseline, error) {
	if b, ok := s.d.baselines[id]; ok {
		return b, nil
	}
	return nil, baselines.ErrNotFound
}

type s9VV struct {
	vv.Service
	d *s9Data
}

func (s *s9VV) LatestResults(projectID string) (map[string]*vv.TestResult, error) {
	out := map[string]*vv.TestResult{}
	for k, v := range s.d.latest[projectID] {
		out[k] = v
	}
	return out, nil
}

func (s *s9VV) ListRuns(projectID string) ([]*vv.TestRun, error) {
	return s.d.runs[projectID], nil
}

type s9Teams struct {
	teams.Service
	d *s9Data
}

func (s *s9Teams) GetTeam(id string) (*teams.TeamGraph, error) {
	if g, ok := s.d.crews[id]; ok {
		return g, nil
	}
	return nil, fmt.Errorf("team %s not found", id)
}

type s9Agents struct {
	agents.Service
	d *s9Data
}

func (s *s9Agents) Get(id string) (*agents.Agent, error) {
	if a, ok := s.d.agents[id]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("agent %s not found", id)
}

// s9Fixture is the wired handler and router over the fixture.
type s9Fixture struct {
	d      *s9Data
	h      *Handler
	router *mux.Router
	export *exports.DefaultService
}

// newS9Fixture wires the real export, report and download services over
// the stand-ins as cmd/server/main.go wires them (the product profile, the
// attribute definitions, the evidence and the workspace sources), and the
// baselines' snapshots through the real export service with their capture
// time pinned.
//
// The handler is vvHandler's, which newTestHandler builds (K6), with every
// service it needs set.
func newS9Fixture(t *testing.T) *s9Fixture {
	t.Helper()
	d := newS9Data(t)
	projectSvc := &s9Projects{d: d}
	exportSvc := exports.NewService(&s9Artifacts{d: d}, &s9Links{d: d}, &s9Attachments{d: d}, &s9ProjectInfo{d: d}, projectSvc)
	exportSvc.SetProductService(&s9Products{d: d})
	exportSvc.SetAttributeService(&s9Attributes{d: d})
	baselineSvc := baselines.NewService(&s9Baselines{d: d})
	reportSvc := reports.NewService(exportSvc, baselineSvc)
	vvSvc := &s9VV{d: d}
	downloadSvc := downloads.NewService(exportSvc, reportSvc)
	downloadSvc.SetEvidenceSource(func(projectID string) (map[string]*vv.TestResult, []*vv.TestRun, error) {
		latest, _ := vvSvc.LatestResults(projectID)
		runs, _ := vvSvc.ListRuns(projectID)
		return latest, runs, nil
	})
	downloadSvc.SetWorkspaceSource(func(string) (reports.Workspace, error) {
		return reports.Workspace{Name: "Fixture Works", Logo: s9PNG, LogoMime: "image/png"}, nil
	})

	// The baselines, as CreateBaseline captures them (exports.Service's
	// Snapshot), with the snapshot's exported_at pinned to its capture.
	d.baselines = map[string]*baselines.Baseline{}
	for _, b := range []struct {
		id, name string
		defs     bool
		at       time.Time
	}{{s9B1, "Fixture baseline 1.0", true, s9At(60)}, {s9B0, "Before definitions", false, s9At(40)}} {
		snap, err := exportSvc.PrepareExport(s9P, b.defs)
		if err != nil {
			t.Fatalf("prepare the snapshot of %s: %v", b.name, err)
		}
		snap.ExportedAt = b.at
		raw, _, err := exportSvc.RenderExport(snap, exports.FormatJSON)
		if err != nil {
			t.Fatalf("render the snapshot of %s: %v", b.name, err)
		}
		d.baselines[b.id] = &baselines.Baseline{ID: b.id, ProjectID: s9P, Name: b.name, Snapshot: raw, CreatedAt: b.at,
			CreatedBy: s9Ptr(s9Admin), CreatedByName: "Ada Admin"}
	}

	h := vvHandler(t, vvSvc)
	h.projectService = projectSvc
	h.exportService = exportSvc
	h.baselineService = baselineSvc
	h.reportService = reportSvc
	h.downloadService = downloadSvc
	h.teamService = &s9Teams{d: d}
	h.agentService = &s9Agents{d: d}
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return &s9Fixture{d: d, h: h, router: router, export: exportSvc}
}

// s9Request is a GET as the fixture's platform admin, who passes every
// project and workspace guard without a membership.
func s9Request(path string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, path, nil)
	return r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: s9Admin, Email: "ada@example.com",
		Name: "Ada Admin", IsAdmin: true}))
}

// s9SortedKeys lists a map's keys in order.
func s9SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
