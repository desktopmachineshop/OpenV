package postgres

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// The agent registry's round trip (refactor plan S15b, OpenV REQ-23),
// pinned as found. The helpers are in repository_roundtrip_helpers_test.go.

func rtAgentSansTimes(a *agents.Agent) agents.Agent {
	c := *a
	c.CreatedAt, c.UpdatedAt, c.SyncedAt = time.Time{}, time.Time{}, nil
	return c
}

func rtAgentNames(list []*agents.Agent) []string {
	var names []string
	for _, a := range list {
		names = append(names, a.Name)
	}
	return names
}

// An agent reads back every field as saved: its times as TIMESTAMP columns
// keep them (the wall clock, to the microsecond, in lib/pq's zone at offset
// 0), no synced_at as NULL that reads back nil, and no tools or config as []
// and {} that read back as an empty list and map. Update rewrites everything
// but the workspace, the slug and created_at, and an id no row has is no
// error. A second agent with a slug its workspace has is
// agents.ErrSlugExists; a second with an id an agent has is Postgres's
// refusal; and an agent with no workspace is agents.ErrWorkspaceRequired,
// with nothing stored (#379 bug 89: it was stored with a NULL workspace, so
// no List or FindBySlug found it, and two such agents could share a slug).
// An agent no row has, and a malformed id, are agents.ErrNotFound, by id or
// by slug (#379 bug 88: they read as no agent and no error), and an empty
// workspace finds none, not even an agent a database from before workspaces
// left with none.
// Delete takes the crew nodes that place the agent.
func TestAgentRepositoryRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewAgentRepository(db)
	orgID, otherOrg := uuid.New().String(), uuid.New().String()
	synced := rtAt(5).In(rtCEST)

	saved := &agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: "requirements-drafter", Name: "Requirements drafter",
		Description: "Drafts “shall” statements", Provider: "claude", Model: "claude-opus", Effort: "high",
		AllowedTools: []string{"Read", "Grep", "mcp__openv__create_artifact"}, WriteMode: "direct", RepoAccess: true,
		MaxTurns: 12, TimeoutSeconds: 600,
		Config: map[string]interface{}{"temperature": 0.2, "mcp": map[string]interface{}{"servers": []interface{}{"openv"}}},
		Locked: true, SystemPrompt: "You draft requirements.\n\nUse ISO 29148 wording.", FilePath: "agents/requirements-drafter.md",
		ContentHash: "sha256:abc", SyncedAt: &synced, CreatedAt: rtAt(0), UpdatedAt: rtAt(1).In(rtCEST)}
	if err := repo.Save(saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(saved.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v, %v", got, err)
	}
	rtWantTimestamp(t, "an agent's created_at", got.CreatedAt, saved.CreatedAt)
	rtWantTimestamp(t, "an agent's updated_at, sent at +02:00", got.UpdatedAt, saved.UpdatedAt)
	if got.SyncedAt == nil {
		t.Errorf("an agent's synced_at read back nil")
	} else {
		rtWantTimestamp(t, "an agent's synced_at, sent at +02:00", *got.SyncedAt, synced)
	}
	rtWantSame(t, "an agent", rtAgentSansTimes(got), rtAgentSansTimes(saved))

	bare := &agents.Agent{ID: uuid.New().String(), OrgID: otherOrg, Slug: "bare", Name: "Bare", Provider: "codex",
		CreatedAt: rtAt(2), UpdatedAt: rtAt(2)}
	if err := repo.Save(bare); err != nil {
		t.Fatalf("Save with nothing optional: %v", err)
	}
	var tools, config string
	if err := db.QueryRow(`SELECT allowed_tools::text, config::text FROM agents WHERE id = $1`, bare.ID).
		Scan(&tools, &config); err != nil || tools != "[]" || config != "{}" {
		t.Errorf("no tools or config stored as %q %q (%v); want [] and {}", tools, config, err)
	}
	if got, err := repo.FindByID(bare.ID); err != nil || got == nil {
		t.Errorf("FindByID(bare): %v, %v", got, err)
	} else {
		want := rtAgentSansTimes(bare)
		want.AllowedTools, want.Config = []string{}, map[string]interface{}{}
		rtWantSame(t, "an agent with nothing optional", rtAgentSansTimes(got), want)
		if got.SyncedAt != nil {
			t.Errorf("no synced_at read back as %v, want nil", got.SyncedAt)
		}
	}

	t.Run("update", func(t *testing.T) {
		changed := *got
		changed.OrgID, changed.Slug = otherOrg, "renamed-slug" // neither is written
		changed.Name, changed.Description, changed.Provider, changed.Model, changed.Effort = "Drafter", "", "gemini", "", "low"
		changed.AllowedTools, changed.WriteMode, changed.RepoAccess = nil, "proposal", false
		changed.MaxTurns, changed.TimeoutSeconds, changed.Config, changed.Locked = 0, 0, nil, false
		changed.SystemPrompt, changed.FilePath, changed.ContentHash, changed.SyncedAt = "", "", "", nil
		changed.CreatedAt, changed.UpdatedAt = rtAt(50), rtAt(60) // created_at is not written either
		if err := repo.Update(&changed); err != nil {
			t.Fatalf("Update: %v", err)
		}
		after, err := repo.FindByID(saved.ID)
		if err != nil || after == nil {
			t.Fatalf("FindByID after update: %v, %v", after, err)
		}
		rtWantTimestamp(t, "created_at after an update", after.CreatedAt, saved.CreatedAt)
		rtWantTimestamp(t, "updated_at after an update", after.UpdatedAt, rtAt(60))
		if after.SyncedAt != nil {
			t.Errorf("synced_at updated to none read back as %v, want nil", after.SyncedAt)
		}
		want := rtAgentSansTimes(&changed)
		want.OrgID, want.Slug, want.AllowedTools, want.Config = orgID, saved.Slug, []string{}, map[string]interface{}{}
		rtWantSame(t, "an updated agent", rtAgentSansTimes(after), want)

		if err := repo.Update(&agents.Agent{ID: uuid.New().String(), Name: "Ghost", Provider: "claude"}); err != nil {
			t.Errorf("Update of an agent no row has: %v, want no error", err)
		}
		rtWantRefused(t, "Update of a malformed id", repo.Update(&agents.Agent{ID: malformed, Name: "x", Provider: "claude"}))
	})

	t.Run("saves", func(t *testing.T) {
		if err := repo.Save(&agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: saved.Slug, Name: "Twin",
			Provider: "claude"}); err != agents.ErrSlugExists {
			t.Errorf("Save of a slug the workspace has: %v, want agents.ErrSlugExists", err)
		}
		err := repo.Save(&agents.Agent{ID: saved.ID, OrgID: orgID, Slug: "another-slug", Name: "Same id", Provider: "claude"})
		rtWantPQ(t, "Save of an id an agent has", err, "23505", "agents_pkey")
		if err == agents.ErrSlugExists {
			t.Errorf("Save of an id an agent has answered agents.ErrSlugExists")
		}
		if err := repo.Save(&agents.Agent{ID: uuid.New().String(), OrgID: otherOrg, Slug: saved.Slug, Name: "Elsewhere",
			Provider: "claude"}); err != nil {
			t.Errorf("Save of a slug another workspace has: %v, want it stored", err)
		}
		for i := 0; i < 2; i++ {
			// Twice with one slug: neither is stored, so neither can share it.
			id := uuid.New().String()
			if err := repo.Save(&agents.Agent{ID: id, Slug: "no-workspace", Name: "No workspace", Provider: "codex",
				CreatedAt: rtAt(3), UpdatedAt: rtAt(3)}); err != agents.ErrWorkspaceRequired {
				t.Errorf("Save of an agent with no workspace: %v, want agents.ErrWorkspaceRequired", err)
			}
			if found, err := repo.FindByID(id); found != nil || err != agents.ErrNotFound {
				t.Errorf("an agent with no workspace was stored: %v, %v", found, err)
			}
		}
		rtWantRefused(t, "Save in a malformed workspace", repo.Save(&agents.Agent{ID: uuid.New().String(), OrgID: malformed,
			Slug: "x", Name: "x", Provider: "claude"}))
	})

	t.Run("not found", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			if found, err := repo.FindByID(id); found != nil || err != agents.ErrNotFound {
				t.Errorf("FindByID(%q) = %v, %v; want nil, agents.ErrNotFound", id, found, err)
			}
		}
	})

	t.Run("by slug", func(t *testing.T) {
		if found, err := repo.FindBySlug(orgID, saved.Slug); err != nil || found == nil || found.ID != saved.ID {
			t.Errorf("FindBySlug: %v, %v; want %s", found, err, saved.ID)
		}
		if found, err := repo.FindBySlug(otherOrg, saved.Slug); err != nil || found == nil || found.Name != "Elsewhere" {
			t.Errorf("FindBySlug in another workspace: %v, %v; want that workspace's agent", found, err)
		}
		// A row from before workspaces, which no Save writes any more.
		rtSeed(t, db, `INSERT INTO agents (id, slug, name, provider) VALUES ($1, 'legacy', 'Legacy', 'claude')`, uuid.New().String())
		for _, c := range []struct{ org, slug string }{
			{orgID, "no-such-slug"},
			{orgID, bare.Slug},
			{uuid.New().String(), saved.Slug},
			{"", saved.Slug},
			{"", "legacy"}, // an empty workspace matches no agent, not those with none
			{malformed, saved.Slug},
			{"\xff", saved.Slug},
		} {
			if found, err := repo.FindBySlug(c.org, c.slug); found != nil || err != agents.ErrNotFound {
				t.Errorf("FindBySlug(%q, %q) = %v, %v; want nil, agents.ErrNotFound", c.org, c.slug, found, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		crews := NewTeamRepository(db)
		crew := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Crew", CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		node := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: saved.ID, Label: "Drafter", CreatedAt: rtAt(0)}
		if err := crews.SaveTeam(crew); err != nil {
			t.Fatal(err)
		}
		if err := crews.SaveNode(node); err != nil {
			t.Fatal(err)
		}
		if err := repo.Delete(saved.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if found, err := repo.FindByID(saved.ID); found != nil || err != agents.ErrNotFound {
			t.Errorf("FindByID after Delete: %v, %v; want nil, agents.ErrNotFound", found, err)
		}
		if found, err := crews.FindNodeByID(node.ID); found != nil || err != teams.ErrNodeNotFound {
			t.Errorf("a crew node placing a deleted agent: %v, %v; want it gone with the agent", found, err)
		}
		if err := repo.Delete(saved.ID); err != nil {
			t.Errorf("Delete of an agent no row has: %v, want no error", err)
		}
		rtWantRefused(t, "Delete of a malformed id", repo.Delete(malformed))
		if found, err := repo.FindByID(bare.ID); found == nil || err != nil {
			t.Errorf("Delete removed another agent: %v, %v", found, err)
		}
	})
}

// List lists a workspace's agents by name, case-insensitively and alike
// under any database collation: by the name in small letters compared byte
// by byte, then by the name itself so compared (a capital first), then by
// id, so two agents of one name come in one order too (#379 bug 94: by name
// alone, in the database's collation, which under C put every capital
// before any small letter). A workspace with none, or an empty workspace id
// (even beside agents with no workspace), lists nil, and a malformed
// workspace id is Postgres's refusal.
func TestAgentRepositoryList(t *testing.T) {
	db := rtDB(t)
	repo := NewAgentRepository(db)
	orgID, otherOrg := uuid.New().String(), uuid.New().String()
	sameLo, sameHi := uuid.New().String(), uuid.New().String()
	if sameLo > sameHi {
		sameLo, sameHi = sameHi, sameLo
	}
	for i, c := range []struct{ id, org, name string }{
		{"", orgID, "charlie"},
		{"", orgID, "tie-a"},
		{"", orgID, "Delta"},
		{sameHi, orgID, "Same"},
		{"", orgID, "echo"},
		{"", orgID, "alpha"},
		{"", orgID, "Echo"},
		{"", orgID, "tie b"},
		{"", orgID, "Bravo"},
		{sameLo, orgID, "Same"},
		{"", otherOrg, "Aardvark"},
	} {
		id := c.id
		if id == "" {
			id = uuid.New().String()
		}
		// Stamped newest first, so that created_at's order is not the name's.
		if err := repo.Save(&agents.Agent{ID: id, OrgID: c.org, Slug: uuid.New().String(), Name: c.name,
			Provider: "claude", CreatedAt: rtAt(-i), UpdatedAt: rtAt(-i)}); err != nil {
			t.Fatalf("Save %q: %v", c.name, err)
		}
	}
	// An agent with no workspace, which no Save writes any more (a database
	// from before workspaces could hold one until the boot backfill).
	rtSeed(t, db, `INSERT INTO agents (id, slug, name, provider) VALUES ($1, 'aaron', 'Aaron', 'claude')`, uuid.New().String())
	for _, collation := range rtCollations(t, db) {
		rtSetCollation(t, db, "agents", "name", collation)
		list, err := repo.List(orgID)
		if err != nil {
			t.Fatalf("List under %s: %v", collation, err)
		}
		var got []string
		for _, a := range list {
			got = append(got, a.Name+" "+a.ID)
		}
		want := []string{"alpha", "Bravo", "charlie", "Delta", "Echo", "echo", "Same", "Same", "tie b", "tie-a"}
		names := rtAgentNames(list)
		if !reflect.DeepEqual(names, want) || len(list) != 10 || list[6].ID != sameLo || list[7].ID != sameHi {
			t.Errorf("a workspace's agents under the %s collation listed %q, want %q, the two named Same by id", collation, got, want)
		}
	}

	list, err := repo.List("")
	rtWantNil(t, "List with no workspace", list, err)
	list, err = repo.List(uuid.New().String())
	rtWantNil(t, "List of a workspace with no agent", list, err)
	for _, id := range malformedIDs {
		list, err = repo.List(id)
		rtWantNil(t, fmt.Sprintf("List of the malformed workspace %q", id), list, err)
	}
}

// What an agent's tools and config read back as when the JSON stored is not
// what the repository writes: JSON null or JSON of another shape reads as
// an empty list or map, never a read error and never nil.
func TestAgentRepositoryReadsJSONOfAnotherShape(t *testing.T) {
	db := rtDB(t)
	repo := NewAgentRepository(db)
	orgID := uuid.New().String()
	for _, c := range []struct{ slug, tools, config string }{
		{"null-json", "null", "null"},
		{"other-shapes", `{"tool": "Read"}`, `[1, 2]`},
		{"half-a-list", `["Read", 7]`, `"text"`},
	} {
		a := &agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: c.slug, Name: c.slug, Provider: "claude",
			CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.Save(a); err != nil {
			t.Fatal(err)
		}
		rtSeed(t, db, `UPDATE agents SET allowed_tools = $2::jsonb, config = $3::jsonb WHERE id = $1`, a.ID, c.tools, c.config)
		got, err := repo.FindByID(a.ID)
		if err != nil || got == nil {
			t.Errorf("FindByID(%s): %v, %v", c.slug, got, err)
			continue
		}
		rtWantSame(t, c.slug+"'s tools", got.AllowedTools, []string{})
		rtWantSame(t, c.slug+"'s config", got.Config, map[string]interface{}{})
	}
	if list, err := repo.List(orgID); err != nil || len(list) != 3 {
		t.Errorf("List over those agents: %d, %v; want all three", len(list), err)
	}
}

// A failure that is not the id's, here a database that fails every
// statement, is handed back as it came by every method (Save's is not
// agents.ErrSlugExists): a read answers no agent with it, never the no row
// and no error of an id no row has, and List answers nil. A config JSON
// cannot encode fails Save and Update with the encoder's error before any
// statement runs.
func TestAgentRepositoryHandsBackAFailure(t *testing.T) {
	repo := NewAgentRepository(rtClosedDB(t))
	id := uuid.New().String()
	agent := &agents.Agent{ID: id, OrgID: id, Slug: "x", Name: "x", Provider: "claude"}
	rtWantClosed(t, "Save", repo.Save(agent), "")
	rtWantClosed(t, "Update", repo.Update(agent), "")
	rtWantClosed(t, "Delete", repo.Delete(id), "")
	found, err := repo.FindByID(id)
	if found != nil {
		t.Errorf("FindByID on a failing database read %v", found)
	}
	rtWantClosed(t, "FindByID", err, "")
	found, err = repo.FindBySlug(id, "x")
	if found != nil {
		t.Errorf("FindBySlug on a failing database read %v", found)
	}
	rtWantClosed(t, "FindBySlug", err, "")
	list, err := repo.List(id)
	if list != nil {
		t.Errorf("List on a failing database listed %v", list)
	}
	rtWantClosed(t, "List", err, "")

	agent.Config = rtUnencodable
	rtWantUnencodable(t, "Save", repo.Save(agent))
	rtWantUnencodable(t, "Update", repo.Update(agent))
}
