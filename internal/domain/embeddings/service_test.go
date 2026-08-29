package embeddings

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// fakeProvider is a configurable in-memory Provider.
type fakeProvider struct {
	enabled  bool
	model    string
	vec      []float32
	calls    int
	lastArgs []string
	err      error
}

func (p *fakeProvider) Enabled() bool { return p.enabled }
func (p *fakeProvider) Model() string { return p.model }
func (p *fakeProvider) Embed(texts []string) ([][]float32, error) {
	p.calls++
	p.lastArgs = texts
	if p.err != nil {
		return nil, p.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = p.vec
	}
	return out, nil
}

// fakeStore is an in-memory Store recording upserts.
type fakeStore struct {
	mu        sync.Mutex
	byID      map[string]*Embedding
	upserts   int
	getErr    error
	upsertErr error
}

func newFakeStore() *fakeStore { return &fakeStore{byID: map[string]*Embedding{}} }

func (s *fakeStore) Upsert(e *Embedding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts++
	cp := *e
	s.byID[e.ArtifactID] = &cp
	return nil
}

func (s *fakeStore) GetByArtifact(id string) (*Embedding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.byID[id], nil
}

func TestContentHashDeterministicAndSensitive(t *testing.T) {
	h1 := ContentHash("Title", "Body")
	h2 := ContentHash("Title", "Body")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s vs %s", h1, h2)
	}
	if ContentHash("Title", "Body") == ContentHash("Title", "Body2") {
		t.Error("hash should change when body changes")
	}
	// The NUL separator must prevent a title|body boundary collision.
	if ContentHash("ab", "c") == ContentHash("a", "bc") {
		t.Error("hash should not collide across a different title/body split")
	}
}

func TestServiceDisabledWhenProviderOff(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: false, model: "m"}
	svc := NewService(prov, store, nil)

	if svc.Enabled() {
		t.Fatal("service should be disabled when the provider is disabled")
	}
	// EmbedArtifact must be a no-op: no provider call, no store write.
	if err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a1", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider was called %d times while disabled", prov.calls)
	}
	if store.upserts != 0 {
		t.Errorf("store was written %d times while disabled", store.upserts)
	}
}

func TestServiceDisabledWithoutStore(t *testing.T) {
	prov := &fakeProvider{enabled: true, model: "m", vec: make([]float32, Dimensions)}
	svc := NewService(prov, nil, nil)
	if svc.Enabled() {
		t.Fatal("service should be disabled without a store")
	}
	if err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a1", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider called without a store")
	}
}

func TestEmbedArtifactStoresWhenNew(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "text-embedding-3-small", vec: make([]float32, Dimensions)}
	svc := NewService(prov, store, nil)

	a := &artifacts.Artifact{ID: "a1", Version: 3, Title: "Login", Body: "authenticate"}
	if err := svc.EmbedArtifact(a); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", prov.calls)
	}
	if want := EmbeddableText("Login", "authenticate"); prov.lastArgs[0] != want {
		t.Errorf("embedded text = %q, want %q", prov.lastArgs[0], want)
	}
	got := store.byID["a1"]
	if got == nil {
		t.Fatal("nothing stored")
	}
	if got.ArtifactVersion != 3 || got.Model != "text-embedding-3-small" {
		t.Errorf("stored metadata wrong: %+v", got)
	}
	if got.ContentHash != ContentHash("Login", "authenticate") {
		t.Errorf("stored content hash mismatch")
	}
}

func TestEmbedArtifactSkipsWhenContentUnchanged(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "m", vec: make([]float32, Dimensions)}
	svc := NewService(prov, store, nil)

	// Pre-seed a matching embedding (same content hash + model).
	store.byID["a1"] = &Embedding{
		ArtifactID:  "a1",
		Model:       "m",
		ContentHash: ContentHash("T", "B"),
	}

	a := &artifacts.Artifact{ID: "a1", Title: "T", Body: "B"}
	if err := svc.EmbedArtifact(a); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider was called %d times for unchanged content; want 0 (skip)", prov.calls)
	}
	if store.upserts != 0 {
		t.Errorf("store was rewritten for unchanged content")
	}
}

