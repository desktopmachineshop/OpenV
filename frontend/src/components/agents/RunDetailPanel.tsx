import React, { useCallback, useEffect, useRef, useState } from 'react';
import { AgentRun, agentRunsAPI, RunLogEntry } from '../../api/client';
import { ExpandableText } from '../ExpandableText';

const TERMINAL_STATUSES = ['succeeded', 'failed', 'timed_out', 'cancelled'];

// Statuses the backend accepts on POST /agent-runs/{id}/retry (succeeded is
// deliberately excluded — retrying success invites duplicate side effects).
const RETRYABLE_STATUSES = ['failed', 'timed_out', 'cancelled'];

// After an SSE drop we retry the stream this many times (with exponential
// backoff) before settling on the 3s polling fallback for good.
const MAX_SSE_RECONNECT_ATTEMPTS = 3;
const SSE_RECONNECT_BASE_DELAY_MS = 1000;

// Human-readable label + color for a run's structured failure class (issue
// #184). Retryable classes (provider_unavailable, timeout, worker_error) read
// amber; terminal-by-design ones (auth, agent_error, workspace) read red.
const ERROR_CLASS_META: Record<string, { label: string; color: string; retryable: boolean }> = {
  provider_unavailable: { label: 'provider unavailable', color: 'var(--warning)', retryable: true },
  timeout: { label: 'timeout', color: 'var(--warning)', retryable: true },
  worker_error: { label: 'worker error', color: 'var(--warning)', retryable: true },
  auth: { label: 'auth', color: 'var(--danger)', retryable: false },
  workspace: { label: 'workspace', color: 'var(--danger)', retryable: false },
  agent_error: { label: 'agent error', color: 'var(--danger)', retryable: false },
};

export const errorClassMeta = (
  errorClass?: string | null
): { label: string; color: string; retryable: boolean } | null => {
  if (!errorClass) return null;
  return ERROR_CLASS_META[errorClass] || { label: errorClass, color: 'var(--danger)', retryable: false };
};

// ErrorClassChip renders a run's failure class as a small pill (nothing for a
// run that succeeded or was cancelled — those carry no class).
export const ErrorClassChip: React.FC<{ errorClass?: string | null; title?: string }> = ({
  errorClass,
  title,
}) => {
  const meta = errorClassMeta(errorClass);
  if (!meta) return null;
  return (
    <span
      title={title || `failure class: ${meta.label}${meta.retryable ? ' (retryable)' : ''}`}
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 12,
        background: meta.color,
        color: '#fff',
        fontSize: 10.5,
        fontWeight: 600,
        marginLeft: 6,
      }}
    >
      {meta.label}
    </span>
  );
};

export const runStatusColor = (status: string): string => {
  switch (status) {
    case 'queued':
    case 'claimed':
      return 'var(--neutral)';
    case 'running':
      return 'var(--accent)';
    case 'awaiting_approval':
      return 'var(--warning)';
    case 'succeeded':
      return 'var(--success)';
    case 'failed':
    case 'timed_out':
      return 'var(--danger)';
    case 'cancelled':
      return 'var(--neutral)';
    default:
      return 'var(--neutral)';
  }
};

interface RunDetailPanelProps {
  runId: string;
  onSelectRun: (runId: string) => void;
  onClose: () => void;
}

const logLine = (entry: RunLogEntry, idx: number): React.ReactNode => {
  const p = entry.payload || {};
  switch (entry.kind) {
    case 'tool_call': {
      const name = p.name || p.tool || 'tool';
      const args = p.args ?? p.input ?? {};
      let argsText = '';
      try {
        argsText = typeof args === 'string' ? args : JSON.stringify(args);
      } catch {
        argsText = String(args);
      }
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--accent)', margin: '3px 0', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
        >
          → tool: {name}(<ExpandableText text={argsText} limit={300} />)
        </div>
      );
    }
    case 'tool_result': {
      let text = p.text || p.result || p.content || '';
      if (typeof text !== 'string') {
        try {
          text = JSON.stringify(text);
        } catch {
          text = String(text);
        }
      }
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--neutral)', margin: '3px 0', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
        >
          <ExpandableText text={text} limit={400} />
        </div>
      );
    }
    case 'error':
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontSize: 12.5, color: 'var(--danger)', margin: '3px 0', whiteSpace: 'pre-wrap' }}
        >
          {p.text || p.error || p.message || JSON.stringify(p)}
        </div>
      );
    default:
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontSize: 13, color: 'var(--text)', margin: '4px 0', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
        >
          <ExpandableText text={typeof p.text === 'string' ? p.text : JSON.stringify(p)} limit={1500} />
        </div>
      );
  }
};

