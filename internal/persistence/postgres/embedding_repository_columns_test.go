package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// The embedding repository's columns and scans (refactor plan X13's
// characterization for embedding_repository.go, in the manner of X13a's),
// pinned as found: the four columns GetByArtifact reads back, the six a
// nearest-neighbour hit holds and the seven a duplicate pair holds, each
// through the read that scans it, the columns that may be NULL, the scan
// errors that name a column's place, what a read answers for a row no one has,
// nil or [] for a list that matched nothing, the best-effort answer where the
// artifact_embeddings table is missing, and what Upsert puts in the row. The
// existing tests (embedding_repository_test.go and
// TestAListByAMalformedParentIDListsAsOneNoRowHas) pin the order, the scope
// and the threshold of the two searches, one stored embedding's metadata and
// the malformed-id lists; none reads a hit's project, title or body back, a
// pair's titles, the columns of the artifact a hit joins, the places of a scan,
// or the answers where there is nothing to read. X13 may give GetByArtifact's
// columns a shared list and the two searches a scan; these tests are what
// holds the repository still while it does. The repository has no transaction
// (every method sends one statement), so there is none to pin.
//
// The tests that read or write rows need the pgvector extension, which the
// "Backend Postgres tests (pgvector)" CI job has: where migration 0016 left no
// artifact_embeddings table (a plain postgres:16) they skip, as
// embedding_repository_test.go's do. The rest run on any database, and
// the ones that send no statement or fail on a closed handle need no server.

// rtEMWorld is a fresh database with the production schema and the
// repositories over it.
type rtEMWorld struct {
	t  *testing.T
	db *sql.DB
	ar *ArtifactRepository
	er *EmbeddingRepository
}

// newRTEMWorld is a fresh database. Where withTable is set, the test skips if
// the database has no artifact_embeddings table (no vector extension).
func newRTEMWorld(t *testing.T, withTable bool) *rtEMWorld {
	t.Helper()
	db := rtDB(t)
	if withTable && !tableExists(t, db, "artifact_embeddings") {
		t.Skip("artifact_embeddings table absent (no vector extension); use pgvector/pgvector:pg16")
	}
	return &rtEMWorld{t: t, db: db, ar: NewArtifactRepository(db), er: NewEmbeddingRepository(db)}
}

// project seeds a project and answers its id.
func (w *rtEMWorld) project() string {
	w.t.Helper()
	id := uuid.New().String()
	seedProjects(w.t, w.db, id)
	return id
}

// artifact saves a current artifact of the project, with an embedding of the
// vector (version 1, a model and a hash made of the title), and answers its id.
func (w *rtEMWorld) artifact(project, typ, title, body string, vec []float32) string {
	w.t.Helper()
	id := w.bare(project, typ, title, body)
	w.embed(id, 1, vec, "model-"+title, "hash-"+title)
	return id
}

// bare saves a current artifact of the project, with no embedding.
func (w *rtEMWorld) bare(project, typ, title, body string) string {
	w.t.Helper()
	a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: typ, Title: title, Body: body})
	if err := w.ar.Save(a); err != nil {
		w.t.Fatalf("save %q: %v", title, err)
	}
	return a.ID
}

// embed stores an embedding.
func (w *rtEMWorld) embed(id string, version int, vec []float32, model, hash string) {
	w.t.Helper()
	if err := w.er.Upsert(&embeddings.Embedding{ArtifactID: id, ArtifactVersion: version, Vector: vec, Model: model, ContentHash: hash}); err != nil {
		w.t.Fatalf("Upsert %s: %v", id, err)
	}
}

// rtEMDistance is the cosine distance of two vectors, 1 minus the cosine of
// the angle between them.
func rtEMDistance(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return 1 - dot/(math.Sqrt(na)*math.Sqrt(nb))
}

// rtEMNear fails unless got is the distance Postgres computes in single
// precision for want.
func rtEMNear(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-5 {
		t.Errorf("%s = %v, want %v (to 1e-5)", what, got, want)
	}
}

// rtEMWantHits fails unless hits are want's, in order, each equal in every
// column and its distance the cosine distance of the vector it holds from the
// query.
func rtEMWantHits(t *testing.T, what string, got []embeddings.NearestHit, err error, query []float32, want []embeddings.NearestHit, vecs [][]float32) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	if len(got) != len(want) {
		t.Errorf("%s: %d hits %s, want %d %s", what, len(got), rtJSON(got), len(want), rtJSON(want))
		return
	}
	for i := range got {
		g, w := got[i], want[i]
		rtEMNear(t, fmt.Sprintf("%s hit %d distance", what, i), g.Distance, rtEMDistance(query, vecs[i]))
		g.Distance, w.Distance = 0, 0
		rtWantSame(t, fmt.Sprintf("%s hit %d", what, i), g, w)
	}
}

