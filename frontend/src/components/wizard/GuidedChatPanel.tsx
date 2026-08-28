import React, { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { guidedAPI, GuidedChatMessage } from '../../api/client';

/** One structured proposal embedded in a copilot reply. */
export interface CopilotSuggestion {
  kind: 'framing' | 'persona' | 'need' | 'requirement' | 'nfr' | 'hazard';
  [key: string]: any;
}

interface GuidedChatPanelProps {
  sessionId: string;
  /** 1-based current wizard step, included in every turn's context. */
  step: number;
  /** Snapshot of everything entered in the wizard so far. */
  getState: () => Record<string, any>;
  /**
   * "<messageId>:<segmentIndex>" keys of suggestions already applied. Owned
   * by the wizard and persisted in the session answers, so the applied state
   * survives navigating away and remounting this panel.
   */
  applied: Record<string, boolean>;
  /**
   * Insert or replace a batch of suggestions in the wizard, applied in order
   * against one snapshot. Each item carries its suggestion key so the wizard
   * can record (and persist) what was applied. Returns one result per
   * suggestion: null on success, or a reason it could not be applied.
   */
  onApplySuggestions: (items: { suggestion: CopilotSuggestion; key: string }[]) => (string | null)[];
}

type Segment =
  | { type: 'text'; text: string }
  | { type: 'suggestion'; suggestion: CopilotSuggestion | null; raw: string };

/** Split a copilot reply into prose and ```openv-suggestion fenced JSON blocks. */
const parseSegments = (content: string): Segment[] => {
  const segments: Segment[] = [];
  const re = /```openv-suggestion\s*([\s\S]*?)```/g;
  let last = 0;
  let match: RegExpExecArray | null;
  while ((match = re.exec(content)) !== null) {
    if (match.index > last) {
      segments.push({ type: 'text', text: content.slice(last, match.index) });
    }
    let suggestion: CopilotSuggestion | null = null;
    try {
      const parsed = JSON.parse(match[1].trim());
      if (parsed && typeof parsed === 'object' && typeof parsed.kind === 'string') {
        suggestion = parsed as CopilotSuggestion;
      }
    } catch {
      // malformed block — shown as plain text below
    }
    segments.push({ type: 'suggestion', suggestion, raw: match[1].trim() });
    last = match.index + match[0].length;
  }
  if (last < content.length) {
    segments.push({ type: 'text', text: content.slice(last) });
  }
  return segments;
};

const KIND_LABELS: Record<string, string> = {
  framing: 'Product framing',
  persona: 'Persona',
  need: 'User need',
  requirement: 'Requirement',
  nfr: 'NFR',
  hazard: 'Hazard',
};

const FRAMING_FIELD_LABELS: Record<string, string> = {
  vision: 'Vision',
  problem_statement: 'Problem statement',
  target_users: 'Target users',
};

// Canned copilot commands, sent as ordinary chat messages so they read
// naturally in the transcript and the reply flows through the normal turn.
const QUICK_ACTIONS: { label: string; title: string; message: string }[] = [
  {
    label: 'Review step',
    title: 'Quality-check the entries on the current step',
    message:
      'Review this step: quality-check each entry I have filled in — clarity, specificity, testability — and call out the weak ones with concrete improvements (use replace suggestions where useful).',
  },
  {
    label: 'Gap analysis',
    title: 'What is missing, here and across the definition',
    message:
      'Do a gap analysis: given everything entered so far, what is missing on this step and across the definition as a whole? Propose the most important missing entries as suggestions.',
  },
  {
    label: 'Conflict check',
    title: 'Find contradictions, duplicates and inconsistencies',
    message:
      'Check for conflicts: contradictions, duplicates, or inconsistencies anywhere in what I have entered so far. List each one, say which entries clash, and how you would resolve it.',
  },
  {
    label: 'Draft for me',
    title: 'Propose entries for the current step',
    message:
      'Draft entries for this step based on everything you know about the product so far, as suggestion blocks I can add or apply.',
  },
];

const suggestionSummary = (s: CopilotSuggestion): { title: string; detail: string } => {
  switch (s.kind) {
    case 'framing':
      return { title: FRAMING_FIELD_LABELS[s.field] || String(s.field || ''), detail: s.text || '' };
    case 'persona':
      return { title: s.name || 'Unnamed persona', detail: s.role || '' };
    case 'need':
      return {
        title: `I need ${s.capability || '…'}`,
        detail: `${s.persona ? `As ${s.persona}, ` : ''}so that ${s.outcome || '…'}`,
      };
    case 'requirement':
      return { title: s.text || '', detail: s.fit_criterion ? `Fit: ${s.fit_criterion}` : '' };
    case 'nfr':
      return { title: `[${s.category || '?'}] ${s.text || ''}`, detail: s.fit_criterion ? `Fit: ${s.fit_criterion}` : '' };
    case 'hazard':
      return { title: s.hazard || '', detail: `${s.harm || ''}${s.severity ? ` (${s.severity})` : ''}` };
    default:
      return { title: JSON.stringify(s), detail: '' };
  }
};

export interface GuidedChatPanelHandle {
  /** Fire a copilot turn reacting to a wizard action (step saved/skipped); shows the thinking indicator immediately. */
  nudge: (step: number, event: string) => void;
}

// AI copilot chat beside the guided wizard: streams the session conversation
// over SSE and renders the copilot's structured proposals as one-click Adds.
export const GuidedChatPanel = forwardRef<GuidedChatPanelHandle, GuidedChatPanelProps>(({
  sessionId,
  step,
  getState,
  applied,
  onApplySuggestions,
}, ref) => {
  const [messages, setMessages] = useState<GuidedChatMessage[]>([]);
  const [composerText, setComposerText] = useState('');
  const [sending, setSending] = useState(false);
  const [typing, setTyping] = useState(false);
  const [sendError, setSendError] = useState('');
  // True when the API reports no runner online: turns queue unanswered, so
  // the panel shows connect instructions instead of a thinking indicator.
  const [runnerOffline, setRunnerOffline] = useState(false);
  // Which suggestions were applied lives in `applied` (persisted by the
  // wizard in the session answers) — NOT local state, so a remount cannot
  // re-arm Apply buttons and duplicate entries.

  const scrollerRef = useRef<HTMLDivElement>(null);
  const lastMessageRef = useRef<HTMLDivElement>(null);
  const esRef = useRef<EventSource | null>(null);
  const retryRef = useRef(0);
  const closedRef = useRef(false);
  const kickedRef = useRef(false);

  // Snapshot getters live in refs so the SSE effect doesn't resubscribe on
  // every wizard keystroke.
  const getStateRef = useRef(getState);
  getStateRef.current = getState;
  const stepRef = useRef(step);
  stepRef.current = step;

  const appendMessage = useCallback((msg: GuidedChatMessage) => {
    setMessages((prev) => {
      if (prev.some((m) => m.id === msg.id)) return prev;
      return [...prev, msg];
    });
    if (msg.role === 'assistant' || msg.role === 'system') {
      setTyping(false);
    }
    // An assistant reply proves a runner is processing turns again.
    if (msg.role === 'assistant') {
      setRunnerOffline(false);
    }
  }, []);

  // Digest a kickoff/nudge/send response: runner offline wins over any
  // status; otherwise "launched"/"pending" mean a reply is on its way.
  const applyTurnStatus = useCallback((data?: { status?: string; runner_online?: boolean }) => {
    if (!data) return;
    if (data.runner_online === false) {
      setRunnerOffline(true);
      setTyping(false);
      return;
    }
    setRunnerOffline(false);
    if (data.status === 'launched' || data.status === 'pending') {
      setTyping(true);
    } else if (data.status === 'unavailable') {
      setTyping(false);
    }
  }, []);

  const connectStream = useCallback(() => {
    if (!sessionId || closedRef.current) return;
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    const es = new EventSource(guidedAPI.chatStreamUrl(sessionId), { withCredentials: true });
    esRef.current = es;
    es.onopen = () => {
      retryRef.current = 0;
    };
    es.addEventListener('message', (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data) as GuidedChatMessage;
        if (msg && msg.id) appendMessage(msg);
      } catch {
        // ignore malformed events
      }
    });
    es.onerror = () => {
      es.close();
      if (esRef.current === es) esRef.current = null;
      if (closedRef.current) return;
      const attempt = Math.min(retryRef.current + 1, 6);
      retryRef.current = attempt;
      const delay = Math.min(1000 * 2 ** attempt, 15000);
      window.setTimeout(() => {
        if (!closedRef.current) connectStream();
      }, delay);
    };
  }, [sessionId, appendMessage]);

  useEffect(() => {
    if (!sessionId) return;
    closedRef.current = false;
    kickedRef.current = false;
    setMessages([]);
    let cancelled = false;
    (async () => {
      try {
        const res = await guidedAPI.listMessages(sessionId);
        if (cancelled) return;
        const transcript = res.data || [];
        transcript.forEach((m) => appendMessage(m));
        connectStream();
        // Empty conversation: ask the copilot to open with a question.
        if (transcript.length === 0 && !kickedRef.current) {
          kickedRef.current = true;
          try {
            const kick = await guidedAPI.kickoffChat(sessionId, stepRef.current, getStateRef.current());
            applyTurnStatus(kick.data);
          } catch {
            // copilot optional — the wizard still works without it
          }
        }
      } catch {
        if (!cancelled) setSendError('Failed to load the copilot conversation.');
      }
    })();
    return () => {
      cancelled = true;
      closedRef.current = true;
      if (esRef.current) {
        esRef.current.close();
        esRef.current = null;
      }
    };
  }, [sessionId, appendMessage, connectStream, applyTurnStatus]);

  // Wizard actions (Next/Skip) call this through a ref so the thinking
  // indicator appears the moment the user acts, not when the reply lands.
  useImperativeHandle(
    ref,
    () => ({
      nudge: (nudgeStep: number, event: string) => {
        if (!sessionId) return;
        setTyping(true);
        guidedAPI
          .nudgeChat(sessionId, nudgeStep, getStateRef.current(), event)
          .then((res) => applyTurnStatus(res.data))
          .catch(() => setTyping(false));
      },
    }),
    [sessionId, applyTurnStatus]
  );

  // Scroll so the START of the newest message is in view — the reader begins
  // at its top, not its tail.
  useEffect(() => {
    const scroller = scrollerRef.current;
    const last = lastMessageRef.current;
    if (!scroller || !last) return;
    const delta = last.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
    scroller.scrollTo({ top: scroller.scrollTop + delta - 4, behavior: 'smooth' });
  }, [messages]);

  // The typing indicator sits below the last message; bring it into view.
  useEffect(() => {
    if (!typing) return;
    const scroller = scrollerRef.current;
    if (scroller) scroller.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' });
  }, [typing]);

  // preset carries a quick-action message; without it the composer text is sent.
  const send = async (preset?: string) => {
    const content = (preset ?? composerText).trim();
    if (!sessionId || !content || sending) return;
    setSending(true);
    setSendError('');
    setTyping(true);
    try {
      const res = await guidedAPI.sendMessage(sessionId, content, stepRef.current, getStateRef.current());
      if (res.data.message) appendMessage(res.data.message);
      if (res.data.runner_online === false) {
        setRunnerOffline(true);
        setTyping(false);
      } else {
        setRunnerOffline(false);
      }
      if (!preset) setComposerText('');
    } catch {
      setTyping(false);
      setSendError('Message failed to send — please try again.');
    } finally {
      setSending(false);
    }
  };

  // Apply every not-yet-applied suggestion of one reply in a single batch.
  // The wizard records and persists which keys applied successfully.
  const applyAll = (pending: { suggestion: CopilotSuggestion; key: string }[]) => {
    const results = onApplySuggestions(pending);
    const errors = results.filter((r): r is string => r !== null);
    setSendError(
      errors.length > 0
        ? `${errors.length} suggestion${errors.length > 1 ? 's' : ''} could not be applied — ${errors[0]}`
        : ''
    );
  };

  const renderSuggestion = (seg: Segment & { type: 'suggestion' }, key: string) => {
    if (!seg.suggestion) {
      return (
        <pre key={key} style={{ fontSize: 11, background: '#f4f6f7', padding: 8, borderRadius: 4, overflowX: 'auto' }}>
          {seg.raw}
        </pre>
      );
    }
    const s = seg.suggestion;
    const { title, detail } = suggestionSummary(s);
    const isAdded = !!applied[key];
    // A suggestion replaces existing content when it targets an entry
    // ("replaces") or rewrites a framing field that already has text.
    let isReplace = !!s.replaces;
    if (s.kind === 'framing') {
      try {
        const step1 = (getStateRef.current()?.step_1 || {}) as Record<string, any>;
        isReplace = !!String(step1[String(s.field)] || '').trim();
      } catch {
        isReplace = false;
      }
    }
    const buttonLabel = isReplace ? 'Replace in wizard' : s.kind === 'framing' ? 'Apply to wizard' : '+ Add to wizard';
    const doneLabel = isReplace ? '✓ Replaced in wizard' : s.kind === 'framing' ? '✓ Applied to wizard' : '✓ Added to wizard';
    return (
      <div
        key={key}
        style={{
          border: '1px solid #d6e4f0',
          background: '#f4f9fd',
          borderRadius: 6,
          padding: '8px 10px',
          margin: '6px 0',
        }}
      >
        <div style={{ fontSize: 10, fontWeight: 700, color: '#3498db', textTransform: 'uppercase', marginBottom: 3 }}>
          {KIND_LABELS[s.kind] || s.kind}
        </div>
        <div style={{ fontSize: 13, color: '#2c3e50', marginBottom: detail ? 2 : 6 }}>{title}</div>
        {detail && <div style={{ fontSize: 12, color: '#7f8c8d', marginBottom: 6 }}>{detail}</div>}
        {s.replaces && (
          <div style={{ fontSize: 11, color: '#95a5a6', fontStyle: 'italic', marginBottom: 6 }}>
            Replaces: {String(s.replaces)}
          </div>
        )}
        {isAdded ? (
          <span style={{ fontSize: 12, color: '#27ae60' }}>{doneLabel}</span>
        ) : (
          <button
            className="button-secondary"
            style={{ padding: '4px 10px', fontSize: 12 }}
            onClick={() => {
              const reason = onApplySuggestions([{ suggestion: s, key }])[0];
              setSendError(reason === null ? '' : reason);
            }}
          >
            {buttonLabel}
          </button>
        )}
      </div>
    );
  };

  return (
    <div
      style={{
        width: 340,
        minWidth: 340,
        display: 'flex',
        flexDirection: 'column',
        background: '#fff',
        border: '1px solid #ddd',
        borderRadius: 4,
        position: 'sticky',
        top: 20,
        height: 'calc(100vh - 160px)',
        minHeight: 420,
      }}
    >
      <div style={{ padding: '10px 14px', borderBottom: '1px solid #e0e0e0' }}>
        <div style={{ fontWeight: 700, color: '#2c3e50', fontSize: 14 }}>Requirements Copilot</div>
        <div style={{ fontSize: 11, color: '#7f8c8d' }}>
          Asks questions and suggests entries you can add with one click.
        </div>
      </div>

      <div ref={scrollerRef} style={{ flex: 1, overflowY: 'auto', padding: 12 }}>
        {messages.length === 0 && !typing && (
          <div style={{ textAlign: 'center', color: '#95a5a6', fontSize: 12, marginTop: 24 }}>
            The copilot will join in a moment — or ask it anything about your requirements.
          </div>
        )}
        {messages.map((m, i) => (
          <div
            key={m.id}
            ref={i === messages.length - 1 ? lastMessageRef : undefined}
            style={{
              display: 'flex',
              justifyContent: m.role === 'user' ? 'flex-end' : m.role === 'system' ? 'center' : 'flex-start',
              marginBottom: 8,
            }}
          >
            {m.role === 'system' ? (
              <div style={{ fontSize: 11, fontStyle: 'italic', color: '#95a5a6', textAlign: 'center' }}>
                {m.content}
              </div>
            ) : (
              <div
                style={{
                  maxWidth: '90%',
                  padding: '8px 12px',
                  borderRadius: 12,
                  fontSize: 13,
                  lineHeight: 1.5,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  background: m.role === 'user' ? '#3498db' : '#eef1f4',
                  color: m.role === 'user' ? '#fff' : '#2c3e50',
                  borderBottomRightRadius: m.role === 'user' ? 4 : 12,
                  borderBottomLeftRadius: m.role === 'assistant' ? 4 : 12,
                }}
              >
                {m.role === 'assistant'
                  ? (() => {
                      const segments = parseSegments(m.content);
                      const pending = segments
                        .map((seg, i) => ({ seg, key: `${m.id}:${i}` }))
                        .filter(
                          (x): x is { seg: Segment & { type: 'suggestion' }; key: string } =>
                            x.seg.type === 'suggestion' && !!x.seg.suggestion && !applied[x.key]
                        )
                        .map((x) => ({ suggestion: x.seg.suggestion as CopilotSuggestion, key: x.key }));
                      return (
                        <>
                          {segments.map((seg, i) =>
                            seg.type === 'text' ? (
                              seg.text.trim() ? <span key={`${m.id}:${i}`}>{seg.text}</span> : null
                            ) : (
                              renderSuggestion(seg, `${m.id}:${i}`)
                            )
                          )}
                          {pending.length >= 2 && (
                            <div style={{ marginTop: 8 }}>
                              <button
                                className="button"
                                onClick={() => applyAll(pending)}
                                style={{
                                  background: '#3498db',
                                  padding: '5px 12px',
                                  fontSize: 12,
                                  width: 'auto',
                                }}
                              >
                                Apply all ({pending.length})
                              </button>
                            </div>
                          )}
                        </>
                      );
                    })()
                  : m.content}
              </div>
            )}
          </div>
        ))}
        {runnerOffline && (
          <div
            style={{
              border: '1px solid #f0c36d',
              background: '#fdf6e3',
              borderRadius: 6,
              padding: '10px 12px',
              marginBottom: 8,
              fontSize: 12,
              color: '#8a6d3b',
              lineHeight: 1.5,
            }}
          >
            <div style={{ fontWeight: 700, marginBottom: 4 }}>⚠ Copilot agent not connected</div>
            No runner is online to answer, so copilot replies are paused. To connect one, open{' '}
            <Link to="/org/settings" style={{ color: '#8a6d3b', fontWeight: 600 }}>
              Workspace Settings → Runners
            </Link>{' '}
            and launch the Agent Connector on your machine — or restart it if it was already running.
            Messages you send are saved and will be answered as soon as a runner connects.
          </div>
        )}
        {typing && !runnerOffline && (
          <div style={{ display: 'flex', justifyContent: 'flex-start', marginBottom: 8 }}>
            <div
              style={{
                padding: '8px 12px',
                borderRadius: 12,
                fontSize: 12,
                fontStyle: 'italic',
                background: '#eef1f4',
                color: '#7f8c8d',
              }}
            >
              The copilot is thinking…
            </div>
          </div>
        )}
      </div>

      <div style={{ borderTop: '1px solid #e0e0e0', padding: 10 }}>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 8 }}>
          {QUICK_ACTIONS.map((qa) => (
            <button
              key={qa.label}
              onClick={() => send(qa.message)}
              disabled={sending}
              title={qa.title}
              style={{
                background: '#f4f9fd',
                border: '1px solid #d6e4f0',
                color: '#2980b9',
                borderRadius: 12,
                padding: '4px 10px',
                fontSize: 11,
                cursor: 'pointer',
                width: 'auto',
                whiteSpace: 'nowrap',
                opacity: sending ? 0.5 : 1,
              }}
            >
              {qa.label}
            </button>
          ))}
        </div>
        {sendError && <div style={{ color: '#e74c3c', fontSize: 11, marginBottom: 6 }}>{sendError}</div>}
        <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end' }}>
          <textarea
            value={composerText}
            onChange={(e) => setComposerText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                e.preventDefault();
                send();
              }
            }}
            placeholder="Ask the copilot… (Ctrl+Enter to send)"
            rows={2}
            style={{ flex: 1, minHeight: 40, resize: 'none', fontSize: 13 }}
          />
          <button
            className="button"
            onClick={() => send()}
            disabled={sending || !composerText.trim()}
            style={{
              background: '#3498db',
              opacity: sending || !composerText.trim() ? 0.5 : 1,
              whiteSpace: 'nowrap',
              padding: '8px 12px',
              fontSize: 13,
            }}
          >
            {sending ? '…' : 'Send'}
          </button>
        </div>
      </div>
    </div>
  );
});

GuidedChatPanel.displayName = 'GuidedChatPanel';
