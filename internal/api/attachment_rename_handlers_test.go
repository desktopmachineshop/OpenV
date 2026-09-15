package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Renaming a figure (REQ-157) through the handler, with the services faked.

type renameAttachmentFake struct {
	attachments.Service
	byID    map[string]*attachments.Attachment
	renamed []struct {
		id, title string
		by        *string
	}
}

func (f *renameAttachmentFake) GetAttachment(id string) (*attachments.Attachment, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("not found")
}

func (f *renameAttachmentFake) RenameFigure(id, title string, by *string) (int, error) {
	title = strings.TrimSpace(title)
	if len([]rune(title)) > attachments.MaxTitleLen {
		return 0, attachments.ErrTitleTooLong
	}
	a, ok := f.byID[id]
	if !ok {
		return 0, nil
	}
	f.renamed = append(f.renamed, struct {
		id, title string
		by        *string
	}{id, title, by})
	a.Title = title
	a.Version++
	return a.Version, nil
}

func renameHandler(plan string) (*Handler, *renameAttachmentFake, *fakeArtifactService, *fakeChatterService) {
	att := &renameAttachmentFake{byID: map[string]*attachments.Attachment{
		"f1": {ID: "f1", ArtifactID: "a1", Filename: "REQ-1-FIG-1.png", OriginalFilename: "Screenshot 2026-09-14 at 09.12.33.png", FigureRef: "REQ-1-FIG-1", FigureNum: 1, Version: 1},
	}}
	art := &fakeArtifactService{byID: map[string]*artifacts.Artifact{
		"a1": {ID: "a1", ProjectID: "p1", Ref: "REQ-1"},
	}}
	notes := &fakeChatterService{}
	h := &Handler{
		attachmentService: att,
		artifactService:   art,
		chatterService:    notes,
		projectService:    &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "o1"}}},
		orgService:        &fakeOrgService{plan: plan},
		memberService:     &fakeMemberService{roles: map[string]map[string]string{"p1": {"u1": "editor", "u2": "viewer"}}},
	}
	return h, att, art, notes
}

func renameRequest(id, userID, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/attachments/"+id, strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"id": id})
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID, Name: "Ada"}))
	}
	return r
}

func TestRenameAttachmentRecordsAVersionAndANote(t *testing.T) {
	h, att, art, notes := renameHandler(orgs.PlanFree)
	w := httptest.NewRecorder()
	h.RenameAttachment(w, renameRequest("f1", "u1", `{"title":"  Pump curve at 50 Hz  "}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got attachments.Attachment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Pump curve at 50 Hz" || got.Version != 2 {
		t.Errorf("answer = title %q v%d, want the trimmed title at version 2", got.Title, got.Version)
	}
	if len(att.renamed) != 1 || att.renamed[0].by == nil || *att.renamed[0].by != "u1" {
		t.Fatalf("rename calls = %+v, want one attributed to u1", att.renamed)
	}
	// The artifact takes an attribute-free version, as a new image gives it.
	if len(art.updateReqs) != 1 || art.updateReqs[0].Title != nil || art.updateReqs[0].Body != nil {
		t.Errorf("artifact updates = %+v, want one empty update", art.updateReqs)
	}
	if len(notes.entries) != 1 {
		t.Fatalf("notes = %d, want one", len(notes.entries))
	}
	msg := notes.entries[0].Message
	for _, probe := range []string{"REQ-1-FIG-1", "version 1 to 2", `"Screenshot 2026-09-14 at 09.12.33.png"`, `"Pump curve at 50 Hz"`} {
		if !strings.Contains(msg, probe) {
			t.Errorf("note %q lacks %q", msg, probe)
		}
	}
	if notes.entries[0].EntryType != "figure-change" || notes.entries[0].AuthorName != "Ada" {
		t.Errorf("note type/author = %q/%q", notes.entries[0].EntryType, notes.entries[0].AuthorName)
	}
}

func TestRenameAttachmentUnchangedTitleWritesNothing(t *testing.T) {
	h, att, art, notes := renameHandler(orgs.PlanFree)
	att.byID["f1"].Title = "Pump curve"
	w := httptest.NewRecorder()
	h.RenameAttachment(w, renameRequest("f1", "u1", `{"title":"Pump curve "}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if len(att.renamed) != 0 || len(art.updateReqs) != 0 || len(notes.entries) != 0 {
		t.Errorf("an unchanged title wrote: renames=%d artifact updates=%d notes=%d", len(att.renamed), len(art.updateReqs), len(notes.entries))
	}
}

func TestRenameAttachmentClearsToTheUploadedName(t *testing.T) {
	h, att, _, notes := renameHandler(orgs.PlanFree)
	att.byID["f1"].Title = "Pump curve"
	w := httptest.NewRecorder()
	h.RenameAttachment(w, renameRequest("f1", "u1", `{"title":""}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if att.byID["f1"].Title != "" {
		t.Errorf("title = %q, want cleared", att.byID["f1"].Title)
	}
	if len(notes.entries) != 1 || !strings.Contains(notes.entries[0].Message, `"Pump curve" is now "Screenshot 2026-09-14 at 09.12.33.png"`) {
		t.Errorf("note = %v", notes.entries)
	}
}

func TestRenameAttachmentRefusals(t *testing.T) {
	long := strings.Repeat("x", attachments.MaxTitleLen+1)
	cases := []struct {
		name   string
		plan   string
		id     string
		user   string
		body   string
		status int
	}{
		{"no user", orgs.PlanFree, "f1", "", `{"title":"x"}`, http.StatusUnauthorized},
		{"viewer", orgs.PlanFree, "f1", "u2", `{"title":"x"}`, http.StatusForbidden},
		{"unknown figure", orgs.PlanFree, "nope", "u1", `{"title":"x"}`, http.StatusNotFound},
		{"bad body", orgs.PlanFree, "f1", "u1", `{`, http.StatusBadRequest},
		{"too long", orgs.PlanFree, "f1", "u1", `{"title":"` + long + `"}`, http.StatusBadRequest},
		// A business workspace sits on the stable channel with no stable
		// release turned on, so every gate is closed.
		{"gated on stable", orgs.PlanBusiness, "f1", "u1", `{"title":"x"}`, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, att, _, _ := renameHandler(tc.plan)
			w := httptest.NewRecorder()
			h.RenameAttachment(w, renameRequest(tc.id, tc.user, tc.body))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if len(att.renamed) != 0 {
				t.Errorf("a refused rename still renamed: %+v", att.renamed)
			}
			if tc.name == "gated on stable" && !strings.Contains(w.Body.String(), "stable release") {
				t.Errorf("gate answer carries no remedy: %s", w.Body.String())
			}
		})
	}
}
