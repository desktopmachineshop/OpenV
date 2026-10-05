package postgres

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// X9a (docs/plans/codebase-refactor.md §6.7) characterizes the link_artifacts
// rows the link service writes, before X9b replaces
// SetArtifactService(interface{}) and its reflective GetArtifact call
// (internal/domain/links/link.go:149-206) with a typed ArtifactVersions port.
// Each test drives the real link service over the Postgres repositories,
// wired to the real artifact service as cmd/server/wire_core.go:24-29 wires
// them, on a fresh database, and reads back every row of link_artifacts,
// written "link artifact version" with the names the test gave its links
// and artifacts. As found:
//   - CreateLink and UpdateLink each write a row for the from end and for
//     the to end at the version each artifact has at that moment; an
//     artifact's new version writes none, so the rows record the version a
//     link was made (or last edited) at;
//   - DeleteLink writes and removes none: the rows stay as history;
//   - an end whose artifact the service cannot read (no artifact service,
//     an id no artifact has, an artifact deleted) gets no row, and the
//     write still succeeds;
//   - the version reads answer from these rows, and the service's own,
//     GetLinksForArtifactVersion, answers the from end only.
// The unit twin, over in-memory repositories, is
// internal/domain/links/artifact_versions_test.go. The one case only the
// reflection reaches here, an artifact service with no GetArtifact method,
// is in link_artifacts_reflect_test.go, which a typed port retires.

// laFixture is a fresh database with the production schema, one project,
// and the artifact and link services over their Postgres repositories.
type laFixture struct {
	t         *testing.T
	db        *sql.DB
	project   string
	artifacts *artifacts.DefaultService
	// links is wired to artifacts as production wires it.
	links *links.DefaultService
	// names maps each id the test made to the name it gave it.
	names map[string]string
}

func newLAFixture(t *testing.T) *laFixture {
	t.Helper()
	db := rtDB(t)
	project := uuid.New().String()
	seedProjects(t, db, project)
	f := &laFixture{t: t, db: db, project: project, names: map[string]string{},
		artifacts: artifacts.NewDefaultService(NewArtifactRepository(db))}
	// cmd/server/wire_core.go:24-29.
	f.links = links.NewDefaultService(NewLinkRepository(db))
	f.links.SetArtifactService(f.artifacts)
	f.artifacts.SetLinkSuspector(f.links)
	return f
}

// unwired is a second link service over the same database that was never
// given an artifact service.
func (f *laFixture) unwired() *links.DefaultService {
	return links.NewDefaultService(NewLinkRepository(f.db))
}

// artifact creates an artifact named name and edits its title until it is at
// version.
func (f *laFixture) artifact(name, typ string, version int) *artifacts.Artifact {
	f.t.Helper()
	a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: f.project, Type: typ, Title: name})
	if err := f.artifacts.CreateArtifact(a); err != nil {
		f.t.Fatalf("create artifact %s: %v", name, err)
	}
	f.names[a.ID] = name
	return f.bump(a, version)
}

// bump edits the artifact's title until it is at version.
func (f *laFixture) bump(a *artifacts.Artifact, version int) *artifacts.Artifact {
	f.t.Helper()
	for a.Version < version {
		title := fmt.Sprintf("%s v%d", f.names[a.ID], a.Version+1)
		next, err := f.artifacts.UpdateArtifact(a.ID, artifacts.UpdateArtifactRequest{Title: &title})
		if err != nil {
			f.t.Fatalf("edit artifact %s: %v", f.names[a.ID], err)
		}
		a = next
	}
	if a.Version != version {
		f.t.Fatalf("artifact %s is at version %d, want %d", f.names[a.ID], a.Version, version)
	}
	return a
}

// newLink is a link named name from one id to another, not yet created.
func (f *laFixture) newLink(name, from, to string) *links.Link {
	l := links.NewLink(links.CreateLinkRequest{FromID: from, ToID: to, Type: "verifies"})
	f.names[l.ID] = name
	return l
}

// link creates a link named name through svc.
func (f *laFixture) link(svc *links.DefaultService, name, from, to string) *links.Link {
	f.t.Helper()
	l := f.newLink(name, from, to)
	if err := svc.CreateLink(l); err != nil {
		f.t.Fatalf("CreateLink %s: %v", name, err)
	}
	return l
}

