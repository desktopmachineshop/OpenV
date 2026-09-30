//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestTourS5cMembersTeams is the S5c tour's members and teams area (refactor
// plan §6.4 S5c, before M9, which splits org_handlers.go into
// org_member_handlers.go, org_team_handlers.go and
// project_team_access_handlers.go, and before M4, X8 and X12; invariants I3
// (the guard's order against the lookup), I4, I5, I10 (the events' JSON
// types); quirks Q1 (bare encodes with no Content-Type), Q14 (null against
// []) and Q19 (a domain error's text as the answer); OpenV REQ-18, REQ-95,
// REQ-143). Its golden is testdata/tour/s5c/members_teams.json.
//
// It takes the eighteen routes of a workspace's people: its members (GET,
// POST, PUT and DELETE /api/v1/orgs/{id}/members), its people-teams (GET and
// POST /api/v1/orgs/{id}/teams, PUT and DELETE /api/v1/org-teams/{id} and
// the team's members), a project's members (GET, POST, PUT and DELETE
// /api/v1/projects/{id}/members), a project's team grants
// (/api/v1/projects/{id}/team-access), and GET /api/v1/users, the active
// workspace's members for the invitation pickers.
//
// Accounts: owner, whose shared workspace W the area works in and which
// reads W's events after every step; m, a plain member of W; a2, which joins
// W with no role and is then made its second admin; and x, an account in no
// workspace the owner shares. m and a2 carry the same name, Tour Peer,
// which sorts after the owner's, so a list ordered by name, then address
// (m's is tour-m@, a2's tour-a2@) differs from one ordered by address alone
// and from one in the order they joined (the owner, m, a2). A second shared
// workspace V, with a team tV, is the owner's too, for the cross-workspace
// refusals; P is a project of W's (S5a's POST /api/v1/projects, as setup).
//
// The area walks, in order:
//   - W's members: the owner adds m with a recorded step (org.member_added
//     {role, user_id}; the 201's membership carries created_at as the zero
//     time, since the handler builds it without reading the row back); a
//     second add, the address in capitals and padded (the same account, 409)
//     and one with a role no workspace has (still 409, since the membership
//     is checked before the role); a2 with a bad role
//     (400, the membership path's text, which quotes the role) and with none
//     (a member); an address no account has (202 with the invitation branch:
//     the row, its one-time link, emailed false with no mail server;
//     org.invitation_sent), with a bad role (400, the invitation path's text,
//     which does not quote it), and not an address at all; the owner's
//     personal workspace (400 on both branches); a plain member's add (403,
//     in the guard's words) and an outsider's (404: to x, W is a workspace no
//     row has, I3); the list as m (by name, then address), as x (404), with
//     W's worker key (401: the guard wants an account), and of a workspace no
//     row has or an id that is not a UUID (the same 404);
//   - roles: the owner, W's only admin, can neither be demoted (400 with
//     SetMemberRole's ErrLastAdmin text, Q19) nor leave (400 with
//     RemoveMember's text, Q19); a2 is made an admin
//     (org.member_role_changed {from, to, user_id}); a role no workspace has,
//     an account that is not in W and an id no account has (400, the latter
//     two with ErrNotMember's "you are not a member of this organization",
//     which is about somebody else here); a malformed body; m's change
//     (403); and a2, now an admin, setting m's role to what it already is
//     (204, and an event from member to member);
//   - W's people-teams: none (null, Q14); two made (201 with the team, the
//     name trimmed, no members key while it has none; the second with a
//     description long enough to put the lists over the compressor's floor),
//     a blank name (400,
//     the domain's text), a malformed body, a member's (403) and an
//     outsider's (404); the list by name; a rename (200 with the team as stored, its
//     members left out), a blank name that keeps the old one, a malformed
//     body; the guard's order (orgTeamChecked looks the team up first, so a
//     member renaming a real team gets 403, and anyone naming a team no row
//     has, or an id that is not a UUID, 404 "team not found", as does x, not
//     in W, and a member renaming V's team, not in V: I3); the team's members (201 with no body, twice
//     for the same account, which the store ignores; an account outside W and
//     an id no account has, 400 with the domain's text; m into V's team,
//     400; a member's add, 403; a team no row has, 404), the list with each
//     team's members by name, then address, and their removal (204, also for
//     one never in the team; 403 for a member; 404 for a phantom team);
//   - P's members: the owner, who made it; m added as a viewer (201 with no
//     body, project.member_added), who then reads the list; a viewer's add
//     (403), an address no account has (404, the text with its em dash), a
//     role no project has (400, the domain's text, quoting the role), a
//     malformed body; x, in no workspace of the owner's, added all the same
//     (a project's members are its own: REQ-16's per-project grants, the
//     supplier of docs/flow-down.md), who then reads P's members; W's worker
//     key reading them (200: a worker passes for any project of its
//     workspace); m made an editor (204, project.member_role_changed); a bad
//     role, a malformed body, m's change of its own role (403); a role given
//     to a2, who is not a member of P, which adds it (from ""), and to an id
//     no account has (404 "user not found"); a2, an admin of W
//     and so an owner of P, leaving P (204, project.member_removed, self
//     true); x removed (self false), and removed again (204 and an event all
//     the same); m removing the owner (403); x reading P once gone (404, as a
//     project no row has);
//   - P's team grants: none (null); a team granted (201 with no body), a
//     second, the first again with another role (201, replaced); V's team
//     (400 "team belongs to a different workspace"), a team no row has and
//     an id that is not a UUID (404), a role no project has (400), a
//     malformed body, and m's grant (403); the grants by team name, as the
//     owner and as m; revocation (204, also for a team revoked already, one
//     never granted and one no row has; 403 for m); and a team deleted
//     (204; then 404; m's deletion of a real team 403 and of a phantom 404)
//     taking its grant with it;
//   - GET /api/v1/users: W's members as id, name, address and avatar, the
//     keys in encoding/json's map order; the same request with X-Org-ID
//     naming the owner's personal workspace (the owner alone), x's (x
//     alone), a worker key (401 "authentication required", the handler's)
//     and no session (the middleware's 401);
//   - leaving W last: m removing a2 (403), x leaving W, which it is not in
//     (404), an id no account has (400 with ErrNotMember's words, as its
//     role change, and no event), m leaving (204, self true), the owner
//     removing a2, then one of two admins (204, self false); W's members
//     afterwards; t, which still lists m with an empty role (its members are
//     read with a left join on W's members, and leaving W leaves the team);
//     and m, gone from W, still reading P, whose direct membership leaving W
//     does not end.
//
// Every success answer here is a bare json.NewEncoder(w).Encode, so it has
// the Content-Type Go sniffs (text/plain; charset=utf-8), the 201 and 202
// bodies of POST /orgs/{id}/members too, which write their status first;
// the 201s and 204s of the team members, grants and project members have no
// body and no Content-Type (Q1). Each GET is also sent with gzip: the team
// lists that hold the second team's long description are compressed, and
// their gzip variant then carries no Content-Type at all (Q1), while every
// other answer here is under the floor and is not compressed. The
// events are read in W, which the owner acts in: org events (the owner is
// W's admin) and P's project events; the people-team and team-grant routes
// publish none, which the golden shows as no events.
//
// Nondeterminism: ids, the invitation's token and times (the generic
// tokens); nothing of the area's own. Not pinned: the notifications these
// events queue (the notifier writes them after the answer, and no step
// reads them; notifications_push does), the invitation's mail (no mail
// server here: emailed false; the invitations area has the catcher), the
// seat limits (tiers are off here: workspace_tiers_on pins them), and the
// platform admin, which passes every guard (S5e's matrix).
func TestTourS5cMembersTeams(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "members_teams",
		about: "Members and teams: a workspace's members and their roles, its people-teams and their members, a " +
			"project's members and team grants, and the active workspace's members (GET /api/v1/users).",
		run: membersTeamsTour,
	})
}

