package notify

import (
	"strings"
	"testing"
	"time"
)

func TestInterviewInviteLink(t *testing.T) {
	if got := InterviewInviteLink(" https://app.example.com/ ", "a/b c"); got != "https://app.example.com/interview/a%2Fb%20c" {
		t.Errorf("link = %q", got)
	}
}

func TestRenderInterviewInviteEmail(t *testing.T) {
	expires := time.Date(2026, 12, 5, 23, 0, 0, 0, time.UTC)
	subject, body := RenderInterviewInviteEmail("Ada", "Brake pedal feel", "https://x/interview/t", &expires)
	if subject != "Ada invited you to an interview on OpenV" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"Ada would like to hear from you", "Brake pedal feel", "https://x/interview/t", "until 5 December 2026", "do not need an account"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}

	// No names and no expiry: neutral copy, and no expiry sentence.
	subject, body = RenderInterviewInviteEmail("", "", "https://x/interview/t", nil)
	if subject != "You are invited to an interview on OpenV" || strings.Contains(body, "until") {
		t.Errorf("subject %q, body:\n%s", subject, body)
	}

	// A name cannot start a header or a line of its own.
	subject, body = RenderInterviewInviteEmail("Eve\r\nBcc: x@example.com", "Q\nlink: https://evil.example", "https://x/interview/t", nil)
	if strings.ContainsAny(subject, "\r\n") {
		t.Errorf("subject carries a line break: %q", subject)
	}
	if strings.Contains(body, "\nlink: https://evil.example") || strings.Contains(body, "\nBcc:") {
		t.Errorf("a name broke the body's lines:\n%s", body)
	}
}

func TestRenderInvitationEmailWithProjects(t *testing.T) {
	plainSubject, plainBody := RenderInvitationEmail("Acme", "Ada", "https://x/login?invite=t", 7*24*time.Hour)
	if s, b := RenderInvitationEmailWithProjects("Acme", "Ada", nil, "https://x/login?invite=t", 7*24*time.Hour); s != plainSubject || b != plainBody {
		t.Error("no projects must be the plain workspace invitation")
	}
	subject, body := RenderInvitationEmailWithProjects("Acme", "Ada", []string{"Brakes"}, "https://x/login?invite=t", 7*24*time.Hour)
	if subject != "You have been invited to the project Brakes on OpenV" {
		t.Errorf("subject = %q", subject)
	}
	if !strings.Contains(body, "Ada has invited you to the project Brakes in Acme on OpenV.") {
		t.Errorf("body:\n%s", body)
	}
	_, body = RenderInvitationEmailWithProjects("Acme", "", []string{"A", " ", "B\nBcc: x", "C"}, "l", 7*24*time.Hour)
	if !strings.Contains(body, "You have been invited to the projects A, B Bcc: x and C in Acme on OpenV.") {
		t.Errorf("body:\n%s", body)
	}
}