func TestEmbedArtifactReembedsOnContentChange(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "m", vec: make([]float32, Dimensions)}
	svc := NewService(prov, store, nil)

	store.byID["a1"] = &Embedding{
		ArtifactID:  "a1",
		Model:       "m",
		ContentHash: ContentHash("T", "old body"),
	}

	a := &artifacts.Artifact{ID: "a1", Title: "T", Body: "new body"}
	if err := svc.EmbedArtifact(a); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 1 {
		t.Errorf("expected re-embed on content change, provider calls = %d", prov.calls)
	}
	if store.upserts != 1 {
		t.Errorf("expected 1 upsert on content change, got %d", store.upserts)
	}
}

func TestEmbedArtifactReembedsOnModelChange(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "new-model", vec: make([]float32, Dimensions)}
	svc := NewService(prov, store, nil)

	// Same content, but stored under a different model.
	store.byID["a1"] = &Embedding{
		ArtifactID:  "a1",
		Model:       "old-model",
		ContentHash: ContentHash("T", "B"),
	}

	if err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a1", Title: "T", Body: "B"}); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 1 {
		t.Errorf("expected re-embed on model change, provider calls = %d", prov.calls)
	}
}

func TestEmbedArtifactEmptyContentNoop(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "m", vec: make([]float32, Dimensions)}
	svc := NewService(prov, store, nil)

	if err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a1", Title: "", Body: "   "}); err != nil {
		t.Fatalf("EmbedArtifact: %v", err)
	}
	if prov.calls != 0 || store.upserts != 0 {
		t.Errorf("blank artifact should not be embedded (calls=%d upserts=%d)", prov.calls, store.upserts)
	}
}

// blockingProvider blocks inside Embed until release is closed, signalling each
// entry on entered. It lets the fan-out test hold worker slots open.
type blockingProvider struct {
	enabled bool
	model   string
	vec     []float32
	entered chan struct{}
	release chan struct{}
	total   int64
}

func (p *blockingProvider) Enabled() bool { return p.enabled }
func (p *blockingProvider) Model() string { return p.model }
func (p *blockingProvider) Embed(texts []string) ([][]float32, error) {
	atomic.AddInt64(&p.total, 1)
	p.entered <- struct{}{}
	<-p.release
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = p.vec
	}
	return out, nil
}

// TestIndexArtifactBoundsFanOut covers issue #244: a burst of IndexArtifact
// calls spawns at most indexWorkerCap concurrent embedding goroutines; the rest
// are dropped rather than blocking the write path or spawning unbounded
// goroutines.
func TestIndexArtifactBoundsFanOut(t *testing.T) {
	entered := make(chan struct{}, 64)
	release := make(chan struct{})
	prov := &blockingProvider{
		enabled: true,
		model:   "m",
		vec:     make([]float32, Dimensions),
		entered: entered,
		release: release,
	}
	svc := NewService(prov, newFakeStore(), nil)

	// Fire far more than the cap. The first indexWorkerCap acquire a slot and
	// block in the provider; the rest find the semaphore full and are dropped
	// synchronously (the write path is never blocked).
	const burst = indexWorkerCap * 4
	for i := 0; i < burst; i++ {
		svc.IndexArtifact(fmt.Sprintf("a%d", i), 1, "Title", "Body")
	}

	// Exactly indexWorkerCap workers reach the (blocked) provider.
	for i := 0; i < indexWorkerCap; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d workers started, expected %d", i, indexWorkerCap)
		}
	}
	// No further worker may start while the cap is saturated.
	select {
	case <-entered:
		t.Fatalf("more than %d concurrent workers ran; fan-out not bounded", indexWorkerCap)
	case <-time.After(150 * time.Millisecond):
	}

	close(release) // let the in-flight workers finish
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&prov.total) < int64(indexWorkerCap) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&prov.total); got != int64(indexWorkerCap) {
		t.Errorf("provider Embed calls = %d, want exactly %d (extras dropped)", got, indexWorkerCap)
	}
}