// membersTeamsLong is the second team's description: its 1,480 bytes put
// each list that holds it over the compressor's 1,400-byte floor, so that
// the golden shows a bare encode's gzip variant too (Q1).
var membersTeamsLong = strings.Repeat("Writes and reviews the requirements. ", 40)

// membersTeamsMember is the body that adds an address with a role.
func membersTeamsMember(email, role string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"email":%q,"role":%q}`, email, role))
}

// membersTeamsRole is the body that sets a role.
func membersTeamsRole(role string) tourOpt { return jsonBody(fmt.Sprintf(`{"role":%q}`, role)) }

// membersTeamsGrant is the body that grants a team a role on a project.
func membersTeamsGrant(team, role string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"org_team_id":%q,"role":%q}`, team, role))
}

func membersTeamsTour(tr *tour) {
	o, anon := tr.owner, tr.anon
	m := tr.register("m", "Tour Peer", "a plain member of W, then a viewer and an editor of P; leaves W at the end")
	a2 := tr.register("a2", "Tour Peer", "joins W with no role and becomes its second admin; the same name as m's, "+
		"so lists show their order by address")
	x := tr.register("x", "Tour Outsider", "an account in no workspace the owner shares")

	// V, the owner's other shared workspace, and its team, for the
	// cross-workspace refusals; then W, where the owner acts from here on.
	tr.sharedWorkspace("v", "Tour Elsewhere")
	tr.setup("V's team", o, "POST /api/v1/orgs/{id}/teams", at("id", "{{v}}"), jsonBody(`{"name":"Tour Elsewhere Team"}`),
		expect(201)).capture("tv", "/id")
	tr.sharedWorkspace("w", "Tour Workshop")
	tr.setup("project P, in W", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Plant"}`)).capture("p", "/id")
	worker := tr.bearerActor("worker", tr.setup("a worker key of W's", o, "POST /api/v1/orgs/{id}/worker-keys",
		at("id", "{{w}}"), jsonBody(`{"name":"tour worker"}`)).value("/key"), "a worker key of W's, as a bearer")

	// W's members.
	const members = "POST /api/v1/orgs/{id}/members"
	inW := at("id", "{{w}}")
	tr.step("W's members before anyone joins: the owner alone", o, "GET /api/v1/orgs/{id}/members", inW)
	tr.step("add m, who has an account: 201 with the membership; org.member_added", o, members, inW,
		membersTeamsMember(m.email, "member"),
		note("the membership is built in the handler, not read back, so its created_at is the zero time"))
	tr.actIn(m, "{{w}}")
	tr.step("add m again, the address in capitals and padded: the same account, 409", o, members, inW,
		membersTeamsMember(" TOUR-M@Example.com ", "member"))
	tr.step("add m again with a role no workspace has: still 409, the membership is checked before the role", o,
		members, inW, membersTeamsMember(m.email, "boss"))
	tr.step("add a2 with a role no workspace has: 400, the membership path's text, which quotes the role", o,
		members, inW, membersTeamsMember(a2.email, "boss"))
	tr.step("add a2 with no role: a member", o, members, inW, jsonBody(`{"email":"tour-a2@example.com"}`))
	tr.actIn(a2, "{{w}}")
	tr.step("bring in an address no account has: 202 with an invitation and its link, not mailed (no mail server); "+
		"org.invitation_sent", o, members, inW, membersTeamsMember("tour-invitee@example.com", "member"))
	tr.step("the same address with a role no workspace has: 400, the invitation path's text, which does not quote it",
		o, members, inW, membersTeamsMember("tour-invitee@example.com", "boss"))
	tr.step("something that is not an address: 400", o, members, inW, membersTeamsMember("not-an-address", "member"))
	tr.step("a malformed body", o, members, inW, jsonBody(`{`))
	tr.step("add m to the owner's personal workspace: 400", o, members, at("id", "{{owner.workspace}}"),
		membersTeamsMember(m.email, "member"))
	tr.step("invite an address no account has to it: the same 400, from the invitation path", o, members,
		at("id", "{{owner.workspace}}"), membersTeamsMember("tour-invitee@example.com", "member"))
	tr.step("m, a plain member, adds somebody: 403", m, members, inW, membersTeamsMember(x.email, "member"))
	tr.step("x, not in W, adds itself: 404, as for a workspace no row has", x, members, inW, membersTeamsMember(x.email, "member"))
	tr.step("W's members as m: by name, then address", m, "GET /api/v1/orgs/{id}/members", inW)
	tr.step("W's members as x, not in W: 404", x, "GET /api/v1/orgs/{id}/members", inW)
	tr.step("the members of a workspace no row has: 404, the answer x gets for W", o, "GET /api/v1/orgs/{id}/members",
		at("id", "{{phantom}}"))
	tr.step("the members of a workspace id that is not a UUID: the same 404", o,
		"GET /api/v1/orgs/{id}/members", at("id", "not-a-workspace"))
	tr.step("W's members with W's worker key: 401, the guard wants an account", worker, "GET /api/v1/orgs/{id}/members", inW)

	// Roles. The owner is W's only admin until a2 is made one.
	const role = "PUT /api/v1/orgs/{id}/members/{userId}"
	tr.step("demote the owner, W's only admin: 400 with the domain's text, as its leaving (Q19)", o, role,
		at("id", "{{w}}", "userId", "{{owner}}"), membersTeamsRole("member"))
	tr.step("the owner leaves W, its only admin: 400 with the domain's text (Q19)", o,
		"DELETE /api/v1/orgs/{id}/members/{userId}", at("id", "{{w}}", "userId", "{{owner}}"))
	tr.step("make a2 an admin: 204; org.member_role_changed from member to admin", o, role,
		at("id", "{{w}}", "userId", "{{a2}}"), membersTeamsRole("admin"))
	tr.step("a role no workspace has: 400", o, role, at("id", "{{w}}", "userId", "{{m}}"), membersTeamsRole("boss"))
	tr.step("x, who is not in W: 400, in words about the caller", o, role, at("id", "{{w}}", "userId", "{{x}}"),
		membersTeamsRole("member"))
	tr.step("an id no account has: the same 400", o, role, at("id", "{{w}}", "userId", "{{phantom}}"),
		membersTeamsRole("member"))
	tr.step("a malformed body", o, role, at("id", "{{w}}", "userId", "{{m}}"), jsonBody(`{`))
	tr.step("m makes itself an admin: 403", m, role, at("id", "{{w}}", "userId", "{{m}}"), membersTeamsRole("admin"))
	tr.step("a2, now an admin, sets m's role to the one it has: 204, and an event from member to member", a2, role,
		at("id", "{{w}}", "userId", "{{m}}"), membersTeamsRole("member"))

	// W's people-teams. No team route publishes an event.
	const teams, team, teamMember = "POST /api/v1/orgs/{id}/teams", "PUT /api/v1/org-teams/{id}",
		"POST /api/v1/org-teams/{id}/members/{userId}"
	tr.step("W's teams: none, written null (Q14)", o, "GET /api/v1/orgs/{id}/teams", inW)
	tr.step("make a team: 201 with the team, its name trimmed, and no members key while it has none", o, teams, inW,
		jsonBody(`{"name":"  Tour Reviewers ","description":"reads everything"}`)).capture("t", "/id")
	tr.step("make a second team, with a description long enough that the lists holding it are compressed", o, teams,
		inW, jsonBody(fmt.Sprintf(`{"name":"Tour Authors","description":%q}`, membersTeamsLong))).capture("t2", "/id")
	tr.step("a blank name: 400 with the domain's text", o, teams, inW, jsonBody(`{"name":"   "}`))
	tr.step("a malformed body", o, teams, inW, jsonBody(`{`))
	tr.step("m, a plain member, makes a team: 403", m, teams, inW, jsonBody(`{"name":"Tour Members"}`))
	tr.step("x, not in W, makes one: 404", x, teams, inW, jsonBody(`{"name":"Tour Outsiders"}`))
	tr.step("W's teams as m: by name; over 1,400 bytes, so the gzip variant is compressed, and then has no "+
		"Content-Type at all (Q1)", m, "GET /api/v1/orgs/{id}/teams", inW)
	tr.step("rename the first team: 200 with the team as stored, its members left out", o, team, at("id", "{{t}}"),
		jsonBody(`{"name":"Tour Reviewers and Approvers"}`))
	tr.step("a blank name and an empty description: the name kept, the description cleared", o, team,
		at("id", "{{t}}"), jsonBody(`{"name":"  ","description":""}`))
	tr.step("a malformed body", o, team, at("id", "{{t}}"), jsonBody(`{`))
	tr.step("m renames it: 403, the team looked up first", m, team, at("id", "{{t}}"), jsonBody(`{"name":"Mine"}`))
	tr.step("m renames a team no row has: 404, before any role check (I3)", m, team, at("id", "{{phantom}}"),
		jsonBody(`{"name":"Mine"}`))
	tr.step("the owner renames a team no row has: the same 404", o, team, at("id", "{{phantom}}"),
		jsonBody(`{"name":"Mine"}`))
	tr.step("an id that is not a UUID: 404 too", o, team, at("id", "not-a-team"), jsonBody(`{"name":"Mine"}`))
	tr.step("x, not in W, renames W's team: 404, as a team no row has", x, team, at("id", "{{t}}"), jsonBody(`{"name":"Mine"}`))
	tr.step("m renames V's team: 404, not a member of V", m, team, at("id", "{{tv}}"), jsonBody(`{"name":"Mine"}`))
	tr.step("add m to the team: 201 with no body", o, teamMember, at("id", "{{t}}", "userId", "{{m}}"))
	tr.step("add m again: 201, the store ignores the repeat", o, teamMember, at("id", "{{t}}", "userId", "{{m}}"))
	tr.step("add a2", o, teamMember, at("id", "{{t}}", "userId", "{{a2}}"))
	tr.step("add x, not in W: 400 with the domain's text", o, teamMember, at("id", "{{t}}", "userId", "{{x}}"))
	tr.step("add an id no account has: the same 400", o, teamMember, at("id", "{{t}}", "userId", "{{phantom}}"))
	tr.step("add m to V's team: 400, m is not in V", o, teamMember, at("id", "{{tv}}", "userId", "{{m}}"))
	tr.step("m adds a2 to the second team: 403", m, teamMember, at("id", "{{t2}}", "userId", "{{a2}}"))
	tr.step("add m to a team no row has: 404", o, teamMember, at("id", "{{phantom}}", "userId", "{{m}}"))
	tr.step("W's teams: each with its members, by name, then address", o, "GET /api/v1/orgs/{id}/teams", inW)
	const teamLeave = "DELETE /api/v1/org-teams/{id}/members/{userId}"
	tr.step("take a2 off the team: 204", o, teamLeave, at("id", "{{t}}", "userId", "{{a2}}"))
	tr.step("take x off it, never on it: 204 all the same", o, teamLeave, at("id", "{{t}}", "userId", "{{x}}"))
	tr.step("m takes itself off: 403, the team's members are an admin's to change", m, teamLeave,
		at("id", "{{t}}", "userId", "{{m}}"))
	tr.step("take m off a team no row has: 404", o, teamLeave, at("id", "{{phantom}}", "userId", "{{m}}"))

	// P's members.
	const pMembers, pRole, pLeave = "POST /api/v1/projects/{id}/members", "PUT /api/v1/projects/{id}/members/{userId}",
		"DELETE /api/v1/projects/{id}/members/{userId}"
	inP := at("id", "{{p}}")
	tr.step("P's members: the owner, who made it", o, "GET /api/v1/projects/{id}/members", inP)
	tr.step("add m as a viewer: 201 with no body; project.member_added", o, pMembers, inP,
		membersTeamsMember(m.email, "viewer"))
	tr.step("P's members as m, a viewer", m, "GET /api/v1/projects/{id}/members", inP)
	tr.step("m, a viewer, adds a2: 403, an owner's to do", m, pMembers, inP, membersTeamsMember(a2.email, "viewer"))
	tr.step("an address no account has: 404", o, pMembers, inP, membersTeamsMember("tour-invitee@example.com", "viewer"))
	tr.step("a role no project has: 400 with the domain's text", o, pMembers, inP, membersTeamsMember(a2.email, "boss"))
	tr.step("a malformed body", o, pMembers, inP, jsonBody(`{`))
	tr.step("add x, in no workspace of the owner's, as a viewer: 201, since a project's members are its own "+
		"(REQ-16's per-project grants: a supplier from outside); the address in capitals and padded", o, pMembers,
		inP, membersTeamsMember(" TOUR-X@Example.com ", "viewer"))
	tr.step("P's members as x", x, "GET /api/v1/projects/{id}/members", inP)
	tr.step("P's members with W's worker key: a worker passes for any project of its workspace", worker,
		"GET /api/v1/projects/{id}/members", inP)
	tr.step("make m an editor: 204; project.member_role_changed from viewer to editor", o, pRole,
		at("id", "{{p}}", "userId", "{{m}}"), membersTeamsRole("editor"))
	tr.step("a role no project has: 400", o, pRole, at("id", "{{p}}", "userId", "{{m}}"), membersTeamsRole("boss"))
	tr.step("a malformed body", o, pRole, at("id", "{{p}}", "userId", "{{m}}"), jsonBody(`{`))
	tr.step("m makes itself an owner: 403", m, pRole, at("id", "{{p}}", "userId", "{{m}}"), membersTeamsRole("owner"))
	tr.step("give a2, not a member of P, a role: 204, which adds it (from \"\")", o, pRole,
		at("id", "{{p}}", "userId", "{{a2}}"), membersTeamsRole("viewer"))
	tr.step("give an id no account has a role: 404, user not found", o, pRole,
		at("id", "{{p}}", "userId", "{{phantom}}"), membersTeamsRole("viewer"))
	tr.step("a2, an admin of W and so an owner of P, leaves P: 204; project.member_removed, self true", a2, pLeave,
		at("id", "{{p}}", "userId", "{{a2}}"))
	tr.step("remove x: 204, self false", o, pLeave, at("id", "{{p}}", "userId", "{{x}}"))
	tr.step("remove x again: 204, and an event all the same", o, pLeave, at("id", "{{p}}", "userId", "{{x}}"))
	tr.step("m, an editor, removes the owner: 403", m, pLeave, at("id", "{{p}}", "userId", "{{owner}}"))
	tr.step("P's members as x, gone from P: 404", x, "GET /api/v1/projects/{id}/members", inP)
	tr.step("P's members: the owner and m", o, "GET /api/v1/projects/{id}/members", inP)

	// P's team grants. No grant route publishes an event.
	const grant, revoke = "PUT /api/v1/projects/{id}/team-access", "DELETE /api/v1/projects/{id}/team-access/{teamId}"
	tr.step("P's team grants: none, written null (Q14)", o, "GET /api/v1/projects/{id}/team-access", inP)
	tr.step("grant the first team editor: 201 with no body", o, grant, inP, membersTeamsGrant("{{t}}", "editor"))
	tr.step("grant the second team viewer", o, grant, inP, membersTeamsGrant("{{t2}}", "viewer"))
	tr.step("grant the first team again, as reviewer: 201, the role replaced", o, grant, inP,
		membersTeamsGrant("{{t}}", "reviewer"))
	tr.step("grant V's team: 400", o, grant, inP, membersTeamsGrant("{{tv}}", "viewer"))
	tr.step("grant a team no row has: 404", o, grant, inP, membersTeamsGrant("{{phantom}}", "viewer"))
	tr.step("a team id that is not a UUID: 404 too", o, grant, inP, membersTeamsGrant("not-a-team", "viewer"))
	tr.step("a role no project has: 400 with the domain's text", o, grant, inP, membersTeamsGrant("{{t}}", "boss"))
	tr.step("a malformed body", o, grant, inP, jsonBody(`{`))
	tr.step("m, an editor, grants: 403", m, grant, inP, membersTeamsGrant("{{t}}", "owner"))
	tr.step("P's grants: by team name", o, "GET /api/v1/projects/{id}/team-access", inP)
	tr.step("P's grants as m", m, "GET /api/v1/projects/{id}/team-access", inP)
	tr.step("revoke the second team's: 204", o, revoke, at("id", "{{p}}", "teamId", "{{t2}}"))
	tr.step("revoke it again: 204 all the same", o, revoke, at("id", "{{p}}", "teamId", "{{t2}}"))
	tr.step("revoke V's team's, never granted: 204", o, revoke, at("id", "{{p}}", "teamId", "{{tv}}"))
	tr.step("revoke a team no row has: 204", o, revoke, at("id", "{{p}}", "teamId", "{{phantom}}"))
	tr.step("m revokes the first team's: 403", m, revoke, at("id", "{{p}}", "teamId", "{{t}}"))
	tr.step("grant the second team again", o, grant, inP, membersTeamsGrant("{{t2}}", "viewer"))
	const dropTeam = "DELETE /api/v1/org-teams/{id}"
	tr.step("m deletes the first team: 403", m, dropTeam, at("id", "{{t}}"))
	tr.step("m deletes a team no row has: 404", m, dropTeam, at("id", "{{phantom}}"))
	tr.step("delete the second team: 204", o, dropTeam, at("id", "{{t2}}"))
	tr.step("delete it again: 404", o, dropTeam, at("id", "{{t2}}"))
	tr.step("P's grants: the deleted team's went with it", o, "GET /api/v1/projects/{id}/team-access", inP)

	// The active workspace's members.
	tr.step("the active workspace's members: W's, each as id, name, address and avatar", o, "GET /api/v1/users")
	tr.step("the same with X-Org-ID naming the owner's personal workspace: the owner alone", o, "GET /api/v1/users",
		actingIn("{{owner.workspace}}"))
	tr.step("as x, whose workspace is its personal one: x alone", x, "GET /api/v1/users")
	tr.step("with a worker key: the handler's 401", worker, "GET /api/v1/users")
	tr.step("with no session: the middleware's 401", anon, "GET /api/v1/users")

	// Leaving W.
	const leave = "DELETE /api/v1/orgs/{id}/members/{userId}"
	tr.step("m removes a2: 403", m, leave, at("id", "{{w}}", "userId", "{{a2}}"))
	tr.step("x leaves W, which it is not in: 404", x, leave, at("id", "{{w}}", "userId", "{{x}}"))
	tr.step("remove an id no account has: 400 with ErrNotMember's words, as its role change, and no event", o, leave,
		at("id", "{{w}}", "userId", "{{phantom}}"))
	tr.step("m leaves W: 204; org.member_removed, self true", m, leave, at("id", "{{w}}", "userId", "{{m}}"))
	tr.step("the owner removes a2, one of two admins: 204, self false", o, leave, at("id", "{{w}}", "userId", "{{a2}}"))
	tr.step("W's members: the owner alone again", o, "GET /api/v1/orgs/{id}/members", inW)
	tr.step("W's teams: m still on the first team, with an empty role (W's members are left-joined)", o,
		"GET /api/v1/orgs/{id}/teams", inW)
	tr.step("P's members as m, gone from W: still an editor of P, whose members are its own", m,
		"GET /api/v1/projects/{id}/members", inP)
}