// Every column a nearest-neighbour hit holds reads back through
// NearestByEmbedding, whole: the artifact, its project, its type, its title,
// its body (empty included) and the cosine distance from the query, nearest
// first and only the projects asked for. Each holds a value no other does,
// and the hits come from two projects, so a swap of two columns in a scan, or
// of a hit's project with another's, shows.
func TestNearestByEmbeddingReadsBackEveryColumn(t *testing.T) {
	w := newRTEMWorld(t, true)
	projA, projB := w.project(), w.project()
	vOne, vTwo, vThree := vecFromWeights(map[int]float32{0: 1, 1: 0.1}), vecFromWeights(map[int]float32{0: 1, 2: 1}), vecFromWeights(map[int]float32{5: 1})
	one := w.artifact(projA, "requirement", "Spindle power", "The spindle shall deliver 4 kW.", vOne)
	two := w.artifact(projB, "persona", "Machinist", "Runs the mill all day.", vTwo)
	three := w.artifact(projA, "test-case", "Spindle test", "", vThree)
	query := vecFromWeights(map[int]float32{0: 1})

	hitOne := embeddings.NearestHit{ArtifactID: one, ProjectID: projA, Type: "requirement", Title: "Spindle power", Body: "The spindle shall deliver 4 kW."}
	hitTwo := embeddings.NearestHit{ArtifactID: two, ProjectID: projB, Type: "persona", Title: "Machinist", Body: "Runs the mill all day."}
	hitThree := embeddings.NearestHit{ArtifactID: three, ProjectID: projA, Type: "test-case", Title: "Spindle test", Body: ""}

	got, err := w.er.NearestByEmbedding([]string{projA, projB}, query, 20)
	rtEMWantHits(t, "NearestByEmbedding of both projects", got, err, query,
		[]embeddings.NearestHit{hitOne, hitTwo, hitThree}, [][]float32{vOne, vTwo, vThree})
	got, err = w.er.NearestByEmbedding([]string{projA}, query, 20)
	rtEMWantHits(t, "NearestByEmbedding of the first project", got, err, query,
		[]embeddings.NearestHit{hitOne, hitThree}, [][]float32{vOne, vThree})
	got, err = w.er.NearestByEmbedding([]string{projB}, query, 20)
	rtEMWantHits(t, "NearestByEmbedding of the second project", got, err, query,
		[]embeddings.NearestHit{hitTwo}, [][]float32{vTwo})

	// A malformed id in the scope is left out of it; the well-formed ones are read.
	got, err = w.er.NearestByEmbedding([]string{"not-a-uuid", projB}, query, 20)
	rtEMWantHits(t, "NearestByEmbedding with a malformed project id beside a good one", got, err, query,
		[]embeddings.NearestHit{hitTwo}, [][]float32{vTwo})
}

// A hit holds the artifact's current version, and only a current artifact is
// in the search: a superseded version's text is not read, an artifact whose
// every version is closed (deleted) is in no hit, and an embedding with no
// artifact row is in none.
func TestNearestByEmbeddingReadsTheCurrentVersionOnly(t *testing.T) {
	w := newRTEMWorld(t, true)
	proj := w.project()
	vec := vecFromWeights(map[int]float32{0: 1})
	edited := w.artifact(proj, "requirement", "Old title", "Old body", vec)
	deleted := w.artifact(proj, "requirement", "Deleted", "Deleted body", vec)
	w.embed(uuid.New().String(), 1, vec, "model-orphan", "hash-orphan")
	rtSeed(t, w.db, `UPDATE artifacts SET valid_to = NOW() WHERE id = $1`, deleted)
	// The artifact is edited: version 1 is closed, version 2 is current.
	rtSeed(t, w.db, `UPDATE artifacts SET valid_to = NOW() WHERE id = $1`, edited)
	rtSeed(t, w.db, `INSERT INTO artifacts (id, project_id, type, title, body, version)
		SELECT id, project_id, type, 'New title', 'New body', 2 FROM artifacts WHERE id = $1 AND version = 1`, edited)

	got, err := w.er.NearestByEmbedding([]string{proj}, vec, 20)
	rtEMWantHits(t, "NearestByEmbedding over an edited, a deleted and an orphaned artifact", got, err, vec,
		[]embeddings.NearestHit{{ArtifactID: edited, ProjectID: proj, Type: "requirement", Title: "New title", Body: "New body"}},
		[][]float32{vec})
}

// A search answers at most limit hits, nearest first; a limit of zero or less
// is twenty, and a limit above what there is answers what there is.
func TestNearestByEmbeddingLimit(t *testing.T) {
	w := newRTEMWorld(t, true)
	proj := w.project()
	const stored = 22
	for i := 0; i < stored; i++ {
		w.artifact(proj, "requirement", fmt.Sprintf("Requirement %02d", i), "body", vecFromWeights(map[int]float32{0: 1, i + 1: 0.01 * float32(i+1)}))
	}
	query := vecFromWeights(map[int]float32{0: 1})

	for _, c := range []struct{ limit, want int }{{1, 1}, {5, 5}, {20, 20}, {0, 20}, {-1, 20}, {-100, 20}, {21, 21}, {stored, stored}, {1000, stored}} {
		hits, err := w.er.NearestByEmbedding([]string{proj}, query, c.limit)
		if err != nil || len(hits) != c.want {
			t.Errorf("NearestByEmbedding with limit %d: %d hits, %v; want %d", c.limit, len(hits), err, c.want)
			continue
		}
		for i := 1; i < len(hits); i++ {
			if hits[i].Distance < hits[i-1].Distance {
				t.Errorf("NearestByEmbedding with limit %d: distances not ascending at %d: %v then %v", c.limit, i, hits[i-1].Distance, hits[i].Distance)
			}
		}
	}
}

