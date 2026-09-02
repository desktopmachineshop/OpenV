package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// fakeNameUserService counts lookups so the memoization can be asserted.
type fakeNameUserService struct {
	users.Service
	byID  map[string]*users.User
	calls int
}

func (f *fakeNameUserService) GetByID(id string) (*users.User, error) {
	f.calls++
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

type fakeNameWorkItemService struct {
	workitems.Service
	byID map[string]*workitems.WorkItem
}

func (f *fakeNameWorkItemService) Get(id string) (*workitems.WorkItem, error) {
	if item, ok := f.byID[id]; ok {
		return item, nil
	}
	return nil, errors.New("work item not found")
}

type fakeNameBaselineService struct {
	baselines.Service
	byID map[string]*baselines.Baseline
}

func (f *fakeNameBaselineService) GetBaseline(id string) (*baselines.Baseline, error) {
	if b, ok := f.byID[id]; ok {
		return b, nil
	}
	return nil, baselines.ErrNotFound
}

func namingHandler() (*Handler, *fakeNameUserService) {
	userSvc := &fakeNameUserService{byID: map[string]*users.User{
		"u1": {ID: "u1", Name: "Dana Ross", Email: "dana@example.com"},
		"u2": {ID: "u2", Email: "sam@example.com"}, // no display name
	}}
	return &Handler{
		userService: userSvc,
		artifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"a1": {ID: "a1", Ref: "REQ-12", Title: "The positioner shall comply with UL1740"},
			"a2": {ID: "a2", Ref: "TC-3", Title: "Compliance test"},
			"a3": {ID: "a3", Title: "Untitled ref-less artifact"},
		}},
		workItemService: &fakeNameWorkItemService{byID: map[string]*workitems.WorkItem{
			"w1": {ID: "w1", Title: "Wire the safety interlock"},
		}},
		baselineService: &fakeNameBaselineService{byID: map[string]*baselines.Baseline{
			"b1": {ID: "b1", Name: "Release 1.2"},
		}},
		runService: &fakeRunService{byID: map[string]*agentruns.Run{
			"r1": {ID: "r1", AgentID: "ag1"},
		}},
		agentService: &fakeAgentService{byID: map[string]*agents.Agent{
			"ag1": {ID: "ag1", Slug: "req-analyst", Name: "Requirements Analyst"},
		}},
	}, userSvc
}

// TestDecorateEventsNames locks in issue: the activity log names the user and
// the entity behind each event's raw IDs, using the payload the publisher
// stamped in where it has one and a lookup where it does not.
func TestDecorateEventsNames(t *testing.T) {
	h, _ := namingHandler()

	cases := []struct {
		name       string
		event      events.Event
		actorKind  string
		actorID    string
		actorName  string
		entityKind string
		entityName string
	}{
		{
			name: "user actor and artifact title from the payload",
			event: events.Event{
				EventType: events.ArtifactUpdated, EntityID: "a1", Actor: "user:u1",
				Payload: map[string]interface{}{"artifact_type": "requirement", "title": "Payload title"},
			},
			actorKind: "user", actorID: "u1", actorName: "Dana Ross",
			entityKind: "requirement", entityName: "Payload title",
		},
		{
			name: "user without a display name falls back to the email",
			event: events.Event{
				EventType: events.ArtifactCreated, EntityID: "a1", Actor: "user:u2",
				Payload: map[string]interface{}{"title": "New requirement"},
			},
			actorKind: "user", actorID: "u2", actorName: "sam@example.com",
			entityKind: "artifact", entityName: "New requirement",
		},
		{
			name:      "deleted artifact carries no payload, so the name is looked up",
			event:     events.Event{EventType: events.ArtifactDeleted, EntityID: "a1", Actor: "user:u1"},
			actorKind: "user", actorID: "u1", actorName: "Dana Ross",
			entityKind: "artifact", entityName: "REQ-12 The positioner shall comply with UL1740",
		},
		{
			name: "agent actor names the agent behind the run",
			event: events.Event{
				EventType: events.RunFinished, EntityID: "r1", Actor: "agent:r1",
				Payload: map[string]interface{}{"agent_id": "ag1", "status": "succeeded"},
			},
			actorKind: "agent", actorID: "r1", actorName: "Requirements Analyst",
			entityKind: "agent run", entityName: "Requirements Analyst",
		},
		{
			name: "link names both ends",
			event: events.Event{
				EventType: events.LinkCreated, EntityID: "l1", Actor: "user:u1",
				Payload: map[string]interface{}{"link_type": "verifies", "from_id": "a2", "to_id": "a1"},
			},
			actorKind: "user", actorID: "u1", actorName: "Dana Ross",
			entityKind: "verifies", entityName: "TC-3 Compliance test → REQ-12 The positioner shall comply with UL1740",
		},
		{
			name: "work item title is looked up when the payload has none",
			event: events.Event{
				EventType: events.WorkItemMoved, EntityID: "w1", Actor: "user:u1",
				Payload: map[string]interface{}{"column": "doing"},
			},
			actorKind: "user", actorID: "u1", actorName: "Dana Ross",
			entityKind: "work item", entityName: "Wire the safety interlock",
		},
		{
			name: "baseline name from the payload",
			event: events.Event{
				EventType: events.BaselineCaptured, EntityID: "b1", Actor: "system",
				Payload: map[string]interface{}{"name": "Release 1.2"},
			},
			actorKind: "system", actorName: "System",
			entityKind: "baseline", entityName: "Release 1.2",
		},
		{
			name: "chatter is named by the artifact it was posted on",
			event: events.Event{
				EventType: events.ChatterCreated, EntityID: "c1", Actor: "user:u1",
				Payload: map[string]interface{}{"artifact_id": "a1", "message": "looks good"},
			},
			actorKind: "user", actorID: "u1", actorName: "Dana Ross",
			entityKind: "comment", entityName: "REQ-12 The positioner shall comply with UL1740",
		},
		{
			name: "test result is named by its test case",
			event: events.Event{
				EventType: events.TestRunRecorded, EntityID: "res1", Actor: "user:u1",
				Payload: map[string]interface{}{"test_case_id": "a2", "status": "pass"},
			},
			actorKind: "user", actorID: "u1", actorName: "Dana Ross",
			entityKind: "test result", entityName: "TC-3 Compliance test",
		},
		{
			name:      "worker key reserved to a user names that user",
			event:     events.Event{EventType: events.ArtifactUpdated, EntityID: "a3", Actor: "worker:org-1:user:u1"},
			actorKind: "worker", actorID: "u1", actorName: "Dana Ross",
			entityKind: "artifact", entityName: "Untitled ref-less artifact",
		},
		{
			name:      "unreserved worker key",
			event:     events.Event{EventType: events.ArtifactUpdated, EntityID: "a3", Actor: "worker:org-1"},
			actorKind: "worker", actorID: "org-1", actorName: "Workspace runner",
			entityKind: "artifact", entityName: "Untitled ref-less artifact",
		},
		{
			name:      "unknown user leaves the name empty rather than inventing one",
			event:     events.Event{EventType: events.ArtifactDeleted, EntityID: "missing", Actor: "user:ghost"},
			actorKind: "user", actorID: "ghost", actorName: "",
			entityKind: "artifact", entityName: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := h.decorateEvents([]events.Event{tc.event})
			if len(got) != 1 {
				t.Fatalf("decorateEvents returned %d views, want 1", len(got))
			}
			v := got[0]
			if v.ActorKind != tc.actorKind || v.ActorID != tc.actorID || v.ActorName != tc.actorName {
				t.Errorf("actor = %q/%q/%q, want %q/%q/%q",
					v.ActorKind, v.ActorID, v.ActorName, tc.actorKind, tc.actorID, tc.actorName)
			}
			if v.EntityKind != tc.entityKind || v.EntityName != tc.entityName {
				t.Errorf("entity = %q/%q, want %q/%q", v.EntityKind, v.EntityName, tc.entityKind, tc.entityName)
			}
			// The raw audit values survive alongside the names.
			if v.Actor != tc.event.Actor || v.EntityID != tc.event.EntityID {
				t.Errorf("raw actor/entity = %q/%q, want %q/%q", v.Actor, v.EntityID, tc.event.Actor, tc.event.EntityID)
			}
		})
	}
}

