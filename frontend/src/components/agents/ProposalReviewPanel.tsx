import React, { useCallback, useEffect, useState } from 'react';
import { useUiRefresh } from '../../hooks/useUiRefresh';
import { Artifact, artifactAPI, BulkProposalOutcome, Link as TraceLink, linkAPI, Proposal, proposalsAPI } from '../../api/client';
import { ConfirmDialog, buttonStyle } from '../ui';
import { ProposalCard } from './ProposalCard';
import type { ProposalContext } from './proposalChanges';

interface ProposalReviewPanelProps {
  projectId: string;
  onCountChange?: (count: number) => void;
  /** Opens the run a proposal came from. */
  onOpenRun?: (runId: string) => void;
  /** When set, only the proposals of these runs (one agent's, from the
   *  Agents page) are listed and counted. */
  onlyRuns?: Set<string>;
}

export const ProposalReviewPanel: React.FC<ProposalReviewPanelProps> = ({
  projectId,
  onCountChange,
  onOpenRun,
  onlyRuns,
}) => {
  const onlyKey = onlyRuns ? [...onlyRuns].sort().join(',') : null;
  const refresh = useUiRefresh();
  const [selecting, setSelecting] = useState(false);
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
  // The project's artifacts and links, to name what each proposal touches
  // and show an edit against the artifact as it is now. Read once there is
  // something to review, and again when the set of proposals changes.
  const [ctx, setCtx] = useState<ProposalContext>({ artifacts: {}, links: {} });
  const proposalKey = proposals.map((p) => p.id).join(',');
  useEffect(() => {
    if (!proposalKey) return;
    let live = true;
    Promise.all([
      artifactAPI.list(projectId).catch(() => ({ data: [] as Artifact[] })),
      linkAPI.list(projectId).catch(() => ({ data: [] as TraceLink[] })),
    ]).then(([arts, links]) => {
      if (!live) return;
      const artifacts: Record<string, Artifact> = {};
      (arts.data || []).forEach((a) => (artifacts[a.id] = a));
      const byId: Record<string, TraceLink> = {};
      (links.data || []).forEach((l) => (byId[l.id] = l));
      setCtx({ artifacts, links: byId });
    });
    return () => {
      live = false;
    };
  }, [projectId, proposalKey]);

  const load = useCallback(() => {
    proposalsAPI
      .list({ project_id: projectId, status: 'pending' })
      .then((res) => {
        const all = res.data || [];
        const list = onlyRuns ? all.filter((p) => onlyRuns.has(p.run_id)) : all;
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
  }, [projectId, onlyKey]);

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

  // The refreshed panel (ui-refresh) keeps the bulk bar out of the way until
  // several proposals are to be decided together.
  const bulkBar = !refresh || selecting;

  return (
    <div style={{ marginBottom: 16 }} data-help-pane="agent-runs.approvals" data-help-key="agent-runs.approvals">
      {error && <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 8 }}>{error}</div>}
      {bulkSummary && (
        <div style={{ color: 'var(--text)', fontSize: 13, marginBottom: 8 }}>{bulkSummary}</div>
      )}
      {refresh && proposals.length > 1 && (
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 6 }}>
          <button
            type="button"
            aria-pressed={selecting}
            onClick={() => setSelecting((on) => !on)}
            className="compact-action"
            style={{ border: 'none', background: 'none', padding: 0, color: 'var(--accent-text)', fontSize: 13, fontWeight: 600, cursor: 'pointer' }}
          >
            {selecting ? 'Done selecting' : 'Select several'}
          </button>
        </div>
      )}
      {bulkBar && proposals.length > 0 && (
        <div
          style={{
            display: 'flex',
            gap: 8,
            alignItems: 'center',
            flexWrap: 'wrap',
            marginBottom: 10,
            padding: '8px 12px',
            background: 'var(--tint-purple)',
            border: '1px solid var(--tint-purple-border)',
            borderRadius: 6,
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
              whiteSpace: 'nowrap',
              minHeight: 36,
            }}
          >
            <input type="checkbox" checked={allSelected} onChange={toggleAll} disabled={bulkBusy} style={{ width: 'auto' }} />
            Select all ({selectedIds.length}/{proposals.length})
          </label>
          <input
            value={bulkNote}
            onChange={(e) => setBulkNote(e.target.value)}
            placeholder="Bulk review note (optional)"
            style={{ flex: 1, minWidth: 180, maxWidth: 640, fontSize: 13, height: 32, padding: '0 10px' }}
            disabled={bulkBusy}
          />
          <button
            type="button"
            style={buttonStyle('primary', { disabled: bulkBusy || selectedIds.length === 0 })}
            disabled={bulkBusy || selectedIds.length === 0}
            onClick={() => bulkAct('approve')}
          >
            Approve Selected ({selectedIds.length})
          </button>
          <button
            type="button"
            style={buttonStyle('danger', { disabled: bulkBusy || selectedIds.length === 0 })}
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
        <ProposalCard
          key={p.id}
          proposal={p}
          ctx={ctx}
          selected={!!selected[p.id]}
          onSelect={(on) => setSelected((sel) => ({ ...sel, [p.id]: on }))}
          note={notes[p.id] || ''}
          onNote={(note) => setNotes((n) => ({ ...n, [p.id]: note }))}
          busy={!!busy[p.id] || bulkBusy}
          outcome={rowResults[p.id]}
          onApprove={() => act(p, 'approve')}
          onReject={() => act(p, 'reject')}
          onOpenRun={onOpenRun}
          selecting={selecting}
        />
      ))}
    </div>
  );
};
