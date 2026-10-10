package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// duplicatesStoreWithoutSearch is an embeddings.Store that is not an
// embeddings.Searcher: the shape of a store that predates the semantic read
// path, for which DuplicatePairs answers ErrVectorUnavailable.
type duplicatesStoreWithoutSearch struct{}

func (duplicatesStoreWithoutSearch) Upsert(*embeddings.Embedding) error { return nil }
func (duplicatesStoreWithoutSearch) GetByArtifact(string) (*embeddings.Embedding, error) {
	return nil, nil
}

// duplicatesStore is an embeddings.Searcher whose duplicate scan answers the
// pairs or the error it is given.
type duplicatesStore struct {
	fakeEmbedStore
	pairs []embeddings.DuplicatePair
	err   error
}

func (s *duplicatesStore) DuplicateCandidates(string, float64, int) ([]embeddings.DuplicatePair, error) {
	return s.pairs, s.err
}

// TestDuplicateCandidatesAnswers pins the whole answer of
// GET /api/v1/projects/{id}/duplicates on each of the handler's paths: the
// status, every header and the body bytes (key order, the omitted note and
// the trailing newline). It pins the three encodes of DuplicateCandidates
// (embeddings unconfigured, the vector store unavailable, and the pairs)
// and the 500 of a failed scan, so that how the handler sets Content-Type
// and writes its JSON can change without changing a byte of an answer
// (invariants I4, I5). TestDuplicateCandidatesDisabled pins the same
// handler's disabled branch by what the body decodes to.
func TestDuplicateCandidatesAnswers(t *testing.T) {
	jsonHeader := http.Header{"Content-Type": {"application/json"}}
	enabled := fakeEmbedProvider{enabled: true}
	service := func(store embeddings.Store) func(*Handler) {
		svc := embeddings.NewService(enabled, store, nil)
		return func(h *Handler) { h.EmbeddingService = svc }
	}
	const unconfigured = `{"enabled":false,"note":"semantic-search embeddings are not configured; duplicate detection is unavailable","pairs":[]}` + "\n"

	cases := []struct {
		name       string
		opts       []func(*Handler)
		wantStatus int
		wantBody   string
	}{
		{"no service", nil, 200, unconfigured},
		{"a disabled provider",
			[]func(*Handler){func(h *Handler) {
				h.EmbeddingService = embeddings.NewService(fakeEmbedProvider{enabled: false}, &fakeEmbedStore{}, nil)
			}}, 200, unconfigured},
		{"a store that cannot search", []func(*Handler){service(duplicatesStoreWithoutSearch{})}, 200,
			`{"enabled":false,"note":"the vector store is unavailable on this database; duplicate detection is disabled","pairs":[]}` + "\n"},
		{"a scan that finds nothing", []func(*Handler){service(&duplicatesStore{})}, 200,
			`{"enabled":true,"pairs":[]}` + "\n"},
		{"a scan that finds a pair", []func(*Handler){service(&duplicatesStore{pairs: []embeddings.DuplicatePair{{
			ArtifactID: "a-1", ArtifactTitle: "Login <flow> & more", ArtifactType: "requirement",
			OtherID: "a-2", OtherTitle: "Sign-in flow", OtherType: "requirement", Distance: 0.125,
		}}})}, 200,
			`{"enabled":true,"pairs":[{"artifact_id":"a-1","artifact_title":"Login \u003cflow\u003e \u0026 more","artifact_type":"requirement","other_id":"a-2","other_title":"Sign-in flow","other_type":"requirement","similarity":0.875}]}` + "\n"},
		{"a scan that fails", []func(*Handler){service(&duplicatesStore{err: errors.New(`pq: relation "secret_table" does not exist`)})}, 500,
			`{"error":"duplicate detection failed"}` + "\n"},
		{"a scan the store reports disabled", []func(*Handler){service(&duplicatesStore{err: embeddings.ErrDisabled})}, 200,
			`{"enabled":false,"note":"the vector store is unavailable on this database; duplicate detection is disabled","pairs":[]}` + "\n"},
		{"a scan the store reports unavailable", []func(*Handler){service(&duplicatesStore{err: embeddings.ErrVectorUnavailable})}, 200,
			`{"enabled":false,"note":"the vector store is unavailable on this database; duplicate detection is disabled","pairs":[]}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t, tc.opts...)
			w := httptest.NewRecorder()
			h.DuplicateCandidates(w, requestWithProjectVar("proj-1"))
			res := w.Result()
			if res.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", res.StatusCode, tc.wantStatus)
			}
			if !reflect.DeepEqual(res.Header, jsonHeader) {
				t.Errorf("header = %v, want %v", res.Header, jsonHeader)
			}
			if got := w.Body.String(); got != tc.wantBody {
				t.Errorf("body = %q\n   want %q", got, tc.wantBody)
			}
		})
	}
}
