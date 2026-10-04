//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDeletingAProjectRemovesItsFilesAndStopsItsRuns deletes a project on
// the real server (#379 bugs 136 and 137), in a database of its own
// (OPENV_TEST_DATABASE_URL; skipped when unset), from outside over HTTP: a
// project with a figure in two versions, an evidence file, a queued agent
// run and a run a worker has claimed, beside another project with a figure
// of its own. Deleting it removes the project's three stored files from
// the uploads directory, and leaves the other project's; cancels the queued
// run; asks the claimed run to stop, which its worker reads on its next log
// push; and refuses the claimed run's token at once. Before the fix the
// files stayed on disk and the runs went on with no project, the token
// still signing in.
func TestDeletingAProjectRemovesItsFilesAndStopsItsRuns(t *testing.T) {
	bin := serverBinary(t)
	s, _ := bootServer(t, bin, nil)
	uploads := filepath.Join(s.tmp, "uploads")

	type request struct {
		method, path, contentType string
		body                      []byte
		session                   *http.Cookie
		bearer                    string
	}
	send := func(r request) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(r.method, s.base+r.path, bytes.NewReader(r.body))
		if err != nil {
			t.Fatal(err)
		}
		if r.contentType != "" {
			req.Header.Set("Content-Type", r.contentType)
		}
		if r.session != nil {
			req.AddCookie(r.session)
		}
		if r.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+r.bearer)
		}
		resp, err := s.client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", r.method, r.path, err, s.output())
		}
		answer, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, answer
	}
	var session *http.Cookie
	// call sends JSON as the account, or with a bearer key, and decodes the
	// answer into into, failing unless the status is want.
	call := func(method, path, body, bearer string, want int, into interface{}) {
		t.Helper()
		r := request{method: method, path: path, body: []byte(body), bearer: bearer}
		if body != "" {
			r.contentType = "application/json"
		}
		if bearer == "" {
			r.session = session
		}
		status, answer := send(r)
		if status != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, status, answer, want)
		}
		if into != nil {
			if err := json.Unmarshal(answer, into); err != nil {
				t.Fatalf("%s %s: %v in %s", method, path, err, answer)
			}
		}
	}
	// upload posts one file as the multipart form field "file", beside the
	// fields given.
	upload := func(path, filename, mime string, content []byte, fields map[string]string, into interface{}) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for k, v := range fields {
			if err := form.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
		header.Set("Content-Type", mime)
		part, err := form.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(content)
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		status, answer := send(request{method: http.MethodPost, path: path, contentType: form.FormDataContentType(),
			body: body.Bytes(), session: session})
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("upload to %s: %d %s", path, status, answer)
		}
		if err := json.Unmarshal(answer, into); err != nil {
			t.Fatalf("upload to %s: %v in %s", path, err, answer)
		}
	}
	picture := func(shade uint8) []byte {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{shade, shade, shade, 255})
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	stored := func() []string {
		t.Helper()
		entries, err := os.ReadDir(uploads)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		return names
	}

	body, _ := json.Marshal(map[string]string{"email": harnessEmail, "password": harnessPassword, "name": "Boot Harness"})
	resp, err := s.client().Post(s.base+"/api/v1/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "openv_session" {
			session = c
		}
	}
	if resp.StatusCode != http.StatusOK || session == nil {
		t.Fatalf("register: %s and no session cookie\n%s", resp.Status, s.output())
	}
	var workspaces struct {
		ActiveOrg string `json:"active_org"`
	}
	call(http.MethodGet, "/api/v1/orgs", "", "", http.StatusOK, &workspaces)
	var key struct {
		Key string `json:"key"`
	}
	call(http.MethodPost, "/api/v1/orgs/"+workspaces.ActiveOrg+"/worker-keys", `{"name":"harness"}`, "", http.StatusCreated, &key)

	var doomed, kept, requirement, keptRequirement, agent, bundle struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/v1/projects", `{"name":"Doomed"}`, "", http.StatusCreated, &doomed)
	call(http.MethodPost, "/api/v1/projects", `{"name":"Kept"}`, "", http.StatusCreated, &kept)
	call(http.MethodPost, "/api/v1/artifacts", `{"project_id":"`+doomed.ID+`","type":"requirement","title":"Brake in time"}`, "",
		http.StatusCreated, &requirement)
	// The other project's figure goes on its second requirement, REQ-2: a
	// figure reference is unique across every project, so a second
	// REQ-1-FIG-1 would be refused.
	call(http.MethodPost, "/api/v1/artifacts", `{"project_id":"`+kept.ID+`","type":"requirement","title":"Start"}`, "",
		http.StatusCreated, nil)
	call(http.MethodPost, "/api/v1/artifacts", `{"project_id":"`+kept.ID+`","type":"requirement","title":"Stay"}`, "",
		http.StatusCreated, &keptRequirement)

	var figure, keptFigure struct {
		ID       string `json:"id"`
		FilePath string `json:"file_path"`
	}
	upload("/api/v1/attachments/upload", "brake.png", "image/png", picture(10), map[string]string{"artifact_id": requirement.ID}, &figure)
	var version struct{}
	upload("/api/v1/attachments/"+figure.ID+"/versions", "brake-v2.png", "image/png", picture(20), nil, &version)
	upload("/api/v1/attachments/upload", "stay.png", "image/png", picture(30), map[string]string{"artifact_id": keptRequirement.ID}, &keptFigure)
	call(http.MethodPost, "/api/v1/projects/"+doomed.ID+"/evidence-bundles", `{"title":"Bench logs"}`, "", http.StatusCreated, &bundle)
	var evidence struct{}
	upload("/api/v1/evidence-bundles/"+bundle.ID+"/files", "bench.log", "text/plain", []byte("brake at 12 ms\n"), nil, &evidence)
	before := stored()
	if len(before) != 4 {
		t.Fatalf("stored before the delete: %q, want 4 files (two versions of a figure, an evidence file, the other project's figure)", before)
	}

	agentBody, _ := json.Marshal(map[string]any{"slug": "harness-worker", "name": "Harness Worker", "provider": "claude-code",
		"allowed_tools": []string{"get_artifact"}, "write_mode": "direct", "system_prompt": "Answer in one line."})
	call(http.MethodPost, "/api/v1/agents", string(agentBody), "", http.StatusCreated, &agent)
	var claimedRun, queuedRun struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/v1/agents/harness-worker/runs", `{"project_id":"`+doomed.ID+`","prompt":"Summarise."}`, "",
		http.StatusCreated, &claimedRun)
	var claim struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
		RunToken string `json:"run_token"`
	}
	call(http.MethodPost, "/api/v1/agent-runs/claim", `{"worker_id":"harness","providers":["claude-code"]}`, key.Key,
		http.StatusOK, &claim)
	if claim.Run.ID != claimedRun.ID || claim.RunToken == "" {
		t.Fatalf("the claim took %q with token %q, want the first run %q and its token", claim.Run.ID, claim.RunToken, claimedRun.ID)
	}
	call(http.MethodPost, "/api/v1/agents/harness-worker/runs", `{"project_id":"`+doomed.ID+`","prompt":"Summarise again."}`, "",
		http.StatusCreated, &queuedRun)
	call(http.MethodGet, "/api/v1/projects", "", claim.RunToken, http.StatusOK, nil)

	call(http.MethodDelete, "/api/v1/projects/"+doomed.ID, "", "", http.StatusNoContent, nil)

	if after, want := stored(), filepath.Base(keptFigure.FilePath); len(after) != 1 || after[0] != want {
		t.Errorf("stored after the delete: %q, want only the other project's figure %q (before: %q)", after, want, before)
	}
	var queued, claimed struct {
		Status          string `json:"status"`
		CancelRequested bool   `json:"cancel_requested"`
	}
	call(http.MethodGet, "/api/v1/agent-runs/"+queuedRun.ID, "", "", http.StatusOK, &queued)
	if queued.Status != "cancelled" {
		t.Errorf("the queued run after the delete: %+v, want cancelled", queued)
	}
	call(http.MethodGet, "/api/v1/agent-runs/"+claimedRun.ID, "", "", http.StatusOK, &claimed)
	if claimed.Status != "claimed" || !claimed.CancelRequested {
		t.Errorf("the claimed run after the delete: %+v, want claimed and asked to stop", claimed)
	}
	var push struct {
		CancelRequested bool `json:"cancel_requested"`
	}
	call(http.MethodPost, "/api/v1/agent-runs/"+claimedRun.ID+"/logs", `{"entries":[]}`, key.Key, http.StatusOK, &push)
	if !push.CancelRequested {
		t.Errorf("the worker's log push for the claimed run after the delete: %+v, want the cancel requested", push)
	}
	if status, answer := send(request{method: http.MethodGet, path: "/api/v1/projects", bearer: claim.RunToken}); status != http.StatusUnauthorized {
		t.Errorf("the claimed run's token after the delete: %d %s, want 401", status, strings.TrimSpace(string(answer)))
	}
}
