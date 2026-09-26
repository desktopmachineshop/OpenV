package runner

import (
	"errors"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// The cases behind testdata/wire/*.json, one list per Client method. Every
// fixture is written with the <tokens> the goldens show; the stub serves it
// with the real values (wireTokens) substituted. Response bodies are shaped
// the way the API writes them (json.Encoder, so a trailing newline), and
// retry cases pin which calls ride out a 5xx (Start, PushLogs, Finish and the
// pool register and release) and which return it at once.

// wireTokens pairs each token with the value that crosses the wire.
var wireTokens = []struct{ token, value string }{
	{"<worker-key>", "wk_4f1d2c3b5a6e7f8091a2b3c4d5e6f708"},
	{"<pool-key>", "pk_0a9b8c7d6e5f40312233445566778899"},
	{"<session-key>", "wk_sess_ab0fcdde1c0a4bccbdce1fcadbecfda0"},
	{"<run-token>", "rt_9aebccad0bef4abbacbd0ebfcadbecf9"},
	{"<worker-id>", "runner-host-4242"},
	{"<run-id>", "0d5e3f7a-8b1c-4d2e-9f3a-1b2c3d4e5f60"},
	{"<org-id>", "1e6f4a8b-9c2d-4e3f-8a4b-2c3d4e5f6a71"},
	{"<agent-id>", "2f7a5b9c-ad3e-4f4a-9b5c-3d4e5f6a7b82"},
	{"<project-id>", "3a8b6cad-be4f-4a5b-8c6d-4e5f6a7b8c93"},
	{"<user-id>", "4b9c7dbe-cf5a-4b6c-9d7e-5f6a7b8c9da4"},
	{"<login-id>", "5cad8ecf-da6b-4c7d-8e8f-6a7b8c9daeb5"},
	{"<repo-id>", "6dbe9fda-eb7c-4d8e-9f9a-7b8c9daebfc6"},
	{"<node-id>", "7ecfaaeb-fc8d-4e9f-8aab-8c9daebfcad7"},
	{"<session-id>", "8fdabbfc-ad9e-4faa-9bbc-9daebfcadbe8"},
	{"<interview-id>", "9aebccad-be0f-4abb-8ccd-aebfcadbecf9"},
	{"<time>", "2026-03-04T05:06:07Z"},
	{"<expires>", "2026-03-04T13:06:07Z"},
}

// wireID is the real value behind a token, for the arguments of a call.
func wireID(token string) string { return wireDenormalise(token) }

func wireGoldenCases() map[string][]wireCase {
	return map[string][]wireCase{
		"Claim":               wireClaimCases(),
		"Start":               wireStartCases(),
		"PushLogs":            wirePushLogsCases(),
		"Finish":              wireFinishCases(),
		"Release":             wireReleaseCases(),
		"ReportDetection":     wireReportDetectionCases(),
		"ClaimLogin":          wireClaimLoginCases(),
		"LoginProgress":       wireLoginProgressCases(),
		"GetLoginFull":        wireGetLoginFullCases(),
		"ListRepoConnections": wireListRepoConnectionsCases(),
		"RegisterPoolNode":    wireRegisterPoolNodeCases(),
		"PoolHeartbeat":       wirePoolHeartbeatCases(),
		"ReleasePoolNode":     wireReleasePoolNodeCases(),
	}
}

// --- Claim ---

// wireRunJSON is a claimed run as ClaimAgentRun encodes an agentruns.Run.
const wireRunJSON = `{"id":"<run-id>","org_id":"<org-id>","agent_id":"<agent-id>","project_id":"<project-id>",` +
	`"status":"claimed","cancel_requested":false,"priority":0,"prompt":"Draft test cases for REQ-12.",` +
	`"worker_id":"<worker-id>","heartbeat_at":"<time>","final_text":"","error":"","attempt_count":1,"max_attempts":3,` +
	`"tokens_in":0,"tokens_out":0,"artifacts_touched":null,"launched_by":"<user-id>","created_at":"<time>",` +
	`"agent_content_hash":"9b1c2d3e4f5a","agent_model":"claude-sonnet-4-5","agent_effort":"medium"}`

// wireInterviewRunJSON is an interview turn: child priority, no project, and
// the interview session that makes its origin untrusted.
const wireInterviewRunJSON = `{"id":"<run-id>","org_id":"<org-id>","agent_id":"<agent-id>",` +
	`"interview_session_id":"<interview-id>","status":"claimed","cancel_requested":false,"priority":20,` +
	`"prompt":"Interviewee: the seal leaks after a week.","worker_id":"<worker-id>","heartbeat_at":"<time>",` +
	`"final_text":"","error":"","tokens_in":0,"tokens_out":0,"artifacts_touched":null,"created_at":"<time>"}`

const wireAgentJSON = `{"id":"<agent-id>","org_id":"<org-id>","slug":"test-drafter","name":"Test drafter",` +
	`"description":"Drafts test cases from requirements.","provider":"claude-code","model":"claude-sonnet-4-5",` +
	`"effort":"medium","allowed_tools":["mcp__openv__get_artifact","mcp__openv__create_artifact"],` +
	`"write_mode":"proposal","repo_access":false,"max_turns":30,"timeout_seconds":1800,"config":{},` +
	`"system_prompt":"You draft test cases.","locked":false,"file_path":"agents/test-drafter.md",` +
	`"content_hash":"9b1c2d3e4f5a","created_at":"<time>","updated_at":"<time>"}`

// wireClaimBody is the claim answer as the API writes it: a map, so its keys
// are sorted, and auth has no omitempty.
func wireClaimBody(run, auth string) string {
	return `{"agent":` + wireAgentJSON + `,"auth":` + auth + `,"run":` + run + `,"run_token":"<run-token>"}` + "\n"
}

func wireClaimCases() []wireCase {
	return []wireCase{
		{
			name:      "a run for a normal slot, on the runner's own CLI sign-in",
			responses: []wireResponse{{200, wireClaimBody(wireRunJSON, `{"mode":"user-account"}`)}},
			calls: []wireCall{{`Claim("<worker-id>", ["claude-code" "codex-cli"], 0, false)`, func(c *Client) (interface{}, error) {
				return c.Claim(wireID("<worker-id>"), []string{"claude-code", "codex-cli"}, 0, false)
			}}},
		},
		{
			name:      "a project on api-key auth names the variable that holds the key",
			responses: []wireResponse{{200, wireClaimBody(wireRunJSON, `{"api_key_env":"ANTHROPIC_API_KEY","mode":"api-key"}`)}},
			calls: []wireCall{{`Claim("<worker-id>", ["claude-code"], 0, false)`, func(c *Client) (interface{}, error) {
				return c.Claim(wireID("<worker-id>"), []string{"claude-code"}, 0, false)
			}}},
		},
		{
			name:      "\"auth\":null decodes to no auth; a hosted runner's child slot",
			responses: []wireResponse{{200, wireClaimBody(wireInterviewRunJSON, `null`)}},
			calls: []wireCall{{`Claim("<worker-id>", ["claude-code"], 10, true)`, func(c *Client) (interface{}, error) {
				return c.Claim(wireID("<worker-id>"), []string{"claude-code"}, 10, true)
			}}},
		},
		{
			name:      "an empty queue answers 204: no run and no error; no providers is sent as null",
			responses: []wireResponse{{204, ""}},
			calls: []wireCall{{`Claim("<worker-id>", nil, 0, false)`, func(c *Client) (interface{}, error) {
				return c.Claim(wireID("<worker-id>"), nil, 0, false)
			}}},
		},
		{
			name:      "a server error is returned at once, not retried",
			responses: []wireResponse{{503, `{"error":"failed to claim a run"}` + "\n"}},
			calls: []wireCall{{`Claim("<worker-id>", ["claude-code"], 0, false)`, func(c *Client) (interface{}, error) {
				return c.Claim(wireID("<worker-id>"), []string{"claude-code"}, 0, false)
			}}},
		},
	}
}

// --- Run lifecycle: start, logs, finish, release ---

func wireStartCall() wireCall {
	return wireCall{`Start("<run-id>")`, func(c *Client) (interface{}, error) {
		return nil, c.Start(wireID("<run-id>"))
	}}
}

func wireStartCases() []wireCase {
	return []wireCase{
		{
			name:      "no body and no content type; 204 is success",
			responses: []wireResponse{{204, ""}},
			calls:     []wireCall{wireStartCall()},
		},
		{
			name:      "a 409 conflict is returned at once, not retried",
			responses: []wireResponse{{409, `{"error":"invalid run status transition"}` + "\n"}},
			calls:     []wireCall{wireStartCall()},
		},
		{
			name:      "a 5xx is retried after a backoff",
			responses: []wireResponse{{503, `{"error":"failed to start run"}` + "\n"}, {204, ""}},
			calls:     []wireCall{wireStartCall()},
		},
	}
}

// wirePushResult is what PushLogs returned besides its error.
type wirePushResult struct {
	CancelRequested bool   `json:"cancel_requested"`
	Status          string `json:"status"`
}

// wireLogEntries is a batch as the pump builds it: no CreatedAt, so the wire
// carries the zero time.
func wireLogEntries() []agentruns.LogEntry {
	return []agentruns.LogEntry{
		{RunID: wireID("<run-id>"), Seq: 1, Kind: agentruns.LogText, Payload: map[string]interface{}{"text": "Reading REQ-12 & REQ-13 <draft>"}},
		{RunID: wireID("<run-id>"), Seq: 2, Kind: agentruns.LogToolCall, Payload: map[string]interface{}{"name": "mcp__openv__get_artifact", "input": map[string]interface{}{"id": "REQ-12"}}},
	}
}

func wirePushCall(desc string, entries []agentruns.LogEntry, partial string) wireCall {
	return wireCall{desc, func(c *Client) (interface{}, error) {
		cancel, status, err := c.PushLogs(wireID("<run-id>"), entries, partial)
		return wirePushResult{CancelRequested: cancel, Status: status}, err
	}}
}

func wirePushLogsCases() []wireCase {
	running := `{"cancel_requested":false,"status":"running"}` + "\n"
	return []wireCase{
		{
			name:      "the streaming body carries the entries and the whole answer so far",
			responses: []wireResponse{{200, running}},
			calls:     []wireCall{wirePushCall(`PushLogs("<run-id>", [2 entries], "The seal holds")`, wireLogEntries(), "The seal holds")},
		},
		{
			name:      "a heartbeat sends an empty list, never null, and reads cancel_requested back",
			responses: []wireResponse{{200, `{"cancel_requested":true,"status":"running"}` + "\n"}},
			calls:     []wireCall{wirePushCall(`PushLogs("<run-id>", nil, "")`, nil, "")},
		},
		{
			name: "an API older than the streaming body answers 400: the batch is resent as the legacy bare array, and the client keeps that shape",
			responses: []wireResponse{
				{400, `{"error":"invalid request body"}` + "\n"},
				{200, running},
				{200, running},
			},
			calls: []wireCall{
				wirePushCall(`PushLogs("<run-id>", [2 entries], "half an answer")`, wireLogEntries(), "half an answer"),
				wirePushCall(`PushLogs("<run-id>", [2 entries], "more of the answer")`, wireLogEntries(), "more of the answer"),
			},
		},
		{
			name:      "a 5xx is retried after a backoff",
			responses: []wireResponse{{502, "bad gateway\n"}, {200, running}},
			calls:     []wireCall{wirePushCall(`PushLogs("<run-id>", nil, "")`, nil, "")},
		},
	}
}

func wireFinishCall(desc string, req agentruns.FinishRequest) wireCall {
	return wireCall{desc, func(c *Client) (interface{}, error) {
		return nil, c.Finish(wireID("<run-id>"), req)
	}}
}

func wireFinishCases() []wireCase {
	exit, cost := 1, 0.0125
	failed := agentruns.FinishRequest{
		Status: agentruns.StatusFailed, ExitCode: &exit, FinalText: "Drafted one of three.",
		Error: "claude exited with status 1", ErrorClass: agentruns.ErrorClassAgentError,
		TokensIn: 1200, TokensOut: 340, CostUSD: &cost,
	}
	succeeded := agentruns.FinishRequest{Status: agentruns.StatusSucceeded, FinalText: "Drafted 3 test cases.", TokensIn: 5, TokensOut: 7}
	finished := `{"id":"<run-id>","status":"failed"}` + "\n"
	return []wireCase{
		{
			name:      "a failed run with every field set",
			responses: []wireResponse{{200, finished}},
			calls:     []wireCall{wireFinishCall(`Finish("<run-id>", {failed, exit 1, agent_error, cost 0.0125})`, failed)},
		},
		{
			name:      "a succeeded run leaves out exit_code, error_class and cost_usd",
			responses: []wireResponse{{200, `{"id":"<run-id>","status":"succeeded"}` + "\n"}},
			calls:     []wireCall{wireFinishCall(`Finish("<run-id>", {succeeded})`, succeeded)},
		},
		{
			name:      "a 409 already-finished conflict is returned at once, not retried",
			responses: []wireResponse{{409, `{"error":"invalid run status transition"}` + "\n"}},
			calls:     []wireCall{wireFinishCall(`Finish("<run-id>", {succeeded})`, succeeded)},
		},
		{
			name:      "a 5xx is retried after a backoff",
			responses: []wireResponse{{500, `{"error":"failed to finish run"}` + "\n"}, {200, finished}},
			calls:     []wireCall{wireFinishCall(`Finish("<run-id>", {failed, exit 1, agent_error, cost 0.0125})`, failed)},
		},
	}
}

func wireReleaseCases() []wireCase {
	call := wireCall{`Release("<run-id>", "<worker-id>")`, func(c *Client) (interface{}, error) {
		return nil, c.Release(wireID("<run-id>"), wireID("<worker-id>"))
	}}
	return []wireCase{
		{
			name:      "hands the run back with the worker id; 204 is success",
			responses: []wireResponse{{204, ""}},
			calls:     []wireCall{call},
		},
		{
			name:      "a server error is returned at once, not retried",
			responses: []wireResponse{{500, `{"error":"failed to release run"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

// --- Provider detection and sign-in ---

func wireReportDetectionCases() []wireCase {
	call := wireCall{`ReportDetection({claude-code: installed and signed in, codex-cli: missing})`, func(c *Client) (interface{}, error) {
		return nil, c.ReportDetection(map[string]map[string]interface{}{
			"claude-code": {"installed": true, "version": "2.0.14", "logged_in": true, "detail": ""},
			"codex-cli":   {"installed": false, "version": "", "logged_in": false, "detail": "codex not found on PATH"},
		})
	}}
	return []wireCase{
		{
			name:      "availability per provider; 204 is success",
			responses: []wireResponse{{204, ""}},
			calls:     []wireCall{call},
		},
		{
			name:      "a server error is returned at once, not retried",
			responses: []wireResponse{{500, `{"error":"failed to record detection"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

// wireLoginJSON is a sign-in request as the API encodes it (Code only once
// the member has pasted one).
func wireLoginJSON(status, code string) string {
	codeField := ""
	if code != "" {
		codeField = `"code":"` + code + `",`
	}
	return `{"id":"<login-id>","org_id":"<org-id>","provider":"claude-code","target":"user","status":"` + status + `",` +
		`"auth_url":"https://claude.ai/oauth/authorize?code=true&state=s1",` + codeField +
		`"detail":"Open the link and paste the code.","requested_by":"<user-id>","created_at":"<time>","updated_at":"<time>"}` + "\n"
}

func wireClaimLoginCases() []wireCase {
	call := wireCall{`ClaimLogin()`, func(c *Client) (interface{}, error) { return c.ClaimLogin() }}
	return []wireCase{
		{
			name:      "nothing pending answers 204: no request and no error",
			responses: []wireResponse{{204, ""}},
			calls:     []wireCall{call},
		},
		{
			name:      "a pending sign-in is claimed with no body",
			responses: []wireResponse{{200, wireLoginJSON("claimed", "")}},
			calls:     []wireCall{call},
		},
		{
			name:      "a server error is returned at once, not retried",
			responses: []wireResponse{{500, `{"error":"failed to claim login request"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

func wireLoginProgressCases() []wireCase {
	call := wireCall{`LoginProgress("<login-id>", "awaiting_code", <auth url>, "Open the link and paste the code.", "code")`, func(c *Client) (interface{}, error) {
		return nil, c.LoginProgress(wireID("<login-id>"), "awaiting_code", "https://claude.ai/oauth/authorize?code=true&state=s1", "Open the link and paste the code.", "code")
	}}
	return []wireCase{
		{
			name:      "reports the step, the link and what the member pastes back",
			responses: []wireResponse{{200, wireLoginJSON("awaiting_code", "")}},
			calls:     []wireCall{call},
		},
		{
			name:      "a refused step is returned as an error",
			responses: []wireResponse{{400, `{"error":"login request is no longer active"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

func wireGetLoginFullCases() []wireCase {
	call := wireCall{`GetLoginFull("<login-id>")`, func(c *Client) (interface{}, error) {
		return c.GetLoginFull(wireID("<login-id>"))
	}}
	return []wireCase{
		{
			name:      "the request with the pasted code",
			responses: []wireResponse{{200, wireLoginJSON("awaiting_code", "4/0AbCdEf")}},
			calls:     []wireCall{call},
		},
		{
			name:      "a request of another workspace answers 404",
			responses: []wireResponse{{404, `{"error":"login request not found"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

// --- Repository connections ---

func wireListRepoConnectionsCases() []wireCase {
	call := wireCall{`ListRepoConnections("<project-id>")`, func(c *Client) (interface{}, error) {
		return c.ListRepoConnections(wireID("<project-id>"))
	}}
	conns := `[{"id":"<repo-id>","project_id":"<project-id>","name":"firmware","remote_url":"git@github.com:acme/firmware.git",` +
		`"default_branch":"main","credential_strategy":"host","created_at":"<time>","updated_at":"<time>","my_local_path":"/home/dev/firmware"}]` + "\n"
	return []wireCase{
		{
			name:      "a project's connections, with the caller's checkout path",
			responses: []wireResponse{{200, conns}},
			calls:     []wireCall{call},
		},
		{
			name:      "null decodes to no connections and no error",
			responses: []wireResponse{{200, "null\n"}},
			calls:     []wireCall{call},
		},
		{
			name:      "a server error is returned at once, not retried",
			responses: []wireResponse{{500, `{"error":"failed to list repo connections"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

// --- Transient runner pool (made with the pool key) ---

const wireNodeJSON = `{"id":"<node-id>","name":"pool-node-a","pool":"default","providers":["claude-code"],"status":"idle",` +
	`"last_seen_at":"<time>","created_at":"<time>"}`

func wireRegisterCall(desc string, providers []string) wireCall {
	return wireCall{desc, func(c *Client) (interface{}, error) {
		return c.RegisterPoolNode("pool-node-a", "default", providers)
	}}
}

func wireRegisterPoolNodeCases() []wireCase {
	return []wireCase{
		{
			name:      "announces the node and its providers; 201 carries the node",
			key:       "<pool-key>",
			responses: []wireResponse{{201, wireNodeJSON + "\n"}},
			calls:     []wireCall{wireRegisterCall(`RegisterPoolNode("pool-node-a", "default", ["claude-code"])`, []string{"claude-code"})},
		},
		{
			name:      "a node with no CLI installed sends providers null",
			key:       "<pool-key>",
			responses: []wireResponse{{201, wireNodeJSON + "\n"}},
			calls:     []wireCall{wireRegisterCall(`RegisterPoolNode("pool-node-a", "default", nil)`, nil)},
		},
		{
			name:      "a 5xx is retried after a backoff",
			key:       "<pool-key>",
			responses: []wireResponse{{503, `{"error":"runner sessions are not enabled"}` + "\n"}, {201, wireNodeJSON + "\n"}},
			calls:     []wireCall{wireRegisterCall(`RegisterPoolNode("pool-node-a", "default", ["claude-code"])`, []string{"claude-code"})},
		},
	}
}

// wireHeartbeat is what PoolHeartbeat returned, and whether its error is the
// sentinel on which the node registers again.
type wireHeartbeat struct {
	Assignment         *PoolAssignment `json:"assignment"`
	ErrPoolNodeUnknown bool            `json:"is_err_pool_node_unknown"`
}

func wirePoolHeartbeatCases() []wireCase {
	call := wireCall{`PoolHeartbeat("<node-id>")`, func(c *Client) (interface{}, error) {
		a, err := c.PoolHeartbeat(wireID("<node-id>"))
		return wireHeartbeat{Assignment: a, ErrPoolNodeUnknown: errors.Is(err, ErrPoolNodeUnknown)}, err
	}}
	lease := `{"session_id":"<session-id>","org_id":"<org-id>","user_id":"<user-id>","user_name":"Dana",` +
		`"worker_key":"<session-key>","expires_at":"<expires>"}`
	return []wireCase{
		{
			name:      "an idle node: assignment null decodes to no lease",
			key:       "<pool-key>",
			responses: []wireResponse{{200, `{"assignment":null,"node":` + wireNodeJSON + `}` + "\n"}},
			calls:     []wireCall{call},
		},
		{
			name:      "the beat that picks a lease up carries the session's worker key",
			key:       "<pool-key>",
			responses: []wireResponse{{200, `{"assignment":` + lease + `,"node":` + wireNodeJSON + `}` + "\n"}},
			calls:     []wireCall{call},
		},
		{
			name:      "an unknown node answers 404: ErrPoolNodeUnknown, so the node registers again",
			key:       "<pool-key>",
			responses: []wireResponse{{404, `{"error":"pool node is not registered"}` + "\n"}},
			calls:     []wireCall{call},
		},
		{
			name:      "a server error is returned at once, not retried",
			key:       "<pool-key>",
			responses: []wireResponse{{503, `{"error":"failed to record pool heartbeat"}` + "\n"}},
			calls:     []wireCall{call},
		},
	}
}

func wireReleasePoolNodeCases() []wireCase {
	call := wireCall{`ReleasePoolNode("<node-id>", "<session-id>")`, func(c *Client) (interface{}, error) {
		return nil, c.ReleasePoolNode(wireID("<node-id>"), wireID("<session-id>"))
	}}
	return []wireCase{
		{
			name:      "reports the wiped lease; 204 is success",
			key:       "<pool-key>",
			responses: []wireResponse{{204, ""}},
			calls:     []wireCall{call},
		},
		{
			name:      "a 5xx is retried after a backoff",
			key:       "<pool-key>",
			responses: []wireResponse{{500, `{"error":"failed to release pool node"}` + "\n"}, {204, ""}},
			calls:     []wireCall{call},
		},
	}
}
