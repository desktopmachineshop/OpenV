package traceability

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// The paths' own behavior is pinned through the API by
// internal/api/traceability_paths_test.go (X11a). These tests pin what the
// Policy fields ask of the ports, including the values no path passes today.

type fakeAccess struct {
	roles    map[string]string // project -> the caller's role
	flowDown bool
	asked    []string
}

func (a *fakeAccess) HasProjectRole(projectID, role string) bool {
	a.asked = append(a.asked, "role "+projectID+" "+role)
	return members.RoleAtLeast(a.roles[projectID], role)
}

func (a *fakeAccess) FlowDownEnabled(projectID string) bool {
	a.asked = append(a.asked, "flow-down "+projectID)
	return a.flowDown
}

func TestCheckAccessAsksWhatThePolicyAsks(t *testing.T) {
	src, dst := End{Type: "requirement", ProjectID: "c"}, End{Type: "requirement", ProjectID: "p"}
	for _, tc := range []struct {
		name     string
		p        Policy
		linkType string
		flowDown bool
		want     string
		asked    []string
	}{
		{"no role asked", Policy{TargetRole: AsksNoRole}, "relates-to", false, "<nil>", nil},
		{"editor on the other end", Policy{TargetRole: AsksEditor}, "relates-to", false,
			"no editor access to the target's project", []string{"role p editor"}},
		{"editor even for refines", Policy{TargetRole: AsksEditor}, links.TypeRefines, true,
			"no editor access to the target's project", []string{"role p editor"}},
		{"viewer for refines while the feature is on", Policy{TargetRole: AsksEditorOrFlowDownViewer},
			links.TypeRefines, true, "<nil>", []string{"flow-down c", "role p viewer"}},
		{"editor for refines while it is off", Policy{TargetRole: AsksEditorOrFlowDownViewer},
			links.TypeRefines, false, "no editor access to the target's project", []string{"flow-down c", "role p editor"}},
		{"the gate, asked once", Policy{RequireFlowDownFeature: true, TargetRole: AsksEditorOrFlowDownViewer},
			links.TypeRefines, true, "<nil>", []string{"flow-down c", "role p viewer"}},
		{"the gate refuses", Policy{RequireFlowDownFeature: true}, links.TypeRefines, false,
			ErrFlowDownClosed.Error(), []string{"flow-down c"}},
		{"the gate asks only of refines", Policy{RequireFlowDownFeature: true}, "relates-to", false, "<nil>", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := &fakeAccess{roles: map[string]string{"c": members.RoleEditor, "p": members.RoleViewer}, flowDown: tc.flowDown}
			s := &Service{Access: access}
			if got := fmt.Sprint(s.CheckAccess(tc.p, "c", src, dst, tc.linkType)); got != tc.want {
				t.Errorf("CheckAccess = %s, want %s", got, tc.want)
			}
			if !reflect.DeepEqual(access.asked, tc.asked) {
				t.Errorf("asked %q, want %q", access.asked, tc.asked)
			}
		})
	}

	t.Run("no Access refuses a role and closes the gate", func(t *testing.T) {
		s := &Service{}
		if err := s.CheckAccess(Policy{TargetRole: AsksEditor}, "c", src, dst, "relates-to"); err == nil {
			t.Error("a role was granted with no Access to ask")
		}
		if err := s.CheckFlowDown(Policy{RequireFlowDownFeature: true}, "c", links.TypeRefines); !errors.Is(err, ErrFlowDownClosed) {
			t.Errorf("CheckFlowDown = %v, want ErrFlowDownClosed", err)
		}
	})

	t.Run("an end in the base project or not resolved asks nothing", func(t *testing.T) {
		access := &fakeAccess{}
		s := &Service{Access: access}
		if err := s.CheckAccess(Policy{TargetRole: AsksEditor}, "c", src, End{}, "relates-to"); err != nil {
			t.Error(err)
		}
		if len(access.asked) != 0 {
			t.Errorf("asked %q", access.asked)
		}
	})
}

func TestCheckRulesSkipsAnEndNotResolved(t *testing.T) {
	tc, req := End{Type: "test-case", ProjectID: "p"}, End{Type: "requirement", ProjectID: "p"}
	if err := CheckRules("verifies", tc, req); err != nil {
		t.Errorf("a valid link: %v", err)
	}
	if err := CheckRules("verifies", req, tc); err == nil {
		t.Error("a link the rules refuse passed")
	}
	if err := CheckRules("verifies", req, End{}); err != nil {
		t.Errorf("a link to a pending ref: %v", err)
	}
}

type published struct {
	eventType, projectID, entityID, actor string
	payload                               map[string]interface{}
}

