package agentruns

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateAnswer(t *testing.T) {
	short := "a short answer"
	if got := TruncateAnswer(short); got != short {
		t.Errorf("short answer should pass through, got %q", got)
	}

	long := strings.Repeat("x", MaxAnswerChars+100)
	got := TruncateAnswer(long)
	if len(got) != MaxAnswerChars+len("…") {
		t.Errorf("want %d bytes, got %d", MaxAnswerChars+len("…"), len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("truncated answer should end with an ellipsis")
	}

	// A multi-byte character straddling the limit must not be split.
	multi := strings.Repeat("x", MaxAnswerChars-1) + strings.Repeat("é", 200)
	got = TruncateAnswer(multi)
	if !utf8.ValidString(got) {
		t.Error("truncation split a UTF-8 character")
	}
	if len(got) > MaxAnswerChars+len("…") {
		t.Errorf("truncated answer too long: %d bytes", len(got))
	}
}

func TestAnswerLengthRuleMentionsLimit(t *testing.T) {
	if !strings.Contains(AnswerLengthRule, "8000") {
		t.Errorf("rule text should state the %d-char limit: %q", MaxAnswerChars, AnswerLengthRule)
	}
}

// Trust is a property of the run's origin, not of the agent it happens to be
// bound to: an interview may name any agent (agent_slug on create), and its
// turns still carry a participant's own words. The flag rides on the run, so
// it survives a restart and reaches whichever runner claims it (REQ-91,
// HAZ-1).
func TestUntrustedOrigin(t *testing.T) {
	session := "sess-1"
	guided := "guided-1"
	cases := []struct {
		name string
		run  *Run
		want bool
	}{
		{"nil", nil, false},
		{"a plain launch", &Run{ID: "r1"}, false},
		{"an interview turn", &Run{ID: "r1", InterviewSessionID: &session}, true},
		// A guided session is a member of the workspace working in the UI,
		// not a stranger on an invite link.
		{"a guided-session run", &Run{ID: "r1", GuidedSessionID: &guided}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.run.UntrustedOrigin(); got != tc.want {
				t.Errorf("UntrustedOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}