// rtEMPairs builds the three requirements the duplicate tests share, in a
// project: a and b are nearly parallel, c sits further out and is nearest to
// b, which makes c-to-b the one pair with a certain direction (b's nearest is
// a, and a's is b). It also seeds, in another project, a requirement
// identical to a, and a persona identical to it, which must pair with
// nothing.
type rtEMPairs struct {
	project             string
	a, b, c             string
	vA, vB, vC          []float32
	other, otherPersona string
	titleOf, typeOf     map[string]string
	dAB, dCB, dCA       float64
}

func newRTEMPairs(t *testing.T, w *rtEMWorld) *rtEMPairs {
	t.Helper()
	p := &rtEMPairs{project: w.project()}
	p.vA = vecFromWeights(map[int]float32{0: 1})
	p.vB = vecFromWeights(map[int]float32{0: 1, 1: 0.05})
	p.vC = vecFromWeights(map[int]float32{0: 1, 1: 0.5})
	p.dAB, p.dCB, p.dCA = rtEMDistance(p.vA, p.vB), rtEMDistance(p.vC, p.vB), rtEMDistance(p.vC, p.vA)
	if !(p.dAB < p.dCB && p.dCB < p.dCA && p.dCA < 0.15) {
		t.Fatalf("the vectors are placed wrongly: a-b %v, c-b %v, c-a %v", p.dAB, p.dCB, p.dCA)
	}
	p.a = w.artifact(p.project, "requirement", "The mill shall hold 0.01 mm", "a body", p.vA)
	p.b = w.artifact(p.project, "requirement", "The mill shall hold ten microns", "b body", p.vB)
	p.c = w.artifact(p.project, "requirement", "The mill shall hold a fifth of a millimetre", "c body", p.vC)
	elsewhere := w.project()
	p.other = w.artifact(elsewhere, "requirement", "Elsewhere", "e body", p.vA)
	p.otherPersona = w.artifact(p.project, "persona", "Machinist", "p body", p.vA)
	p.titleOf = map[string]string{p.a: "The mill shall hold 0.01 mm", p.b: "The mill shall hold ten microns",
		p.c: "The mill shall hold a fifth of a millimetre"}
	p.typeOf = map[string]string{p.a: "requirement", p.b: "requirement", p.c: "requirement"}
	return p
}

// wantPair fails unless pair holds the ids with each id's own title and type,
// whichever way round it is, and the distance.
func (p *rtEMPairs) wantPair(t *testing.T, what string, pair embeddings.DuplicatePair, ids [2]string, distance float64) {
	t.Helper()
	if !((pair.ArtifactID == ids[0] && pair.OtherID == ids[1]) || (pair.ArtifactID == ids[1] && pair.OtherID == ids[0])) {
		t.Errorf("%s holds %s and %s, want %s and %s", what, pair.ArtifactID, pair.OtherID, ids[0], ids[1])
		return
	}
	want := embeddings.DuplicatePair{ArtifactID: pair.ArtifactID, ArtifactTitle: p.titleOf[pair.ArtifactID], ArtifactType: p.typeOf[pair.ArtifactID],
		OtherID: pair.OtherID, OtherTitle: p.titleOf[pair.OtherID], OtherType: p.typeOf[pair.OtherID], Distance: pair.Distance}
	rtWantSame(t, what, pair, want)
	rtEMNear(t, what+" distance", pair.Distance, distance)
}

// Every column a duplicate pair holds reads back through DuplicateCandidates,
// whole: the artifact, its title and type, the other artifact, its title and
// type, and the cosine distance between them, closest pair first, scoped to
// the project (an identical requirement in another project, and an identical
// persona in this one, pair with nothing). a and b are each other's nearest
// and collapse to one pair, whichever direction is read first; c's nearest is
// b, which no other row repeats, so that pair is c then b. The distance
// ceiling is inclusive of a pair at it and excludes one past it.
func TestDuplicateCandidatesReadsBackEveryColumn(t *testing.T) {
	w := newRTEMWorld(t, true)
	p := newRTEMPairs(t, w)

	pairs, err := w.er.DuplicateCandidates(p.project, 0.1, embeddings.MaxDuplicatePairs)
	if err != nil || len(pairs) != 2 {
		t.Fatalf("DuplicateCandidates = %s, %v; want two pairs", rtJSON(pairs), err)
	}
	p.wantPair(t, "the first pair", pairs[0], [2]string{p.a, p.b}, p.dAB)
	rtWantSame(t, "the second pair", pairs[1], embeddings.DuplicatePair{
		ArtifactID: p.c, ArtifactTitle: p.titleOf[p.c], ArtifactType: "requirement",
		OtherID: p.b, OtherTitle: p.titleOf[p.b], OtherType: "requirement", Distance: pairs[1].Distance})
	rtEMNear(t, "the second pair's distance", pairs[1].Distance, p.dCB)

	// A ceiling between the two distances leaves the closer pair.
	pairs, err = w.er.DuplicateCandidates(p.project, (p.dAB+p.dCB)/2, embeddings.MaxDuplicatePairs)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("DuplicateCandidates under a lower ceiling = %s, %v; want one pair", rtJSON(pairs), err)
	}
	p.wantPair(t, "the pair under a lower ceiling", pairs[0], [2]string{p.a, p.b}, p.dAB)

	// A ceiling under every distance leaves none: an empty list, not nil.
	pairs, err = w.er.DuplicateCandidates(p.project, p.dAB/2, embeddings.MaxDuplicatePairs)
	rtWantEmpty(t, "DuplicateCandidates under a ceiling nothing clears", pairs, err)

	// The requirement identical to a, in another project, has no partner there.
	pairs, err = w.er.DuplicateCandidates(w.projectOf(t, p.other), 0.1, embeddings.MaxDuplicatePairs)
	rtWantEmpty(t, "DuplicateCandidates of a project holding one requirement", pairs, err)
}

