package postgres

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/links"
)

// X13's literal equality for the link repository: every statement it builds
// from linkColumns, liveLinkColumns, prefixedLinkColumns and
// prefixedLiveLinkColumns is the literal it replaced, byte for byte (spacing,
// line breaks, tabs and the trailing spaces of the per-version reads
// included). The statements are read off the wire by the recorder in
// pgkit_test.go, so the test needs no server and checks what each method
// sends, not a copy of it. The repository has no transaction: each method
// sends its one statement and nothing else.

// The statements X13 assembles, as the base (51f079d) wrote them, byte for
// byte, and FindSuspectByProject's, which X13 leaves as it was.
const (
	// oldLinkSaveInsert, Save
	oldLinkSaveInsert = `
		INSERT INTO links (id, from_id, to_id, type, suspect, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	// oldLinkFindByIDSelect, FindByID
	oldLinkFindByIDSelect = `
		SELECT id, from_id, to_id, type, suspect, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM links
		WHERE id = $1 AND valid_to IS NULL
	`
	// oldLinkFindByFromIDSelect, FindByFromID
	oldLinkFindByFromIDSelect = `
		SELECT id, from_id, to_id, type, suspect, attributes, version, valid_from, created_at, updated_at
		FROM links
		WHERE from_id = $1 AND valid_to IS NULL
		ORDER BY created_at DESC
	`
	// oldLinkFindByToIDSelect, FindByToID
	oldLinkFindByToIDSelect = `
		SELECT id, from_id, to_id, type, suspect, attributes, version, valid_from, created_at, updated_at
		FROM links
		WHERE to_id = $1 AND valid_to IS NULL
		ORDER BY created_at DESC
	`
	// oldLinkFindAllSelect, FindAll
	oldLinkFindAllSelect = `
		SELECT l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.valid_from, l.created_at, l.updated_at
		FROM links l
		INNER JOIN artifacts fa ON l.from_id = fa.id AND fa.valid_to IS NULL
		INNER JOIN artifacts ta ON l.to_id = ta.id AND ta.valid_to IS NULL
		WHERE (fa.project_id = $1 OR ta.project_id = $1) AND l.valid_to IS NULL
		ORDER BY l.created_at DESC
	`
	// oldLinkFindByFromIDForVersionSelect, FindByFromIDForVersion
	oldLinkFindByFromIDForVersionSelect = `
		SELECT DISTINCT l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.valid_from, l.valid_to, l.created_at, l.updated_at
		FROM links l
		WHERE l.from_id = $1
		AND EXISTS (
			SELECT 1 FROM link_artifacts 
			WHERE link_id = l.id 
			AND artifact_id = $1 
			AND artifact_version = $2
		)
		ORDER BY l.created_at DESC
	`
	// oldLinkFindByToIDForVersionSelect, FindByToIDForVersion
	oldLinkFindByToIDForVersionSelect = `
		SELECT DISTINCT l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.valid_from, l.valid_to, l.created_at, l.updated_at
		FROM links l
		WHERE l.to_id = $1
		AND EXISTS (
			SELECT 1 FROM link_artifacts 
			WHERE link_id = l.id 
			AND artifact_id = $1 
			AND artifact_version = $2
		)
		ORDER BY l.created_at DESC
	`
	// oldLinkFindSuspectByProjectSelect, FindSuspectByProject
	oldLinkFindSuspectByProjectSelect = `
		SELECT l.id, l.type, l.updated_at,
		       fa.id, fa.title, fa.type,
		       ta.id, ta.title, ta.type
		FROM links l
		INNER JOIN artifacts fa ON fa.id = l.from_id AND fa.valid_to IS NULL
		INNER JOIN artifacts ta ON ta.id = l.to_id AND ta.valid_to IS NULL
		WHERE l.valid_to IS NULL AND l.suspect
		  AND (fa.project_id = $1 OR ta.project_id = $1)
		ORDER BY l.updated_at DESC
	`
)

// The lists the base wrote out, on one line each.
const (
	oldLinkColumns     = `id, from_id, to_id, type, suspect, attributes, version, valid_from, valid_to, created_at, updated_at`
	oldLiveLinkColumns = `id, from_id, to_id, type, suspect, attributes, version, valid_from, created_at, updated_at`
	// oldPrefixedLinkColumns, the per-version reads' list, qualified by l
	oldPrefixedLinkColumns = `l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.valid_from, l.valid_to, l.created_at, l.updated_at`
	// oldPrefixedLiveLinkColumns, FindAll's list, qualified by l
	oldPrefixedLiveLinkColumns = `l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.valid_from, l.created_at, l.updated_at`
)

// The columns are the old lists, and each qualified list is the old one with
// l. before each column.
func TestLinkColumnsAreTheOldLists(t *testing.T) {
	if linkColumns != oldLinkColumns {
		t.Errorf("linkColumns =\n  %q\nwant\n  %q", linkColumns, oldLinkColumns)
	}
	if liveLinkColumns != oldLiveLinkColumns {
		t.Errorf("liveLinkColumns =\n  %q\nwant\n  %q", liveLinkColumns, oldLiveLinkColumns)
	}
	if got := prefixedLinkColumns("l"); got != oldPrefixedLinkColumns {
		t.Errorf("prefixedLinkColumns(\"l\") =\n  %q\nwant\n  %q", got, oldPrefixedLinkColumns)
	}
	if got := prefixedLiveLinkColumns("l"); got != oldPrefixedLiveLinkColumns {
		t.Errorf("prefixedLiveLinkColumns(\"l\") =\n  %q\nwant\n  %q", got, oldPrefixedLiveLinkColumns)
	}
	if want := "l." + strings.ReplaceAll(oldLinkColumns, ", ", ", l."); oldPrefixedLinkColumns != want {
		t.Errorf("the old qualified list is not the bare one with l. before each column: %q", want)
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: FindByID the error "link not found", each list nil and the suspect
// queue an empty list.
func TestLinkReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewLinkRepository(db)
	id := uuid.New().String()

	if got, err := repo.FindByID(id); got != nil || err == nil || err.Error() != "link not found" {
		t.Errorf("FindByID with no row: %v, %v; want nil and link not found", got, err)
	}
	wantEvents(t, "FindByID", rec.take(), "QUERY "+oldLinkFindByIDSelect)

	for _, r := range []struct {
		name  string
		read  func() ([]*links.Link, error)
		query string
	}{
		{"FindByFromID", func() ([]*links.Link, error) { return repo.FindByFromID(id) }, oldLinkFindByFromIDSelect},
		{"FindByToID", func() ([]*links.Link, error) { return repo.FindByToID(id) }, oldLinkFindByToIDSelect},
		{"FindAll", func() ([]*links.Link, error) { return repo.FindAll(id) }, oldLinkFindAllSelect},
		{"FindByFromIDForVersion", func() ([]*links.Link, error) { return repo.FindByFromIDForVersion(id, 1) }, oldLinkFindByFromIDForVersionSelect},
		{"FindByToIDForVersion", func() ([]*links.Link, error) { return repo.FindByToIDForVersion(id, 1) }, oldLinkFindByToIDForVersionSelect},
	} {
		if list, err := r.read(); list != nil || err != nil {
			t.Errorf("%s with no rows: %#v, %v; want nil and no error", r.name, list, err)
		}
		wantEvents(t, r.name, rec.take(), "QUERY "+r.query)
	}

	if list, err := repo.FindSuspectByProject(id); list == nil || len(list) != 0 || err != nil {
		t.Errorf("FindSuspectByProject with no rows: %#v, %v; want an empty list", list, err)
	}
	wantEvents(t, "FindSuspectByProject", rec.take(), "QUERY "+oldLinkFindSuspectByProjectSelect)
}

// Save sends its old literal, as one statement with no transaction around it.
func TestLinkSaveSendsItsOldLiteral(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewLinkRepository(db)
	l := links.NewLink(links.CreateLinkRequest{FromID: uuid.New().String(), ToID: uuid.New().String(), Type: "verifies"})
	if err := repo.Save(l); err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantEvents(t, "Save", rec.take(), "EXEC "+oldLinkSaveInsert)
}
