package runner

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// The runner sent the worker wire's request bodies as map literals until
// refactor step P3 named them in internal/workerproto. R10: each named body
// marshals, as doAs does, to the map's bytes. The maps below are Client's as
// they were, verbatim; the S7 goldens pin the same bytes through the real
// methods.

func marshalLikeTheClient(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sameBytes(t *testing.T, what string, old, named interface{}) {
	t.Helper()
	o, n := marshalLikeTheClient(t, old), marshalLikeTheClient(t, named)
	if !bytes.Equal(o, n) {
		t.Errorf("%s:\nmap    %s\nstruct %s", what, o, n)
	}
}

func TestWorkerRequestsWriteTheMapsBytes(t *testing.T) {
	for _, providers := range [][]string{nil, {}, {"claude-code", "codex-cli"}} {
		for _, hosted := range []bool{false, true} {
			workerID, minPriority := "w-1 <&>", 10
			sameBytes(t, "claim", map[string]interface{}{
				"worker_id":    workerID,
				"providers":    providers,
				"min_priority": minPriority,
				"hosted":       hosted,
			}, workerproto.ClaimRequest{
				Hosted:      hosted,
				MinPriority: minPriority,
				Providers:   providers,
				WorkerID:    workerID,
			})
		}
	}

	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, entries := range [][]agentruns.LogEntry{{}, {
		{RunID: "run-1", Seq: 1, Kind: agentruns.LogText, Payload: map[string]interface{}{"text": "a <b> & c"}, CreatedAt: at},
		{RunID: "run-1", Seq: 2, Kind: "tool_call", Payload: nil},
	}} {
		for _, partialText := range []string{"", "so far <ok>"} {
			sameBytes(t, "log push", map[string]interface{}{"entries": entries, "partial_text": partialText},
				workerproto.LogsRequest{Entries: entries, PartialText: partialText})
		}
		sameBytes(t, "legacy log push", entries, workerproto.LegacyLogsRequest(entries))
	}

	sameBytes(t, "release", map[string]string{
		"worker_id": "w-1",
	}, workerproto.ReleaseRequest{
		WorkerID: "w-1",
	})

	for _, report := range []map[string]map[string]interface{}{nil, {}, {
		"claude-code": {"installed": true, "version": "1.2.3", "logged_in": true, "detail": "<ok>"},
		"codex-cli":   {"installed": false, "version": "", "logged_in": false, "detail": "not found"},
	}} {
		sameBytes(t, "detection", report, workerproto.DetectionReport(report))
	}

	for _, pasteKind := range []string{"", "code"} {
		status, authURL, detail := "awaiting_code", "https://x.test/?a=1&b=<2>", "Open the link & paste"
		sameBytes(t, "sign-in progress", map[string]string{
			"status":     status,
			"auth_url":   authURL,
			"detail":     detail,
			"paste_kind": pasteKind,
		}, workerproto.LoginProgress{
			AuthURL:   authURL,
			Detail:    detail,
			PasteKind: pasteKind,
			Status:    status,
		})
	}

	for _, providers := range [][]string{nil, {"claude-code"}} {
		name, pool := "pool-a", "default"
		sameBytes(t, "pool registration", map[string]interface{}{
			"name":      name,
			"pool":      pool,
			"providers": providers,
		}, workerproto.PoolNodeRegistration{
			Name:      name,
			Pool:      pool,
			Providers: providers,
		})
	}

	sameBytes(t, "pool heartbeat", map[string]interface{}{}, workerproto.PoolHeartbeatRequest{})

	sameBytes(t, "pool release", map[string]interface{}{
		"session_id": "sess-1",
	}, workerproto.PoolNodeRelease{
		SessionID: "sess-1",
	})
}

// The log push's answer, which PushLogs decoded into an anonymous struct,
// decodes the same into workerproto.LogsResponse.
func TestLogPushAnswerDecodesAsTheAnonymousStructDid(t *testing.T) {
	for _, body := range []string{`{"cancel_requested":true,"status":"running"}`, `{"Cancel_Requested":true}`, `{}`, `null`, `{"status":5}`} {
		var old struct {
			CancelRequested bool   `json:"cancel_requested"`
			Status          string `json:"status"`
		}
		var named workerproto.LogsResponse
		errOld, errNamed := json.Unmarshal([]byte(body), &old), json.Unmarshal([]byte(body), &named)
		if (errOld == nil) != (errNamed == nil) || old.CancelRequested != named.CancelRequested || old.Status != named.Status {
			t.Errorf("%s: old %+v (%v), named %+v (%v)", body, old, errOld, named, errNamed)
		}
		if reflect.TypeOf(old).NumField() != reflect.TypeOf(named).NumField() {
			t.Fatalf("the named answer has %d fields, the old %d", reflect.TypeOf(named).NumField(), reflect.TypeOf(old).NumField())
		}
	}
}
