import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { publicInterviewAPI, InterviewMessage } from '../api/client';

type Phase = 'loading' | 'error' | 'name' | 'chat' | 'done';

// Public standalone interview page — no auth, no app shell. Mobile-friendly
// full-height chat reached via /interview/:token invite links.
export const InterviewChat: React.FC = () => {
  const { token } = useParams<{ token: string }>();

  const [phase, setPhase] = useState<Phase>('loading');
  const [interviewName, setInterviewName] = useState('');
  const [messages, setMessages] = useState<InterviewMessage[]>([]);
  const [participantName, setParticipantName] = useState('');
  const [nameInput, setNameInput] = useState('');
  const [composerText, setComposerText] = useState('');
  const [sending, setSending] = useState(false);
  const [typing, setTyping] = useState(false);
  const [sendError, setSendError] = useState('');

  const bottomRef = useRef<HTMLDivElement>(null);
  const esRef = useRef<EventSource | null>(null);
  const retryRef = useRef<number>(0);
  const closedRef = useRef(false);

  const appendMessage = useCallback((msg: InterviewMessage) => {
    setMessages((prev) => {
      if (prev.some((m) => m.id === msg.id)) return prev;
      return [...prev, msg];
    });
    if (msg.role === 'assistant' || msg.role === 'system') {
      setTyping(false);
    }
  }, []);

  // Live updates via SSE, with reconnect + backoff.
  const connectStream = useCallback(() => {
    if (!token || closedRef.current) return;
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    const es = new EventSource(publicInterviewAPI.streamUrl(token), { withCredentials: false });
    esRef.current = es;
    es.onopen = () => {
      retryRef.current = 0;
    };
    es.addEventListener('message', (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data) as InterviewMessage;
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
  }, [token, appendMessage]);

  useEffect(() => {
    if (!token) {
      setPhase('error');
      return;
    }
    closedRef.current = false;
    let cancelled = false;
    (async () => {
      try {
        const res = await publicInterviewAPI.intro(token);
        if (cancelled) return;
        setInterviewName(res.data.interview_name || 'Interview');
        setMessages(res.data.transcript || []);
        const session = res.data.session;
        if (session?.status === 'finished' || session?.status === 'completed') {
          setPhase('done');
          return;
        }
        if (session?.participant_name) {
          setParticipantName(session.participant_name);
          setPhase('chat');
        } else {
          setPhase('name');
        }
        connectStream();
      } catch {
        if (!cancelled) setPhase('error');
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
  }, [token, connectStream]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, typing]);

  const submitName = (e: React.FormEvent) => {
    e.preventDefault();
    if (!nameInput.trim()) return;
    setParticipantName(nameInput.trim());
    setPhase('chat');
  };

  const send = async () => {
    const content = composerText.trim();
    if (!token || !content || sending) return;
    setSending(true);
    setSendError('');
    setTyping(true);
    try {
      const res = await publicInterviewAPI.sendMessage(token, content, participantName);
      if (res.data.message) appendMessage(res.data.message);
      setComposerText('');
    } catch (err: any) {
      setTyping(false);
      // Surface server-provided messages (e.g. the rate-limit notice) verbatim.
      const serverMsg = err?.response?.data?.error;
      setSendError(
        typeof serverMsg === 'string' && serverMsg
          ? serverMsg
          : 'Message failed to send — please try again.'
      );
    } finally {
      setSending(false);
    }
  };

  const endInterview = async () => {
    if (!token) return;
    if (!window.confirm('End the interview? You will not be able to continue afterwards.')) return;
    try {
      await publicInterviewAPI.finish(token);
    } catch {
      // Even if finish fails, show the thank-you screen — the link may already be closed.
    }
    closedRef.current = true;
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    setPhase('done');
  };

  const page = (children: React.ReactNode) => (
    <div
      style={{
        minHeight: '100vh',
        background: 'var(--bg-app)',
        display: 'flex',
        justifyContent: 'center',
      }}
    >
      <div
        style={{
          width: '100%',
          maxWidth: 640,
          display: 'flex',
          flexDirection: 'column',
          height: '100vh',
        }}
      >
        {children}
      </div>
    </div>
  );

  if (phase === 'loading') {
    return page(
      <div style={{ margin: 'auto', color: 'var(--text-muted)', fontSize: 15 }}>Loading interview…</div>
    );
  }

  if (phase === 'error') {
    return page(
      <div style={{ margin: 'auto', textAlign: 'center', padding: 24 }}>
        <div style={{ fontSize: 44, marginBottom: 12 }}>🔗</div>
        <h2 style={{ color: 'var(--text)', marginBottom: 10 }}>This link isn't working</h2>
        <p style={{ color: 'var(--text-muted)', lineHeight: 1.6 }}>
          The interview invite may have expired or been revoked. Please ask the person who sent it to
          you for a new link.
        </p>
      </div>
    );
  }

  if (phase === 'done') {
    return page(
      <div style={{ margin: 'auto', textAlign: 'center', padding: 24 }}>
        <div style={{ fontSize: 44, marginBottom: 12 }}>🙏</div>
        <h2 style={{ color: 'var(--success)', marginBottom: 10 }}>Thank you!</h2>
        <p style={{ color: 'var(--text-muted)', lineHeight: 1.6 }}>
          Your feedback has been recorded. You can close this page now.
        </p>
      </div>
    );
  }

  if (phase === 'name') {
    return page(
      <div style={{ margin: 'auto', width: '100%', padding: 24 }}>
        <div
          style={{
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 28,
            textAlign: 'center',
          }}
        >
          <h2 style={{ color: 'var(--text)', marginBottom: 8 }}>{interviewName}</h2>
          <p style={{ color: 'var(--text-muted)', fontSize: 14, marginBottom: 20, lineHeight: 1.6 }}>
            You've been invited to a short interview. Before we begin, what should we call you?
          </p>
          <form onSubmit={submitName}>
            <input
              value={nameInput}
              onChange={(e) => setNameInput(e.target.value)}
              placeholder="Your name"
              autoFocus
              style={{ marginBottom: 14, textAlign: 'center' }}
            />
            <button
              type="submit"
              className="button"
              style={{ background: 'var(--accent)', width: '100%' }}
              disabled={!nameInput.trim()}
            >
              Start interview
            </button>
          </form>
        </div>
      </div>
    );
  }

  // phase === 'chat'
  return page(
    <>
      <header
        style={{
          padding: '14px 16px',
          background: 'var(--surface)',
          borderBottom: '1px solid var(--border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 10,
        }}
      >
        <div style={{ minWidth: 0 }}>
          <div style={{ fontWeight: 700, color: 'var(--text)', fontSize: 15, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {interviewName}
          </div>
          {participantName && (
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Interviewing {participantName}</div>
          )}
        </div>
        <button
          onClick={endInterview}
          style={{
            background: 'none',
            border: '1px solid var(--danger)',
            color: 'var(--danger)',
            borderRadius: 4,
            padding: '6px 12px',
            fontSize: 12,
            cursor: 'pointer',
            width: 'auto',
            whiteSpace: 'nowrap',
          }}
        >
          End interview
        </button>
      </header>

      <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
        {messages.length === 0 && !typing && (
          <div style={{ textAlign: 'center', color: 'var(--neutral)', fontSize: 13, marginTop: 30 }}>
            Say hello to get started — the interviewer will guide the conversation.
          </div>
        )}
        {messages.map((m) => (
          <div
            key={m.id}
            style={{
              display: 'flex',
              justifyContent:
                m.role === 'participant' ? 'flex-end' : m.role === 'system' ? 'center' : 'flex-start',
              marginBottom: 10,
            }}
          >
            {m.role === 'system' ? (
              <div style={{ fontSize: 12, fontStyle: 'italic', color: 'var(--neutral)', textAlign: 'center' }}>
                {m.content}
              </div>
            ) : (
              <div
                style={{
                  maxWidth: '80%',
                  padding: '10px 14px',
                  borderRadius: 14,
                  fontSize: 14,
                  lineHeight: 1.5,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  background: m.role === 'participant' ? 'var(--accent)' : 'var(--surface-alt)',
                  color: m.role === 'participant' ? '#fff' : 'var(--text)',
                  borderBottomRightRadius: m.role === 'participant' ? 4 : 14,
                  borderBottomLeftRadius: m.role === 'assistant' ? 4 : 14,
                }}
              >
                {m.content}
              </div>
            )}
          </div>
        ))}
        {typing && (
          <div style={{ display: 'flex', justifyContent: 'flex-start', marginBottom: 10 }}>
            <div
              style={{
                padding: '10px 14px',
                borderRadius: 14,
                fontSize: 13,
                fontStyle: 'italic',
                background: 'var(--surface-alt)',
                color: 'var(--text-muted)',
              }}
            >
              The interviewer is thinking…
            </div>
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      <div style={{ background: 'var(--surface)', borderTop: '1px solid var(--border)', padding: 12 }}>
        {sendError && (
          <div style={{ color: 'var(--danger)', fontSize: 12, marginBottom: 6 }}>{sendError}</div>
        )}
        <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
          <textarea
            value={composerText}
            onChange={(e) => setComposerText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                e.preventDefault();
                send();
              }
            }}
            placeholder="Type your answer… (Ctrl+Enter to send)"
            rows={2}
            style={{ flex: 1, minHeight: 44, resize: 'none', fontSize: 14 }}
          />
          <button
            className="button"
            onClick={send}
            disabled={sending || !composerText.trim()}
            style={{
              background: 'var(--accent)',
              opacity: sending || !composerText.trim() ? 0.5 : 1,
              whiteSpace: 'nowrap',
            }}
          >
            {sending ? 'Sending…' : 'Send'}
          </button>
        </div>
      </div>
    </>
  );
};