export const RunDetailPanel: React.FC<RunDetailPanelProps> = ({ runId, onSelectRun, onClose }) => {
  const [run, setRun] = useState<AgentRun | null>(null);
  const [logs, setLogs] = useState<RunLogEntry[]>([]);
  const [tree, setTree] = useState<AgentRun[]>([]);
  const [error, setError] = useState('');
  const [retrying, setRetrying] = useState(false);
  const logsEndRef = useRef<HTMLDivElement>(null);
  const lastSeqRef = useRef(0);
  const statusRef = useRef('');

  const loadRun = useCallback(() => {
    agentRunsAPI
      .get(runId)
      .then((res) => {
        setRun(res.data);
        statusRef.current = res.data.status;
      })
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load run')
      );
    agentRunsAPI
      .tree(runId)
      .then((res) => setTree(res.data || []))
      .catch(() => setTree([]));
  }, [runId]);

  // Log streaming with polling fallback
  useEffect(() => {
    setLogs([]);
    lastSeqRef.current = 0;
    loadRun();

    let es: EventSource | null = null;
    let pollTimer: number | null = null;
    let closed = false;

    const appendLogs = (entries: RunLogEntry[]) => {
      if (entries.length === 0) return;
      setLogs((prev) => {
        const seen = new Set(prev.map((l) => l.seq));
        const fresh = entries.filter((l) => !seen.has(l.seq));
        if (fresh.length === 0) return prev;
        const next = [...prev, ...fresh].sort((a, b) => a.seq - b.seq);
        lastSeqRef.current = next[next.length - 1]?.seq || lastSeqRef.current;
        return next;
      });
    };

    const startPolling = () => {
      if (pollTimer !== null || closed) return;
      const poll = () => {
        agentRunsAPI
          .logs(runId, lastSeqRef.current)
          .then((res) => appendLogs(res.data || []))
          .catch(() => undefined);
        agentRunsAPI
          .get(runId)
          .then((res) => {
            setRun(res.data);
            statusRef.current = res.data.status;
            if (TERMINAL_STATUSES.includes(res.data.status) && pollTimer !== null) {
              window.clearInterval(pollTimer);
              pollTimer = null;
            }
          })
          .catch(() => undefined);
      };
      poll();
      pollTimer = window.setInterval(poll, 3000);
    };

    let reconnectAttempts = 0;
    let reconnectTimer: number | null = null;

    const connectStream = () => {
      if (closed) return;
      try {
        // Resume from the last seq we saw so nothing is lost across drops.
        es = new EventSource(agentRunsAPI.streamUrl(runId, lastSeqRef.current), {
          withCredentials: true,
        });
        es.addEventListener('log', (evt: MessageEvent) => {
          // A live event proves the stream is healthy again — reset the budget
          // so the next drop gets a fresh set of reconnect attempts.
          reconnectAttempts = 0;
          try {
            const entry: RunLogEntry = JSON.parse(evt.data);
            appendLogs([entry]);
          } catch {
            // ignore malformed events
          }
        });
        es.addEventListener('status', (evt: MessageEvent) => {
          reconnectAttempts = 0;
          let status = '';
          try {
            const data = JSON.parse(evt.data);
            status = typeof data === 'string' ? data : data.status || '';
            setRun((prev) => (prev ? { ...prev, ...((typeof data === 'object' && data) || {}), status } : prev));
          } catch {
            status = evt.data;
            setRun((prev) => (prev ? { ...prev, status } : prev));
          }
          statusRef.current = status;
          if (TERMINAL_STATUSES.includes(status)) {
            es?.close();
            // Refresh once for final text / tokens.
            loadRun();
          }
        });
        es.onerror = () => {
          es?.close();
          es = null;
          if (closed || TERMINAL_STATUSES.includes(statusRef.current)) return;
          if (reconnectAttempts < MAX_SSE_RECONNECT_ATTEMPTS) {
            const delay = SSE_RECONNECT_BASE_DELAY_MS * 2 ** reconnectAttempts;
            reconnectAttempts += 1;
            // Catch up on anything missed while disconnected, then retry SSE.
            agentRunsAPI
              .logs(runId, lastSeqRef.current)
              .then((res) => appendLogs(res.data || []))
              .catch(() => undefined);
            reconnectTimer = window.setTimeout(connectStream, delay);
          } else {
            // Reconnect budget exhausted — settle on polling.
            startPolling();
          }
        };
      } catch {
        startPolling();
      }
    };

    connectStream();

    return () => {
      closed = true;
      es?.close();
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer);
      if (pollTimer !== null) window.clearInterval(pollTimer);
    };
  }, [runId, loadRun]);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs.length]);

  const cancel = async () => {
    try {
      const res = await agentRunsAPI.cancel(runId);
      setRun(res.data);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to cancel run');
    }
  };

  const retry = async () => {
    setRetrying(true);
    setError('');
    try {
      // The backend enqueues a NEW run (same agent/prompt/project, launched
      // by the current user); jump the panel to it.
      const res = await agentRunsAPI.retry(runId);
      onSelectRun(res.data.id);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to retry run');
    } finally {
      setRetrying(false);
    }
  };

  const renderTree = (nodes: AgentRun[], parentId: string | null, depth: number): React.ReactNode =>
    nodes
      .filter((n) => (n.parent_run_id || null) === parentId)
      .map((n) => (
        <React.Fragment key={n.id}>
          <div
            onClick={() => onSelectRun(n.id)}
            style={{
              paddingLeft: depth * 18,
              padding: `4px 6px 4px ${6 + depth * 18}px`,
              cursor: 'pointer',
              fontSize: 13,
              background: n.id === runId ? 'var(--tint-blue)' : 'transparent',
              borderRadius: 3,
              display: 'flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <span style={{ color: 'var(--text)' }}>{n.agent_name || n.agent_id}</span>
            <span
              style={{
                fontSize: 10.5,
                background: runStatusColor(n.status),
                color: '#fff',
                borderRadius: 8,
                padding: '1px 7px',
                fontWeight: 600,
              }}
            >
              {n.status}
            </span>
          </div>
          {renderTree(nodes, n.id, depth + 1)}
        </React.Fragment>
      ));

  const isLive = run && !TERMINAL_STATUSES.includes(run.status);
  const isRetryable = run && RETRYABLE_STATUSES.includes(run.status);

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: 4,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid var(--neutral-soft)',
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        <strong style={{ flex: 1, fontSize: 14, color: 'var(--text)' }}>
          {run?.agent_name || 'Run'}{' '}
          {run && (
            <span
              style={{
                fontSize: 11,
                background: runStatusColor(run.status),
                color: '#fff',
                borderRadius: 10,
                padding: '2px 9px',
                fontWeight: 600,
                marginLeft: 6,
              }}
            >
              {run.status}
            </span>
          )}
          {run && <ErrorClassChip errorClass={run.error_class} />}
        </strong>
        {isLive && (
          <button
            onClick={cancel}
            style={{
              background: 'var(--danger)',
              color: '#fff',
              border: 'none',
              padding: '5px 12px',
              borderRadius: 4,
              cursor: 'pointer',
              fontSize: 12,
            }}
          >
            Cancel
          </button>
        )}
        {isRetryable && (
          <button
            onClick={retry}
            disabled={retrying}
            title="Re-run this prompt as a new run with the same agent and project"
            style={{
              background: 'var(--accent)',
              color: 'var(--accent-fg)',
              border: 'none',
              padding: '5px 12px',
              borderRadius: 4,
              cursor: retrying ? 'default' : 'pointer',
              fontSize: 12,
              opacity: retrying ? 0.6 : 1,
            }}
          >
            {retrying ? 'Retrying…' : 'Retry'}
          </button>
        )}
        <button
          onClick={onClose}
          style={{ background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: 'var(--text-muted)' }}
        >
          ×
        </button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
        {error && <div style={{ color: 'var(--danger)', fontSize: 12, marginBottom: 8 }}>{error}</div>}

        {run && (
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10, lineHeight: 1.7 }}>
            <div>
              Started: {run.started_at ? new Date(run.started_at).toLocaleString() : '—'} · Tokens:{' '}
              {run.tokens_in + run.tokens_out}
              {run.cost_usd != null && ` · Cost: $${run.cost_usd.toFixed(4)}`}
            </div>
            {run.retried_from_run_id && (
              <div>
                Retry of{' '}
                <span
                  onClick={() => onSelectRun(run.retried_from_run_id!)}
                  style={{ color: 'var(--accent)', cursor: 'pointer', textDecoration: 'underline' }}
                >
                  an earlier run
                </span>
              </div>
            )}
            {run.max_attempts != null && run.max_attempts > 1 && run.attempt_count != null && (
              <div title="Bounded auto-retry: a retryable failure re-enqueues a fresh attempt until this budget is spent.">
                Attempt {run.attempt_count} of {run.max_attempts}
              </div>
            )}
            <div style={{ whiteSpace: 'pre-wrap', color: 'var(--text-body)' }}>Prompt: {run.prompt}</div>
          </div>
        )}

        {tree.length > 1 && (
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4 }}>
              Run tree
            </div>
            {renderTree(tree, null, 0)}
          </div>
        )}

        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4 }}>Log</div>
        <div
          style={{
            background: 'var(--surface-alt)',
            border: '1px solid var(--neutral-soft)',
            borderRadius: 4,
            padding: 10,
            minHeight: 120,
          }}
        >
          {logs.length === 0 && <div style={{ color: 'var(--neutral-mid)', fontSize: 12 }}>No log output yet…</div>}
          {logs.map((entry, idx) => logLine(entry, idx))}
          <div ref={logsEndRef} />
        </div>

        {run?.error && (
          <div
            style={{
              marginTop: 12,
              background: 'var(--tint-red)',
              border: '1px solid var(--danger)',
              color: 'var(--danger-strong)',
              borderRadius: 4,
              padding: '8px 12px',
              fontSize: 12.5,
              whiteSpace: 'pre-wrap',
            }}
          >
            {run.error}
          </div>
        )}

        {run?.final_text && (
          <div style={{ marginTop: 12 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4 }}>
              Final output
            </div>
            <div
              style={{
                background: 'var(--tint-green)',
                border: '1px solid var(--tint-green-border)',
                borderRadius: 4,
                padding: '10px 12px',
                fontSize: 13,
                whiteSpace: 'pre-wrap',
                color: 'var(--text)',
              }}
            >
              {run.final_text}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
