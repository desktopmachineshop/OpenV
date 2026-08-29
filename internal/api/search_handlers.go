package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
)

// searchResponse is the envelope GET /api/v1/search returns. Hits keep the same
// per-row shape as before (SearchHit, now optionally carrying a semantic
// score); ModeUsed tells the client which path actually ran — it differs from
// the requested mode when a semantic/hybrid request fell back to keyword
// because embeddings are unconfigured or the vector store is unavailable.
type searchResponse struct {
	ModeUsed string                 `json:"mode_used"`
	Hits     []*artifacts.SearchHit `json:"hits"`
}

const (
	modeKeyword  = "keyword"
	modeSemantic = "semantic"
	modeHybrid   = "hybrid"
)

// GlobalSearch handles GET /api/v1/search?q=&limit=&mode= — a cross-project
// artifact search scoped to the caller's active workspace. Org admins (and
// platform admins) search every project in the workspace; plain members only
// the projects they can access (viewer role suffices, mirroring
// ListDomainEvents/ListAgentRuns).
//
// mode selects the ranking path and defaults to keyword to preserve the
// original behaviour:
//
//   - keyword  — trigram/ILIKE title+body match (the original path).
//   - semantic — nearest-neighbour over artifact embeddings; falls back to
//     keyword when embeddings are unconfigured or the vector store is absent.
//   - hybrid   — union of keyword and semantic, ranked by a blended score.
//
// The response carries mode_used so the client can tell the user when a
// semantic/hybrid request degraded to keyword.
func (h *Handler) GlobalSearch(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}

	params := r.URL.Query()
	query := strings.TrimSpace(params.Get("q"))
	mode := strings.ToLower(strings.TrimSpace(params.Get("mode")))
	if mode == "" {
		mode = modeKeyword
	}
	limit, _ := strconv.Atoi(params.Get("limit"))
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	if query == "" {
		writeSearchResponse(w, modeKeyword, nil)
		return
	}

	projectIDs, names, ok := h.searchableProjects(w, r)
	if !ok {
		return // response already written (error) — or empty scope below
	}
	if len(projectIDs) == 0 {
		writeSearchResponse(w, modeKeyword, nil)
		return
	}

	var (
		hits     []*artifacts.SearchHit
		modeUsed string
		err      error
	)
	switch mode {
	case modeSemantic:
		hits, modeUsed, err = h.semanticSearch(projectIDs, query, limit)
	case modeHybrid:
		hits, modeUsed, err = h.hybridSearch(projectIDs, query, limit)
	default:
		hits, err = h.keywordSearch(projectIDs, query, limit)
		modeUsed = modeKeyword
	}
	if err != nil {
		respondInternal(w, r, "search failed", err)
		return
	}

	for _, hit := range hits {
		hit.ProjectName = names[hit.ProjectID]
	}
	writeSearchResponse(w, modeUsed, hits)
}

// searchableProjects resolves the project ids (and id→name map) the caller may
// search in the active workspace, applying the same fail-closed scoping as the
// original GlobalSearch. ok is false only when a response has already been
// written: an error (500) or an unresolved active org (empty list, 200). An
// empty-but-ok result means "no accessible projects".
func (h *Handler) searchableProjects(w http.ResponseWriter, r *http.Request) ([]string, map[string]string, bool) {
	// Scope to the active workspace in SQL, and fail closed: an unresolved
	// active org (empty) searches nothing rather than every tenant's projects.
	activeOrg := ActiveOrg(r)
	if activeOrg == "" {
		writeSearchResponse(w, modeKeyword, nil)
		return nil, nil, false
	}

	projectList, err := h.projectService.ListProjectsByOrg(activeOrg)
	if err != nil {
		respondInternal(w, r, "failed to resolve searchable projects", err)
		return nil, nil, false
	}

	// Plain members only search projects they can access.
	if !h.isOrgAdmin(r, activeOrg) {
		user := CurrentUser(r)
		allowed := map[string]bool{}
		if h.memberService != nil {
			ids, err := h.memberService.ProjectIDsForUser(user.ID)
			if err != nil {
				respondInternal(w, r, "failed to resolve project memberships", err)
				return nil, nil, false
			}
			for _, id := range ids {
				allowed[id] = true
			}
		}
		accessible := projectList[:0]
		for _, p := range projectList {
			if allowed[p.ID] {
				accessible = append(accessible, p)
			}
		}
		projectList = accessible
	}

	projectIDs := make([]string, 0, len(projectList))
	names := make(map[string]string, len(projectList))
	for _, p := range projectList {
		projectIDs = append(projectIDs, p.ID)
		names[p.ID] = p.Name
	}
	return projectIDs, names, true
}

