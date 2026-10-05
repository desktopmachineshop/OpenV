import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { publicInterviewAPI, InterviewMessage } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useConfirm } from '../components/ui';
import { ChatMarkdown } from '../components/ChatMarkdown';
import { RECONNECT, useEventStream } from '../hooks/useEventStream';

type Phase = 'loading' | 'error' | 'name' | 'chat' | 'done';

// What the page says when its intro gets no answer at all (#379 bug 215).
const OFFLINE = "We couldn't reach the server. Check your connection and reload the page.";
// A session the intro reports is over: the page shows its thank-you.
const isOver = (status?: string) => status === 'finished' || status === 'completed';

// Public standalone interview page — no auth, no app shell. Mobile-friendly
// full-height chat reached via /interview/:token invite links.
export const InterviewChat: React.FC = () => {
  const { token } = useParams<{ token: string }>();
  const confirm = useConfirm();

  const [phase, setPhase] = useState<Phase>('loading');
  const [interviewName, setInterviewName] = useState('');
  const [messages, setMessages] = useState<InterviewMessage[]>([]);
  const [participantName, setParticipantName] = useState('');
  const [nameInput, setNameInput] = useState('');
  const [composerText, setComposerText] = useState('');
  const [sending, setSending] = useState(false);
  const [typing, setTyping] = useState(false);
  // The interviewer's reply as it is written (`assistant_partial`): always
  // the whole text so far, replaced by the final message when it lands.
  const [partial, setPartial] = useState('');
  const [sendError, setSendError] = useState('');
  // Why the intro failed when the link itself is fine: the server's words
  // for a rate limit, or OFFLINE. Empty for a refusal: a broken link.
  const [notice, setNotice] = useState('');
  // The interview whose live stream is open: its token once the intro is in
  // and the session still open, null when the interview ends or the page
  // leaves the link. No stream for a finished session or a broken link.
  const [liveToken, setLiveToken] = useState<string | null>(null);

  const bottomRef = useRef<HTMLDivElement>(null);

  const appendMessage = useCallback((msg: InterviewMessage) => {
    setMessages((prev) => {
      if (prev.some((m) => m.id === msg.id)) return prev;
      return [...prev, msg];
    });
    if (msg.role === 'assistant' || msg.role === 'system') {
      setTyping(false);
      setPartial('');
    }
  }, []);

  useEffect(() => {
    if (!token) {
      setPhase('error');
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const res = await publicInterviewAPI.intro(token);
        if (cancelled) return;
        setInterviewName(res.data.interview_name || 'Interview');
        setMessages(res.data.transcript || []);
        const session = res.data.session;
        if (isOver(session?.status)) {
          setPhase('done');
          return;
        }
        if (session?.participant_name) {
          setParticipantName(session.participant_name);
          setPhase('chat');
        } else {
          setPhase('name');
        }
        setLiveToken(token);
      } catch (err: any) {
        if (cancelled) return;
        // Only a refusal (404, 410, any answer but a 429) says the link is
        // broken; a rate limit or no answer at all says to wait (#379 bug 215).
        const status = err?.response?.status;
        if (status === 429) setNotice(apiErrorMessage(err, 'Please wait a moment and reload the page.'));
        else setNotice(status ? '' : OFFLINE);
        setPhase('error');
      }
    })();
    return () => {
      cancelled = true;
      setLiveToken(null);
    };
  }, [token]);

  // The interview is over: its thank-you page, and no stream.
  const showThanks = () => {
    setLiveToken(null);
    setPhase('done');
  };

  // A drop asks the intro again before its retry: an interview ended
  // elsewhere refuses its stream (409), which an EventSource cannot read, so
  // a completed session shows the thank-you, which cancels the retry; no
  // answer lets it go ahead (#379 bug 216).
  const tokenRef = useRef(token);
  tokenRef.current = token;
  const recheck = () => {
    const asked = token;
    if (!asked) return;
    publicInterviewAPI.intro(asked).then(
      (res) => {
        if (asked === tokenRef.current && isOver(res.data.session?.status)) showThanks();
      },
      () => {}
    );
  };

  // Live updates via SSE, from liveToken: opened once the intro is in, and
  // closed the moment the link changes, before the new link's intro is asked
  // for. A drop retries for ever after 2, 4, 8, then 15 s, an open restarting
  // the count (RECONNECT.cappedExponent); a retry pending when the link
  // changes, the interview ends or the page unmounts is cancelled (#379, bug 203).
  useEventStream(
    liveToken !== null && liveToken === token ? publicInterviewAPI.streamUrl(liveToken) : null,
    {
      message: (event) => {
        try {
          const msg = JSON.parse(event.data) as InterviewMessage;
          if (msg && msg.id) appendMessage(msg);
        } catch {
          // ignore malformed events
        }
      },
      assistant_partial: (event) => {
        try {
          const data = JSON.parse(event.data) as { run_id?: string; text?: string };
          if (typeof data?.text !== 'string' || !data.text) return;
          setPartial(data.text);
          setTyping(false);
        } catch {
          // ignore malformed events
        }
      },
    },
    { withCredentials: false, reconnect: { ...RECONNECT.cappedExponent, onRetry: recheck } }
  );

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, typing, partial]);

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
      // The interview has ended elsewhere: the answer is refused (#379 bug 216).
      if (err?.response?.status === 409) return showThanks();
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
    const ok = await confirm({
      title: 'End interview',
      message: 'End the interview? You will not be able to continue afterwards.',
      confirmLabel: 'End interview',
      danger: true,
    });
    if (!ok) return;
    try {
      await publicInterviewAPI.finish(token);
    } catch {
      // Even if finish fails, show the thank-you screen — the link may already be closed.
    }
    showThanks();
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
        <div style={{ fontSize: 44, marginBottom: 12 }}>{notice ? '⏳' : '🔗'}</div>
        <h2 style={{ color: 'var(--text)', marginBottom: 10 }}>
          {notice ? "The interview didn't load" : "This link isn't working"}
        </h2>
        <p style={{ color: 'var(--text-muted)', lineHeight: 1.6 }}>
          {notice ||
            'The interview invite may have expired or been revoked. Please ask the person who sent it to you for a new link.'}
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
        {messages.length === 0 && !typing && !partial && (
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
                  // The participant's own words are literal; the assistant
                  // replies in markdown and brings its own block layout.
                  whiteSpace: m.role === 'participant' ? 'pre-wrap' : 'normal',
                  wordBreak: 'break-word',
                  background: m.role === 'participant' ? 'var(--accent)' : 'var(--surface-alt)',
                  color: m.role === 'participant' ? 'var(--accent-fg)' : 'var(--text)',
                  borderBottomRightRadius: m.role === 'participant' ? 4 : 14,
                  borderBottomLeftRadius: m.role === 'assistant' ? 4 : 14,
                }}
              >
                {m.role === 'participant' ? m.content : <ChatMarkdown text={m.content} />}
              </div>
            )}
          </div>
        ))}
        {partial && (
          <div data-testid="assistant-partial" style={{ display: 'flex', justifyContent: 'flex-start', marginBottom: 10 }}>
            <div
              style={{
                maxWidth: '80%',
                padding: '10px 14px',
                borderRadius: 14,
                borderBottomLeftRadius: 4,
                fontSize: 14,
                lineHeight: 1.5,
                wordBreak: 'break-word',
                background: 'var(--surface-alt)',
                color: 'var(--text)',
              }}
            >
              <ChatMarkdown text={partial} streaming />
            </div>
          </div>
        )}
        {typing && !partial && (
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
            enterKeyHint="enter"
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

// The page on its route, /interview/:token. A new link mounts a new page, so
// nothing of the previous link's interview (its conversation, a half-typed
// answer) stays on show while the new one loads, nor after (#379 bug 219).
export const InterviewChatRoute: React.FC = () => {
  const { token } = useParams<{ token: string }>();
  return <InterviewChat key={token} />;
};