// projectOf answers the project an artifact is in.
func (w *rtEMWorld) projectOf(t *testing.T, artifact string) string {
	t.Helper()
	var project string
	if err := w.db.QueryRow(`SELECT project_id::text FROM artifacts WHERE id = $1 AND valid_to IS NULL`, artifact).Scan(&project); err != nil {
		t.Fatal(err)
	}
	return project
}

// DuplicateCandidates answers at most limit pairs, closest first; a limit of
// zero or less, or above the cap, is the cap.
func TestDuplicateCandidatesLimit(t *testing.T) {
	w := newRTEMWorld(t, true)
	p := newRTEMPairs(t, w)

	pairs, err := w.er.DuplicateCandidates(p.project, 0.1, 1)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("DuplicateCandidates with limit 1 = %s, %v; want one pair", rtJSON(pairs), err)
	}
	p.wantPair(t, "the pair of limit 1", pairs[0], [2]string{p.a, p.b}, p.dAB)
	for _, limit := range []int{0, -1, embeddings.MaxDuplicatePairs, embeddings.MaxDuplicatePairs + 1, 100000} {
		pairs, err = w.er.DuplicateCandidates(p.project, 0.1, limit)
		if err != nil || len(pairs) != 2 {
			t.Errorf("DuplicateCandidates with limit %d = %s, %v; want both pairs", limit, rtJSON(pairs), err)
		}
	}
}

// A requirement's superseded version, a deleted requirement and an embedding
// with no artifact are not in the pairs: only the current text is read.
func TestDuplicateCandidatesReadsTheCurrentVersionOnly(t *testing.T) {
	w := newRTEMWorld(t, true)
	proj := w.project()
	vec := vecFromWeights(map[int]float32{0: 1})
	edited := w.artifact(proj, "requirement", "Old title", "body", vec)
	deleted := w.artifact(proj, "requirement", "Deleted", "body", vec)
	partner := w.artifact(proj, "requirement", "Partner", "body", vec)
	w.embed(uuid.New().String(), 1, vec, "model-orphan", "hash-orphan")
	rtSeed(t, w.db, `UPDATE artifacts SET valid_to = NOW() WHERE id = $1`, deleted)
	rtSeed(t, w.db, `UPDATE artifacts SET valid_to = NOW() WHERE id = $1`, edited)
	rtSeed(t, w.db, `INSERT INTO artifacts (id, project_id, type, title, body, version)
		SELECT id, project_id, type, 'New title', 'body', 2 FROM artifacts WHERE id = $1 AND version = 1`, edited)

	pairs, err := w.er.DuplicateCandidates(proj, 0.1, embeddings.MaxDuplicatePairs)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("DuplicateCandidates = %s, %v; want one pair", rtJSON(pairs), err)
	}
	p := pairs[0]
	titles := map[string]string{edited: "New title", partner: "Partner"}
	if titles[p.ArtifactID] == "" || titles[p.OtherID] == "" || p.ArtifactID == p.OtherID ||
		p.ArtifactTitle != titles[p.ArtifactID] || p.OtherTitle != titles[p.OtherID] {
		t.Errorf("the pair = %s, want the edited requirement's current title and the partner", rtJSON(p))
	}
}

// Where a search matched nothing it answers an empty list, never nil (JSON
// [], not null), for no embeddings, a project no row has, a malformed
// project id and a limit that is all empty; and a scope or query that is
// empty, or a scope of malformed ids alone, answers [] without sending a
// statement (the repository has no database to send one to).
func TestSearchesAnswerAnEmptyListWhereNothingMatched(t *testing.T) {
	w := newRTEMWorld(t, true)
	proj := w.project()
	query := vecFromWeights(map[int]float32{0: 1})

	got, err := w.er.NearestByEmbedding([]string{proj}, query, 10)
	rtWantEmpty(t, "NearestByEmbedding of a project with no embeddings", got, err)
	got, err = w.er.NearestByEmbedding([]string{uuid.New().String()}, query, 10)
	rtWantEmpty(t, "NearestByEmbedding of a project no row has", got, err)
	pairs, err := w.er.DuplicateCandidates(proj, 0.1, 10)
	rtWantEmpty(t, "DuplicateCandidates of a project with no embeddings", pairs, err)
	pairs, err = w.er.DuplicateCandidates(uuid.New().String(), 0.1, 10)
	rtWantEmpty(t, "DuplicateCandidates of a project no row has", pairs, err)
	pairs, err = w.er.DuplicateCandidates("not-a-uuid", 0.1, 10)
	rtWantEmpty(t, "DuplicateCandidates of a malformed project id", pairs, err)
}

