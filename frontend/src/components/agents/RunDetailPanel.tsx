import React, { useCallback, useEffect, useRef, useState } from 'react';
import { AgentRun, agentRunsAPI, RunLogEntry } from '../../api/client';

const TERMINAL_STATUSES = ['succeeded', 'failed', 'timed_out', 'cancelled'];

export const runStatusColor = (status: string): string => {
  switch (status) {
    case 'queued':
    case 'claimed':
      return '#95a5a6';
    case 'running':
      return '#3498db';
    case 'awaiting_approval':
      return '#f39c12';
    case 'succeeded':
      return '#27ae60';
    case 'failed':
    case 'timed_out':
      return '#e74c3c';
    case 'cancelled':
      return '#95a5a6';
    default:
      return '#95a5a6';
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
      if (argsText.length > 300) argsText = argsText.slice(0, 300) + '…';
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontFamily: 'monospace', fontSize: 12, color: '#3498db', margin: '3px 0' }}
        >
          → tool: {name}({argsText})
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
      if (text.length > 400) text = text.slice(0, 400) + '…';
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontFamily: 'monospace', fontSize: 12, color: '#95a5a6', margin: '3px 0', whiteSpace: 'pre-wrap' }}
        >
          {text}
        </div>
      );
    }
    case 'error':
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontSize: 12.5, color: '#e74c3c', margin: '3px 0', whiteSpace: 'pre-wrap' }}
        >
          {p.text || p.error || p.message || JSON.stringify(p)}
        </div>
      );
    default:
      return (
        <div
          key={`${entry.seq}-${idx}`}
          style={{ fontSize: 13, color: '#2c3e50', margin: '4px 0', whiteSpace: 'pre-wrap' }}
        >
          {typeof p.text === 'string' ? p.text : JSON.stringify(p)}
        </div>
      );
  }
};

export const RunDetailPanel: React.FC<RunDetailPanelProps> = ({ runId, onSelectRun, onClose }) => {
  const [run, setRun] = useState<AgentRun | null>(null);
  const [logs, setLogs] = useState<RunLogEntry[]>([]);
  const [tree, setTree] = useState<AgentRun[]>([]);
  const [error, setError] = useState('');
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

    try {
      es = new EventSource(agentRunsAPI.streamUrl(runId));
      es.addEventListener('log', (evt: MessageEvent) => {
        try {
          const entry: RunLogEntry = JSON.parse(evt.data);
          appendLogs([entry]);
        } catch {
          // ignore malformed events
        }
      });
      es.addEventListener('status', (evt: MessageEvent) => {
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
        if (!TERMINAL_STATUSES.includes(statusRef.current)) {
          es?.close();
          startPolling();
        } else {
          es?.close();
        }
      };
    } catch {
      startPolling();
    }

    return () => {
      closed = true;
      es?.close();
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
              background: n.id === runId ? '#eaf2f8' : 'transparent',
              borderRadius: 3,
              display: 'flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <span style={{ color: '#2c3e50' }}>{n.agent_name || n.agent_id}</span>
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

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: '#fff',
        border: '1px solid #ddd',
        borderRadius: 4,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid #ecf0f1',
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        <strong style={{ flex: 1, fontSize: 14, color: '#2c3e50' }}>
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
        </strong>
        {isLive && (
          <button
            onClick={cancel}
            style={{
              background: '#e74c3c',
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
        <button
          onClick={onClose}
          style={{ background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: '#7f8c8d' }}
        >
          ×
        </button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
        {error && <div style={{ color: '#e74c3c', fontSize: 12, marginBottom: 8 }}>{error}</div>}

        {run && (
          <div style={{ fontSize: 12, color: '#7f8c8d', marginBottom: 10, lineHeight: 1.7 }}>
            <div>
              Started: {run.started_at ? new Date(run.started_at).toLocaleString() : '—'} · Tokens:{' '}
              {run.tokens_in + run.tokens_out}
              {run.cost_usd != null && ` · Cost: $${run.cost_usd.toFixed(4)}`}
            </div>
            <div style={{ whiteSpace: 'pre-wrap', color: '#555' }}>Prompt: {run.prompt}</div>
          </div>
        )}

        {tree.length > 1 && (
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: '#7f8c8d', marginBottom: 4 }}>
              Run tree
            </div>
            {renderTree(tree, null, 0)}
          </div>
        )}

        <div style={{ fontSize: 12, fontWeight: 600, color: '#7f8c8d', marginBottom: 4 }}>Log</div>
        <div
          style={{
            background: '#fafbfc',
            border: '1px solid #ecf0f1',
            borderRadius: 4,
            padding: 10,
            minHeight: 120,
          }}
        >
          {logs.length === 0 && <div style={{ color: '#bdc3c7', fontSize: 12 }}>No log output yet…</div>}
          {logs.map((entry, idx) => logLine(entry, idx))}
          <div ref={logsEndRef} />
        </div>

        {run?.error && (
          <div
            style={{
              marginTop: 12,
              background: '#fdedec',
              border: '1px solid #e74c3c',
              color: '#c0392b',
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
            <div style={{ fontSize: 12, fontWeight: 600, color: '#7f8c8d', marginBottom: 4 }}>
              Final output
            </div>
            <div
              style={{
                background: '#eafaf1',
                border: '1px solid #a9dfbf',
                borderRadius: 4,
                padding: '10px 12px',
                fontSize: 13,
                whiteSpace: 'pre-wrap',
                color: '#2c3e50',
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