func TestPublishLinkEventFollowsEmitEventsAndActor(t *testing.T) {
	var got []published
	s := &Service{Publish: func(eventType, projectID, entityID, actor string, payload map[string]interface{}) {
		got = append(got, published{eventType, projectID, entityID, actor, payload})
	}}
	link := &links.Link{ID: "l1", FromID: "a", ToID: "b", Type: "verifies"}
	s.PublishLinkEvent(Policy{EmitEvents: false, Actor: "user:u"}, "link.created", "p", link)
	if len(got) != 0 {
		t.Fatalf("published %v with EmitEvents false", got)
	}
	s.PublishLinkEvent(Policy{EmitEvents: true, Actor: "system"}, "link.deleted", "p", link)
	want := []published{{"link.deleted", "p", "l1", "system",
		map[string]interface{}{"link_type": "verifies", "from_id": "a", "to_id": "b"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
	(&Service{}).PublishLinkEvent(Policy{EmitEvents: true}, "link.created", "p", link) // no Publisher: nothing
}

// stores is the artifact, link and note store the write tests share.
type stores struct {
	arts    map[string]*artifacts.Artifact
	links   []*links.Link
	deleted []string
	notes   []string
	updates []string
}

func (s *stores) GetArtifact(id string) (*artifacts.Artifact, error) {
	if a, ok := s.arts[id]; ok {
		return a, nil
	}
	return nil, artifacts.ErrNotFound
}

func (s *stores) UpdateArtifact(id string, req artifacts.UpdateArtifactRequest) (*artifacts.Artifact, error) {
	s.updates = append(s.updates, id)
	return s.arts[id], nil
}

func (s *stores) GetLink(id string) (*links.Link, error) {
	for _, l := range s.links {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, errors.New("link not found")
}

func (s *stores) CreateLink(l *links.Link) error { s.links = append(s.links, l); return nil }
func (s *stores) DeleteLink(id string) error     { s.deleted = append(s.deleted, id); return nil }

func (s *stores) GetLinksTo(id string) ([]*links.Link, error) {
	var out []*links.Link
	for _, l := range s.links {
		if l.ToID == id {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *stores) GetLinksFrom(id string) ([]*links.Link, error) {
	var out []*links.Link
	for _, l := range s.links {
		if l.FromID == id {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *stores) CreateEntry(e *chatter.ChatterEntry) error {
	s.notes = append(s.notes, e.ArtifactID+": "+e.Message)
	return nil
}

func newStores() *stores {
	return &stores{arts: map[string]*artifacts.Artifact{
		"tc":  {ID: "tc", ProjectID: "p", Type: "test-case", Version: 1},
		"req": {ID: "req", ProjectID: "p", Type: "requirement", Version: 3},
	}}
}

func TestApplyManagedLinkEditsUnderRefuseStopsAtTheFirstRefusal(t *testing.T) {
	st := newStores()
	s := &Service{Artifacts: st, Links: st, Notes: st}
	adds := []interface{}{
		map[string]interface{}{"from_id": "tc", "to_id": "req", "type": "verifies"},
		map[string]interface{}{"from_id": "req", "to_id": "tc", "type": "verifies"}, // the rules refuse it
		map[string]interface{}{"from_id": "tc", "to_id": "req", "type": "impacts"},
	}
	changes, err := s.ApplyManagedLinkEdits(Policy{OnInvalid: Refuse}, "p", "tc", adds, nil)
	if err == nil || changes != nil {
		t.Fatalf("got %v, %v; want the rules' refusal", changes, err)
	}
	if len(st.links) != 1 {
		t.Errorf("made %d links, want the one before the refusal", len(st.links))
	}

	st = newStores()
	s = &Service{Artifacts: st, Links: st, Notes: st}
	changes, err = s.ApplyManagedLinkEdits(Policy{OnInvalid: Skip}, "p", "tc", adds, nil)
	if err != nil || len(changes.Added) != 2 || !reflect.DeepEqual(changes.Affected, []string{"req"}) {
		t.Fatalf("got %+v, %v; want two links made and req affected", changes, err)
	}
	if got := changes.Added[0].Attributes; got == nil || len(got) != 0 {
		t.Errorf("an add with no attributes stores %#v, want an empty object", got)
	}
	if len(st.updates) != 0 || len(st.notes) != 0 {
		t.Errorf("the edits versioned %v and noted %v; the caller does that", st.updates, st.notes)
	}
}

func TestSetLinksSnapshotWritesOnlyWhileALinkRemains(t *testing.T) {
	st := newStores()
	s := &Service{Artifacts: st, Links: st, Notes: st}
	current := map[string]interface{}{"status": "draft"}

	var req artifacts.UpdateArtifactRequest
	s.SetLinksSnapshot(&req, "tc", current)
	if req.Attributes != nil {
		t.Errorf("no link remains, yet the attributes became %v (Q4)", req.Attributes)
	}

	link, err := s.CreateLink(links.CreateLinkRequest{FromID: "tc", ToID: "req", Type: "verifies"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tc: Auto-updated to version 2 due to link changes",
		"req: Auto-updated to version 4 due to link changes"}; !reflect.DeepEqual(st.notes, want) {
		t.Errorf("notes %q, want %q", st.notes, want)
	}
	s.SetLinksSnapshot(&req, "tc", current)
	if want := []interface{}{link}; req.Attributes["status"] != "draft" || !reflect.DeepEqual(req.Attributes["links_snapshot"], want) {
		t.Errorf("attributes %v, want the current ones and the snapshot", req.Attributes)
	}
	if _, touched := current["links_snapshot"]; touched {
		t.Error("the artifact's own attributes were written")
	}
}
