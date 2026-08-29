package api

import (
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// recordingBus captures every published event so applier tests can assert
// that approved proposals emit the same domain events a direct write does.
type recordingBus struct {
	events.Bus
	published []events.Event
}

func (b *recordingBus) Publish(e events.Event) { b.published = append(b.published, e) }
func (b *recordingBus) Subscribe(func(events.Event)) {}

func (b *recordingBus) types() []string {
	out := make([]string, 0, len(b.published))
	for _, e := range b.published {
		out = append(out, e.EventType)
	}
	return out
}

// applierArtifactService records the writes the appliers drive so tests can
// assert both the effect and — crucially for #176 — the absence of a silent
// drop when a proposal carries managed link edits.
type applierArtifactService struct {
	artifacts.Service
	byID       map[string]*artifacts.Artifact
	created    []*artifacts.Artifact
	updated    []string
	updateReqs []artifacts.UpdateArtifactRequest
	deleted    []string
}

func (f *applierArtifactService) GetArtifact(id string) (*artifacts.Artifact, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("artifact not found")
}

func (f *applierArtifactService) CreateArtifact(a *artifacts.Artifact) error {
	f.created = append(f.created, a)
	if f.byID == nil {
		f.byID = map[string]*artifacts.Artifact{}
	}
	f.byID[a.ID] = a
	return nil
}

func (f *applierArtifactService) UpdateArtifact(id string, req artifacts.UpdateArtifactRequest) (*artifacts.Artifact, error) {
	f.updated = append(f.updated, id)
	f.updateReqs = append(f.updateReqs, req)
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return &artifacts.Artifact{ID: id}, nil
}

func (f *applierArtifactService) DeleteArtifact(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type applierLinkService struct {
	links.Service
	byID    map[string]*links.Link
	created []*links.Link
	deleted []string
}

func (f *applierLinkService) CreateLink(l *links.Link) error {
	f.created = append(f.created, l)
	return nil
}

func (f *applierLinkService) GetLink(id string) (*links.Link, error) {
	if l, ok := f.byID[id]; ok {
		return l, nil
	}
	return nil, errors.New("link not found")
}

func (f *applierLinkService) DeleteLink(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *applierLinkService) GetLinksFrom(string) ([]*links.Link, error) { return nil, nil }
func (f *applierLinkService) GetLinksTo(string) ([]*links.Link, error)   { return nil, nil }

func newApplierHandler(bus *recordingBus, artSvc *applierArtifactService, linkSvc *applierLinkService) *Handler {
	return &Handler{
		artifactService: artSvc,
		linkService:     linkSvc,
		chatterService:  &fakeChatterService{},
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
		bus: bus,
	}
}

// TestApplyUpdateArtifactRejectsLinkOps is the #176 no-silent-drop guarantee:
// the UpdateArtifact applier decodes into the same request the HTTP handler
// uses, but only the HTTP handler processes pendingLinkAdds/pendingLinkRemoves.
// Applying such a payload here must reject (surfacing as apply_failed) rather
// than run the artifact update and quietly discard the link edits.
func TestApplyUpdateArtifactRejectsLinkOps(t *testing.T) {
	bus := &recordingBus{}
	artSvc := &applierArtifactService{byID: map[string]*artifacts.Artifact{
		"art-1": {ID: "art-1", ProjectID: "proj-1", Type: "requirement", Title: "Req", Version: 1},
	}}
	h := newApplierHandler(bus, artSvc, &applierLinkService{})

	cases := []struct {
		name    string
		payload map[string]interface{}
	}{
		{"pending adds", map[string]interface{}{
			"title":           "Renamed",
			"pendingLinkAdds": []interface{}{map[string]interface{}{"from_id": "art-1", "to_id": "art-2", "type": "verifies"}},
		}},
		{"pending removes", map[string]interface{}{
			"title":              "Renamed",
			"pendingLinkRemoves": []interface{}{"link-9"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			artSvc.updated = nil
			bus.published = nil
			_, err := h.applyUpdateArtifact("art-1", tc.payload)
			if err == nil {
				t.Fatal("applyUpdateArtifact accepted a payload carrying link ops; want rejection")
			}
			if len(artSvc.updated) != 0 {
				t.Errorf("artifact was updated despite link ops being present (silent drop): %v", artSvc.updated)
			}
			if len(bus.published) != 0 {
				t.Errorf("an event was published for a rejected apply: %v", bus.types())
			}
		})
	}
}

// TestApplyArtifactAppliersPublishEvents locks in that approved artifact
// writes emit the same domain events the HTTP handlers do, so the activity
// log, SSE refresh, and automations see agent-applied writes.
func TestApplyArtifactAppliersPublishEvents(t *testing.T) {
	bus := &recordingBus{}
	artSvc := &applierArtifactService{byID: map[string]*artifacts.Artifact{
		"art-1": {ID: "art-1", ProjectID: "proj-1", Type: "requirement", Title: "Req", Version: 2},
	}}
	h := newApplierHandler(bus, artSvc, &applierLinkService{})

	// create
	id, err := h.applyCreateArtifact(map[string]interface{}{"project_id": "proj-1", "type": "requirement", "title": "New"})
	if err != nil {
		t.Fatalf("applyCreateArtifact: %v", err)
	}
	if id == "" {
		t.Fatal("applyCreateArtifact returned empty id")
	}

	// update (no link ops)
	if _, err := h.applyUpdateArtifact("art-1", map[string]interface{}{"title": "Renamed"}); err != nil {
		t.Fatalf("applyUpdateArtifact: %v", err)
	}

	// delete
	if err := h.applyDeleteArtifact("art-1"); err != nil {
		t.Fatalf("applyDeleteArtifact: %v", err)
	}
	if len(artSvc.deleted) != 1 || artSvc.deleted[0] != "art-1" {
		t.Fatalf("delete not applied: %v", artSvc.deleted)
	}

	want := []string{events.ArtifactCreated, events.ArtifactUpdated, events.ArtifactDeleted}
	got := bus.types()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	// Every applied event is stamped system-actor and carries the org.
	for _, e := range bus.published {
		if e.Actor != events.ActorSystem {
			t.Errorf("event %s actor = %q, want %q", e.EventType, e.Actor, events.ActorSystem)
		}
		if e.OrgID != "org-1" {
			t.Errorf("event %s org = %q, want org-1", e.EventType, e.OrgID)
		}
	}
}

// TestApplyLinkAppliersPublishAndAutoVersion locks in that approved link
// writes both emit their event and refresh the endpoints' link snapshots
// (autoVersionLinkedArtifacts), matching the HTTP link handlers.
func TestApplyLinkAppliersPublishAndAutoVersion(t *testing.T) {
	bus := &recordingBus{}
	artSvc := &applierArtifactService{byID: map[string]*artifacts.Artifact{
		"art-1": {ID: "art-1", ProjectID: "proj-1", Type: "requirement", Title: "From", Version: 1},
		"art-2": {ID: "art-2", ProjectID: "proj-1", Type: "test_case", Title: "To", Version: 1},
	}}
	linkSvc := &applierLinkService{byID: map[string]*links.Link{
		"link-1": {ID: "link-1", FromID: "art-1", ToID: "art-2", Type: "verifies"},
	}}
	h := newApplierHandler(bus, artSvc, linkSvc)

	// create link
	if _, err := h.applyCreateLink(map[string]interface{}{"from_id": "art-1", "to_id": "art-2", "type": "verifies"}); err != nil {
		t.Fatalf("applyCreateLink: %v", err)
	}
	if len(linkSvc.created) != 1 {
		t.Fatalf("link not created: %v", linkSvc.created)
	}
	// autoVersion bumps both endpoints (one UpdateArtifact each).
	if len(artSvc.updated) != 2 {
		t.Errorf("applyCreateLink auto-versioned %d artifacts, want 2 (both endpoints): %v", len(artSvc.updated), artSvc.updated)
	}

	artSvc.updated = nil

	// delete link
	if err := h.applyDeleteLink("link-1"); err != nil {
		t.Fatalf("applyDeleteLink: %v", err)
	}
	if len(linkSvc.deleted) != 1 || linkSvc.deleted[0] != "link-1" {
		t.Fatalf("link not deleted: %v", linkSvc.deleted)
	}
	if len(artSvc.updated) != 2 {
		t.Errorf("applyDeleteLink auto-versioned %d artifacts, want 2: %v", len(artSvc.updated), artSvc.updated)
	}

	want := []string{events.LinkCreated, events.LinkDeleted}
	got := bus.types()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for _, e := range bus.published {
		if e.ProjectID != "proj-1" {
			t.Errorf("link event %s project = %q, want proj-1", e.EventType, e.ProjectID)
		}
	}
}
