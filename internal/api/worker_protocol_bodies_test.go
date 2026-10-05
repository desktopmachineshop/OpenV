package api

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// The worker wire's server side answered three bodies as map literals and
// decoded six into anonymous structs until refactor step P3 named them in
// internal/workerproto. R10: each named body writes the map's bytes, through
// json.NewEncoder as the handlers do, and each request type decodes what the
// anonymous struct did. The maps and structs below are the handlers' as they
// were, verbatim.

// encodeLikeAHandler is json.NewEncoder(w).Encode(v)'s bytes.
func encodeLikeAHandler(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestWorkerAnswersWriteTheMapsBytes(t *testing.T) {
	project, parent := "proj-1", "run-0"
	exit, cost := 1, 0.5
	at := time.Date(2026, 3, 4, 5, 6, 7, 8, time.UTC)
	runs := map[string]*agentruns.Run{
		"bare": {ID: "run-1", OrgID: "org-1", AgentID: "agent-1", Status: agentruns.StatusClaimed},
		"full": {ID: "run-2", OrgID: "org-1", AgentID: "agent-1", ProjectID: &project, ParentRunID: &parent,
			Status: agentruns.StatusRunning, CancelRequested: true, Priority: 10, Prompt: "Check <REQ-1> & REQ-2",
			WorkerID: "w-1", HeartbeatAt: &at, ExitCode: &exit, CostUSD: &cost, AttemptCount: 2, MaxAttempts: 3,
			ArtifactsTouched: []map[string]interface{}{{"id": "a-1"}}},
		"nil": nil,
	}
	agentsByName := map[string]*agents.Agent{
		"agent": {ID: "agent-1", OrgID: "org-1", Slug: "drafter", Name: "Drafter <beta>", Provider: "claude-code",
			AllowedTools: []string{"mcp__openv__get_artifact"}},
		"nil": nil,
	}
	auths := []map[string]string{
		{"mode": "user-account"},
		{"mode": "api-key", "api_key_env": "ANTHROPIC_API_KEY"},
		{"mode": "api-key", "api_key_env": ""}, // a provider with no default key variable
	}
	for runName, run := range runs {
		for agentName, agent := range agentsByName {
			for _, auth := range auths {
				for _, token := range []string{"tok-1", "", "a&b<c>"} {
					old := encodeLikeAHandler(t, map[string]interface{}{
						"run":       run,
						"agent":     agent,
						"run_token": token,
						"auth":      auth,
					})
					named := encodeLikeAHandler(t, workerproto.ClaimResponse{
						Agent:    agent,
						Auth:     auth,
						Run:      run,
						RunToken: token,
					})
					if !bytes.Equal(old, named) {
						t.Errorf("claim answer (run %s, agent %s, auth %v, token %q):\nmap    %s\nstruct %s", runName, agentName, auth, token, old, named)
					}
				}
			}
		}
	}

	for _, run := range []*agentruns.Run{
		{Status: agentruns.StatusRunning},
		{Status: agentruns.StatusRunning, CancelRequested: true},
		{Status: ""},
	} {
		old := encodeLikeAHandler(t, map[string]interface{}{
			"cancel_requested": run.CancelRequested,
			"status":           run.Status,
		})
		named := encodeLikeAHandler(t, workerproto.LogsResponse{
			CancelRequested: run.CancelRequested,
			Status:          run.Status,
		})
		if !bytes.Equal(old, named) {
			t.Errorf("log push answer:\nmap    %s\nstruct %s", old, named)
		}
	}

	session := "sess-1"
	nodes := []*runnersessions.Node{nil, {ID: "node-1", Name: "pool-a", Pool: "default", Providers: []string{"claude-code"},
		Status: "leased", SessionID: &session, LastSeenAt: at, CreatedAt: at}}
	assignments := []*runnersessions.Assignment{nil, {SessionID: session, OrgID: "org-1", UserID: "u-1", ExpiresAt: at},
		{SessionID: session, OrgID: "org-1", UserID: "u-1", UserName: "Ada <L>", WorkerKey: "wk&1", ExpiresAt: at}}
	for _, node := range nodes {
		for _, assignment := range assignments {
			old := encodeLikeAHandler(t, map[string]interface{}{
				"node":       node,
				"assignment": assignment,
			})
			named := encodeLikeAHandler(t, workerproto.PoolHeartbeatResponse{
				Assignment: assignment,
				Node:       node,
			})
			if !bytes.Equal(old, named) {
				t.Errorf("pool heartbeat answer:\nmap    %s\nstruct %s", old, named)
			}
		}
	}
}

// sameDecode decodes body into both and requires the same outcome: both
// refuse it, or both accept it with equal values in every field of the old
// type, matched to the new type's by name.
func sameDecode(t *testing.T, what, body string, old, named any) {
	t.Helper()
	errOld := json.Unmarshal([]byte(body), old)
	errNamed := json.Unmarshal([]byte(body), named)
	if (errOld == nil) != (errNamed == nil) {
		t.Errorf("%s %s: the old type's error is %v, the named type's %v", what, body, errOld, errNamed)
		return
	}
	o, n := reflect.ValueOf(old).Elem(), reflect.ValueOf(named).Elem()
	if o.Kind() != reflect.Struct {
		if !reflect.DeepEqual(o.Interface(), n.Convert(o.Type()).Interface()) {
			t.Errorf("%s %s: old %#v, named %#v", what, body, o.Interface(), n.Interface())
		}
		return
	}
	if o.NumField() != n.NumField() {
		t.Fatalf("%s: the old type has %d fields, the named one %d", what, o.NumField(), n.NumField())
	}
	for i := 0; i < o.NumField(); i++ {
		f := o.Type().Field(i)
		nf, ok := n.Type().FieldByName(f.Name)
		if !ok || nf.Tag != f.Tag || nf.Type != f.Type {
			t.Fatalf("%s: field %s %s %q has no twin in the named type", what, f.Name, f.Type, f.Tag)
		}
		if !reflect.DeepEqual(o.Field(i).Interface(), n.FieldByName(f.Name).Interface()) {
			t.Errorf("%s %s: field %s old %#v, named %#v", what, body, f.Name, o.Field(i).Interface(), n.FieldByName(f.Name).Interface())
		}
	}
}

func TestWorkerRequestsDecodeAsTheAnonymousStructsDid(t *testing.T) {
	common := []string{`{}`, `null`, `[]`, `{"unknown": 1}`, `{`}
	for _, body := range append([]string{
		`{"worker_id":"w-1","providers":["claude-code","codex-cli"],"min_priority":10,"hosted":true}`,
		`{"WORKER_ID":"w-1","Providers":null,"min_priority":0,"hosted":false}`,
		`{"min_priority":"10"}`, `{"providers":"claude-code"}`,
	}, common...) {
		var old struct {
			WorkerID    string   `json:"worker_id"`
			Providers   []string `json:"providers"`
			MinPriority int      `json:"min_priority"`
			Hosted      bool     `json:"hosted"`
		}
		sameDecode(t, "claim", body, &old, new(workerproto.ClaimRequest))
	}
	for _, body := range append([]string{`{"worker_id":"w-1"}`, `{"Worker_Id":"w-2"}`, `{"worker_id":5}`}, common...) {
		var old struct {
			WorkerID string `json:"worker_id"`
		}
		sameDecode(t, "release", body, &old, new(workerproto.ReleaseRequest))
	}
	for _, body := range append([]string{
		`{"status":"awaiting_code","auth_url":"https://x/?a=1&b=2","detail":"Paste <it>","paste_kind":"code"}`,
		`{"status":"completed"}`, `{"paste_kind":7}`,
	}, common...) {
		var old struct {
			Status  string `json:"status"`
			AuthURL string `json:"auth_url"`
			Detail  string `json:"detail"`
			// PasteKind ("code" or "url") is what the worker is waiting for the
			// member to paste back; older workers omit it.
			PasteKind string `json:"paste_kind"`
		}
		sameDecode(t, "sign-in progress", body, &old, new(workerproto.LoginProgress))
	}
	for _, body := range append([]string{
		`{"claude-code":{"installed":true,"version":"1.2.3","logged_in":true,"detail":""},"codex-cli":{"installed":false}}`,
		`{"claude-code":null}`, `{"claude-code":5}`,
	}, common...) {
		var old map[string]map[string]interface{}
		sameDecode(t, "detection", body, &old, new(workerproto.DetectionReport))
	}
	for _, body := range append([]string{`{"name":"pool-a","pool":"default","providers":["claude-code"]}`, `{"name":"pool-a","providers":null}`, `{"pool":1}`}, common...) {
		var old struct {
			Name      string   `json:"name"`
			Pool      string   `json:"pool"`
			Providers []string `json:"providers"`
		}
		sameDecode(t, "pool registration", body, &old, new(workerproto.PoolNodeRegistration))
	}
	for _, body := range append([]string{`{"session_id":"sess-1"}`, `{"session_id":null}`, `{"session_id":[]}`}, common...) {
		var old struct {
			SessionID string `json:"session_id"`
		}
		sameDecode(t, "pool release", body, &old, new(workerproto.PoolNodeRelease))
	}
	entries := `[{"run_id":"run-1","seq":1,"kind":"text","payload":{"text":"a <b> & c"},"created_at":"2026-03-04T05:06:07Z"},{"seq":2}]`
	for _, body := range append([]string{`{"entries":` + entries + `,"partial_text":"so far"}`, `{"entries":null}`, `{"entries":{}}`}, common...) {
		var old struct {
			Entries     []agentruns.LogEntry `json:"entries"`
			PartialText string               `json:"partial_text"`
		}
		sameDecode(t, "log push", body, &old, new(workerproto.LogsRequest))
	}
	for _, body := range []string{entries, `[]`, `null`, `[1]`, `{}`} {
		var old []agentruns.LogEntry
		sameDecode(t, "legacy log push", body, &old, new(workerproto.LegacyLogsRequest))
	}
}
