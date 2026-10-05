package links_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// X9a (docs/plans/codebase-refactor.md §6.7) characterizes the version
// records the link service writes through Repository.RecordLinkForArtifactVersion,
// the rows of link_artifacts, before X9b replaces SetArtifactService(interface{})
// and its reflective GetArtifact call (link.go:149-206) with a typed
// ArtifactVersions port. These tests reach the rule only through seams X9b
// keeps: the service's public methods, an in-memory links.Repository that
// records every call it is handed, and the real artifact service, wired as
// cmd/server/wire_core.go:24-26 wires it, over an in-memory
// artifacts.Repository. The rows on Postgres are pinned by
// internal/persistence/postgres/link_artifacts_test.go. As found:
//   - CreateLink and UpdateLink record (link, artifact, current version) for
//     the from end, then the to end, every time, even when nothing changed;
//     DeleteLink records nothing;
//   - each end is skipped on its own when the artifact service is not set or
//     its lookup of that end fails, and the write still succeeds;
//   - an error recording an end is dropped: the other end is still recorded
//     and the write succeeds;
//   - a lookup that answers no artifact and no error records version 0;
//   - GetLinksForArtifactVersion reads the from end's records only.
// The cases only the reflection can reach (a value with no GetArtifact
// method, or one of another shape) are in artifact_versions_reflect_test.go:
// a typed port makes them compile errors, so that file can go with the
// reflection while this one stays as it is.

// avLinkRepo is a links.Repository in memory. It keeps the links it saves
// and lists every version record it is asked for, as "link artifact
// version", in call order. Every method it does not declare panics.
type avLinkRepo struct {
	links.Repository

	saved     map[string]*links.Link
	deleted   []string
	saveErr   error
	updateErr error
	// recordErr, when set, answers each RecordLinkForArtifactVersion call
	// after it is listed.
	recordErr func(linkID, artifactID string) error
	recorded  []string
	// versionReads lists the version reads asked for, as "from|to id version".
	versionReads []string
	fromList     []*links.Link
	toList       []*links.Link
}

func newAVLinkRepo() *avLinkRepo {
	return &avLinkRepo{saved: map[string]*links.Link{}}
}

func (r *avLinkRepo) Save(l *links.Link) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	c := *l
	r.saved[l.ID] = &c
	return nil
}

func (r *avLinkRepo) FindByID(id string) (*links.Link, error) {
	l, ok := r.saved[id]
	if !ok {
		return nil, errors.New("link not found")
	}
	c := *l
	return &c, nil
}

func (r *avLinkRepo) Update(l *links.Link) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	c := *l
	r.saved[l.ID] = &c
	return nil
}

func (r *avLinkRepo) Delete(id string) error {
	r.deleted = append(r.deleted, id)
	delete(r.saved, id)
	return nil
}

func (r *avLinkRepo) RecordLinkForArtifactVersion(linkID, artifactID string, version int) error {
	r.recorded = append(r.recorded, fmt.Sprintf("%s %s %d", linkID, artifactID, version))
	if r.recordErr != nil {
		return r.recordErr(linkID, artifactID)
	}
	return nil
}

func (r *avLinkRepo) FindByFromIDForVersion(id string, version int) ([]*links.Link, error) {
	r.versionReads = append(r.versionReads, fmt.Sprintf("from %s %d", id, version))
	return r.fromList, nil
}

func (r *avLinkRepo) FindByToIDForVersion(id string, version int) ([]*links.Link, error) {
	r.versionReads = append(r.versionReads, fmt.Sprintf("to %s %d", id, version))
	return r.toList, nil
}

// avArtifactRepo is an artifacts.Repository in memory: FindByID answers the
// current version set with at, the error set with fail, (nil, nil) for an
// id set to nil in current, and artifacts.ErrNotFound for any other id.
// Every other method panics.
type avArtifactRepo struct {
	artifacts.Repository

	current map[string]*artifacts.Artifact
	errs    map[string]error
}

func newAVArtifactRepo() *avArtifactRepo {
	return &avArtifactRepo{current: map[string]*artifacts.Artifact{}, errs: map[string]error{}}
}

func (r *avArtifactRepo) at(id string, version int) *avArtifactRepo {
	r.current[id] = &artifacts.Artifact{ID: id, ProjectID: "project-1", Type: artifacts.TypeRequirement,
		Title: id, Status: artifacts.StatusDraft, Version: version}
	return r
}

func (r *avArtifactRepo) fail(id string, err error) *avArtifactRepo {
	r.errs[id] = err
	return r
}

func (r *avArtifactRepo) FindByID(id string) (*artifacts.Artifact, error) {
	if err, ok := r.errs[id]; ok {
		return nil, err
	}
	if a, ok := r.current[id]; ok {
		return a, nil
	}
	return nil, artifacts.ErrNotFound
}

// avWired is a link service over a fresh avLinkRepo, given the real artifact
// service over arts as production gives it its own (wire_core.go:24-26).
func avWired(arts *avArtifactRepo) (*links.DefaultService, *avLinkRepo) {
	repo := newAVLinkRepo()
	svc := links.NewDefaultService(repo)
	svc.SetArtifactService(artifacts.NewDefaultService(arts))
	return svc, repo
}

