package runner

import (
	"strings"
	"testing"
)

// feed replays recorded stream-json lines through a parser, discarding the
// events (this file is about the answer-so-far, not the run log).
func feed(p *claudeParser, lines ...string) {
	for _, line := range lines {
		p.ParseLine(line, func(RunEvent) {})
	}
}

// With --include-partial-messages the CLI emits the raw model stream. The
// text deltas are what a reader wants to watch appear; tool-use input deltas
// are noise and must not reach the chat bubble.
func TestClaudeParserAccumulatesTextDeltas(t *testing.T) {
	p := &claudeParser{}
	if got := p.PartialText(); got != "" {
		t.Fatalf("fresh parser partial = %q, want empty", got)
	}

	feed(p,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1","role":"assistant"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Your vision "}}}`,
	)
	if got := p.PartialText(); got != "Your vision " {
		t.Fatalf("partial after one delta = %q", got)
	}

	feed(p, `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"statement is vague."}}}`)
	if got := p.PartialText(); got != "Your vision statement is vague." {
		t.Fatalf("partial after two deltas = %q", got)
	}

	// A tool call's arguments stream as input_json_delta: never part of the
	// answer the reader sees.
	feed(p, `{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"ref\":\"REQ-1\"}"}}}`)
	if got := p.PartialText(); got != "Your vision statement is vague." {
		t.Fatalf("tool input leaked into the partial answer: %q", got)
	}
}

// The completed `assistant` message is authoritative for the text it reports:
// the deltas that built it must not be counted a second time.
func TestClaudeParserAssistantMessageReplacesItsDeltas(t *testing.T) {
	p := &claudeParser{}
	feed(p,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"the requirements"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Checking the requirements."},{"type":"tool_use","id":"t1","name":"get_project_map","input":{}}]}}`,
	)
	if got := p.PartialText(); got != "Checking the requirements." {
		t.Fatalf("partial after the assistant message = %q, want the message's own text once", got)
	}

	// A second message continues the answer, paragraph-separated.
	feed(p,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"REQ-1 "}}}`,
	)
	if got := p.PartialText(); got != "Checking the requirements.\n\nREQ-1 " {
		t.Fatalf("partial across two messages = %q", got)
	}
	feed(p, `{"type":"assistant","message":{"content":[{"type":"text","text":"REQ-1 is not testable."}]}}`)
	if got := p.PartialText(); got != "Checking the requirements.\n\nREQ-1 is not testable." {
		t.Fatalf("partial after the second message = %q", got)
	}
}

// An older CLI (or one without the flag) emits no stream_event lines at all;
// the answer then grows a whole message at a time, which is still ahead of
// the run's finish.
func TestClaudeParserStreamsWithoutPartialMessages(t *testing.T) {
	p := &claudeParser{}
	feed(p,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"One moment."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"get_context","input":{}}]}}`,
	)
	if got := p.PartialText(); got != "One moment." {
		t.Fatalf("partial = %q, want only the text message", got)
	}

	res, err := p.Result(0, "")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if res.FinalText != "" {
		t.Fatalf("final text = %q without a result line", res.FinalText)
	}
}

// Whitespace-only text blocks are not an answer forming.
func TestClaudeParserIgnoresBlankText(t *testing.T) {
	p := &claudeParser{}
	feed(p,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"   "}]}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"\n"}}}`,
	)
	if got := p.PartialText(); strings.TrimSpace(got) != "" {
		t.Fatalf("partial = %q, want nothing worth showing", got)
	}
}

// The pump caps what it ships so one runaway run cannot push megabytes every
// 750ms; the cut must not split a multi-byte rune.
func TestTruncatePartialKeepsRunesWhole(t *testing.T) {
	if got := truncatePartial("short"); got != "short" {
		t.Fatalf("truncatePartial(short) = %q", got)
	}
	long := strings.Repeat("é", partialTextLimit) // 2 bytes each
	got := truncatePartial(long)
	if len(got) > partialTextLimit {
		t.Fatalf("truncated length = %d, want <= %d", len(got), partialTextLimit)
	}
	if !strings.HasPrefix(long, got) {
		t.Fatal("truncation did not keep the head of the text")
	}
	for _, r := range got {
		if r != 'é' {
			t.Fatalf("truncation split a rune: found %q", r)
		}
	}
}

// Codex reports whole agent messages; they accumulate the same way.
func TestCodexParserAccumulatesAgentMessages(t *testing.T) {
	p := &codexParser{}
	p.ParseLine(`{"msg":{"type":"agent_message","message":"First part."}}`, func(RunEvent) {})
	p.ParseLine(`{"msg":{"type":"agent_reasoning","text":"thinking"}}`, func(RunEvent) {})
	p.ParseLine(`{"msg":{"type":"agent_message","message":"Second part."}}`, func(RunEvent) {})
	if got := p.PartialText(); got != "First part.\n\nSecond part." {
		t.Fatalf("codex partial = %q", got)
	}
}

// gemini buffers one JSON object until the end, so it has nothing to stream
// and must not claim otherwise.
func TestGeminiParserReportsNoPartialText(t *testing.T) {
	var parser streamParser = &geminiParser{}
	if _, ok := parser.(PartialTextSource); ok {
		t.Fatal("gemini parser advertises partial text it cannot produce")
	}
}
