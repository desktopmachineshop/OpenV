package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/commentreads"
)

// TestCommentReadRepositoryRoundTrip pins the comment read state: a time
// read back as written (UTC, to the microsecond), a second write replacing
// it (mark unread moves it back), only live artifacts of the member's
// workspace stored, one member's state invisible to another and to another
// workspace, and a deleted project taking its rows with it.
func TestCommentReadRepositoryRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	org, other := uuid.New().String(), uuid.New().String()
	for i, id := range []string{org, other} {
		if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Read Org', $2)`, id, "read-org-"+id[:8]+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	me, sam := uuid.New().String(), uuid.New().String()
	for _, u := range []string{me, sam} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Member')`, u, u+"@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	project, foreign := uuid.New().String(), uuid.New().String()
	seedProjects(t, db, project, foreign)
	if _, err := db.Exec(`UPDATE projects SET org_id = $1 WHERE id = $2`, org, project); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE projects SET org_id = $1 WHERE id = $2`, other, foreign); err != nil {
		t.Fatal(err)
	}
	art, theirs := uuid.New().String(), uuid.New().String()
	for _, a := range [][2]string{{art, project}, {theirs, foreign}} {
		if _, err := db.Exec(`INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R')`, a[0], a[1]); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewCommentReadRepository(db)

	if got, err := repo.List(me, org, []string{art}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nothing read: %v %v", got, err)
	}
	at := time.Date(2026, 11, 9, 10, 0, 0, 123456000, time.UTC)
	stored, err := repo.Set(me, org, []commentreads.Read{
		{ArtifactID: art, ReadAt: at},
		{ArtifactID: theirs, ReadAt: at},
		{ArtifactID: uuid.New().String(), ReadAt: at},
	})
	if err != nil || len(stored) != 1 || stored[0].ArtifactID != art {
		t.Fatalf("set: %v %v", stored, err)
	}
	got, err := repo.List(me, org, []string{art, theirs})
	if err != nil || len(got) != 1 || !got[0].ReadAt.Equal(at) || got[0].ReadAt.Location() != time.UTC {
		t.Fatalf("list: %+v %v", got, err)
	}
	back := at.Add(-time.Hour)
	if _, err := repo.Set(me, org, []commentreads.Read{{ArtifactID: art, ReadAt: back}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.List(me, org, []string{art}); len(got) != 1 || !got[0].ReadAt.Equal(back) {
		t.Fatalf("moved back: %+v", got)
	}
	if got, _ := repo.List(sam, org, []string{art}); len(got) != 0 {
		t.Fatalf("another member: %+v", got)
	}
	if got, _ := repo.List(me, other, []string{art}); len(got) != 0 {
		t.Fatalf("another workspace: %+v", got)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE id = $1`, project); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM comment_reads WHERE user_id = $1`, me).Scan(&n)
	if n != 0 {
		t.Fatalf("a deleted project's rows remain: %d", n)
	}
}