// TestEmbedDimensionMismatchDisables covers issue #247: a provider that returns
// a vector of the wrong width triggers a clear error once and disables the
// service, so subsequent writes are silent no-ops rather than repeated failures.
func TestEmbedDimensionMismatchDisables(t *testing.T) {
	store := newFakeStore()
	// Wrong width: one element too many.
	prov := &fakeProvider{enabled: true, model: "m", vec: make([]float32, Dimensions+1)}
	svc := NewService(prov, store, nil)

	err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a1", Title: "T", Body: "B"})
	if err == nil {
		t.Fatal("expected a dimension-mismatch error")
	}
	if !strings.Contains(err.Error(), "dimension") {
		t.Errorf("error should explain the dimension mismatch: %v", err)
	}
	if store.upserts != 0 {
		t.Error("nothing should be stored on a dimension mismatch")
	}
	// The service latches disabled.
	if svc.Enabled() {
		t.Error("service should report disabled after a dimension mismatch")
	}
	// A subsequent embed is a silent no-op: the provider is not called again.
	before := prov.calls
	if err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a2", Title: "T2", Body: "B2"}); err != nil {
		t.Errorf("post-disable embed should be a no-op, got %v", err)
	}
	if prov.calls != before {
		t.Errorf("provider was called again after disable (%d -> %d)", before, prov.calls)
	}
	// IndexArtifact is also a no-op once disabled.
	svc.IndexArtifact("a3", 1, "T3", "B3")
	if prov.calls != before {
		t.Errorf("IndexArtifact drove the provider after disable (%d -> %d)", before, prov.calls)
	}
}

func TestEmbedArtifactProviderErrorPropagates(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "m", err: errors.New("boom")}
	svc := NewService(prov, store, nil)

	err := svc.EmbedArtifact(&artifacts.Artifact{ID: "a1", Title: "T", Body: "B"})
	if err == nil {
		t.Fatal("expected provider error to propagate from EmbedArtifact")
	}
	if store.upserts != 0 {
		t.Error("nothing should be stored when the provider errors")
	}
}

// fakeReader satisfies ArtifactReader for ReindexProject.
type fakeReader struct {
	byProject map[string][]*artifacts.Artifact
	err       error
}

func (r *fakeReader) GetArtifactsByProject(projectID string) ([]*artifacts.Artifact, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.byProject[projectID], nil
}

func TestReindexProjectEmbedsAll(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: true, model: "m", vec: make([]float32, Dimensions)}
	reader := &fakeReader{byProject: map[string][]*artifacts.Artifact{
		"p1": {
			{ID: "a1", Title: "One", Body: "b1"},
			{ID: "a2", Title: "Two", Body: "b2"},
		},
	}}
	svc := NewService(prov, store, reader)

	n, err := svc.ReindexProject("p1")
	if err != nil {
		t.Fatalf("ReindexProject: %v", err)
	}
	if n != 2 {
		t.Errorf("examined %d artifacts, want 2", n)
	}
	if store.upserts != 2 {
		t.Errorf("upserts = %d, want 2", store.upserts)
	}
}

func TestReindexProjectNoopWhenDisabled(t *testing.T) {
	store := newFakeStore()
	prov := &fakeProvider{enabled: false}
	reader := &fakeReader{byProject: map[string][]*artifacts.Artifact{"p1": {{ID: "a1"}}}}
	svc := NewService(prov, store, reader)

	n, err := svc.ReindexProject("p1")
	if err != nil {
		t.Fatalf("ReindexProject: %v", err)
	}
	if n != 0 || store.upserts != 0 {
		t.Errorf("disabled reindex should be a no-op (n=%d upserts=%d)", n, store.upserts)
	}
}