// A page repeats the same actor on most rows; the resolver must not query the
// user once per row.
func TestDecorateEventsMemoizesLookups(t *testing.T) {
	h, userSvc := namingHandler()

	var page []events.Event
	for i := 0; i < 25; i++ {
		page = append(page, events.Event{EventType: events.ArtifactUpdated, EntityID: "a1", Actor: "user:u1",
			Payload: map[string]interface{}{"title": "t"}})
	}
	h.decorateEvents(page)

	if userSvc.calls != 1 {
		t.Errorf("user lookups = %d, want 1 for 25 events by the same actor", userSvc.calls)
	}
}

// Services are optional on the handler (and a lookup can fail); naming must
// degrade to the raw IDs rather than panicking.
func TestDecorateEventsWithoutServices(t *testing.T) {
	h := &Handler{}
	got := h.decorateEvents([]events.Event{
		{EventType: events.ArtifactDeleted, EntityID: "a1", Actor: "user:u1"},
		{EventType: events.RunFinished, EntityID: "r1", Actor: "agent:r1"},
	})
	if len(got) != 2 {
		t.Fatalf("decorateEvents returned %d views, want 2", len(got))
	}
	for _, v := range got {
		if v.EntityName != "" || v.ActorName != "" {
			t.Errorf("event %s: names = %q/%q, want both empty", v.EventType, v.ActorName, v.EntityName)
		}
		if v.ActorID == "" {
			t.Errorf("event %s: actor id should still be parsed from %q", v.EventType, v.Actor)
		}
	}
}

// The names reach the wire: GET /api/v1/events serves them next to the raw
// actor/entity IDs the activity log still shows.
func TestListDomainEventsServesNames(t *testing.T) {
	const orgID = "org-1"
	h, _ := namingHandler()
	h.eventRepo = &fakeEventRepo{byOrg: map[string][]events.Event{
		orgID: {{
			ID: "e1", OrgID: orgID, ProjectID: "proj-1", EventType: events.ArtifactUpdated,
			EntityID: "a1", Actor: "user:u1",
			Payload: map[string]interface{}{"artifact_type": "requirement", "title": "The positioner shall comply with UL1740"},
		}},
	}}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1", IsAdmin: true})
	ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
	w := httptest.NewRecorder()
	h.ListDomainEvents(w, r.WithContext(ctx))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d events, want 1", len(list))
	}
	want := map[string]string{
		"actor":       "user:u1",
		"actor_kind":  "user",
		"actor_id":    "u1",
		"actor_name":  "Dana Ross",
		"entity_id":   "a1",
		"entity_kind": "requirement",
		"entity_name": "The positioner shall comply with UL1740",
	}
	for key, wantVal := range want {
		if got, _ := list[0][key].(string); got != wantVal {
			t.Errorf("%s = %q, want %q", key, got, wantVal)
		}
	}
}
