# Assistant response latency: where the time goes

Date: 2026-09-07. Question from the maintainer: the AI agents' replies feel
slow — how much is the model thinking, and how much is everything else
between pressing Enter and the reply appearing?

## The path

One V&V Assistant turn (guided wizard or notes panel) is:

1. The browser posts the message (`POST …/messages`, or `chat/nudge` for a
   wizard event). The API stores it, builds the prompt (system rules, product
   profile, up to 12 KB of wizard state, the last 40 transcript messages) and
   enqueues an **agent run** at interview priority. Handler time: 8–70 ms.
2. A runner (the member's own agentd, or a leased pool node on Railway)
   **polls the queue every 2 s** and claims the run. Wait: 0.2–1.5 s.
3. The runner prepares a workspace (no repository for this agent, ~10 ms),
   marks the run started, and launches `claude -p` with the OpenV MCP server
   attached, the prompt on stdin.
4. The CLI boots, starts the MCP server, calls the model (one or more
   round trips if it uses tools), prints a result and exits.
5. The runner posts the finish; the API appends the reply to the transcript
   and broadcasts it on the session's SSE channel. Milliseconds.

The reply is shown only at step 5: nothing streams to the panel while the
model writes.

## Measured: one phone session on the leased pool runner, 2026-09-07

From the API request log and the pool node's log (UTC, session
`b36969be`, agent V&V Assistant, provider claude-code, effort low):

| Turn | Trigger | Enter → claim | Prep | CLI wall (start → finish) | Enter → reply |
|---|---|---|---|---|---|
| kickoff | wizard opened | 1.15 s | 0.01 s | 12.9 s | **14.1 s** |
| message | user typed | 0.30 s | 0.01 s | 14.6 s | **15.0 s** |
| message | user typed | 1.54 s | 0.01 s | 15.8 s | **17.3 s** |
| nudge | wizard step saved | 0.68 s | 0.02 s | 20.1 s | **20.8 s** |
| message | user typed | 0.77 s | 0.01 s | 19.2 s | **20.0 s** |
| nudge | wizard step saved | 0.23 s | 0.04 s | 18.0 s | **18.3 s** |
| nudge | wizard step saved | 0.20 s | 0.01 s | 19.5 s | **19.7 s** |

Everything outside the CLI (API handlers, queue wait, workspace, delivery)
is **0.3–1.6 s per turn, about 5 %**. The rest is inside `claude -p`.

## Measured: what the CLI itself costs

Headless `claude -p --output-format stream-json` with a one-line prompt, on
this session's machine (Claude Code 2.1.263, Sonnet), three runs:

| | Boot to ready | Model API time | Wall |
|---|---|---|---|
| no MCP server | 1.6 s | 1.7 s | 3.5 s |
| with the OpenV MCP server (67 tools listed) | 1.4 s | 2.0 s | 3.4 s |
| with the OpenV MCP server, again | 1.3 s | 1.5 s | 3.0 s |

So the fixed cost of a turn is **about 1.5 s of CLI boot** (the MCP server
adds nothing measurable) plus whatever the model needs. Subtracting that
from the production turns leaves **11–18 s of model time per reply**, about
90 % of what the person waits for.

Why the model takes that long is the prompt: every turn resends the whole
context (rules, profile, up to 12 KB of wizard state as JSON, forty
transcript messages), and the agent may call OpenV tools or web search
before answering, each a further round trip. Runs reported no timing until
now; the runner now records the CLI's `duration_ms`, `duration_api_ms` and
`num_turns` in each run's usage log entry and its finished log line, so
from the next release the split is exact per run.

## What would make it faster, in order of payoff

1. **Stream the reply.** The runner already ships the model's text to the API
   every 750 ms; the chat panel shows nothing until the run finishes. Showing
   text as it arrives would cut *perceived* latency from ~15 s to the first
   token (a few seconds) with no change to the model work.
2. **Send less prompt.** Continue the CLI session (`--resume`) instead of
   resending forty messages and the full state each turn, or trim to the
   current step's state and the last ten messages. Fewer input tokens is
   less model time and less cost, and a stable prefix caches.
3. **Pick the model for the job.** The assistant runs at low effort on the
   CLI's default model. A smaller model for chat turns roughly halves model
   time; a maintainer decision on quality.
4. **Stop paying for nudges.** Every wizard save launches a full turn (three
   of the seven above), and messages sent while one is running are answered
   by nobody. Debounce nudges, or make them a cheaper, shorter prompt.
5. **Poll faster for chat.** The 2 s claim poll costs 0.2–1.5 s a turn; a
   1 s poll for interview-priority runs, or a long-poll, takes that to near
   zero for a small increase in idle requests.
6. **Keep the CLI warm.** The 1.5 s boot recurs per turn; a resident CLI
   session per chat would remove it, but that is the largest change here and
   worth it only after 1–3.
