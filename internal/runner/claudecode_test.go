package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
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
// 750ms; the cut must not split a multi-byte rune. The cap is the API's own
// (agentruns), so the runner ships exactly what the API will store.
func TestTruncatePartialKeepsRunesWhole(t *testing.T) {
	if got := agentruns.TruncatePartial("short"); got != "short" {
		t.Fatalf("TruncatePartial(short) = %q", got)
	}
	long := strings.Repeat("é", agentruns.PartialTextLimit) // 2 bytes each
	got := agentruns.TruncatePartial(long)
	if len(got) > agentruns.PartialTextLimit {
		t.Fatalf("truncated length = %d, want <= %d", len(got), agentruns.PartialTextLimit)
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

// Codex reports whole agent messages, and its answer is the LAST one: the
// partial must show that same message, or the bubble would show text the
// finished reply then drops.
func TestCodexParserPartialIsTheCurrentAgentMessage(t *testing.T) {
	p := &codexParser{}
	p.ParseLine(`{"msg":{"type":"agent_message","message":"First part."}}`, func(RunEvent) {})
	if got := p.PartialText(); got != "First part." {
		t.Fatalf("codex partial after one message = %q", got)
	}
	p.ParseLine(`{"msg":{"type":"agent_reasoning","text":"thinking"}}`, func(RunEvent) {})
	p.ParseLine(`{"msg":{"type":"agent_message","message":"Second part."}}`, func(RunEvent) {})
	if got := p.PartialText(); got != "Second part." {
		t.Fatalf("codex partial = %q, want only the current message", got)
	}

	// Without a task_complete the result falls back to that same message.
	res, err := p.Result(0, "")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if res.FinalText != p.PartialText() {
		t.Fatalf("final %q does not match the partial %q the reader was shown", res.FinalText, p.PartialText())
	}
}

// task_complete names the answer explicitly; the partial shown just before it
// must be the same text, so nothing visibly changes when the reply lands.
func TestCodexPartialMatchesTaskCompleteFinal(t *testing.T) {
	p := &codexParser{}
	p.ParseLine(`{"msg":{"type":"agent_message","message":"Working on it."}}`, func(RunEvent) {})
	p.ParseLine(`{"msg":{"type":"agent_message","message":"REQ-1 is not testable."}}`, func(RunEvent) {})
	shown := p.PartialText()
	p.ParseLine(`{"msg":{"type":"task_complete","last_agent_message":"REQ-1 is not testable."}}`, func(RunEvent) {})

	res, err := p.Result(0, "")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if res.FinalText != shown {
		t.Fatalf("final %q, but the reader was shown %q", res.FinalText, shown)
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

// resetPartialMessagesProbe puts the process-wide probe cache back the way the
// test found it.
func resetPartialMessagesProbe(t *testing.T) {
	t.Helper()
	realProbe := claudeHelpProbe
	t.Cleanup(func() {
		claudeHelpProbe = realProbe
		partialMessagesProbe.mu.Lock()
		defer partialMessagesProbe.mu.Unlock()
		partialMessagesProbe.resolved = false
		partialMessagesProbe.supported = false
		partialMessagesProbe.loggedResolved = false
		partialMessagesProbe.loggedInconclusive = false
	})
	partialMessagesProbe.mu.Lock()
	defer partialMessagesProbe.mu.Unlock()
	partialMessagesProbe.resolved = false
	partialMessagesProbe.supported = false
	partialMessagesProbe.loggedResolved = false
	partialMessagesProbe.loggedInconclusive = false
}

// A probe that could not read `claude --help` — a cancelled context, a probe
// timeout, a momentarily unavailable binary — says nothing about the CLI, so
// it must not disable token streaming for the rest of the process.
func TestPartialMessagesProbeDoesNotLatchAnInconclusiveAnswer(t *testing.T) {
	resetPartialMessagesProbe(t)

	probes := 0
	claudeHelpProbe = func(context.Context) (string, error) {
		probes++
		return "", errors.New("signal: killed")
	}
	if claudeSupportsPartialMessages(context.Background()) {
		t.Fatal("a failed probe claimed the flag is supported")
	}
	if claudeSupportsPartialMessages(context.Background()) {
		t.Fatal("a failed probe claimed the flag is supported on the retry")
	}
	if probes != 2 {
		t.Fatalf("probes = %d, want the question asked again after an inconclusive answer", probes)
	}

	// The CLI answers this time: now the answer is cached.
	claudeHelpProbe = func(context.Context) (string, error) {
		probes++
		return "Usage: claude [options]\n  --include-partial-messages  stream deltas\n", nil
	}
	if !claudeSupportsPartialMessages(context.Background()) {
		t.Fatal("a successful probe did not report the advertised flag")
	}
	if probes != 3 {
		t.Fatalf("probes = %d, want the successful probe to have run", probes)
	}

	claudeHelpProbe = func(context.Context) (string, error) {
		probes++
		return "", errors.New("probe should not run again")
	}
	if !claudeSupportsPartialMessages(context.Background()) || probes != 3 {
		t.Fatalf("a definite answer was not cached: supported re-probed, probes = %d", probes)
	}
}

// A CLI that does not advertise the flag is a definite answer too: cached, so
// an older CLI is probed once and then left alone.
func TestPartialMessagesProbeLatchesAnUnsupportedCLI(t *testing.T) {
	resetPartialMessagesProbe(t)

	probes := 0
	claudeHelpProbe = func(context.Context) (string, error) {
		probes++
		// Help printed to stderr with a non-zero exit is still help.
		return "Usage: claude [options]\n  --output-format <fmt>\n", errors.New("exit status 1")
	}
	if claudeSupportsPartialMessages(context.Background()) {
		t.Fatal("the flag was reported on a CLI that does not advertise it")
	}
	if claudeSupportsPartialMessages(context.Background()) || probes != 1 {
		t.Fatalf("the negative answer was not cached: probes = %d", probes)
	}
}
