package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/interviews"
)

// fakeInterviewService embeds the interface so only the methods the public
// handlers touch need implementations.
type fakeInterviewService struct {
	interviews.Service
	interview *interviews.Interview
	invite    *interviews.Invite
	session   *interviews.Session // active session, nil for a first visit
	messages  []*interviews.Message
	starts    int // StartOrResumeSession call count
}

func (f *fakeInterviewService) ResolveInviteToken(rawToken string) (*interviews.Interview, *interviews.Invite, error) {
	if rawToken != "good-token" {
		return nil, nil, interviews.ErrInviteNotFound
	}
	return f.interview, f.invite, nil
}

func (f *fakeInterviewService) FindActiveSession(inviteID string) (*interviews.Session, error) {
	return f.session, nil
}

func (f *fakeInterviewService) StartOrResumeSession(inviteID, interviewID, participantName string) (*interviews.Session, error) {
	f.starts++
	if f.session == nil {
		f.session = &interviews.Session{
			ID:              "sess-1",
			InterviewID:     interviewID,
			InviteID:        inviteID,
			ParticipantName: participantName,
			Status:          interviews.SessionStatusActive,
			StartedAt:       time.Now(),
		}
	}
	return f.session, nil
}

func (f *fakeInterviewService) AppendMessage(sessionID, role, content string) (*interviews.Message, error) {
	m := &interviews.Message{ID: "msg", SessionID: sessionID, Role: role, Content: content}
	f.messages = append(f.messages, m)
	return m, nil
}

func (f *fakeInterviewService) GetTranscript(sessionID string) ([]*interviews.Message, error) {
	return f.messages, nil
}

func newInterviewTestHandler() (*Handler, *fakeInterviewService) {
	fake := &fakeInterviewService{
		// AgentID nil => launchInterviewTurn fails fast and the handler
		// appends the "interviewer unavailable" system note instead of
		// touching run/product/project services.
		interview: &interviews.Interview{ID: "int-1", ProjectID: "proj-1", Name: "Test Interview", Status: interviews.InterviewStatusOpen},
		invite:    &interviews.Invite{ID: "inv-1", InterviewID: "int-1"},
	}
	h := &Handler{
		interviewService:    fake,
		sseHub:              NewSSEHub(),
		interviewMsgLimiter: newRateLimiter(defaultInterviewMsgBurst, defaultInterviewMsgRefill),
		interviewIPLimiter:  newRateLimiter(defaultInterviewIPBurst, defaultInterviewIPRefill),
	}
	return h, fake
}

func postMessage(h *Handler, token string) *httptest.ResponseRecorder {
	body := strings.NewReader(`{"participant_name":"Ada","content":"hello"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/public/interviews/"+token+"/messages", body)
	r = mux.SetURLVars(r, map[string]string{"token": token})
	w := httptest.NewRecorder()
	h.PublicInterviewMessage(w, r)
	return w
}

func TestPublicInterviewMessageRateLimited(t *testing.T) {
	h, fake := newInterviewTestHandler()

	for i := 0; i < defaultInterviewMsgBurst; i++ {
		w := postMessage(h, "good-token")
		if w.Code != http.StatusOK {
			t.Fatalf("message %d: status = %d, body %q", i+1, w.Code, w.Body.String())
		}
	}

	w := postMessage(h, "good-token")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th rapid message: status = %d, want 429 (body %q)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("429 Content-Type = %q, want application/json", ct)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 response missing Retry-After header")
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload.Error == "" {
		t.Fatalf("429 body is not friendly JSON: %q (err %v)", w.Body.String(), err)
	}

	// The throttled message must not have been recorded or answered.
	participantMsgs := 0
	for _, m := range fake.messages {
		if m.Role == interviews.RoleParticipant {
			participantMsgs++
		}
	}
	if participantMsgs != defaultInterviewMsgBurst {
		t.Fatalf("participant messages recorded = %d, want %d", participantMsgs, defaultInterviewMsgBurst)
	}
}

func TestPublicInterviewMessageInvalidToken(t *testing.T) {
	h, _ := newInterviewTestHandler()
	w := postMessage(h, "bogus")
	if w.Code != http.StatusNotFound {
		t.Fatalf("invalid token: status = %d, want 404", w.Code)
	}
}

func TestPublicInterviewIntroDoesNotCreateSession(t *testing.T) {
	h, fake := newInterviewTestHandler()

	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/interviews/good-token", nil)
	r = mux.SetURLVars(r, map[string]string{"token": "good-token"})
	w := httptest.NewRecorder()
	h.PublicInterviewIntro(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("intro: status = %d, body %q", w.Code, w.Body.String())
	}
	if fake.starts != 0 {
		t.Fatalf("intro created a session (StartOrResumeSession called %d times)", fake.starts)
	}
	var payload struct {
		InterviewName string              `json:"interview_name"`
		Session       *interviews.Session `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("intro body: %v", err)
	}
	if payload.Session != nil {
		t.Fatal("intro returned a session for a first visit")
	}
	if payload.InterviewName != "Test Interview" {
		t.Fatalf("interview_name = %q", payload.InterviewName)
	}
}

func TestPublicInterviewIntroPerIPRateLimited(t *testing.T) {
	h, _ := newInterviewTestHandler()

	intro := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/public/interviews/good-token", nil)
		r.RemoteAddr = "203.0.113.5:4444"
		r = mux.SetURLVars(r, map[string]string{"token": "good-token"})
		w := httptest.NewRecorder()
		h.PublicInterviewIntro(w, r)
		return w
	}

	for i := 0; i < defaultInterviewIPBurst; i++ {
		if w := intro(); w.Code != http.StatusOK {
			t.Fatalf("intro %d: status = %d", i+1, w.Code)
		}
	}
	if w := intro(); w.Code != http.StatusTooManyRequests {
		t.Fatalf("intro past burst: status = %d, want 429", w.Code)
	}

	// A different IP is unaffected.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/interviews/good-token", nil)
	r.RemoteAddr = "198.51.100.20:5555"
	r = mux.SetURLVars(r, map[string]string{"token": "good-token"})
	w := httptest.NewRecorder()
	h.PublicInterviewIntro(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("intro from second IP: status = %d, want 200", w.Code)
	}
}
