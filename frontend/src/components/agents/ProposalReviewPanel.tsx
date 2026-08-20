import React, { useCallback, useEffect, useState } from 'react';
import { Proposal, proposalsAPI } from '../../api/client';

interface ProposalReviewPanelProps {
  projectId: string;
  onCountChange?: (count: number) => void;
}

const opBadgeColor = (op: string): string => {
  switch ((op || '').toLowerCase()) {
    case 'create':
    case 'create_artifact':
    case 'create_link':
      return '#27ae60';
    case 'update':
    case 'update_artifact':
      return '#3498db';
    case 'delete':
    case 'delete_artifact':
    case 'delete_link':
      return '#e74c3c';
    default:
      return '#95a5a6';
  }
};

export const ProposalReviewPanel: React.FC<ProposalReviewPanelProps> = ({
  projectId,
  onCountChange,
}) => {
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const [error, setError] = useState('');

  const load = useCallback(() => {
    proposalsAPI
      .list({ project_id: projectId, status: 'pending' })
      .then((res) => {
        const list = res.data || [];
        setProposals(list);
        onCountChange?.(list.length);
      })
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load proposals')
      );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  useEffect(() => {
    load();
    const timer = window.setInterval(load, 10000);
    return () => window.clearInterval(timer);
  }, [load]);

  const act = async (proposal: Proposal, action: 'approve' | 'reject') => {
    setBusy((b) => ({ ...b, [proposal.id]: true }));
    setError('');
    try {
      if (action === 'approve') {
        await proposalsAPI.approve(proposal.id, notes[proposal.id]);
      } else {
        await proposalsAPI.reject(proposal.id, notes[proposal.id]);
      }
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || `Failed to ${action} proposal`);
    } finally {
      setBusy((b) => ({ ...b, [proposal.id]: false }));
    }
  };

  if (proposals.length === 0 && !error) {
    return null;
  }

  return (
    <div style={{ marginBottom: 16 }}>
      {error && <div style={{ color: '#e74c3c', fontSize: 13, marginBottom: 8 }}>{error}</div>}
      {proposals.map((p) => (
        <div
          key={p.id}
          className="card"
          style={{ marginBottom: 10, borderLeft: '4px solid #f39c12', padding: 16 }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8, flexWrap: 'wrap' }}>
            <span
              style={{
                background: opBadgeColor(p.op),
                color: '#fff',
                borderRadius: 10,
                padding: '2px 10px',
                fontSize: 11,
                fontWeight: 700,
                textTransform: 'uppercase',
              }}
            >
              {p.op}
            </span>
            <span style={{ fontSize: 13, color: '#2c3e50' }}>
              Target: <strong>{p.target_id || 'new'}</strong>
            </span>
            <span style={{ fontSize: 12, color: '#7f8c8d' }}>
              {new Date(p.created_at).toLocaleString()}
            </span>
          </div>
          <pre
            style={{
              background: '#fafbfc',
              border: '1px solid #ecf0f1',
              borderRadius: 4,
              padding: 10,
              fontSize: 12,
              overflowX: 'auto',
              marginBottom: 10,
              maxHeight: 260,
              overflowY: 'auto',
            }}
          >
            {JSON.stringify(p.payload, null, 2)}
          </pre>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            <input
              value={notes[p.id] || ''}
              onChange={(e) => setNotes((n) => ({ ...n, [p.id]: e.target.value }))}
              placeholder="Review note (optional)"
              style={{ flex: 1, minWidth: 180, fontSize: 13, padding: '7px 10px' }}
            />
            <button
              className="button"
              style={{ padding: '7px 16px', fontSize: 13 }}
              disabled={busy[p.id]}
              onClick={() => act(p, 'approve')}
            >
              Approve
            </button>
            <button
              style={{
                background: '#e74c3c',
                color: '#fff',
                border: 'none',
                padding: '7px 16px',
                borderRadius: 4,
                cursor: 'pointer',
                fontSize: 13,
              }}
              disabled={busy[p.id]}
              onClick={() => act(p, 'reject')}
            >
              Reject
            </button>
          </div>
        </div>
      ))}
    </div>
  );
};