// keywordSearch is the original trigram/ILIKE path.
func (h *Handler) keywordSearch(projectIDs []string, query string, limit int) ([]*artifacts.SearchHit, error) {
	hits, err := h.artifactService.SearchArtifacts(projectIDs, query, limit)
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []*artifacts.SearchHit{}
	}
	return hits, nil
}

// semanticSearch runs the nearest-neighbour path, degrading to keyword when
// embeddings are unconfigured or the vector store is unavailable. It returns
// the mode that actually ran.
func (h *Handler) semanticSearch(projectIDs []string, query string, limit int) ([]*artifacts.SearchHit, string, error) {
	if h.embeddingService == nil || !h.embeddingService.Enabled() {
		hits, err := h.keywordSearch(projectIDs, query, limit)
		return hits, modeKeyword, err
	}
	near, err := h.embeddingService.SemanticSearch(projectIDs, query, limit)
	if err != nil {
		if errors.Is(err, embeddings.ErrDisabled) || errors.Is(err, embeddings.ErrVectorUnavailable) {
			hits, kerr := h.keywordSearch(projectIDs, query, limit)
			return hits, modeKeyword, kerr
		}
		return nil, "", err
	}
	return nearestToHits(near, query), modeSemantic, nil
}

// hybridSearch blends the keyword and semantic result sets, deduping by
// artifact and ranking by a combined score. When embeddings are unavailable it
// degrades to the keyword result reported as mode keyword.
func (h *Handler) hybridSearch(projectIDs []string, query string, limit int) ([]*artifacts.SearchHit, string, error) {
	keywordHits, err := h.keywordSearch(projectIDs, query, limit)
	if err != nil {
		return nil, "", err
	}

	if h.embeddingService == nil || !h.embeddingService.Enabled() {
		return keywordHits, modeKeyword, nil
	}
	near, err := h.embeddingService.SemanticSearch(projectIDs, query, limit)
	if err != nil {
		if errors.Is(err, embeddings.ErrDisabled) || errors.Is(err, embeddings.ErrVectorUnavailable) {
			return keywordHits, modeKeyword, nil
		}
		return nil, "", err
	}

	return blendHits(keywordHits, nearestToHits(near, query), limit), modeHybrid, nil
}

// nearestToHits converts semantic nearest-neighbour results into the shared
// SearchHit shape, attaching a 0..1 similarity score and a body snippet built
// the same way the keyword path builds it (head-of-body when the query does not
// occur literally, which is common for semantic matches).
func nearestToHits(near []embeddings.NearestHit, query string) []*artifacts.SearchHit {
	hits := make([]*artifacts.SearchHit, 0, len(near))
	for _, n := range near {
		hits = append(hits, &artifacts.SearchHit{
			ArtifactID: n.ArtifactID,
			ProjectID:  n.ProjectID,
			Type:       n.Type,
			Title:      n.Title,
			Snippet:    artifacts.Snippet(n.Body, query),
			Score:      n.Similarity(),
		})
	}
	return hits
}

// blendHits unions keyword and semantic hits, deduping by artifact id and
// ranking by a combined score. Semantic hits contribute their similarity
// (0..1); keyword hits contribute a rank-decayed weight so an artifact that
// appears in both rises to the top. Order within the input is treated as rank.
func blendHits(keyword, semantic []*artifacts.SearchHit, limit int) []*artifacts.SearchHit {
	type scored struct {
		hit   *artifacts.SearchHit
		score float64
		order int
	}
	byID := map[string]*scored{}
	order := 0

	add := func(hit *artifacts.SearchHit, weight float64) {
		if existing, ok := byID[hit.ArtifactID]; ok {
			existing.score += weight
			// Prefer a hit that carries a semantic score / non-empty snippet.
			if existing.hit.Snippet == "" && hit.Snippet != "" {
				existing.hit.Snippet = hit.Snippet
			}
			if hit.Score > existing.hit.Score {
				existing.hit.Score = hit.Score
			}
			return
		}
		copied := *hit
		byID[hit.ArtifactID] = &scored{hit: &copied, score: weight, order: order}
		order++
	}

	n := len(keyword)
	for i, hit := range keyword {
		// Rank-decayed weight in (0,1]; the top keyword hit weighs ~1.
		add(hit, float64(n-i)/float64(n))
	}
	for _, hit := range semantic {
		add(hit, hit.Score)
	}

	merged := make([]*scored, 0, len(byID))
	for _, s := range byID {
		merged = append(merged, s)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		return merged[i].order < merged[j].order
	})

	out := make([]*artifacts.SearchHit, 0, len(merged))
	for _, s := range merged {
		out = append(out, s.hit)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// writeSearchResponse writes the search envelope, normalizing a nil hit slice
// to an empty JSON array.
func writeSearchResponse(w http.ResponseWriter, modeUsed string, hits []*artifacts.SearchHit) {
	if hits == nil {
		hits = []*artifacts.SearchHit{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(searchResponse{ModeUsed: modeUsed, Hits: hits})
}