func (f *laFixture) update(svc *links.DefaultService, l *links.Link) {
	f.t.Helper()
	if _, err := svc.UpdateLink(l.ID, links.UpdateLinkRequest{Type: "verifies"}); err != nil {
		f.t.Fatalf("UpdateLink %s: %v", f.names[l.ID], err)
	}
}

func (f *laFixture) name(id string) string {
	if n, ok := f.names[id]; ok {
		return n
	}
	return id
}

// rows is every row of link_artifacts, as "link artifact version", sorted.
// Every row must be active, the column's default, which nothing writes.
func (f *laFixture) rows() []string {
	f.t.Helper()
	rs, err := f.db.Query(`SELECT link_id, artifact_id, artifact_version, active FROM link_artifacts`)
	if err != nil {
		f.t.Fatalf("read link_artifacts: %v", err)
	}
	defer rs.Close()
	got := []string{}
	for rs.Next() {
		var link, artifact string
		var version int
		var active sql.NullBool
		if err := rs.Scan(&link, &artifact, &version, &active); err != nil {
			f.t.Fatalf("scan link_artifacts: %v", err)
		}
		row := fmt.Sprintf("%s %s %d", f.name(link), f.name(artifact), version)
		if !active.Valid || !active.Bool {
			f.t.Errorf("link_artifacts row %s: active = %v, want true", row, active)
		}
		got = append(got, row)
	}
	if err := rs.Err(); err != nil {
		f.t.Fatalf("read link_artifacts: %v", err)
	}
	slices.Sort(got)
	return got
}

// want compares every row of link_artifacts with want, in any order.
func (f *laFixture) want(step string, want ...string) {
	f.t.Helper()
	if want == nil {
		want = []string{}
	}
	want = slices.Clone(want)
	slices.Sort(want)
	if got := f.rows(); !slices.Equal(got, want) {
		f.t.Errorf("%s: link_artifacts rows %q, want %q", step, got, want)
	}
}

// listed names the links of a read, in its order; a nil list is "nil".
func (f *laFixture) listed(list []*links.Link, err error) string {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("read links: %v", err)
	}
	if list == nil {
		return "nil"
	}
	out := "["
	for i, l := range list {
		if i > 0 {
			out += " "
		}
		out += f.name(l.ID)
		if l.ValidTo != nil {
			out += "(closed)"
		}
	}
	return out + "]"
}

// Case 1: a link from an artifact at version 1 to one at version 3 writes
// one row for each end, at that version.
func TestLinkArtifactsRowsOnCreate(t *testing.T) {
	f := newLAFixture(t)
	from := f.artifact("from", artifacts.TypeTestCase, 1)
	to := f.artifact("to", artifacts.TypeRequirement, 3)
	f.want("before any link")
	f.link(f.links, "link", from.ID, to.ID)
	f.want("CreateLink", "link from 1", "link to 3")
}

// Case 2: the rows record the version current when the link is written. An
// artifact's new version writes none for the links it has; a link made
// afterwards records the new versions; UpdateLink records both ends'
// current versions beside the old rows, and again (a no-op, ON CONFLICT DO
// NOTHING) when nothing changed.
func TestLinkArtifactsRowsRecordTheVersionAtLinkTime(t *testing.T) {
	f := newLAFixture(t)
	from := f.artifact("from", artifacts.TypeTestCase, 1)
	to := f.artifact("to", artifacts.TypeRequirement, 1)
	first := f.link(f.links, "first", from.ID, to.ID)
	f.want("the first link", "first from 1", "first to 1")

	from = f.bump(from, 3)
	to = f.bump(to, 2)
	f.want("both ends edited", "first from 1", "first to 1")

	f.link(f.links, "second", from.ID, to.ID)
	f.want("a second link", "first from 1", "first to 1", "second from 3", "second to 2")

	f.update(f.links, first)
	f.want("the first link updated", "first from 1", "first to 1", "first from 3", "first to 2",
		"second from 3", "second to 2")

	f.update(f.links, first)
	f.want("the first link updated again", "first from 1", "first to 1", "first from 3", "first to 2",
		"second from 3", "second to 2")
}

