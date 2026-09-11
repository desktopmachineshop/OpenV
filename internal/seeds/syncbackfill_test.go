package seeds

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

// memAgentRepo is an in-memory agents.Repository, enough to drive a real
// FileService through a real sync from disk.
type memAgentRepo struct{ rows map[string]*agents.Agent }

func newMemAgentRepo() *memAgentRepo { return &memAgentRepo{rows: map[string]*agents.Agent{}} }

func (r *memAgentRepo) key(orgID, slug string) string { return orgID + "/" + slug }

func (r *memAgentRepo) Save(a *agents.Agent) error {
	r.rows[r.key(a.OrgID, a.Slug)] = a
	return nil
}

func (r *memAgentRepo) Update(a *agents.Agent) error {
	r.rows[r.key(a.OrgID, a.Slug)] = a
	return nil
}

func (r *memAgentRepo) FindByID(id string) (*agents.Agent, error) {
	for _, a := range r.rows {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}

func (r *memAgentRepo) FindBySlug(orgID, slug string) (*agents.Agent, error) {
	return r.rows[r.key(orgID, slug)], nil
}

func (r *memAgentRepo) List(orgID string) ([]*agents.Agent, error) {
	var out []*agents.Agent
	for _, a := range r.rows {
		if a.OrgID == orgID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memAgentRepo) Delete(id string) error {
	for k, a := range r.rows {
		if a.ID == id {
			delete(r.rows, k)
		}
	}
	return nil
}

// A seeded agent whose file on disk predates mandatory allowlists (REQ-91)
// must come out of the *file sync* holding its own seed's list — not the
// generic mcp__openv__* fallback.
//
// The sync is where this is decided, and it is easy to get wrong: cmd/server
// runs SyncAllFromDisk before seeds.EnsureOrgDefaults, and the registry-side
// backfill only ever fires on a row with no allowlist at all. So a sync that
// filled a legacy `developer.md` in with the generic list left the Developer
// permanently without Read, Grep, Glob, Edit, Write or a shell, and the
// seed-aware backfill downstream never got a chance to notice.
//
// And it is written back to the file, so the "no allowed_tools" decision — and
// its log line — happens once, not on every sync for the life of the install.
func TestFileSyncBackfillsASeededAgentFromItsSeed(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	dir := t.TempDir()
	svc, err := agents.NewFileService(dir, newMemAgentRepo(), agents.WithSeedAllowedTools(SeedAllowedTools))
	if err != nil {
		t.Fatalf("NewFileService: %v", err)
	}
	orgDir := filepath.Join(dir, "org-1")
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(orgDir, "developer.md")
	legacy := "---\nslug: developer\nname: Developer\nprovider: claude-code\nrepo_access: true\n---\nYou are a software developer.\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	want := SeedAllowedTools("developer")
	if len(want) < 2 {
		t.Fatalf("SeedAllowedTools(%q) = %v; the seeded Developer names more than that", "developer", want)
	}

	if err := svc.SyncFromDisk("org-1"); err != nil {
		t.Fatalf("SyncFromDisk: %v", err)
	}

	// In the registry.
	row, err := svc.GetBySlug("org-1", "developer")
	if err != nil || row == nil {
		t.Fatalf("GetBySlug: %v (row %v)", err, row)
	}
	if !slices.Equal(row.AllowedTools, want) {
		t.Errorf("registry allowed_tools = %v, want the seed's own list %v", row.AllowedTools, want)
	}

	// And on disk: the file now stands on its own, so a strict parse — the one
	// a person's save goes through, which refuses a missing allowlist — accepts
	// it.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	def, err := agents.ParseFile(string(data))
	if err != nil {
		t.Fatalf("the rewritten file does not parse strictly: %v\n%s", err, data)
	}
	if !slices.Equal(def.AllowedTools, want) {
		t.Errorf("file allowed_tools = %v, want %v", def.AllowedTools, want)
	}
	if !strings.Contains(string(data), "You are a software developer.") {
		t.Errorf("the rewrite lost the system prompt:\n%s", data)
	}
	if def.RepoAccess != true || def.Provider != "claude-code" {
		t.Errorf("the rewrite lost frontmatter: repo_access=%v provider=%q", def.RepoAccess, def.Provider)
	}

	// A second sync has nothing left to decide: the file is unchanged and the
	// "no allowed_tools" line is logged once, not once per sync forever.
	before := string(data)
	if err := svc.SyncFromDisk("org-1"); err != nil {
		t.Fatalf("second SyncFromDisk: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != before {
		t.Errorf("the second sync rewrote the file again:\n%s", after)
	}
	if n := strings.Count(logs.String(), "has no allowed_tools"); n != 1 {
		t.Errorf("the backfill logged %d times across two syncs, want 1:\n%s", n, logs.String())
	}
}

// An agent nobody seeded still gets the generic OpenV allowlist: the lookup
// narrows to the seeds it knows and never invents tools for anything else.
func TestFileSyncBackfillsAnUnseededAgentWithTheDefault(t *testing.T) {
	dir := t.TempDir()
	svc, err := agents.NewFileService(dir, newMemAgentRepo(), agents.WithSeedAllowedTools(SeedAllowedTools))
	if err != nil {
		t.Fatalf("NewFileService: %v", err)
	}
	orgDir := filepath.Join(dir, "org-1")
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := "---\nslug: homegrown\nname: Homegrown\nprovider: claude-code\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(orgDir, "homegrown.md"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := svc.SyncFromDisk("org-1"); err != nil {
		t.Fatalf("SyncFromDisk: %v", err)
	}
	row, _ := svc.GetBySlug("org-1", "homegrown")
	if row == nil {
		t.Fatal("the agent dropped out of the registry")
	}
	if !slices.Equal(row.AllowedTools, agents.DefaultAllowedTools()) {
		t.Errorf("allowed_tools = %v, want %v", row.AllowedTools, agents.DefaultAllowedTools())
	}
}

// SeedAllowedTools is the same map the registry-side backfill reads, and it
// hands out copies: a caller that trims or reorders what it gets must not be
// able to edit the seed table underneath every other workspace.
func TestSeedAllowedToolsIsACopyOfTheSeedList(t *testing.T) {
	if got := SeedAllowedTools("no-such-agent"); got != nil {
		t.Errorf("SeedAllowedTools(unseeded) = %v, want nil", got)
	}
	for slug, want := range seedAllowedTools() {
		got := SeedAllowedTools(slug)
		if !slices.Equal(got, want) {
			t.Errorf("SeedAllowedTools(%q) = %v, want %v", slug, got, want)
		}
		if len(got) > 0 {
			got[0] = "mutated"
			if seedAllowedTools()[slug][0] == "mutated" {
				t.Fatalf("SeedAllowedTools(%q) handed out the seed's own slice", slug)
			}
		}
	}
}
