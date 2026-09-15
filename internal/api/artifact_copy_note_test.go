package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// A duplicate or a paste opens the copy's feed with one note naming the
// source, and only when the caller can read that source.

type copyArtifactFake struct {
	artifacts.Service
	byID    map[string]*artifacts.Artifact
	created []*artifacts.Artifact
}

func (f *copyArtifactFake) GetArtifact(id string) (*artifacts.Artifact, error) {
	return f.byID[id], nil
}

func (f *copyArtifactFake) CreateArtifact(a *artifacts.Artifact) error {
	f.created = append(f.created, a)
	f.byID[a.ID] = a
	return nil
}

func copyHandler() (*Handler, *fakeChatterService) {
	notes := &fakeChatterService{}
	h := &Handler{
		artifactService: &copyArtifactFake{byID: map[string]*artifacts.Artifact{
			"src":   {ID: "src", ProjectID: "p1", Ref: "REQ-12", Title: "Pump pressure", Version: 3},
			"other": {ID: "other", ProjectID: "p2", Ref: "REQ-1", Title: "Secret", Version: 1},
		}},
		chatterService: notes,
		projectService: &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "o1"}, "p2": {ID: "p2", OrgID: "o2"}}},
		memberService:  &fakeMemberService{roles: map[string]map[string]string{"p1": {"u1": "editor"}}},
		orgService:     &fakeOrgService{},
	}
	return h, notes
}

func createReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1", Name: "Ada"}))
}

func TestCreateArtifactNotesWhereACopyCameFrom(t *testing.T) {
	cases := []struct {
		name string
		body string
		note string
	}{
		{"duplicate", `{"project_id":"p1","type":"requirement","title":"Pump pressure (copy)","body":"x","copied_from":"src"}`, "Copied from REQ-12 (version 3)."},
		{"plain create", `{"project_id":"p1","type":"requirement","title":"New","body":"x"}`, ""},
		{"unknown source", `{"project_id":"p1","type":"requirement","title":"New","body":"x","copied_from":"nope"}`, ""},
		{"source the caller cannot read", `{"project_id":"p1","type":"requirement","title":"New","body":"x","copied_from":"other"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, notes := copyHandler()
			w := httptest.NewRecorder()
			h.CreateArtifact(w, createReq(tc.body))
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if tc.note == "" {
				if len(notes.entries) != 0 {
					t.Fatalf("notes = %+v, want none", notes.entries)
				}
				return
			}
			if len(notes.entries) != 1 {
				t.Fatalf("notes = %d, want one", len(notes.entries))
			}
			e := notes.entries[0]
			if e.Message != tc.note || e.EntryType != "copy" || !e.IsAutoEntry || e.AuthorName != "Ada" {
				t.Errorf("note = %+v", e)
			}
			created := h.artifactService.(*copyArtifactFake).created
			if len(created) != 1 || e.ArtifactID != created[0].ID {
				t.Errorf("note is on %q, want the new artifact %v", e.ArtifactID, created)
			}
		})
	}
}
