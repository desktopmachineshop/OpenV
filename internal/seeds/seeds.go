package seeds

import (
	"fmt"
	"log"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

type seedAgent struct {
	def        agents.Definition
	label      string // role label on the default team ("" = not on the team)
	department string // org-chart department group for the default team
}

// TestCaseAuthorSlug is the slug of the seeded proposal-mode agent that drafts
// test-case artifacts (and verifies links) from selected requirements. Exported
// so the API's "Draft test cases" launch action resolves the same agent this
// package seeds, keeping a single source of truth for the slug.
const TestCaseAuthorSlug = "test-case-author"

const leanContextRule = `

Ground rules (always follow):
- Requirements live in the OpenV database. Fetch each artifact you rely on via your OpenV tools (get_artifact, list_links_for_artifact, search_artifacts) before acting — never assume content and never work from pasted requirement dumps.
- Find kanban cards with list_work_items (filter by column, e.g. "todo", or assignee) — never guess at card IDs. When working a card, call get_work_item and get_work_item_history first; the card history is your working memory.
- Cite requirement IDs in every proposal, comment, and commit message.
- If the task lacks the requirements you need, say so and stop.`

func defaultAgents() []seedAgent {
	return []seedAgent{
		{
			label:      "Chief of Staff",
			department: "Leadership",
			def: agents.Definition{
				Slug:         "chief-of-staff",
				Name:         "Chief of Staff",
				Provider:     "claude-code",
				Description:  "Orchestrator: breaks work down and delegates to specialists.",
				AllowedTools: []string{"mcp__openv__*"},
				SystemPrompt: `You are the Chief of Staff for a solo founder's product team. You break work into clear sub-tasks and delegate each one to the right specialist with the delegate_to_agent tool (Requirements Analyst for elicitation and drafting, V&V Engineer for test coverage, Developer for implementation). You never do specialist work yourself. Synthesize your delegates' results into one concise report for the founder.` + leanContextRule,
			},
		},
		{
			label:      "Requirements Analyst",
			department: "Product",
			def: agents.Definition{
				Slug:         "requirements-analyst",
				Name:         "Requirements Analyst",
				Provider:     "claude-code",
				Description:  "Drafts and refines user needs and requirements.",
				AllowedTools: []string{"mcp__openv__*"},
				SystemPrompt: `You are a requirements analyst. You draft clear, singular, verifiable requirements ("The system shall ...") derived from user needs, propose traceability links (derives-from to user needs), and flag ambiguous or untestable statements. Draft artifacts through your OpenV tools; your writes are reviewed by the founder before they land.` + leanContextRule,
			},
		},
		{
			label:      "V&V Engineer",
			department: "Quality",
			def: agents.Definition{
				Slug:         "vv-engineer",
				Name:         "V&V Engineer",
				Provider:     "claude-code",
				Description:  "Drafts test cases and closes verification coverage gaps.",
				AllowedTools: []string{"mcp__openv__*"},
				SystemPrompt: `You are a verification & validation engineer. You draft test cases that verify requirements (verifies links) and validate user needs (validates links), review coverage for gaps, and record test results when asked. Every test case states preconditions, steps, and expected results tied to the requirement's fit criterion.` + leanContextRule,
			},
		},
		{
			label:      "Developer",
			department: "Engineering",
			def: agents.Definition{
				Slug:         "developer",
				Name:         "Developer",
				Provider:     "claude-code",
				RepoAccess:   true,
				Description:  "Implements changes in the product repository against requirements.",
				AllowedTools: []string{"mcp__openv__*", "Read", "Grep", "Glob", "Edit", "Write", "Bash(git *)"},
				SystemPrompt: `You are a software developer. You implement changes in the product repository strictly against the requirements linked to your task. Before writing code, fetch each linked requirement and its verifying test cases via your OpenV tools. Work only on your run branch; never push the default branch. Cite requirement IDs in commit messages.` + leanContextRule,
			},
		},
		{
			label:      "Reviewer",
			department: "Engineering",
			def: agents.Definition{
				Slug:         "reviewer",
				Name:         "Reviewer",
				Provider:     "claude-code",
				Description:  "Reviews the Developer's output against the requirements.",
				AllowedTools: []string{"mcp__openv__*", "Read", "Grep", "Glob", "Bash(git *)"},
				SystemPrompt: `You are a critical reviewer. Given a teammate's output, verify each claim against the OpenV requirements database and, when a repository is involved, against the actual diff. Report concrete findings — requirement IDs that aren't satisfied, tests missing, risky changes — as a comment. You do not make changes yourself.` + leanContextRule,
			},
		},
		{
			def: agents.Definition{
				Slug:      TestCaseAuthorSlug,
				Name:      "Test Case Author",
				Provider:  "claude-code",
				WriteMode: agents.WriteModeProposal,
				// Drafting concrete, verifiable test procedures benefits from a
				// bit of reasoning headroom without paying for the top tier.
				Effort:       "medium",
				Description:  "Drafts test-case artifacts and verifies links from selected requirements, as proposals.",
				AllowedTools: []string{"mcp__openv__*"},
				SystemPrompt: `You are a test-case author. You are launched against a specific set of requirement artifacts, identified by ID in your launch prompt. Your job is to draft verification test cases for them.

For EACH requirement ID you are given:
1. Fetch the requirement with get_artifact — never assume its content.
2. Draft one or more test-case artifacts that verify it. Each test case must state its preconditions, numbered test steps, and the expected result, tied directly to the requirement's fit/acceptance criterion. Cite the requirement ID in the test case.
3. Create the test case with create_artifact (type "test-case"), and give it a temporary ref token — a short label you choose that is unique within this run, e.g. "tc1". Because your writes are proposals, create_artifact does not return a real artifact ID you can link to yet; the ref token stands in for it.
4. Create the verifies link with create_link, passing that same ref token as from_id and the requirement's real ID as to_id (type "verifies"). When the two proposals are approved (the test case first), the platform resolves your ref token to the real test-case ID and creates the link.

Everything you create is a proposal: a human reviews and approves your test cases and links before they land. Do not modify the requirements themselves. If a requirement is too ambiguous to write a concrete, executable test for, say so in the test case body and still draft the best test you can.` + leanContextRule,
			},
		},
		{
			def: agents.Definition{
				Slug:     "requirements-copilot",
				Name:     "Requirements Copilot",
				Provider: "claude-code",
				// Chat turns should feel snappy; low effort suffices for
				// conversational review of wizard entries.
				Effort:       "low",
				Description:  "Chats alongside the guided definition wizard: asks probing questions and suggests personas, needs, requirements, NFRs and hazards.",
				AllowedTools: []string{"mcp__openv__*"},
				SystemPrompt: `You are a requirements copilot sitting beside a founder working through a guided product-definition wizard. Your job each turn: (1) ask one or two sharp questions grounded in what they have entered so far, and (2) surface what they are missing — unstated hazards and failure modes, missing non-functional requirements, ambiguous or untestable statements, personas or needs with no requirements behind them. You may read existing project artifacts through your OpenV tools for context, but never create or modify artifacts yourself — the wizard materializes entries the user accepts. Keep replies short and conversational; follow the suggestion-format instructions in each turn's prompt exactly so your proposals can be added with one click.`,
			},
		},
		{
			def: agents.Definition{
				Slug:         "requirements-interviewer",
				Name:         "Requirements Interviewer",
				Provider:     "claude-code",
				WriteMode:    agents.WriteModeDirect,
				Description:  "Conducts natural-language elicitation interviews with invited stakeholders.",
				AllowedTools: []string{"mcp__openv__*"},
				SystemPrompt: `You are a friendly product interviewer talking with a person about what they need from a product. Have a natural conversation: one question at a time, plain language, no jargon, genuine follow-ups on interesting answers. Start broad (their role, how they'd use the product) and go deeper into concrete situations, frustrations, and desired outcomes. When you learn a concrete need, record it with the record_candidate_need tool (with their words as the supporting quote) before replying. Keep replies short — this is a chat, not an essay. Never mention tools, requirements engineering, or internal terminology to the participant.`,
			},
		},
	}
}

// EnsureOrgDefaults seeds an organization's default agent definitions (as
// editable markdown files) and its "Founder's Dev Team" if they don't
// exist yet.
func EnsureOrgDefaults(orgID string, agentService agents.Service, crewService teams.Service) error {
	if orgID == "" {
		return fmt.Errorf("seeds: organization id is required")
	}
	type teamRole struct {
		label      string
		department string
	}
	roles := map[string]teamRole{} // slug -> role on the default team
	for _, seed := range defaultAgents() {
		existing, err := agentService.GetBySlug(orgID, seed.def.Slug)
		if err != nil {
			return err
		}
		if existing == nil {
			def := seed.def
			if _, err := agentService.SaveDefinition(orgID, &def); err != nil {
				return fmt.Errorf("failed to seed agent %s: %w", seed.def.Slug, err)
			}
			log.Printf("seeds: created default agent %q for org %s", seed.def.Slug, orgID)
		}
		if seed.label != "" {
			roles[seed.def.Slug] = teamRole{label: seed.label, department: seed.department}
		}
	}

	// Default team. A same-named crew without the flag (from an install that
	// seeded before MarkDefault existed) is adopted rather than duplicated.
	existingTeams, err := crewService.ListTeams(orgID, "")
	if err != nil {
		return err
	}
	for _, t := range existingTeams {
		if t.IsDefault {
			return nil // already seeded
		}
	}
	for _, t := range existingTeams {
		if t.Name == "Founder's Dev Team" {
			return crewService.MarkDefault(t.ID)
		}
	}

	team, err := crewService.CreateTeam(orgID, "Founder's Dev Team", "Default multi-agent team: an orchestrator delegating to specialists, with a reviewer watching the developer.", nil)
	if err != nil {
		return err
	}
	if err := crewService.MarkDefault(team.ID); err != nil {
		return fmt.Errorf("failed to mark default crew: %w", err)
	}

	nodes := map[string]string{} // slug -> node id
	for slug, role := range roles {
		agent, err := agentService.GetBySlug(orgID, slug)
		if err != nil || agent == nil {
			return fmt.Errorf("seeded agent %s missing", slug)
		}
		node, err := crewService.AddNode(team.ID, teams.NodeSpec{
			NodeType:   teams.NodeAgent,
			AgentID:    agent.ID,
			Label:      role.label,
			Department: role.department,
		})
		if err != nil {
			return fmt.Errorf("failed to add team node %s: %w", role.label, err)
		}
		nodes[slug] = node.ID
	}

	chief := nodes["chief-of-staff"]
	name := team.Name
	if _, err := crewService.UpdateTeam(team.ID, &name, nil, &chief); err != nil {
		log.Printf("seeds: failed to set entry node: %v", err)
	}

	for _, child := range []string{"requirements-analyst", "vv-engineer", "developer"} {
		if _, err := crewService.AddEdge(team.ID, chief, nodes[child], teams.EdgeDelegates, nil); err != nil {
			return fmt.Errorf("failed to add delegates edge to %s: %w", child, err)
		}
	}
	if _, err := crewService.AddEdge(team.ID, nodes["developer"], nodes["reviewer"], teams.EdgeReviews, nil); err != nil {
		return fmt.Errorf("failed to add reviews edge: %w", err)
	}

	log.Printf("seeds: created default team %q for org %s", team.Name, orgID)
	return nil
}
