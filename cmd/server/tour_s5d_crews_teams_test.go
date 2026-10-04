//go:build unix

package main

import (
	"encoding/json"
	"testing"
)

// TestTourS5dCrewsTeams is the S5d tour's crews area (refactor plan §6.4 S5d,
// before M7, which splits agent_handlers.go and with it the crew handlers and
// both of their route tables; invariants I2 (each /teams alias bound to its
// /crews route's handler), I3, I4, I5, I8 (the export's Content-Disposition),
// I10 (the launches' events) and I15 (the export's filename and media type,
// and what an import carries over); quirks Q1, Q14 and Q19; OpenV REQ-143).
// Its golden is testdata/tour/s5d/crews_teams.json.
//
// The area walks the canonical /crews routes, in the owner's shared workspace
// W, with a project P in W (the member is its editor), a project Q in the
// owner's personal workspace, a plain member m of W and an outsider:
//   - the crew templates: the static presets with their portable documents;
//   - W's default crew, Founder's Dev Team, which the workspace was seeded
//     with: the list (created_at order) and its graph; the list refused to a
//     worker key (the handler's 401: crews are for users);
//   - create: workspace-wide as W's admin, pinned to P as P's editor (the
//     member), and each refusal in the handler's order (the member without
//     a project: workspace admin access; Q's project, another workspace's; a
//     project no one has; no name; a body that does not decode); a new crew's
//     graph is {team, nodes: null, edges: null} (Q14), its team without
//     entry_node_id or project_id (omitempty); the list narrowed to P;
//   - nodes: an agent node with node_type left out (the default) and with it,
//     a human node for the member (the GET decorates it with user_name; the
//     POST's answer does not), a human node for the outsider, and the
//     identity rules (node_type, label, agent_id, user_id), the member's 403
//     and a crew no one has; positions are Go maps, written with sorted keys;
//   - edges: delegates-to, hands-off-to (a prompt_template config) and
//     reviews, then the graph rules in the service's order: a self-edge, an
//     edge type no crew has, a node of another crew, delegating to a person,
//     a cycle, a chain deeper than 3, a second incoming delegates-to edge,
//     and a repeated reviews edge, which the graph check lets through and the
//     table's unique key refuses with the database's text (Q19);
//   - PUT of the crew (a rename, the entry node, and the refusals: a human
//     entry, another crew's node, an empty name, the member, a crew no one
//     has), of a node (label, department, position; user_id on an agent node,
//     agent_id on a human node, the outsider as a human node's user, a node no
//     one has) and of an edge's config ({} clears it; an edge no one has);
//   - node and edge removal (204; again, 404; the member's 403); a removed
//     node's edges go with it (ON DELETE CASCADE: the imported crew below);
//   - clone: 201 with the team, not the graph; the default crew's clone has
//     its entry node remapped and is_default false, and its graph each node
//     and edge copied, each edge joining the copies; a clone's project_id is
//     checked as create checks a new crew's, so a project no one has is
//     refused (400) and a clone pinned to P, made as setup, stays W's
//     admin's to rename, since the crew-write guard asks for a role in P,
//     which W's admins hold; the clone's graph, whose nodes and edges share
//     one created_at;
//   - export: application/json with Content-Disposition attachment and a
//     filename made from the crew's name (founders-dev-team.crew.json for
//     Founder's Dev Team), human nodes and their edges left out, exported_at
//     minted at the step; any member may export; the not-found text is "crew
//     not found" here and "team not found" on every other crew route;
//   - import: the export's answer, byte for byte, into POST /crews/import:
//     201 {"team":...}, whose team lacks the entry_node_id Import sets just
//     after (the GET shows it); a document naming an agent W does not have
//     (201 with warnings: the node skipped, its edge dropped, and a self-edge
//     rejected); another document kind; a blank name; a body that does not
//     decode; the member workspace-wide (403) and pinned to P (?project_id,
//     201);
//   - launch, POST /crews/{id}/runs: a crew with no entry node, a crew whose
//     entry node was removed (the foreign key clears entry_node_id, so it
//     has none), the member without a project (403), a body that does not
//     decode, no prompt, then 201 in P as W's admin (a run with team_id and
//     team_node_id, at the entry node's agent, and the tracking card's
//     workitem.created), a worker key of W in P (201 with no launched_by: the
//     handler has no user check, and the project guard lets a worker of the
//     project's workspace through) and without a project (the admin guard's
//     401), and the P-pinned crew, which the member, P's editor, gives a node
//     and an entry and launches with no project: the run is scoped to the
//     pin, P, with its tracking card there;
//   - delete: 204, then 404; the member's 403; and Q14's null list, in the
//     owner's personal workspace once its default crew is deleted.
//
// Then every /teams alias with a 2xx (and a refusal where it shows the same
// handler), answering as its /crews twin: the list and a graph read back to
// back with their twins, and no deprecation header on any alias. GET
// /crew-templates has no alias: GET /teams/templates is GET /teams/{id}.
//
// Every 2xx JSON answer is a bare encode (text/plain by sniffing, Q1) except
// the export (application/json); errors are application/json. Crews publish
// no events; only the launches do. Nondeterminism: the default crew's nodes
// were added by ranging over a Go map, so their created_at order changes from
// run to run (unordered /nodes on its graph and export; its edges were added
// in a fixed order and the entry node comes first, so the ids keep their
// numbers); a clone stamps every node and edge with one created_at, so the
// order of both ties (unordered /nodes and /edges: the sort numbers the
// sorted nodes' ids before it keys the edges, so the default crew's copied
// delegations, which differ only in the node each joins, are told apart). The
// templates and the default crew follow the seeds and the presets, so this
// golden changes when they do. The launched runs stay queued: no worker
// claims them here, and no model is ever called (outbound_requests is none).
// Not pinned here: a crew run's successors and hand-offs when a node finishes
// and the budget guard's refusal of a crew launch (Q9), which need a run
// finished over the worker wire (another S5d area's), and the "failed to ..."
// 500s, which need a failing database.
func TestTourS5dCrewsTeams(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "crews_teams",
		about: "Crews and their /teams aliases: the templates, the default crew, create, nodes and edges with the " +
			"graph rules, update, clone, export and import, launch, delete, and each alias answering as its twin.",
		run: crewsTeamsTour,
		accounts: []tourAccount{
			{name: "member", display: "Tour Member", about: "a plain member of W and an editor of P: the human " +
				"nodes, the member refusals, the P-pinned crew and its launch"},
			{name: "outsider", display: "Tour Outsider", about: "no member of W: the human node refused, and the " +
				"reads refused"},
		},
	})
}

