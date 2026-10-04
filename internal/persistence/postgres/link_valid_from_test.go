package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// TestLinkFindAllReadsValidFrom: the project-wide link list, which every
// export, baseline and report is built from, reads each link's valid_from
// as stored, not the zero time (#379 bug 69).
func TestLinkFindAllReadsValidFrom(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	artifactRepo := NewArtifactRepository(db)
	linkRepo := NewLinkRepository(db)

	project := uuid.New().String()
	seedProjects(t, db, project)
	var ids []string
	for _, title := range []string{"Requirement", "Test case"} {
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: "requirement", Title: title})
		if err := artifactRepo.Save(a); err != nil {
			t.Fatalf("save artifact: %v", err)
		}
		ids = append(ids, a.ID)
	}

	validFrom := time.Date(2025, 6, 2, 8, 30, 0, 0, time.UTC)
	link := links.NewLink(links.CreateLinkRequest{FromID: ids[1], ToID: ids[0], Type: "verifies"})
	link.ValidFrom = validFrom
	if err := linkRepo.Save(link); err != nil {
		t.Fatalf("save link: %v", err)
	}

	all, err := linkRepo.FindAll(project)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("FindAll = %d links, want 1", len(all))
	}
	if !all[0].ValidFrom.Equal(validFrom) {
		t.Errorf("FindAll valid_from = %v, want %v as stored", all[0].ValidFrom, validFrom)
	}
	if all[0].ValidTo != nil {
		t.Errorf("FindAll valid_to = %v, want none for a live link", *all[0].ValidTo)
	}
}

// TestLinkReadsCarryTheirValidity: every link read of the repository reads
// each link's valid_from as stored, and a read that can return a closed
// link, the per-version history reads, its valid_to too (#379 bug 130).
// An artifact's link lists and the links_snapshot written from them read
// these, which is why every snapshot showed 0001-01-01.
func TestLinkReadsCarryTheirValidity(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewLinkRepository(db)

	from, to := uuid.New().String(), uuid.New().String()
	validFrom := time.Date(2025, 6, 2, 8, 30, 0, 0, time.UTC)
	save := func() *links.Link {
		t.Helper()
		l := links.NewLink(links.CreateLinkRequest{FromID: from, ToID: to, Type: "verifies"})
		l.ValidFrom = validFrom
		if err := repo.Save(l); err != nil {
			t.Fatalf("save link: %v", err)
		}
		for _, end := range []string{from, to} {
			if err := repo.RecordLinkForArtifactVersion(l.ID, end, 1); err != nil {
				t.Fatalf("record link for version 1: %v", err)
			}
		}
		return l
	}
	live := save()
	closed := save()
	if err := repo.Delete(closed.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var closedAt time.Time
	if err := db.QueryRow(`SELECT valid_to FROM links WHERE id = $1`, closed.ID).Scan(&closedAt); err != nil {
		t.Fatalf("read the closed link's valid_to: %v", err)
	}

	one := func(l *links.Link, err error) ([]*links.Link, error) { return []*links.Link{l}, err }
	for _, read := range []struct {
		name string
		list func() ([]*links.Link, error)
		// closed says whether the read returns the closed link too.
		closed bool
	}{
		{"FindByID", func() ([]*links.Link, error) { return one(repo.FindByID(live.ID)) }, false},
		{"FindByFromID", func() ([]*links.Link, error) { return repo.FindByFromID(from) }, false},
		{"FindByToID", func() ([]*links.Link, error) { return repo.FindByToID(to) }, false},
		{"FindByFromIDForVersion", func() ([]*links.Link, error) { return repo.FindByFromIDForVersion(from, 1) }, true},
		{"FindByToIDForVersion", func() ([]*links.Link, error) { return repo.FindByToIDForVersion(to, 1) }, true},
	} {
		t.Run(read.name, func(t *testing.T) {
			got, err := read.list()
			if err != nil {
				t.Fatalf("%s: %v", read.name, err)
			}
			want := 1
			if read.closed {
				want = 2
			}
			if len(got) != want {
				t.Fatalf("%s = %d links, want %d", read.name, len(got), want)
			}
			for _, l := range got {
				if !l.ValidFrom.Equal(validFrom) {
					t.Errorf("%s: link %s valid_from = %v, want %v as stored", read.name, l.ID, l.ValidFrom, validFrom)
				}
				switch {
				case l.ID == closed.ID && (l.ValidTo == nil || !l.ValidTo.Equal(closedAt)):
					t.Errorf("%s: the closed link's valid_to = %v, want %v", read.name, l.ValidTo, closedAt)
				case l.ID == live.ID && l.ValidTo != nil:
					t.Errorf("%s: the live link's valid_to = %v, want none", read.name, *l.ValidTo)
				}
			}
		})
	}
}
