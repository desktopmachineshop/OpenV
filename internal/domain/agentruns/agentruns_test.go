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