// Case 3: DeleteLink closes the link and leaves its rows as they are; an
// update of the closed link fails and writes none.
func TestLinkArtifactsRowsOnDelete(t *testing.T) {
	f := newLAFixture(t)
	from := f.artifact("from", artifacts.TypeTestCase, 1)
	to := f.artifact("to", artifacts.TypeRequirement, 3)
	l := f.link(f.links, "link", from.ID, to.ID)
	if err := f.links.DeleteLink(l.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	f.want("DeleteLink", "link from 1", "link to 3")
	var closed bool
	if err := f.db.QueryRow(`SELECT valid_to IS NOT NULL FROM links WHERE id = $1`, l.ID).Scan(&closed); err != nil {
		t.Fatalf("read the link: %v", err)
	}
	if !closed {
		t.Error("DeleteLink left the link's valid_to NULL; want it closed")
	}

	f.bump(from, 2)
	if _, err := f.links.UpdateLink(l.ID, links.UpdateLinkRequest{Type: "verifies"}); err == nil || err.Error() != "link not found" {
		t.Errorf("UpdateLink of the deleted link = %v, want link not found", err)
	}
	f.want("UpdateLink of the deleted link", "link from 1", "link to 3")
}

// Case 4: an end whose version the service cannot read gets no row, and the
// write still succeeds. Each case links an artifact "from" at version 1 to
// one "to" at version 3, unless it says otherwise, then updates the link
// once, which writes nothing new.
func TestLinkArtifactsRowsSkipAnEndWhoseVersionIsUnread(t *testing.T) {
	for _, tc := range []struct {
		name string
		// make creates the link and answers it with the service that made it.
		make func(f *laFixture, from, to *artifacts.Artifact) (*links.DefaultService, *links.Link)
		want []string
	}{
		{"no artifact service", func(f *laFixture, from, to *artifacts.Artifact) (*links.DefaultService, *links.Link) {
			svc := f.unwired()
			return svc, f.link(svc, "link", from.ID, to.ID)
		}, nil},
		{"the artifact service set to nil", func(f *laFixture, from, to *artifacts.Artifact) (*links.DefaultService, *links.Link) {
			svc := f.unwired()
			svc.SetArtifactService(nil)
			return svc, f.link(svc, "link", from.ID, to.ID)
		}, nil},
		{"a to end no artifact has", func(f *laFixture, from, _ *artifacts.Artifact) (*links.DefaultService, *links.Link) {
			missing := uuid.New().String()
			f.names[missing] = "missing"
			return f.links, f.link(f.links, "link", from.ID, missing)
		}, []string{"link from 1"}},
		{"a from end no artifact has", func(f *laFixture, _, to *artifacts.Artifact) (*links.DefaultService, *links.Link) {
			missing := uuid.New().String()
			f.names[missing] = "missing"
			return f.links, f.link(f.links, "link", missing, to.ID)
		}, []string{"link to 3"}},
		{"neither end an artifact", func(f *laFixture, _, _ *artifacts.Artifact) (*links.DefaultService, *links.Link) {
			return f.links, f.link(f.links, "link", uuid.New().String(), uuid.New().String())
		}, nil},
		{"a deleted to end", func(f *laFixture, from, to *artifacts.Artifact) (*links.DefaultService, *links.Link) {
			if err := f.artifacts.DeleteArtifact(to.ID); err != nil {
				f.t.Fatalf("DeleteArtifact: %v", err)
			}
			return f.links, f.link(f.links, "link", from.ID, to.ID)
		}, []string{"link from 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLAFixture(t)
			from := f.artifact("from", artifacts.TypeTestCase, 1)
			to := f.artifact("to", artifacts.TypeRequirement, 3)
			svc, l := tc.make(f, from, to)
			var saved int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM links WHERE id = $1 AND valid_to IS NULL`, l.ID).Scan(&saved); err != nil {
				t.Fatalf("read the link: %v", err)
			}
			if saved != 1 {
				t.Errorf("live links rows for the link = %d, want 1", saved)
			}
			f.want("CreateLink", tc.want...)
			f.update(svc, l)
			f.want("CreateLink then UpdateLink", tc.want...)
		})
	}
}

// A link from an artifact to itself writes one row: both ends ask for the
// same one, and the second is a no-op (ON CONFLICT DO NOTHING).
func TestLinkArtifactsRowsOfASelfLink(t *testing.T) {
	f := newLAFixture(t)
	a := f.artifact("self", artifacts.TypeRequirement, 2)
	f.link(f.links, "link", a.ID, a.ID)
	f.want("CreateLink", "link self 2")
}

// A link the repository refuses to save (its id is taken) writes no row,
// though its ends have moved on since the link that holds the id was made.
func TestLinkArtifactsRowsNeedTheLinkSaved(t *testing.T) {
	f := newLAFixture(t)
	from := f.artifact("from", artifacts.TypeTestCase, 1)
	to := f.artifact("to", artifacts.TypeRequirement, 3)
	l := f.link(f.links, "link", from.ID, to.ID)
	f.bump(from, 2)
	again := f.newLink("again", from.ID, to.ID)
	again.ID = l.ID
	if err := f.links.CreateLink(again); err == nil {
		t.Fatal("CreateLink of a taken id succeeded; want the repository's error")
	}
	f.want("a refused CreateLink", "link from 1", "link to 3")
}

// Case 5: what the reads of link_artifacts answer for the rows above: the
// repository's two version reads, each in created_at order, newest first,
// with a deleted link among them, closed; nil where no row matches; and the
// service's GetLinksForArtifactVersion, which is the from read alone, so it
// answers nil for the to end at a version it has rows for.
func TestLinkArtifactsVersionReads(t *testing.T) {
	f := newLAFixture(t)
	repo := NewLinkRepository(f.db)
	from := f.artifact("from", artifacts.TypeTestCase, 1)
	to := f.artifact("to", artifacts.TypeRequirement, 1)
	first := f.link(f.links, "first", from.ID, to.ID)
	from = f.bump(from, 3)
	to = f.bump(to, 2)
	f.link(f.links, "second", from.ID, to.ID)
	f.update(f.links, first)
	gone := f.link(f.links, "gone", from.ID, to.ID)
	if err := f.links.DeleteLink(gone.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	f.want("the reads' rows", "first from 1", "first to 1", "first from 3", "first to 2",
		"second from 3", "second to 2", "gone from 3", "gone to 2")

	for _, tc := range []struct {
		read string
		got  string
		want string
	}{
		{"FindByFromIDForVersion(from, 1)", f.listed(repo.FindByFromIDForVersion(from.ID, 1)), "[first]"},
		{"FindByFromIDForVersion(from, 2)", f.listed(repo.FindByFromIDForVersion(from.ID, 2)), "nil"},
		{"FindByFromIDForVersion(from, 3)", f.listed(repo.FindByFromIDForVersion(from.ID, 3)), "[gone(closed) second first]"},
		{"FindByFromIDForVersion(to, 2)", f.listed(repo.FindByFromIDForVersion(to.ID, 2)), "nil"},
		{"FindByToIDForVersion(to, 1)", f.listed(repo.FindByToIDForVersion(to.ID, 1)), "[first]"},
		{"FindByToIDForVersion(to, 2)", f.listed(repo.FindByToIDForVersion(to.ID, 2)), "[gone(closed) second first]"},
		{"FindByToIDForVersion(to, 3)", f.listed(repo.FindByToIDForVersion(to.ID, 3)), "nil"},
		{"FindByToIDForVersion(from, 3)", f.listed(repo.FindByToIDForVersion(from.ID, 3)), "nil"},
		{"GetLinksForArtifactVersion(from, 1)", f.listed(f.links.GetLinksForArtifactVersion(from.ID, 1)), "[first]"},
		{"GetLinksForArtifactVersion(from, 3)", f.listed(f.links.GetLinksForArtifactVersion(from.ID, 3)), "[gone(closed) second first]"},
		{"GetLinksForArtifactVersion(to, 1)", f.listed(f.links.GetLinksForArtifactVersion(to.ID, 1)), "nil"},
		{"GetLinksForArtifactVersion(to, 2)", f.listed(f.links.GetLinksForArtifactVersion(to.ID, 2)), "nil"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %s, want %s", tc.read, tc.got, tc.want)
		}
	}
}
