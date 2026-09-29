//go:build unix

package main

import (
	"encoding/json"
	"net/http"
)

// The identities S5e's matrix areas share (refactor plan §6.4 S5e: every
// route × {anonymous, viewer, editor, owner, other-workspace member, worker
// key, run token}), built the same way by every area that calls
// tour.matrixCast, with the platform admin as an eighth column: the one
// identity that passes the project guard on a project that does not exist,
// so the only one that shows what a handler does past its guard with a
// phantom id.
//
// An area boots with tourMatrixEnv, tourMatrixFiles and tourMatrixAccounts
// (viewer, editor and outsider: with admin and owner, the five registrations
// one server takes from the tour's address). matrixCast then makes, as
// setup:
//   - W, the owner's shared workspace, where the owner acts, as its admin;
//   - P, a project in W, which the owner creates and so owns;
//   - viewer and editor: plain members of W (a workspace role is member or
//     admin), acting there, with the project role that names them in P;
//   - outsider: in no workspace but its own personal one, where it acts;
//   - the direct-mode agent tour-matrix in W;
//   - worker: W's workspace worker key (worker.token, worker.key);
//   - run: a run of tour-matrix in P (its id <run>), launched by the owner,
//     claimed by the worker (worker id tour-matrix) and started, so running,
//     the state an agent's calls come from; its token is the column (run.token).
//     Nothing finishes, releases or cancels it: the reaper's first tick is at
//     30 s and it fails only runs silent for 2 minutes, which no area lasts.
// admin acts in its personal workspace, anonymous sends no credential.

// tourMatrixAccounts are the accounts a matrix area registers beyond admin
// and owner.
func tourMatrixAccounts() []tourAccount {
	return []tourAccount{
		{name: "viewer", display: "Tour Viewer", about: "a plain member of W, acting there, and a viewer of P"},
		{name: "editor", display: "Tour Editor", about: "a plain member of W, acting there, and an editor of P"},
		{name: "outsider", display: "Tour Outsider", about: "the other-workspace member: in no workspace but its " +
			"personal one, where it acts"},
	}
}

// tourMatrixEnv is what a matrix area sets in its server's environment: the
// header ownAddress writes a client address in, and a connector download
// directory with no bundle in it, so that the download routes answer the
// same on every machine (the default, ./dist, holds one once someone has run
// make connector-dist).
func tourMatrixEnv() map[string]string {
	return map[string]string{
		"OPENV_CLIENT_IP_HEADER": "CF-Connecting-IP",
		"CONNECTOR_DIST_DIR":     "{{files}}/dist",
	}
}

// tourMatrixFiles is the area's files: a connector download directory that
// holds a README and no bundle.
func tourMatrixFiles() map[string][]byte {
	return map[string][]byte{
		"dist/README": []byte("The S5e matrix's connector download directory: no bundle, so every download is refused.\n"),
	}
}

// tourMatrixCast is the columns of a matrix area.
type tourMatrixCast struct {
	anon, viewer, editor, owner, outsider, worker, run, admin *tourActor
}

// columns are the cast in the matrix's order.
func (c *tourMatrixCast) columns() []*tourActor {
	return []*tourActor{c.anon, c.viewer, c.editor, c.owner, c.outsider, c.worker, c.run, c.admin}
}

// signedIn are the columns with a session.
func (c *tourMatrixCast) signedIn() []*tourActor {
	return []*tourActor{c.viewer, c.editor, c.owner, c.outsider, c.admin}
}

// tourMatrixAgent is the agent the run belongs to: direct mode, no
// repository access, one tool.
func tourMatrixAgent() string {
	b, _ := json.Marshal(map[string]any{
		"slug": "tour-matrix", "name": "Tour Matrix", "description": "The S5e matrix's agent.",
		"provider": tourDefaultProvider, "allowed_tools": []string{"get_artifact"}, "write_mode": "direct",
		"repo_access": false, "system_prompt": "You answer in one line.",
	})
	return string(b)
}

// matrixCast builds the matrix's identities (see the top of this file).
func (tr *tour) matrixCast() *tourMatrixCast {
	tr.t.Helper()
	o := tr.owner
	c := &tourMatrixCast{anon: tr.anon, viewer: tr.actor("viewer"), editor: tr.actor("editor"), owner: o,
		outsider: tr.actor("outsider"), admin: tr.admin}
	tr.sharedWorkspace("w", "Tour Matrix")
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Matrix"}`),
		expect(http.StatusCreated)).capture("p", "/id")
	for _, m := range []struct {
		a    *tourActor
		role string
	}{{c.viewer, "viewer"}, {c.editor, "editor"}} {
		tr.join(m.a, "{{w}}", "member")
		body, _ := json.Marshal(map[string]string{"email": m.a.email, "role": m.role})
		tr.setup("make "+m.a.name+" a "+m.role+" of P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
			rawBody("application/json", body))
	}
	tr.setup("the agent tour-matrix", o, "POST /api/v1/agents", jsonBody(tourMatrixAgent())).capture("agent", "/id")
	c.worker = tr.workerKey("worker", "{{w}}", "W's workspace worker key (a workspace runner)")
	tr.queueRun("run", o, "tour-matrix", `{"project_id":"{{p}}","prompt":"Summarise P."}`)
	c.run = tr.takeRun(c.worker, "tour-matrix", "run", "the token of a direct-mode run of tour-matrix in P, "+
		"launched by the owner, claimed by W's worker key and started: what an agent's tools send")
	tr.setup("start the run, as a runner does", c.worker, "POST /api/v1/agent-runs/{id}/start", at("id", "{{run}}"))
	return c
}

// secondSession signs an account in again, as setup from an address of its
// own (ownAddress), and makes an actor of that session (tour.session,
// registered as name.session), acting where the account acts: for a request
// that ends the session it is sent with, so that the account's own session
// lives on.
func (tr *tour) secondSession(a *tourActor, name, about string) *tourActor {
	tr.t.Helper()
	body, _ := json.Marshal(map[string]string{"email": a.email, "password": tourPassword})
	res := tr.setup("sign "+a.name+" in again", tr.anon, "POST /api/v1/auth/login", rawBody("application/json", body),
		ownAddress())
	s := tr.session(a, res, name, about)
	s.org = a.org
	return s
}
