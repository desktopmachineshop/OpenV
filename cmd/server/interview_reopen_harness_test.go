//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAnEndedInterviewLinkReopensOnItsThankYouPage opens an interview link
// again after its participant ended the interview, on the real server, in a
// database of its own (OPENV_TEST_DATABASE_URL; skipped when unset), as
// #379 bug 214 found it. The intro answers the invite's session, completed,
// without its transcript (null: the link reads no finished conversation), so
// the page shows its thank-you and opens no stream; before the fix it
// answered no session, and the page asked for a name again and opened a
// stream, which started a new, anonymous session on the same invite. The
// intro still writes nothing: a fresh invite has no session after it, and an
// ended one keeps its one session.
func TestAnEndedInterviewLinkReopensOnItsThankYouPage(t *testing.T) {
	bin := serverBinary(t)
	s, _ := bootServer(t, bin, nil)

	var session *http.Cookie
	call := func(method, path, body string, signedIn bool, want int, into interface{}) {
		t.Helper()
		req, err := http.NewRequest(method, s.base+path, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if signedIn {
			req.AddCookie(session)
		}
		resp, err := s.client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, s.output())
		}
		answer, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, resp.StatusCode, strings.TrimSpace(string(answer)), want)
		}
		if into != nil {
			if err := json.Unmarshal(answer, into); err != nil {
				t.Fatalf("%s %s: %v in %s", method, path, err, answer)
			}
		}
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

	var project, interview struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/v1/projects", `{"name":"Interviews"}`, true, http.StatusCreated, &project)
	call(http.MethodPost, "/api/v1/projects/"+project.ID+"/interviews",
		`{"name":"Onboarding","brief":"How do you onboard a new machinist?"}`, true, http.StatusCreated, &interview)
	invite := func(label string) string {
		t.Helper()
		var created struct {
			Token string `json:"token"`
		}
		call(http.MethodPost, "/api/v1/interviews/"+interview.ID+"/invites", `{"invitee_label":"`+label+`"}`, true,
			http.StatusCreated, &created)
		return created.Token
	}
	dana, lee := invite("Dana"), invite("Lee")

	type sessionRow struct {
		ID              string `json:"id"`
		ParticipantName string `json:"participant_name"`
		Status          string `json:"status"`
	}
	// intro answers the session, what its transcript says, and the
	// transcript as sent.
	intro := func(token string) (*sessionRow, []string, string) {
		t.Helper()
		var answer struct {
			Session    *sessionRow     `json:"session"`
			Transcript json.RawMessage `json:"transcript"`
		}
		call(http.MethodGet, "/api/v1/public/interviews/"+token, "", false, http.StatusOK, &answer)
		var messages []struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(answer.Transcript, &messages); err != nil {
			t.Fatalf("the intro's transcript %s: %v", answer.Transcript, err)
		}
		var said []string
		for _, m := range messages {
			said = append(said, m.Content)
		}
		return answer.Session, said, string(answer.Transcript)
	}
	sessions := func() []sessionRow {
		t.Helper()
		var rows []sessionRow
		call(http.MethodGet, "/api/v1/interviews/"+interview.ID+"/sessions", "", true, http.StatusOK, &rows)
		return rows
	}

	// A fresh invite: no session, and the intro opens none.
	if got, _, _ := intro(lee); got != nil {
		t.Fatalf("the intro of a fresh invite: session %+v, want none", *got)
	}
	if rows := sessions(); len(rows) != 0 {
		t.Fatalf("sessions after the intro of a fresh invite: %+v, want none (the intro is read-only)", rows)
	}

	// Dana's interview, under way: the intro answers its active session.
	const said = "I need fast exports."
	var started struct {
		Session sessionRow `json:"session"`
	}
	call(http.MethodPost, "/api/v1/public/interviews/"+dana+"/messages",
		`{"participant_name":"Dana","content":"`+said+`"}`, false, http.StatusOK, &started)
	got, transcript, _ := intro(dana)
	if got == nil || got.ID != started.Session.ID || got.Status != "active" || got.ParticipantName != "Dana" {
		t.Fatalf("the intro of Dana's interview under way: session %+v, want %s, active, Dana's", got, started.Session.ID)
	}
	if len(transcript) == 0 || transcript[0] != said {
		t.Fatalf("the intro of Dana's interview under way: transcript %q, want her message first", transcript)
	}

	// Dana ends it, and opens her link again: the same session, completed,
	// which the page shows as its thank-you, with no stream, and without its
	// transcript.
	call(http.MethodPost, "/api/v1/public/interviews/"+dana+"/finish", "", false, http.StatusNoContent, nil)
	for i := 0; i < 2; i++ {
		var sent string
		got, _, sent = intro(dana)
		if got == nil {
			t.Fatalf("the intro of Dana's ended interview: no session, so the page asks her name again and its " +
				"stream starts a new session")
		}
		if got.ID != started.Session.ID || got.Status != "completed" || got.ParticipantName != "Dana" {
			t.Fatalf("the intro of Dana's ended interview: session %+v, want %s, completed, Dana's", *got,
				started.Session.ID)
		}
		if sent != "null" {
			t.Fatalf("the intro of Dana's ended interview: transcript %s, want null (only an active session's is sent)",
				sent)
		}
	}
	if rows := sessions(); len(rows) != 1 || rows[0].ID != started.Session.ID || rows[0].Status != "completed" {
		t.Fatalf("sessions after the intros of Dana's ended interview: %+v, want only hers, completed", rows)
	}
	if got, _, _ := intro(lee); got != nil {
		t.Fatalf("the intro of the fresh invite, once Dana's ended: session %+v, want none", *got)
	}
}