// crewsTeamsAgent is an agent definition with a short prompt, for the crews'
// entry node, so that a launch's run holds the area's own agent rather than
// a seeded one, whose prompt changes with feature work.
func crewsTeamsAgent(slug, name string) string {
	b, _ := json.Marshal(map[string]any{
		"slug": slug, "name": name, "description": "An agent of the S5d tour.", "provider": tourDefaultProvider,
		"allowed_tools": []string{"get_artifact"}, "write_mode": "direct", "system_prompt": "You answer in one line.",
	})
	return string(b)
}

func crewsTeamsTour(tr *tour) {
	o, m, x := tr.owner, tr.actor("member"), tr.actor("outsider")
	tr.sharedWorkspace("w", "Tour Shared")
	tr.join(m, "{{w}}", "member")
	const (
		templates  = "GET /api/v1/crew-templates"
		list       = "GET /api/v1/crews"
		create     = "POST /api/v1/crews"
		get        = "GET /api/v1/crews/{id}"
		update     = "PUT /api/v1/crews/{id}"
		remove     = "DELETE /api/v1/crews/{id}"
		clone      = "POST /api/v1/crews/{id}/clone"
		export     = "GET /api/v1/crews/{id}/export"
		imports    = "POST /api/v1/crews/import"
		addNode    = "POST /api/v1/crews/{id}/nodes"
		launch     = "POST /api/v1/crews/{id}/runs"
		updateNode = "PUT /api/v1/crew-nodes/{id}"
		removeNode = "DELETE /api/v1/crew-nodes/{id}"
		addEdge    = "POST /api/v1/crews/{id}/edges"
		updateEdge = "PUT /api/v1/crew-edges/{id}"
		removeEdge = "DELETE /api/v1/crew-edges/{id}"
	)
	id := func(name string) tourOpt { return at("id", "{{"+name+"}}") }
	randomNodes := unordered("/nodes", "the default crew's nodes were added by ranging over a Go map "+
		"(seeds.EnsureOrgDefaults), so their created_at order changes from run to run")

	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Crews"}`)).capture("p", "/id")
	tr.setup("the member edits P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-member@example.com","role":"editor"}`))
	tr.setup("project Q, in the owner's personal workspace", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Elsewhere"}`), actingIn("{{owner.workspace}}")).capture("q", "/id")
	seeded := tr.setup("W's agents, seeded with the workspace", o, "GET /api/v1/agents")
	seeded.captureWhere("chief", "", "slug", "chief-of-staff", "id")
	seeded.captureWhere("analyst", "", "slug", "requirements-analyst", "id")
	seeded.captureWhere("vv", "", "slug", "vv-engineer", "id")
	seeded.captureWhere("developer", "", "slug", "developer", "id")
	seeded.captureWhere("reviewer", "", "slug", "reviewer", "id")
	tr.setup("the agent tour-lead", o, "POST /api/v1/agents", jsonBody(crewsTeamsAgent("tour-lead", "Tour Lead"))).
		capture("lead", "/id")
	box := tr.workerKey("box", "{{w}}", "a worker key of W: a crew launch with no user")

	// (1) The templates.
	tr.step("the crew templates: the built-in presets, each with its portable document", o, templates,
		note("static (crewtemplates.BuiltinCrewTemplates): this step changes when a preset does; exported_at is "+
			"the zero time, which omitempty does not leave out of a struct"))

	// (2) The default crew.
	tr.step("W's crews: the default crew alone, seeded with the workspace", o, list).capture("default", "/0/id")
	tr.step("the default crew's graph: the entry node delegating to three specialists, a reviewer watching the "+
		"developer", o, get, id("default"), randomNodes).capture("default.chief", "/team/entry_node_id")

	// (3) Create.
	tr.step("create a workspace-wide crew as W's admin: 201, the team with no entry_node_id or project_id "+
		"(omitempty)", o, create, jsonBody(`{"name":"Tour Crew","description":"The tour's crew."}`)).
		capture("crew", "/id")
	tr.step("the member creates a crew pinned to P, where it is an editor: 201", m, create,
		jsonBody(`{"name":"Tour P Crew","project_id":"{{p}}"}`)).capture("pcrew", "/id")
	tr.step("the member creates a workspace-wide crew: 403", m, create, jsonBody(`{"name":"Tour Member Crew"}`))
	tr.step("pin a crew to Q, a project of another workspace: 400", o, create,
		jsonBody(`{"name":"Tour Q Crew","project_id":"{{q}}"}`))
	tr.step("pin a crew to a project no one has: 400", o, create,
		jsonBody(`{"name":"Tour Phantom Crew","project_id":"{{phantom}}"}`))
	tr.step("create a crew with no name: 400, the service's text", o, create, jsonBody(`{"description":"Nameless."}`))
	tr.step("create with a body that does not decode", o, create, jsonBody(`{`))
	tr.step("the new crew's graph: nodes and edges null (Q14)", o, get, id("crew"))
	tr.step("W's crews narrowed to P: P's own and the workspace-wide ones, in created_at order", o, list,
		query("project_id={{p}}"))
	tr.step("the box key lists crews: the handler's 401, a key is no user", box, list)

	// (4) Nodes.
	tr.step("add an agent node, node_type left out (agent is the default), with a position whose keys come "+
		"unsorted: written sorted, as a Go map", o, addNode, id("crew"),
		jsonBody(`{"agent_id":"{{lead}}","label":"Lead","department":"Leadership","position":{"org":{"y":0,"x":240},`+
			`"network":{"y":10,"x":20}}}`)).capture("crew.lead", "/id")
	tr.step("add the analyst as an agent node", o, addNode, id("crew"),
		jsonBody(`{"node_type":"agent","agent_id":"{{analyst}}","label":"Analyst","department":"Product"}`)).
		capture("crew.analyst", "/id")
	tr.setup("add the developer", o, addNode, id("crew"),
		jsonBody(`{"agent_id":"{{developer}}","label":"Developer","department":"Engineering"}`)).
		capture("crew.developer", "/id")
	tr.setup("add the reviewer", o, addNode, id("crew"),
		jsonBody(`{"agent_id":"{{reviewer}}","label":"Reviewer","department":"Engineering"}`)).
		capture("crew.reviewer", "/id")
	tr.setup("add the chief of staff", o, addNode, id("crew"),
		jsonBody(`{"agent_id":"{{chief}}","label":"Chief","department":"Leadership"}`)).capture("crew.chief", "/id")
	tr.step("add a human node for the member: 201, with no user_name in the answer", o, addNode, id("crew"),
		jsonBody(`{"node_type":"human","user_id":"{{member}}","label":"Product owner","department":"Product"}`)).
		capture("crew.human", "/id")
	tr.step("add a human node for the outsider, no member of W: 400", o, addNode, id("crew"),
		jsonBody(`{"node_type":"human","user_id":"{{outsider}}","label":"Stranger"}`))
	tr.step("a node_type no crew has: 400", o, addNode, id("crew"),
		jsonBody(`{"node_type":"robot","agent_id":"{{analyst}}","label":"Robot"}`))
	tr.step("an agent node with no label: 400", o, addNode, id("crew"), jsonBody(`{"agent_id":"{{analyst}}"}`))
	tr.step("an agent node with no agent_id: 400", o, addNode, id("crew"), jsonBody(`{"label":"Nobody"}`))
	tr.step("an agent node with a user_id: 400", o, addNode, id("crew"),
		jsonBody(`{"agent_id":"{{analyst}}","user_id":"{{member}}","label":"Both"}`))
	tr.step("a human node with no user_id: 400", o, addNode, id("crew"),
		jsonBody(`{"node_type":"human","label":"Someone"}`))
	tr.step("a human node with an agent_id: 400", o, addNode, id("crew"),
		jsonBody(`{"node_type":"human","user_id":"{{member}}","agent_id":"{{analyst}}","label":"Both"}`))
	tr.step("the member adds a node to the workspace-wide crew: 403, before the body is read", m, addNode, id("crew"),
		jsonBody(`{`))
	tr.step("a node on a crew no one has: 404", o, addNode, id("phantom"),
		jsonBody(`{"agent_id":"{{analyst}}","label":"Analyst"}`))
	tr.step("add a node with a body that does not decode", o, addNode, id("crew"), jsonBody(`{`))
	tr.step("the crew's graph: nodes in created_at order, the human node decorated with its user's name", o, get,
		id("crew"))

	// (5) Edges.
	tr.step("the lead delegates to the analyst: 201, config {} when none is sent", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.lead}}","to_node_id":"{{crew.analyst}}","edge_type":"delegates-to"}`)).
		capture("crew.delegates", "/id")
	tr.step("the analyst hands off to the member, with a prompt template: 201", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.analyst}}","to_node_id":"{{crew.human}}","edge_type":"hands-off-to",`+
			`"config":{"prompt_template":"Check what the analyst drafted.","mode":"card"}}`)).
		capture("crew.handoff", "/id")
	tr.step("the reviewer watches the developer (reviews, from the developer to the reviewer): 201", o, addEdge,
		id("crew"),
		jsonBody(`{"from_node_id":"{{crew.developer}}","to_node_id":"{{crew.reviewer}}","edge_type":"reviews"}`)).
		capture("crew.reviews", "/id")
	tr.step("a self-edge: 400, before the nodes are read", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.lead}}","to_node_id":"{{crew.lead}}","edge_type":"delegates-to"}`))
	tr.step("an edge type no crew has: 400, first", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.lead}}","to_node_id":"{{crew.lead}}","edge_type":"mentors"}`))
	tr.step("an edge from another crew's node: 400", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{default.chief}}","to_node_id":"{{crew.analyst}}","edge_type":"reviews"}`))
	tr.step("delegate to a person: 400", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.lead}}","to_node_id":"{{crew.human}}","edge_type":"delegates-to"}`))
	tr.step("the analyst delegates to the developer, with a note: 201, a chain of two", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.analyst}}","to_node_id":"{{crew.developer}}","edge_type":"delegates-to",`+
			`"config":{"note":"Build what the analyst specified."}}`)).capture("crew.chain", "/id")
	tr.step("the developer delegates back to the lead: 400, a cycle", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.developer}}","to_node_id":"{{crew.lead}}","edge_type":"delegates-to"}`))
	tr.step("the developer delegates to the reviewer: 201, depth 3, the most there may be", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.developer}}","to_node_id":"{{crew.reviewer}}","edge_type":"delegates-to"}`)).
		capture("crew.deep", "/id")
	tr.step("the reviewer delegates to the chief: 400, depth 4", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.reviewer}}","to_node_id":"{{crew.chief}}","edge_type":"delegates-to"}`))
	tr.step("the chief delegates to the analyst, which the lead delegates to: 400, a second incoming "+
		"delegates-to edge, the node named by its id", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.chief}}","to_node_id":"{{crew.analyst}}","edge_type":"delegates-to"}`))
	tr.step("the developer reviews the reviewer again: the graph check lets it through and the unique key "+
		"refuses it, the database's text passed through (Q19)", o, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.developer}}","to_node_id":"{{crew.reviewer}}","edge_type":"reviews"}`))
	tr.step("the member adds an edge to the workspace-wide crew: 403", m, addEdge, id("crew"),
		jsonBody(`{"from_node_id":"{{crew.lead}}","to_node_id":"{{crew.chief}}","edge_type":"reviews"}`))
	tr.step("an edge on a crew no one has: 404", o, addEdge, id("phantom"),
		jsonBody(`{"from_node_id":"{{crew.lead}}","to_node_id":"{{crew.chief}}","edge_type":"reviews"}`))
	tr.step("add an edge with a body that does not decode", o, addEdge, id("crew"), jsonBody(`{`))

	// (6) PUT the crew.
	tr.step("rename the crew: 200, the team", o, update, id("crew"),
		jsonBody(`{"name":"Tour Crew Renamed","description":"Led by the tour."}`))
	tr.step("make the lead the entry node: 200", o, update, id("crew"), jsonBody(`{"entry_node_id":"{{crew.lead}}"}`))
	tr.step("make the member's node the entry: 400", o, update, id("crew"),
		jsonBody(`{"entry_node_id":"{{crew.human}}"}`))
	tr.step("make another crew's node the entry: 400", o, update, id("crew"),
		jsonBody(`{"entry_node_id":"{{default.chief}}"}`))
	tr.step("an empty name: 400", o, update, id("crew"), jsonBody(`{"name":""}`))
	tr.step("the member renames the workspace-wide crew: 403, before the body is read", m, update, id("crew"),
		jsonBody(`{`))
	tr.step("rename a crew no one has: 404", o, update, id("phantom"), jsonBody(`{"name":"Ghost"}`))
	tr.step("rename with a body that does not decode", o, update, id("crew"), jsonBody(`{`))

	// (7) PUT a node, an edge's config.
	tr.step("relabel the analyst and place it: 200, the position's keys sorted", o, updateNode, id("crew.analyst"),
		jsonBody(`{"label":"Senior Analyst","department":"Requirements","position":{"org":{"y":120,"x":0}}}`))
	tr.step("set user_id on an agent node: 400", o, updateNode, id("crew.lead"), jsonBody(`{"user_id":"{{member}}"}`))
	tr.step("set agent_id on a human node: 400", o, updateNode, id("crew.human"), jsonBody(`{"agent_id":"{{chief}}"}`))
	tr.step("make the outsider the human node's user: 400", o, updateNode, id("crew.human"),
		jsonBody(`{"user_id":"{{outsider}}"}`))
	tr.step("an empty label: 400", o, updateNode, id("crew.lead"), jsonBody(`{"label":""}`))
	tr.step("the member relabels a node of the workspace-wide crew: 403", m, updateNode, id("crew.lead"),
		jsonBody(`{"label":"Mine"}`))
	tr.step("a node no one has: 404", o, updateNode, id("phantom"), jsonBody(`{"label":"Ghost"}`))
	tr.step("the hand-off's prompt template, changed: 200, the config replaced whole", o, updateEdge,
		id("crew.handoff"), jsonBody(`{"config":{"prompt_template":"Check the analyst's draft again."}}`))
	tr.step("the hand-off's config sent as {}: 200, config {}, cleared", o, updateEdge, id("crew.handoff"),
		jsonBody(`{}`))
	tr.step("the member changes an edge of the workspace-wide crew: 403", m, updateEdge, id("crew.handoff"),
		jsonBody(`{"config":{}}`))
	tr.step("an edge no one has: 404", o, updateEdge, id("phantom"), jsonBody(`{"config":{}}`))
	tr.step("change an edge with a body that does not decode", o, updateEdge, id("crew.handoff"), jsonBody(`{`))

	// Removal of an edge and a node.
	tr.step("remove the depth-3 delegation: 204", o, removeEdge, id("crew.deep"))
	tr.step("remove it again: 404", o, removeEdge, id("crew.deep"))
	tr.step("the member removes an edge of the workspace-wide crew: 403", m, removeEdge, id("crew.reviews"))
	tr.step("remove the chief's node: 204", o, removeNode, id("crew.chief"))
	tr.step("remove it again: 404", o, removeNode, id("crew.chief"))
	tr.step("the member removes a node of the workspace-wide crew: 403", m, removeNode, id("crew.lead"))
	tr.step("the crew's graph: renamed, the lead its entry, the analyst relabelled, the chief and the deep "+
		"delegation gone", o, get, id("crew"))

	// (8) Clone.
	copied := tr.step("clone the default crew: 201, the team (not its graph), its entry node remapped, is_default "+
		"false", o, clone, id("default"), jsonBody(`{"name":"Tour Founders Copy"}`))
	copied.capture("copy", "/id")
	copied.capture("copy.chief", "/entry_node_id")
	tr.step("the default crew's clone's graph: its five nodes and four edges copied under new ids with one "+
		"created_at, the entry node the copy of the chief, each edge joining the copies of the nodes it joined",
		o, get, id("copy"),
		unordered("/nodes", "CloneTeam stamps every node with one created_at, so ORDER BY created_at ties"),
		unordered("/edges", "CloneTeam stamps every edge with one created_at, so ORDER BY created_at ties; the "+
			"chief's three delegations differ only in the node each joins, numbered in the sorted /nodes"))
	tr.step("clone the tour's crew pinned to a project no one has: 400, the project checked as create checks it",
		o, clone, id("crew"), jsonBody(`{"name":"Tour Crew Copy","project_id":"{{phantom}}"}`))
	pinned := tr.setup("clone the tour's crew pinned to P", o, clone, id("crew"),
		jsonBody(`{"name":"Tour Crew Copy","project_id":"{{p}}"}`), expect(201))
	pinned.capture("pcopy", "/id")
	pinned.capture("pcopy.lead", "/entry_node_id")
	tr.step("the clone pinned to P: its graph, every node and edge copied under new ids with one created_at", o, get,
		id("pcopy"),
		unordered("/nodes", "CloneTeam stamps every node with one created_at, so ORDER BY created_at ties"),
		unordered("/edges", "CloneTeam stamps every edge with one created_at, so ORDER BY created_at ties"))
	tr.step("W's admin renames the clone pinned to P: 200, the crew-write guard's role in P, which W's admins hold",
		o, update, id("pcopy"), jsonBody(`{"name":"Tour P Copy"}`))
	tr.step("clone with no name: 400", o, clone, id("crew"), jsonBody(`{}`))
	tr.step("the member clones the workspace-wide crew: 403", m, clone, id("crew"), jsonBody(`{"name":"Mine"}`))
	tr.step("clone a crew no one has: 404", o, clone, id("phantom"), jsonBody(`{"name":"Ghost"}`))
	tr.step("clone with a body that does not decode", o, clone, id("crew"), jsonBody(`{`))

	// (9) Export.
	exported := tr.step("export the tour's crew: application/json, an attachment named after the crew; the human "+
		"node and its hand-off left out; exported_at minted now", o, export, id("crew"))
	tr.step("export the default crew: founders-dev-team.crew.json, the apostrophe dropped", o, export,
		id("default"), randomNodes)
	tr.step("the member exports: any member of W may", m, export, id("crew"))
	tr.step("the outsider exports W's crew: 404, as a crew no row has", x, export, id("crew"))
	tr.step("export a crew no one has: 404, \"crew not found\" (GET says \"team not found\")", o, export,
		id("phantom"))
	tr.step("the outsider reads W's crew: 404, as a crew no row has", x, get, id("crew"))
	tr.step("read a crew no one has: 404", o, get, id("phantom"))

	// (10) Import.
	tr.step("the member imports workspace-wide: 403, before the body is read", m, imports, jsonBody(`{`))
	imported := tr.step("import the export, byte for byte: 201 {\"team\":...}, whose team has no entry_node_id, "+
		"though Import sets one just after", o, imports, answerOf(exported, "application/json"))
	imported.capture("imported", "/team/id")
	tr.step("the imported crew: the agent nodes and their edges, the lead its entry", o, get, id("imported")).
		capture("imported.lead", "/team/entry_node_id")
	tr.step("import a document naming an agent W does not have: 201 with warnings, the node skipped, its edge "+
		"dropped, a self-edge rejected", o, imports, jsonBody(`{"kind":"openv.crew","version":"1.0",`+
		`"name":"Tour Partial","entry_node_key":"lead","nodes":[{"key":"lead","agent_slug":"tour-lead",`+
		`"label":"Lead"},{"key":"ghost","agent_slug":"no-such-agent","label":"Ghost"}],"edges":[{"from":"lead",`+
		`"to":"ghost","edge_type":"delegates-to"},{"from":"lead","to":"lead","edge_type":"reviews"}]}`)).
		capture("partial", "/team/id")
	tr.step("import another document kind: 400", o, imports, jsonBody(`{"kind":"openv.team","name":"Tour Other"}`))
	tr.step("import with a blank name: 400", o, imports, jsonBody(`{"kind":"openv.crew","name":"  "}`))
	tr.step("import a body that does not decode", o, imports, jsonBody(`{`))
	tr.step("import into Q, a project of another workspace: 400", o, imports, query("project_id={{q}}"),
		jsonBody(`{"name":"Tour Q Import"}`))
	tr.step("the member imports into P (?project_id), where it is an editor: 201, the crew pinned to P", m, imports,
		query("project_id={{p}}"), jsonBody(`{"kind":"openv.crew","version":"1.0","name":"Tour Pair",`+
			`"entry_node_key":"a","nodes":[{"key":"a","agent_slug":"requirements-analyst","label":"Analyst"},`+
			`{"key":"b","agent_slug":"vv-engineer","label":"V&V"}],"edges":[{"from":"a","to":"b",`+
			`"edge_type":"hands-off-to"}]}`)).capture("pair", "/team/id")
	tr.step("the pair: the label V&V written V\\u0026V (HTML escaping, I4)", m, get, id("pair"))

	// (11) Launch.
	tr.step("launch the P crew, which has no entry node: 400", m, launch, id("pcrew"),
		jsonBody(`{"prompt":"Plan P."}`))
	tr.setup("remove the imported crew's entry node", o, removeNode, id("imported.lead"))
	tr.step("the imported crew has no entry_node_id: the foreign key cleared it with the node", o, get, id("imported"))
	tr.step("launch it: 400, it has no entry node", o, launch, id("imported"),
		jsonBody(`{"prompt":"Plan W."}`))
	tr.step("the member launches the workspace-wide crew with no project: 403", m, launch, id("crew"),
		jsonBody(`{"prompt":"Plan W."}`))
	tr.step("launch with a body that does not decode: 400, after the entry node is found", o, launch, id("crew"),
		jsonBody(`{`))
	tr.step("launch with no prompt: 400, the service's text", o, launch, id("crew"),
		jsonBody(`{"project_id":"{{p}}","prompt":""}`))
	tr.step("launch a crew no one has: 404", o, launch, id("phantom"), jsonBody(`{"prompt":"Plan."}`))
	run := tr.step("launch the tour's crew in P: 201, a run of the entry node's agent with team_id and "+
		"team_node_id, and the board's tracking card", o, launch, id("crew"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Plan P's next release."}`))
	run.capture("crun", "/id")
	run.capture("crun.card", "/work_item_id")
	boxRun := tr.step("the box key launches it in P: 201 with no launched_by (no user check; the project guard "+
		"lets a worker of P's workspace through)", box, launch, id("crew"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Plan P from the box."}`))
	boxRun.capture("crun.box", "/id")
	boxRun.capture("crun.box.card", "/work_item_id")
	tr.step("the box key launches it with no project: the admin guard's 401", box, launch, id("crew"),
		jsonBody(`{"prompt":"Plan W from the box."}`))
	tr.step("the member, P's editor, puts the lead on the P crew: 201, the pinned crew's write guard", m, addNode,
		id("pcrew"), jsonBody(`{"agent_id":"{{lead}}","label":"Lead"}`)).capture("pcrew.lead", "/id")
	tr.step("the member makes it the entry: 200", m, update, id("pcrew"),
		jsonBody(`{"entry_node_id":"{{pcrew.lead}}"}`))
	pinnedRun := tr.step("the member launches the P crew with no project: 201, the run scoped to the pin, P, and "+
		"the board's tracking card there", m, launch, id("pcrew"), jsonBody(`{"prompt":"Plan my week."}`))
	pinnedRun.capture("crun.pinned", "/id")
	pinnedRun.capture("crun.pinned.card", "/work_item_id")

	// (12) Delete.
	tr.step("the member deletes the workspace-wide crew: 403", m, remove, id("crew"))
	tr.step("delete the default crew's copy: 204", o, remove, id("copy"))
	tr.step("the copy: 404", o, get, id("copy"))
	tr.step("delete it again: 404", o, remove, id("copy"))
	tr.step("W's crews, in created_at order", o, list)
	home := tr.setup("the owner's personal crews", o, list, actingIn("{{owner.workspace}}"))
	home.capture("home.default", "/0/id")
	tr.step("delete the owner's personal default crew: 204", o, remove, id("home.default"),
		actingIn("{{owner.workspace}}"))
	tr.step("the owner's personal crews: none, null (Q14)", o, list, actingIn("{{owner.workspace}}"))

	// The /teams aliases, each answering as its /crews twin.
	alias := note("a deprecated alias of the /crews route: the same handler, and no deprecation header")
	tr.step("W's crews through the canonical route", o, list)
	tr.step("the same through /teams: the same bytes", o, "GET /api/v1/teams", alias)
	tr.step("create a crew through /teams: 201", o, "POST /api/v1/teams",
		jsonBody(`{"name":"Tour Team","description":"Made through the alias."}`), alias).capture("team", "/id")
	tr.step("the member creates one workspace-wide through /teams: 403", m, "POST /api/v1/teams",
		jsonBody(`{"name":"Tour Member Team"}`), alias)
	tr.step("its graph through /teams: nodes and edges null", o, "GET /api/v1/teams/{id}", id("team"), alias)
	tr.step("add the lead through /teams: 201", o, "POST /api/v1/teams/{id}/nodes", id("team"),
		jsonBody(`{"agent_id":"{{lead}}","label":"Lead"}`), alias).capture("team.lead", "/id")
	tr.step("add the analyst through /teams: 201", o, "POST /api/v1/teams/{id}/nodes", id("team"),
		jsonBody(`{"agent_id":"{{analyst}}","label":"Analyst"}`), alias).capture("team.analyst", "/id")
	tr.step("a node with no label through /teams: 400", o, "POST /api/v1/teams/{id}/nodes", id("team"),
		jsonBody(`{"agent_id":"{{analyst}}"}`), alias)
	tr.step("relabel the analyst through /team-nodes: 200", o, "PUT /api/v1/team-nodes/{id}", id("team.analyst"),
		jsonBody(`{"label":"Team Analyst","position":{"network":{"y":1,"x":2}}}`), alias)
	tr.step("a node no one has through /team-nodes: 404", o, "PUT /api/v1/team-nodes/{id}", id("phantom"),
		jsonBody(`{"label":"Ghost"}`), alias)
	tr.step("the lead delegates to the analyst through /teams: 201", o, "POST /api/v1/teams/{id}/edges", id("team"),
		jsonBody(`{"from_node_id":"{{team.lead}}","to_node_id":"{{team.analyst}}","edge_type":"delegates-to"}`),
		alias).capture("team.edge", "/id")
	tr.step("a self-edge through /teams: 400", o, "POST /api/v1/teams/{id}/edges", id("team"),
		jsonBody(`{"from_node_id":"{{team.lead}}","to_node_id":"{{team.lead}}","edge_type":"reviews"}`), alias)
	tr.step("its config through /team-edges: 200", o, "PUT /api/v1/team-edges/{id}", id("team.edge"),
		jsonBody(`{"config":{"prompt_template":"Draft the requirements."}}`), alias)
	tr.step("an edge no one has through /team-edges: 404", o, "PUT /api/v1/team-edges/{id}", id("phantom"),
		jsonBody(`{"config":{}}`), alias)
	tr.step("rename it and make the lead its entry through /teams: 200", o, "PUT /api/v1/teams/{id}", id("team"),
		jsonBody(`{"name":"Tour Team Renamed","entry_node_id":"{{team.lead}}"}`), alias)
	tr.step("its graph through the canonical route", o, get, id("team"))
	tr.step("the same through /teams: the same bytes", o, "GET /api/v1/teams/{id}", id("team"), alias)
	tr.step("GET /teams/templates is GET /teams/{id}, since /crew-templates has no alias: 404", o,
		"GET /api/v1/teams/{id}", at("id", "templates"), alias)
	tr.step("clone it through /teams: 201", o, "POST /api/v1/teams/{id}/clone", id("team"),
		jsonBody(`{"name":"Tour Team Copy"}`), alias).capture("team.copy", "/id")
	teamExport := tr.step("export it through /teams: the same headers, tour-team-renamed.crew.json", o,
		"GET /api/v1/teams/{id}/export", id("team"), alias)
	tr.step("export a crew no one has through /teams: 404, crew not found", o, "GET /api/v1/teams/{id}/export",
		id("phantom"), alias)
	tr.step("import that export through /teams: 201", o, "POST /api/v1/teams/import",
		answerOf(teamExport, "application/json"), alias).capture("team.imported", "/team/id")
	tr.step("the member imports workspace-wide through /teams: 403", m, "POST /api/v1/teams/import",
		jsonBody(`{"name":"Tour Member Import"}`), alias)
	teamRun := tr.step("launch it in P through /teams: 201, a run at its entry node, and the tracking card", o,
		"POST /api/v1/teams/{id}/runs", id("team"), jsonBody(`{"project_id":"{{p}}","prompt":"Draft P's goals."}`),
		alias)
	teamRun.capture("trun", "/id")
	teamRun.capture("trun.card", "/work_item_id")
	tr.step("the member launches it with no project through /teams: 403", m, "POST /api/v1/teams/{id}/runs",
		id("team"), jsonBody(`{"prompt":"Draft W's goals."}`), alias)
	tr.step("remove the delegation through /team-edges: 204", o, "DELETE /api/v1/team-edges/{id}", id("team.edge"),
		alias)
	tr.step("remove it again through /team-edges: 404", o, "DELETE /api/v1/team-edges/{id}", id("team.edge"), alias)
	tr.step("remove the analyst through /team-nodes: 204", o, "DELETE /api/v1/team-nodes/{id}", id("team.analyst"),
		alias)
	tr.step("remove it again through /team-nodes: 404", o, "DELETE /api/v1/team-nodes/{id}", id("team.analyst"),
		alias)
	tr.step("delete the crew through /teams: 204", o, "DELETE /api/v1/teams/{id}", id("team"), alias)
	tr.step("delete it again through /teams: 404", o, "DELETE /api/v1/teams/{id}", id("team"), alias)
	tr.step("W's crews at the end, through /teams", o, "GET /api/v1/teams", alias)
}