// The searches that need no statement answer without one: the repository
// here has no database, so a statement would panic. An empty or nil scope, a
// scope of nothing but malformed ids, and an empty or nil query answer an
// empty list, not nil; an empty project id answers one too.
func TestSearchesThatSendNothingAnswerAnEmptyList(t *testing.T) {
	er := NewEmbeddingRepository(nil)
	query := vecFromWeights(map[int]float32{0: 1})
	id := uuid.New().String()

	for _, c := range []struct {
		name     string
		projects []string
		query    []float32
	}{
		{"a nil scope", nil, query},
		{"an empty scope", []string{}, query},
		{"a scope of malformed ids", []string{"not-a-uuid", "", "1234"}, query},
		{"a nil query", []string{id}, nil},
		{"an empty query", []string{id}, []float32{}},
	} {
		got, err := er.NearestByEmbedding(c.projects, c.query, 10)
		rtWantEmpty(t, "NearestByEmbedding of "+c.name, got, err)
	}
	pairs, err := er.DuplicateCandidates("", 0.1, 10)
	rtWantEmpty(t, "DuplicateCandidates of an empty project id", pairs, err)
	if err := er.Upsert(nil); err != nil {
		t.Errorf("Upsert of nil: %v, want nil and no statement", err)
	}
}

// GetByArtifact reads the four columns it scans: the artifact, its version,
// its model and its content hash, each holding a value no other does, in two
// rows so a swap shows. The vector is not read back (nil). An artifact with
// no embedding, and an id that is not a uuid, are no row: (nil, nil).
func TestGetByArtifactReadsBackEveryColumn(t *testing.T) {
	w := newRTEMWorld(t, true)
	one, two := uuid.New().String(), uuid.New().String()
	w.embed(one, 7, vecFromWeights(map[int]float32{0: 1}), "model-one", "hash-one")
	w.embed(two, 9, vecFromWeights(map[int]float32{1: 1}), "model-two", "hash-two")

	for id, want := range map[string]embeddings.Embedding{
		one: {ArtifactID: one, ArtifactVersion: 7, Model: "model-one", ContentHash: "hash-one"},
		two: {ArtifactID: two, ArtifactVersion: 9, Model: "model-two", ContentHash: "hash-two"},
	} {
		got, err := w.er.GetByArtifact(id)
		if err != nil || got == nil {
			t.Fatalf("GetByArtifact %s = %v, %v", id, got, err)
		}
		rtWantSame(t, "GetByArtifact "+id, *got, want)
		if got.Vector != nil {
			t.Errorf("GetByArtifact %s read a vector back: %v, want none", id, got.Vector)
		}
	}
	for _, id := range []string{uuid.New().String(), "not-a-uuid", ""} {
		got, err := w.er.GetByArtifact(id)
		if got != nil || err != nil {
			t.Errorf("GetByArtifact(%q) = %v, %v; want nil, nil", id, got, err)
		}
	}
}

// rtEMStoredVector reads the vector stored for an artifact as pgvector writes
// it out, parsed.
func rtEMStoredVector(t *testing.T, db *sql.DB, id string) []float32 {
	t.Helper()
	var text string
	if err := db.QueryRow(`SELECT embedding::text FROM artifact_embeddings WHERE artifact_id = $1`, id).Scan(&text); err != nil {
		t.Fatalf("read the vector of %s: %v", id, err)
	}
	var vec []float32
	for _, part := range strings.Split(strings.Trim(text, "[]"), ",") {
		f, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil {
			t.Fatalf("parse %q of %q: %v", part, text, err)
		}
		vec = append(vec, float32(f))
	}
	return vec
}

// rtEMRow is the raw columns of an embedding row, as stored.
type rtEMRow struct {
	Version     int
	Model, Hash string
	CreatedAt   time.Time
}

func rtEMReadRow(t *testing.T, db *sql.DB, id string) rtEMRow {
	t.Helper()
	var r rtEMRow
	if err := db.QueryRow(`SELECT artifact_version, model, content_hash, created_at FROM artifact_embeddings WHERE artifact_id = $1`, id).
		Scan(&r.Version, &r.Model, &r.Hash, &r.CreatedAt); err != nil {
		t.Fatalf("read the row of %s: %v", id, err)
	}
	return r
}

