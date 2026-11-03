// One pending proposal as a card: what kind of write, on which artifact,
// the change itself, a note, and approve or reject.
import React from 'react';
import type { BulkProposalOutcome, Proposal } from '../../api/client';
import { Chip, buttonStyle } from '../ui';
import { ProposalChanges, ProposalContext, ProposalTarget, opMeta } from './proposalChanges';

interface Props {
  proposal: Proposal;
  ctx: ProposalContext;
  selected: boolean;
  onSelect: (selected: boolean) => void;
  note: string;
  onNote: (note: string) => void;
  busy: boolean;
  outcome?: BulkProposalOutcome;
  onApprove: () => void;
  onReject: () => void;
  onOpenRun?: (runId: string) => void;
}

export const ProposalCard: React.FC<Props> = ({
  proposal: p,
  ctx,
  selected,
  onSelect,
  note,
  onNote,
  busy,
  outcome,
  onApprove,
  onReject,
  onOpenRun,
}) => {
  const meta = opMeta(p.op);
  return (
    <div
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderLeft: '3px solid var(--purple-strong)',
        borderRadius: 6,
        marginBottom: 10,
        padding: 12,
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <input
          type="checkbox"
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
          disabled={busy}
          aria-label="Select proposal"
          style={{ width: 'auto', margin: 0 }}
        />
        <Chip tone={meta.tone} title={p.op}>
          {meta.label}
        </Chip>
        <span style={{ flex: '1 1 200px', minWidth: 0, fontSize: 13, color: 'var(--text)' }}>
          <ProposalTarget proposal={p} ctx={ctx} />
        </span>
        {p.ref && (
          <span title="Other proposals in this run can link to this artifact using this reference token.">
            <Chip tone="neutral">ref: {p.ref}</Chip>
          </span>
        )}
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {new Date(p.created_at).toLocaleString()}
          {onOpenRun && p.run_id && (
            <>
              {' · '}
              <button
                type="button"
                onClick={() => onOpenRun(p.run_id)}
                style={{ border: 'none', background: 'none', padding: 0, color: 'var(--accent-text)', fontSize: 12, cursor: 'pointer', minHeight: 32 }}
              >
                from its run
              </button>
            </>
          )}
        </span>
      </div>
      {outcome && !outcome.ok && (
        <div style={{ color: 'var(--danger-strong)', fontSize: 13 }}>{outcome.error || 'Review failed'}</div>
      )}
      <ProposalChanges proposal={p} ctx={ctx} />
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <input
          value={note}
          onChange={(e) => onNote(e.target.value)}
          placeholder="Review note (optional)"
          style={{ flex: 1, minWidth: 180, maxWidth: 640, fontSize: 13, height: 32, padding: '0 10px' }}
        />
        <button type="button" style={buttonStyle('primary', { disabled: busy })} disabled={busy} onClick={onApprove}>
          Approve
        </button>
        <button type="button" style={buttonStyle('danger', { disabled: busy })} disabled={busy} onClick={onReject}>
          Reject
        </button>
      </div>
    </div>
  );
};