// avLink is a new link with a fixed id, so the records read literally.
func avLink(id, from, to string) *links.Link {
	l := links.NewLink(links.CreateLinkRequest{FromID: from, ToID: to, Type: "verifies"})
	l.ID = id
	return l
}

func avCreate(t *testing.T, svc *links.DefaultService, l *links.Link) {
	t.Helper()
	if err := svc.CreateLink(l); err != nil {
		t.Fatalf("CreateLink(%s): %v", l.ID, err)
	}
}

func avUpdate(t *testing.T, svc *links.DefaultService, id string) {
	t.Helper()
	if _, err := svc.UpdateLink(id, links.UpdateLinkRequest{Type: "verifies"}); err != nil {
		t.Fatalf("UpdateLink(%s): %v", id, err)
	}
}

func avWant(t *testing.T, step string, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got == nil {
		got = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: version records %q, want %q", step, got, want)
	}
}

// Case 1: a new link records each end at the version it has now, from end
// first.
func TestCreateLinkRecordsEachEndAtItsCurrentVersion(t *testing.T) {
	svc, repo := avWired(newAVArtifactRepo().at("tc-1", 1).at("req-1", 3))
	avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
	avWant(t, "CreateLink", repo.recorded, "link-1 tc-1 1", "link-1 req-1 3")
	if _, ok := repo.saved["link-1"]; !ok {
		t.Error("CreateLink did not save the link")
	}
}

// Case 2: the version recorded is the one current when the link is written.
// An artifact's new version records nothing for the links it already has; a
// link made afterwards records the new version, and so does an update of an
// existing link, which records both ends again each time, changed or not.
func TestLinkVersionRecordsFollowTheCurrentVersion(t *testing.T) {
	arts := newAVArtifactRepo().at("tc-1", 1).at("req-1", 1)
	svc, repo := avWired(arts)
	avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
	avWant(t, "the first link", repo.recorded, "link-1 tc-1 1", "link-1 req-1 1")

	arts.at("tc-1", 3).at("req-1", 2)
	avCreate(t, svc, avLink("link-2", "tc-1", "req-1"))
	avWant(t, "a second link after both ends moved on", repo.recorded,
		"link-1 tc-1 1", "link-1 req-1 1",
		"link-2 tc-1 3", "link-2 req-1 2")

	avUpdate(t, svc, "link-1")
	avWant(t, "the first link updated", repo.recorded,
		"link-1 tc-1 1", "link-1 req-1 1",
		"link-2 tc-1 3", "link-2 req-1 2",
		"link-1 tc-1 3", "link-1 req-1 2")

	avUpdate(t, svc, "link-1")
	avWant(t, "the first link updated again, nothing changed", repo.recorded,
		"link-1 tc-1 1", "link-1 req-1 1",
		"link-2 tc-1 3", "link-2 req-1 2",
		"link-1 tc-1 3", "link-1 req-1 2",
		"link-1 tc-1 3", "link-1 req-1 2")
}

// Case 3: deleting a link records nothing and asks the repository for
// nothing but the delete; an update of a link the repository no longer
// finds fails and records nothing.
func TestDeleteLinkRecordsNothing(t *testing.T) {
	svc, repo := avWired(newAVArtifactRepo().at("tc-1", 1).at("req-1", 3))
	avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
	if err := svc.DeleteLink("link-1"); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	avWant(t, "after DeleteLink", repo.recorded, "link-1 tc-1 1", "link-1 req-1 3")
	if !slices.Equal(repo.deleted, []string{"link-1"}) {
		t.Errorf("deleted %q, want [link-1]", repo.deleted)
	}
	if _, err := svc.UpdateLink("link-1", links.UpdateLinkRequest{Type: "verifies"}); err == nil {
		t.Error("UpdateLink of a link the repository no longer finds succeeded; want its error")
	}
	avWant(t, "after UpdateLink of the deleted link", repo.recorded, "link-1 tc-1 1", "link-1 req-1 3")
}

