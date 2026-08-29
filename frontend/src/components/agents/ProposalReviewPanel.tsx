import React, { useCallback, useEffect, useState } from 'react';
import { BulkProposalOutcome, Proposal, proposalsAPI } from '../../api/client';
import { ConfirmDialog } from '../ui';

interface ProposalReviewPanelProps {
  projectId: string;
  onCountChange?: (count: number) => void;
}

const opBadgeColor = (op: string): string => {
  switch ((op || '').toLowerCase()) {
    case 'create':
    case 'create_artifact':
    case 'create_link':
      return 'var(--success)';
    case 'update':
    case 'update_artifact':
      return 'var(--accent)';
    case 'delete':
    case 'delete_artifact':
    case 'delete_link':
      return 'var(--danger)';
    default:
      return 'var(--neutral)';
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
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [bulkNote, setBulkNote] = useState('');
  const [bulkBusy, setBulkBusy] = useState(false);
  const [bulkSummary, setBulkSummary] = useState('');
  const [rowResults, setRowResults] = useState<Record<string, BulkProposalOutcome>>({});
  const [confirmReject, setConfirmReject] = useState(false);

  const load = useCallback(() => {
    proposalsAPI
      .list({ project_id: projectId, status: 'pending' })
      .then((res) => {
        const list = res.data || [];
        setProposals(list);
        // Prune selections for proposals no longer pending (reviewed here or
        // elsewhere) so select-all and the selected count stay honest.
        setSelected((sel) => {
          const next: Record<string, boolean> = {};
          list.forEach((p) => {
            if (sel[p.id]) next[p.id] = true;
          });
          return next;
        });
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
      setRowResults((res) => {
        const next = { ...res };
        delete next[proposal.id];
        return next;
      });
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || `Failed to ${action} proposal`);
    } finally {
      setBusy((b) => ({ ...b, [proposal.id]: false }));
    }
  };

  const selectedIds = proposals.filter((p) => selected[p.id]).map((p) => p.id);
  const allSelected = proposals.length > 0 && selectedIds.length === proposals.length;

  const toggleAll = () => {
    if (allSelected) {
      setSelected({});
    } else {
      const next: Record<string, boolean> = {};
      proposals.forEach((p) => {
        next[p.id] = true;
      });
      setSelected(next);
    }
  };

  const bulkAct = async (action: 'approve' | 'reject') => {
    if (selectedIds.length === 0) return;
    setBulkBusy(true);
    setError('');
    setBulkSummary('');
    try {
      const res = await proposalsAPI.bulkReview(selectedIds, action, bulkNote);
      const results = res.data?.results || [];
      const byId: Record<string, BulkProposalOutcome> = {};
      results.forEach((r) => {
        byId[r.id] = r;
      });
      setRowResults(byId);
      const okCount = results.filter((r) => r.ok).length;
      const failCount = results.length - okCount;
      const verb = action === 'approve' ? 'Approved' : 'Rejected';
      setBulkSummary(
        failCount === 0
          ? `${verb} ${okCount} proposal${okCount === 1 ? '' : 's'}.`
          : `${verb} ${okCount} of ${results.length} proposals; ${failCount} failed (see rows below).`
      );
      setBulkNote('');
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || `Failed to ${action} proposals`);
    } finally {
      setBulkBusy(false);
    }
  };

  if (proposals.length === 0 && !error && !bulkSummary) {
    return null;
  }

  return (
    <div style={{ marginBottom: 16 }}>
      {error && <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 8 }}>{error}</div>}
      {bulkSummary && (
        <div style={{ color: 'var(--text)', fontSize: 13, marginBottom: 8 }}>{bulkSummary}</div>
      )}
      {proposals.length > 0 && (
        <div
          style={{
            display: 'flex',
            gap: 8,
            alignItems: 'center',
            flexWrap: 'wrap',
            marginBottom: 10,
          }}
        >
          <label
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              fontSize: 13,
              color: 'var(--text)',
              cursor: 'pointer',
            }}
          >
            <input type="checkbox" checked={allSelected} onChange={toggleAll} disabled={bulkBusy} />
            Select all ({selectedIds.length}/{proposals.length})
          </label>
          <input
            value={bulkNote}
            onChange={(e) => setBulkNote(e.target.value)}
            placeholder="Bulk review note (optional)"
            style={{ flex: 1, minWidth: 180, fontSize: 13, padding: '7px 10px' }}
            disabled={bulkBusy}
          />
          <button
            className="button"
            style={{ padding: '7px 16px', fontSize: 13 }}
            disabled={bulkBusy || selectedIds.length === 0}
            onClick={() => bulkAct('approve')}
          >
            Approve Selected ({selectedIds.length})
          </button>
          <button
            style={{
              background: 'var(--danger)',
              color: '#fff',
              border: 'none',
              padding: '7px 16px',
              borderRadius: 4,
              cursor: 'pointer',
              fontSize: 13,
            }}
            disabled={bulkBusy || selectedIds.length === 0}
            onClick={() => setConfirmReject(true)}
          >
            Reject Selected ({selectedIds.length})
          </button>
        </div>
      )}
      {confirmReject && (
        <ConfirmDialog
          title="Reject selected proposals"
          message={`Reject ${selectedIds.length} selected proposal${
            selectedIds.length === 1 ? '' : 's'
          }? The agent's proposed writes will be discarded.`}
          confirmLabel="Reject"
          danger
          onConfirm={() => {
            setConfirmReject(false);
            bulkAct('reject');
          }}
          onCancel={() => setConfirmReject(false)}
        />
      )}
      {proposals.map((p) => (
        <div
          key={p.id}
          className="card"
          style={{ marginBottom: 10, borderLeft: '4px solid var(--warning)', padding: 16 }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8, flexWrap: 'wrap' }}>
            <input
              type="checkbox"
              checked={!!selected[p.id]}
              onChange={(e) => setSelected((sel) => ({ ...sel, [p.id]: e.target.checked }))}
              disabled={bulkBusy}
              aria-label="Select proposal"
            />
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
            <span style={{ fontSize: 13, color: 'var(--text)' }}>
              Target: <strong>{p.target_id || 'new'}</strong>
            </span>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              {new Date(p.created_at).toLocaleString()}
            </span>
            {p.ref && (
              <span
                title="Other proposals in this run can link to this artifact using this reference token."
                style={{
                  fontSize: 11,
                  fontFamily: 'monospace',
                  color: 'var(--text-muted)',
                  background: 'var(--surface-alt)',
                  border: '1px solid var(--neutral-soft)',
                  borderRadius: 4,
                  padding: '1px 6px',
                }}
              >
                ref: {p.ref}
              </span>
            )}
          </div>
          {rowResults[p.id] && !rowResults[p.id].ok && (
            <div style={{ color: 'var(--danger)', fontSize: 12.5, marginBottom: 8 }}>
              {rowResults[p.id].error || 'Review failed'}
            </div>
          )}
          <pre
            style={{
              background: 'var(--surface-alt)',
              border: '1px solid var(--neutral-soft)',
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
              disabled={busy[p.id] || bulkBusy}
              onClick={() => act(p, 'approve')}
            >
              Approve
            </button>
            <button
              style={{
                background: 'var(--danger)',
                color: '#fff',
                border: 'none',
                padding: '7px 16px',
                borderRadius: 4,
                cursor: 'pointer',
                fontSize: 13,
              }}
              disabled={busy[p.id] || bulkBusy}
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
