package agents

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// REQ-91: every agent definition must name the tools it may use. An empty
// list used to mean "the vendor CLI gets all of them", which is the hazard.
func TestValidateRequiresAllowedTools(t *testing.T) {
	base := func() *Definition {
		return &Definition{Slug: "an-agent", Name: "An Agent", Provider: "claude-code"}
	}
	for _, tools := range [][]string{nil, {}, {""}, {"   "}} {
		def := base()
		def.AllowedTools = tools
		err := def.Validate()
		if err == nil {
			t.Fatalf("Validate accepted allowed_tools %q", tools)
		}
		if err.Error() != AllowedToolsRequired {
			t.Errorf("Validate(%q) = %q, want the shared wording", tools, err)
		}
	}

	def := base()
	def.AllowedTools = []string{" mcp__openv__* ", "", "WebFetch"}
	if err := def.Validate(); err != nil {
		t.Fatalf("Validate rejected a real allowlist: %v", err)
	}
	// Blank entries are dropped and the rest trimmed, so what is stored is
	// what is passed to the CLI.
	if len(def.AllowedTools) != 2 || def.AllowedTools[0] != "mcp__openv__*" {
		t.Errorf("allowed_tools = %q, want the trimmed non-empty entries", def.AllowedTools)
	}
}

func TestUntrustedInput(t *testing.T) {
	cases := []struct {
		name  string
		agent *Agent
		want  bool
	}{
		{"nil", nil, false},
		{"openv only", &Agent{Slug: "vv-engineer", AllowedTools: []string{"mcp__openv__*"}}, false},
		{"openv tools and local file reads", &Agent{Slug: "reviewer", AllowedTools: []string{"mcp__openv__*", "Read", "Grep", "Glob"}}, false},
		// A shell is a way out of the workspace whatever scope it carries:
		// `git fetch` under Bash(git:*), curl and wget under a bare one. The
		// scope is a string match on the command line, not a network policy.
		{"a scoped shell, colon form", &Agent{Slug: "reviewer", AllowedTools: []string{"mcp__openv__*", "Read", "Bash(git:*)"}}, true},
		{"a scoped shell, glob form", &Agent{Slug: "reviewer", AllowedTools: []string{"mcp__openv__*", "Read", "Bash(git *)"}}, true},
		{"a literal command scope", &Agent{Slug: "reviewer", AllowedTools: []string{"Bash(npm test)"}}, true},
		{"an unscoped shell", &Agent{Slug: "reviewer", AllowedTools: []string{"Bash"}}, true},
		{"an unscoped shell, oddly cased", &Agent{Slug: "reviewer", AllowedTools: []string{"bash(*)"}}, true},
		{"the interviewer", &Agent{Slug: InterviewerSlug, AllowedTools: []string{"mcp__openv__get_artifact"}}, true},
		{"repo access", &Agent{Slug: "developer", RepoAccess: true, AllowedTools: []string{"mcp__openv__*"}}, true},
		{"web fetch", &Agent{Slug: "a", AllowedTools: []string{"mcp__openv__*", "WebFetch"}}, true},
		{"web search, oddly cased", &Agent{Slug: "a", AllowedTools: []string{"websearch"}}, true},
		{"a foreign MCP server", &Agent{Slug: "a", AllowedTools: []string{"mcp__github__search_code"}}, true},
		{"an openv MCP tool is not foreign", &Agent{Slug: "a", AllowedTools: []string{"mcp__openv__get_context"}}, false},
		// Claude Code's server-wide spelling: "mcp__openv" means every tool
		// from the openv server, not some other server whose name happens to
		// start "mcp__openv". Classifying it as foreign would mark a
		// perfectly ordinary OpenV-only agent untrusted.
		{"the bare openv server name is not foreign", &Agent{Slug: "a", AllowedTools: []string{"mcp__openv"}}, false},
		{"a server merely named like openv is foreign", &Agent{Slug: "a", AllowedTools: []string{"mcp__openvpn"}}, true},
		{"a scoped foreign tool is still foreign", &Agent{Slug: "a", AllowedTools: []string{"mcp__github__search_code(x)"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.UntrustedInput(); got != tc.want {
				t.Errorf("UntrustedInput() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A definition already on disk from before allowlists were mandatory still
// loads: refusing it would drop the agent out of the registry on the next
// sync. It is backfilled to the OpenV tools instead, which narrows it. Content
// somebody is saving now is refused rather than quietly fixed.
func TestParseFileAndSyncedFileDisagreeOnMissingTools(t *testing.T) {
	const legacy = "---\nslug: legacy\nname: Legacy\nprovider: claude-code\n---\nBody.\n"

	if _, err := ParseFile(legacy); err == nil {
		t.Error("ParseFile accepted content with no allowed_tools; a new save must be refused")
	}

	def, backfilled, err := parseSyncedFile(legacy, nil)
	if err != nil {
		t.Fatalf("parseSyncedFile: %v", err)
	}
	if !backfilled {
		t.Error("parseSyncedFile filled the allowlist in but did not say so; the file is never rewritten")
	}
	if len(def.AllowedTools) != 1 || def.AllowedTools[0] != "mcp__openv__*" {
		t.Errorf("allowed_tools = %q, want the default OpenV allowlist", def.AllowedTools)
	}

	// With a seed lookup wired in, a seeded slug is filled in with its own
	// seed's list instead — the generic fallback would take tools away from
	// an agent the platform itself shipped.
	seeded := func(slug string) []string {
		if slug == "legacy" {
			return []string{"mcp__openv__*", "Read", "Edit", "Bash(git:*)"}
		}
		return nil
	}
	def, backfilled, err = parseSyncedFile(legacy, seeded)
	if err != nil {
		t.Fatalf("parseSyncedFile with a seed lookup: %v", err)
	}
	if !backfilled {
		t.Error("backfilled = false, want true")
	}
	if !slices.Equal(def.AllowedTools, []string{"mcp__openv__*", "Read", "Edit", "Bash(git:*)"}) {
		t.Errorf("allowed_tools = %q, want the seed's own list", def.AllowedTools)
	}

	// A slug the lookup does not know still gets the generic fallback.
	def, _, err = parseSyncedFile(legacy, func(string) []string { return nil })
	if err != nil {
		t.Fatalf("parseSyncedFile: %v", err)
	}
	if !slices.Equal(def.AllowedTools, DefaultAllowedTools()) {
		t.Errorf("allowed_tools = %q, want the default for an unseeded slug", def.AllowedTools)
	}
}

// REQ-91: repository access is only expressible on claude-code, whose
// allowlist names the editing tools one at a time. On a CLI whose only lever
// is a whole-workspace sandbox or approval mode the setting can do nothing but
// defeat the agent or unconfine the run, so the definition is refused where it
// is written — the API answers 400 and the agent editor shows the message.
func TestValidateRefusesRepoAccessOffClaudeCode(t *testing.T) {
	base := func(provider string) *Definition {
		return &Definition{
			Slug: "developer", Name: "Developer", Provider: provider, RepoAccess: true,
			AllowedTools: []string{"mcp__openv__*", "Read", "Edit", "Write", "Bash(git:*)"},
		}
	}
	for _, provider := range []string{"codex-cli", "gemini-cli", "anthropic-api"} {
		err := base(provider).Validate()
		if err == nil {
			t.Fatalf("Validate accepted repo access on %s", provider)
		}
		if !strings.Contains(err.Error(), "claude-code") || !strings.Contains(err.Error(), provider) {
			t.Errorf("Validate(%s) = %q, want it to name both the provider and the remedy", provider, err)
		}
	}
	if err := base("claude-code").Validate(); err != nil {
		t.Fatalf("Validate refused repo access on claude-code: %v", err)
	}
	// Without repo access the same providers are fine.
	def := base("codex-cli")
	def.RepoAccess = false
	if err := def.Validate(); err != nil {
		t.Fatalf("Validate refused an ordinary codex agent: %v", err)
	}
}

// memRepo is an in-memory agents.Repository, enough to drive a FileService
// through a real sync from disk.
type memRepo struct {
	rows map[string]*Agent // orgID + "/" + slug
}

func newMemRepo() *memRepo { return &memRepo{rows: map[string]*Agent{}} }

func (r *memRepo) key(orgID, slug string) string { return orgID + "/" + slug }

func (r *memRepo) Save(a *Agent) error {
	r.rows[r.key(a.OrgID, a.Slug)] = a
	return nil
}

func (r *memRepo) Update(a *Agent) error {
	r.rows[r.key(a.OrgID, a.Slug)] = a
	return nil
}

func (r *memRepo) FindByID(id string) (*Agent, error) {
	for _, a := range r.rows {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}

func (r *memRepo) FindBySlug(orgID, slug string) (*Agent, error) {
	return r.rows[r.key(orgID, slug)], nil
}

func (r *memRepo) List(orgID string) ([]*Agent, error) {
	var out []*Agent
	for _, a := range r.rows {
		if a.OrgID == orgID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memRepo) Delete(id string) error {
	for k, a := range r.rows {
		if a.ID == id {
			delete(r.rows, k)
		}
	}
	return nil
}

// The file sync and the registry reconcile (seeds.backfillAllowedTools) must
// apply ONE policy to a locked agent with no allowlist: leave it alone. They
// used to disagree — the registry side skipped a locked agent and logged,
// while the sync backfilled it regardless, so the very next SyncFromDisk
// rewrote what the lock said would not change. A locked agent stays as
// written and stays unable to run (the worker and every adapter refuse an
// empty allowlist) until a person sets a list themselves.
func TestSyncFromDiskNeverBackfillsALockedAgent(t *testing.T) {
	dir := t.TempDir()
	repo := newMemRepo()
	svc, err := NewFileService(dir, repo)
	if err != nil {
		t.Fatalf("NewFileService: %v", err)
	}
	orgDir := filepath.Join(dir, "org-1")
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	write := func(slug, extra string) {
		content := "---\nslug: " + slug + "\nname: " + slug + "\nprovider: claude-code\n" + extra + "---\nBody.\n"
		if err := os.WriteFile(filepath.Join(orgDir, slug+".md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", slug, err)
		}
	}
	write("pinned", "locked: true\n")
	write("ordinary", "")

	if err := svc.SyncFromDisk("org-1"); err != nil {
		t.Fatalf("SyncFromDisk: %v", err)
	}

	pinned, _ := svc.GetBySlug("org-1", "pinned")
	if pinned == nil {
		t.Fatal("the locked agent dropped out of the registry; it must stay visible and editable")
	}
	if !pinned.Locked {
		t.Error("the lock did not survive the sync")
	}
	if len(NonEmptyTools(pinned.AllowedTools)) != 0 {
		t.Errorf("allowed_tools = %v, want none: a locked agent is never backfilled", pinned.AllowedTools)
	}

	ordinary, _ := svc.GetBySlug("org-1", "ordinary")
	if ordinary == nil {
		t.Fatal("the unlocked agent dropped out of the registry")
	}
	if !slices.Equal(ordinary.AllowedTools, DefaultAllowedTools()) {
		t.Errorf("allowed_tools = %v, want %v: an unlocked legacy agent is still backfilled",
			ordinary.AllowedTools, DefaultAllowedTools())
	}
}