// Case 4: each end is skipped on its own when its version cannot be read,
// and the create and the update still succeed. Each case creates link-1
// from tc-1 to req-1, then updates it once, which records the same again.
func TestLinkVersionRecordsSkipAnEndWhoseVersionIsUnread(t *testing.T) {
	lost := errors.New("connection reset by peer")
	for _, tc := range []struct {
		name string
		// arts is nil for a link service never given an artifact service.
		arts *avArtifactRepo
		// setNil gives the link service a nil artifact service.
		setNil bool
		want   []string
	}{
		{name: "no artifact service", arts: nil},
		{name: "the artifact service set to nil", arts: newAVArtifactRepo().at("tc-1", 1).at("req-1", 3), setNil: true},
		{name: "the from end not found", arts: newAVArtifactRepo().at("req-1", 3),
			want: []string{"link-1 req-1 3"}},
		{name: "the to end not found", arts: newAVArtifactRepo().at("tc-1", 1),
			want: []string{"link-1 tc-1 1"}},
		{name: "neither end found", arts: newAVArtifactRepo()},
		{name: "the from end's lookup fails", arts: newAVArtifactRepo().at("tc-1", 1).at("req-1", 3).fail("tc-1", lost),
			want: []string{"link-1 req-1 3"}},
		{name: "the to end's lookup fails", arts: newAVArtifactRepo().at("tc-1", 1).at("req-1", 3).fail("req-1", lost),
			want: []string{"link-1 tc-1 1"}},
		// A quirk: the reflection marshals the nil *Artifact to JSON null,
		// which decodes to no version at all, so version 0 is recorded.
		{name: "the to end's lookup answers no artifact and no error",
			arts: func() *avArtifactRepo { r := newAVArtifactRepo().at("tc-1", 1); r.current["req-1"] = nil; return r }(),
			want: []string{"link-1 tc-1 1", "link-1 req-1 0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newAVLinkRepo()
			svc := links.NewDefaultService(repo)
			switch {
			case tc.setNil:
				svc.SetArtifactService(nil)
			case tc.arts != nil:
				svc.SetArtifactService(artifacts.NewDefaultService(tc.arts))
			}
			avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
			if _, ok := repo.saved["link-1"]; !ok {
				t.Error("CreateLink did not save the link")
			}
			avWant(t, "CreateLink", repo.recorded, tc.want...)
			avUpdate(t, svc, "link-1")
			avWant(t, "CreateLink then UpdateLink", repo.recorded, append(slices.Clone(tc.want), tc.want...)...)
		})
	}
}

// A failure to record an end is dropped: both ends are still asked for,
// and the create succeeds.
func TestLinkVersionRecordErrorsAreDropped(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failing map[string]bool
	}{
		{"the from end's record fails", map[string]bool{"tc-1": true}},
		{"both ends' records fail", map[string]bool{"tc-1": true, "req-1": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := avWired(newAVArtifactRepo().at("tc-1", 1).at("req-1", 3))
			repo.recordErr = func(_, artifactID string) error {
				if tc.failing[artifactID] {
					return errors.New("insert into link_artifacts: connection reset by peer")
				}
				return nil
			}
			avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
			avWant(t, "CreateLink", repo.recorded, "link-1 tc-1 1", "link-1 req-1 3")
			avUpdate(t, svc, "link-1")
			avWant(t, "CreateLink then UpdateLink", repo.recorded,
				"link-1 tc-1 1", "link-1 req-1 3", "link-1 tc-1 1", "link-1 req-1 3")
		})
	}
}

// A write the repository refuses records nothing, and its error is the
// service's answer.
func TestLinkVersionRecordsNeedTheWriteToSucceed(t *testing.T) {
	refused := errors.New("duplicate key value violates unique constraint \"links_pkey\"")

	svc, repo := avWired(newAVArtifactRepo().at("tc-1", 1).at("req-1", 3))
	repo.saveErr = refused
	if err := svc.CreateLink(avLink("link-1", "tc-1", "req-1")); !errors.Is(err, refused) {
		t.Errorf("CreateLink with Save refused = %v, want %v", err, refused)
	}
	avWant(t, "a refused CreateLink", repo.recorded)

	repo.saveErr = nil
	avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
	repo.updateErr = refused
	if _, err := svc.UpdateLink("link-1", links.UpdateLinkRequest{Type: "verifies"}); !errors.Is(err, refused) {
		t.Errorf("UpdateLink with Update refused = %v, want %v", err, refused)
	}
	avWant(t, "a refused UpdateLink", repo.recorded, "link-1 tc-1 1", "link-1 req-1 3")
}

// A link from an artifact to itself looks the artifact up for each end and
// asks for the same record twice (Postgres keeps one row: ON CONFLICT DO
// NOTHING).
func TestSelfLinkRecordsItsArtifactForEachEnd(t *testing.T) {
	svc, repo := avWired(newAVArtifactRepo().at("req-1", 2))
	avCreate(t, svc, avLink("link-1", "req-1", "req-1"))
	avWant(t, "CreateLink", repo.recorded, "link-1 req-1 2", "link-1 req-1 2")
}

// GetLinksForArtifactVersion answers the repository's read of the links
// FROM the artifact at that version, as it is; the to end's records are not
// read.
func TestGetLinksForArtifactVersionReadsTheFromEndOnly(t *testing.T) {
	svc, repo := avWired(newAVArtifactRepo())
	repo.fromList = []*links.Link{avLink("link-1", "tc-1", "req-1")}
	repo.toList = []*links.Link{avLink("link-2", "tc-2", "tc-1")}
	got, err := svc.GetLinksForArtifactVersion("tc-1", 3)
	if err != nil {
		t.Fatalf("GetLinksForArtifactVersion: %v", err)
	}
	if !slices.Equal(repo.versionReads, []string{"from tc-1 3"}) {
		t.Errorf("version reads %q, want [from tc-1 3]", repo.versionReads)
	}
	if len(got) != 1 || got[0] != repo.fromList[0] {
		t.Errorf("GetLinksForArtifactVersion = %v, want the from read's list as is", got)
	}
	avWant(t, "a version read", repo.recorded)
}