// Upsert writes the version, the vector, the model, the hash and a created_at
// of now, one row an artifact: a second Upsert replaces every one of them in
// place (created_at included, moved forward), leaving the other artifact's row
// as it was.
func TestUpsertWritesEveryColumnAndReplacesInPlace(t *testing.T) {
	w := newRTEMWorld(t, true)
	one, two := uuid.New().String(), uuid.New().String()
	vOne, vTwo := vecFromWeights(map[int]float32{0: 0.5, 3: -2.25, 1535: 0.1}), vecFromWeights(map[int]float32{7: 1})
	before := time.Now().Add(-time.Minute)
	w.embed(one, 7, vOne, "model-one", "hash-one")
	w.embed(two, 9, vTwo, "model-two", "hash-two")

	rowOne, rowTwo := rtEMReadRow(t, w.db, one), rtEMReadRow(t, w.db, two)
	if rowOne.Version != 7 || rowOne.Model != "model-one" || rowOne.Hash != "hash-one" ||
		rowTwo.Version != 9 || rowTwo.Model != "model-two" || rowTwo.Hash != "hash-two" {
		t.Errorf("the rows are %+v and %+v, want version, model and hash as sent", rowOne, rowTwo)
	}
	if rowOne.CreatedAt.Before(before) || rowOne.CreatedAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("created_at = %v, want about now", rowOne.CreatedAt)
	}
	for id, sent := range map[string][]float32{one: vOne, two: vTwo} {
		stored := rtEMStoredVector(t, w.db, id)
		if len(stored) != len(sent) {
			t.Fatalf("the vector of %s has %d components, want %d", id, len(stored), len(sent))
		}
		for i := range sent {
			if math.Abs(float64(stored[i]-sent[i])) > 1e-6 {
				t.Errorf("the vector of %s component %d = %v, want %v", id, i, stored[i], sent[i])
			}
		}
	}

	vNew := vecFromWeights(map[int]float32{2: 4})
	w.embed(one, 8, vNew, "model-three", "hash-three")
	got, err := w.er.GetByArtifact(one)
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	rtWantSame(t, "GetByArtifact after a second Upsert", *got, embeddings.Embedding{ArtifactID: one, ArtifactVersion: 8, Model: "model-three", ContentHash: "hash-three"})
	if replaced := rtEMReadRow(t, w.db, one); !replaced.CreatedAt.After(rowOne.CreatedAt) {
		t.Errorf("created_at after a second Upsert = %v, want after %v", replaced.CreatedAt, rowOne.CreatedAt)
	}
	stored := rtEMStoredVector(t, w.db, one)
	if len(stored) != len(vNew) || stored[2] != 4 || stored[0] != 0 || stored[3] != 0 {
		t.Errorf("the vector after a second Upsert is not the new one: [%v ...]", stored[:4])
	}
	if untouched := rtEMReadRow(t, w.db, two); untouched.Version != rowTwo.Version || untouched.Model != rowTwo.Model ||
		untouched.Hash != rowTwo.Hash || !untouched.CreatedAt.Equal(rowTwo.CreatedAt) {
		t.Errorf("the other artifact's row changed: %+v, was %+v", untouched, rowTwo)
	}
	var count int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM artifact_embeddings`).Scan(&count); err != nil || count != 2 {
		t.Errorf("rows = %d, %v; want one an artifact, 2", count, err)
	}
}

// Upsert hands back what Postgres refuses, as it came and in no other
// error: an id that is not a uuid, a vector of the wrong width, and an empty
// one. A refused Upsert leaves the stored embedding as it was.
func TestUpsertHandsBackWhatPostgresRefuses(t *testing.T) {
	w := newRTEMWorld(t, true)
	id := uuid.New().String()
	w.embed(id, 1, vecFromWeights(map[int]float32{0: 1}), "model", "hash")

	err := w.er.Upsert(&embeddings.Embedding{ArtifactID: "not-a-uuid", ArtifactVersion: 1, Vector: vecFromWeights(map[int]float32{0: 1}), Model: "m", ContentHash: "h"})
	rtWantRefused(t, "Upsert of a malformed id", err)
	for name, vec := range map[string][]float32{"a short vector": {1, 2, 3}, "an empty vector": {}, "no vector": nil} {
		err := w.er.Upsert(&embeddings.Embedding{ArtifactID: id, ArtifactVersion: 2, Vector: vec, Model: "other", ContentHash: "other"})
		if err == nil || errors.Is(err, embeddings.ErrVectorUnavailable) {
			t.Errorf("Upsert of %s: %v, want Postgres's refusal", name, err)
		}
	}
	got, err := w.er.GetByArtifact(id)
	if err != nil || got == nil || got.ArtifactVersion != 1 || got.Model != "model" || got.ContentHash != "hash" {
		t.Errorf("GetByArtifact after refused Upserts = %v, %v; want the embedding as it was", got, err)
	}
}

const (
	rtEMStr   = "converting NULL to string is unsupported"
	rtEMInt   = "converting NULL to int is unsupported"
	rtEMFloat = "converting NULL to float64 is unsupported"
)

func rtEMScanError(index int, column, message string) string {
	return fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", index, column, message)
}

// A column of the embedding the schema forbids NULL fails GetByArtifact,
// naming the column and its place among the four scanned (artifact_id 0,
// artifact_version 1, model 2, content_hash 3), unwrapped, with no embedding.
// (The artifact id is the primary key.)
func TestGetByArtifactScanErrorsNameTheColumn(t *testing.T) {
	w := newRTEMWorld(t, true)
	for _, column := range []string{"artifact_version", "model", "content_hash"} {
		rtSeed(t, w.db, `ALTER TABLE artifact_embeddings ALTER COLUMN `+column+` DROP NOT NULL`)
	}
	vec := encodeVector(vecFromWeights(map[int]float32{0: 1}))
	for _, c := range []struct {
		column  string
		index   int
		message string
	}{
		{"artifact_version", 1, rtEMInt},
		{"model", 2, rtEMStr},
		{"content_hash", 3, rtEMStr},
	} {
		id := uuid.New().String()
		values := map[string]string{"artifact_version": "3", "model": "'model'", "content_hash": "'hash'"}
		values[c.column] = "NULL"
		rtSeed(t, w.db, fmt.Sprintf(`INSERT INTO artifact_embeddings (artifact_id, artifact_version, embedding, model, content_hash)
			VALUES ($1, %s, $2::vector, %s, %s)`, values["artifact_version"], values["model"], values["content_hash"]), id, vec)

		got, err := w.er.GetByArtifact(id)
		if want := rtEMScanError(c.index, c.column, c.message); got != nil || err == nil || err.Error() != want {
			t.Errorf("GetByArtifact over a row with a NULL %s: %v, %v; want nil and %s", c.column, got, err, want)
		}
	}
}

// A column of the artifact a hit joins that is NULL fails NearestByEmbedding,
// naming the column and its place among the six scanned (artifact_id 0,
// project_id 1, type 2, title 3, body 4, distance 5), unwrapped, with no list
// (nil, not []): the good hit beside it is read before it and dropped with it.
// The body is the one the schema lets be NULL (a column of TEXT with no NOT
// NULL), and a NULL body fails the search all the same.
func TestNearestByEmbeddingScanErrorsNameTheColumn(t *testing.T) {
	w := newRTEMWorld(t, true)
	rtSeed(t, w.db, `ALTER TABLE artifacts ALTER COLUMN title DROP NOT NULL`)
	rtSeed(t, w.db, `ALTER TABLE artifacts ALTER COLUMN type DROP NOT NULL`)
	proj := w.project()
	good := w.artifact(proj, "requirement", "Good", "good body", vecFromWeights(map[int]float32{0: 1}))
	bad := w.artifact(proj, "requirement", "Bad", "bad body", vecFromWeights(map[int]float32{0: 1, 1: 1}))
	query := vecFromWeights(map[int]float32{0: 1})

	if hits, err := w.er.NearestByEmbedding([]string{proj}, query, 20); err != nil || len(hits) != 2 || hits[0].ArtifactID != good {
		t.Fatalf("NearestByEmbedding before a NULL = %s, %v; want both, the good first", rtJSON(hits), err)
	}
	for _, c := range []struct {
		column  string
		index   int
		message string
	}{
		{"type", 2, rtEMStr},
		{"title", 3, rtEMStr},
		{"body", 4, rtEMStr},
	} {
		var old sql.NullString
		if err := w.db.QueryRow(`SELECT `+c.column+` FROM artifacts WHERE id = $1`, bad).Scan(&old); err != nil {
			t.Fatal(err)
		}
		rtSeed(t, w.db, `UPDATE artifacts SET `+c.column+` = NULL WHERE id = $1`, bad)

		got, err := w.er.NearestByEmbedding([]string{proj}, query, 20)
		if want := rtEMScanError(c.index, c.column, c.message); got != nil || err == nil || err.Error() != want {
			t.Errorf("NearestByEmbedding over a NULL %s: %v, %v; want nil and %s", c.column, got, err, want)
		}
		rtSeed(t, w.db, `UPDATE artifacts SET `+c.column+` = $2 WHERE id = $1`, bad, old)
	}
}

// A NULL title of an artifact in a pair fails DuplicateCandidates, naming the
// column and its place among the seven scanned (id 0, title 1, type 2,
// artifact_id 3, title 4, type 5, distance 6), unwrapped, with no list (nil,
// not []): the pairs read before it are dropped with it. c is no one's
// nearest, so its NULL title is the artifact's own (place 1); a is in a pair
// with b, read as a-to-b or b-to-a, so its NULL title is the artifact's (1) or
// the other's (4), whichever row comes first. (A NULL type is no requirement,
// so it is in no pair; the schema forbids a NULL id.)
func TestDuplicateCandidatesScanErrorsNameTheColumn(t *testing.T) {
	w := newRTEMWorld(t, true)
	rtSeed(t, w.db, `ALTER TABLE artifacts ALTER COLUMN title DROP NOT NULL`)
	p := newRTEMPairs(t, w)

	rtSeed(t, w.db, `UPDATE artifacts SET title = NULL WHERE id = $1`, p.c)
	got, err := w.er.DuplicateCandidates(p.project, 0.1, embeddings.MaxDuplicatePairs)
	if want := rtEMScanError(1, "title", rtEMStr); got != nil || err == nil || err.Error() != want {
		t.Errorf("DuplicateCandidates over c's NULL title: %v, %v; want nil and %s", got, err, want)
	}

	rtSeed(t, w.db, `UPDATE artifacts SET title = $2 WHERE id = $1`, p.c, p.titleOf[p.c])
	rtSeed(t, w.db, `UPDATE artifacts SET title = NULL WHERE id = $1`, p.a)
	got, err = w.er.DuplicateCandidates(p.project, 0.1, embeddings.MaxDuplicatePairs)
	first, second := rtEMScanError(1, "title", rtEMStr), rtEMScanError(4, "title", rtEMStr)
	if got != nil || err == nil || (err.Error() != first && err.Error() != second) {
		t.Errorf("DuplicateCandidates over a's NULL title: %v, %v; want nil and %s or %s", got, err, first, second)
	}
}

// A missing artifact_embeddings table (a database without the vector
// extension, or one the table is dropped from) is not one answer for every
// method: the two searches map Postgres's "undefined table" to
// embeddings.ErrVectorUnavailable, with no list (nil, not []), for a
// well-formed or a malformed project id; Upsert and GetByArtifact hand
// Postgres's error back as it came, unmapped. It runs with or without the
// extension, dropping the table where migration 0016 made it.
func TestAMissingEmbeddingsTableIsBestEffort(t *testing.T) {
	w := newRTEMWorld(t, false)
	rtSeed(t, w.db, `DROP TABLE IF EXISTS artifact_embeddings`)
	query := vecFromWeights(map[int]float32{0: 1})
	proj := uuid.New().String()

	hits, err := w.er.NearestByEmbedding([]string{proj}, query, 10)
	if hits != nil || err != embeddings.ErrVectorUnavailable {
		t.Errorf("NearestByEmbedding without the table: %v, %v; want nil and ErrVectorUnavailable", hits, err)
	}
	for _, id := range []string{proj, "not-a-uuid"} {
		pairs, err := w.er.DuplicateCandidates(id, 0.1, 10)
		if pairs != nil || err != embeddings.ErrVectorUnavailable {
			t.Errorf("DuplicateCandidates(%q) without the table: %v, %v; want nil and ErrVectorUnavailable", id, pairs, err)
		}
	}

	err = w.er.Upsert(&embeddings.Embedding{ArtifactID: proj, ArtifactVersion: 1, Vector: query, Model: "m", ContentHash: "h"})
	if !isUndefinedTable(err) || errors.Is(err, embeddings.ErrVectorUnavailable) {
		t.Errorf("Upsert without the table: %v, want Postgres's undefined_table, unmapped", err)
	}
	got, err := w.er.GetByArtifact(proj)
	if got != nil || !isUndefinedTable(err) || errors.Is(err, embeddings.ErrVectorUnavailable) {
		t.Errorf("GetByArtifact without the table: %v, %v; want nil and Postgres's undefined_table, unmapped", got, err)
	}
}

// A database that fails a statement for any other reason is handed back as it
// came, unwrapped and unmapped, with no result and no list (nil, not []), by
// every method that sends one. It needs no server.
func TestEmbeddingRepositoryHandsBackADatabaseFailure(t *testing.T) {
	er := NewEmbeddingRepository(rtClosedDB(t))
	id := uuid.New().String()
	query := vecFromWeights(map[int]float32{0: 1})

	rtWantClosed(t, "Upsert", er.Upsert(&embeddings.Embedding{ArtifactID: id, ArtifactVersion: 1, Vector: query, Model: "m", ContentHash: "h"}), "")
	got, err := er.GetByArtifact(id)
	rtWantClosed(t, "GetByArtifact", err, "")
	if got != nil {
		t.Errorf("GetByArtifact on a failing database answered %v, want nil", got)
	}
	hits, err := er.NearestByEmbedding([]string{id}, query, 10)
	rtWantClosed(t, "NearestByEmbedding", err, "")
	if hits != nil {
		t.Errorf("NearestByEmbedding on a failing database answered %v, want nil", hits)
	}
	pairs, err := er.DuplicateCandidates(id, 0.1, 10)
	rtWantClosed(t, "DuplicateCandidates", err, "")
	if pairs != nil {
		t.Errorf("DuplicateCandidates on a failing database answered %v, want nil", pairs)
	}
}

// The text a vector is sent as, the key two ids collapse under and the test
// for a missing table, as they are: a literal of the shortest decimals that
// read back, in brackets with no spaces, "[]" for none; a key of the two ids
// in order whichever order they were given; and the undefined-table code only
// where it is Postgres's. They need no server.
func TestEmbeddingHelpersAsFound(t *testing.T) {
	for _, c := range []struct {
		vec  []float32
		want string
	}{
		{nil, "[]"},
		{[]float32{}, "[]"},
		{[]float32{0}, "[0]"},
		{[]float32{1, 0.5, -2.25}, "[1,0.5,-2.25]"},
		{[]float32{0.1, 0.2, 0.3}, "[0.1,0.2,0.3]"},
		{[]float32{1e-7, 16777216, 3.4028235e38}, "[1e-07,1.6777216e+07,3.4028235e+38]"},
	} {
		if got := encodeVector(c.vec); got != c.want {
			t.Errorf("encodeVector(%v) = %q, want %q", c.vec, got, c.want)
		}
	}
	if pairKey("b", "a") != "a|b" || pairKey("a", "b") != "a|b" || pairKey("a", "a") != "a|a" {
		t.Errorf("pairKey: %q %q %q, want a|b a|b a|a", pairKey("b", "a"), pairKey("a", "b"), pairKey("a", "a"))
	}
	if isUndefinedTable(nil) || isUndefinedTable(errors.New("42P01")) {
		t.Error("isUndefinedTable is true for an error that is not Postgres's")
	}
}
