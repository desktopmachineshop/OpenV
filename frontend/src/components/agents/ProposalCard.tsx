// One pending proposal as a card: what kind of write, on which artifact,
// the change itself, a note, and approve or reject.
import React, { useState } from 'react';
import type { BulkProposalOutcome, Proposal } from '../../api/client';
import { Chip, buttonStyle } from '../ui';
import { ProposalChanges, ProposalContext, ProposalTarget, opMeta } from './proposalChanges';
import { useUiRefresh } from '../../hooks/useUiRefresh';
import { useViewport } from '../../hooks/useViewport';

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
  /**
   * The refreshed card (ui-refresh) shows its tick only while several are
   * being chosen together; the card as it was always shows it.
   */
  selecting?: boolean;
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
  selecting = false,
}) => {
  const meta = opMeta(p.op);
  // The refreshed card leads with the decision: Approve is one press, and the
  // note is asked for where it is wanted, on Reject or on request.
  const refresh = useUiRefresh();
  const { isPhone } = useViewport();
  const [noteOpen, setNoteOpen] = useState(false);
  const showNote = !refresh || noteOpen || note !== '';
  const big: React.CSSProperties = refresh && isPhone ? { flex: 1, minHeight: 44 } : {};
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
        {(!refresh || selecting) && <input
          type="checkbox"
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
          disabled={busy}
          aria-label="Select proposal"
          style={{ width: 'auto', margin: 0 }}
        />}
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
        {showNote && (
          <input
            value={note}
            onChange={(e) => onNote(e.target.value)}
            placeholder={refresh ? 'Why? (optional)' : 'Review note (optional)'}
            aria-label="Review note"
            autoFocus={refresh && noteOpen}
            style={{ flex: refresh && isPhone ? '1 1 100%' : 1, minWidth: 180, maxWidth: 640, fontSize: 13, height: refresh && isPhone ? 40 : 32, padding: '0 10px' }}
          />
        )}
        {!showNote && (
          <button
            type="button"
            onClick={() => setNoteOpen(true)}
            className="compact-action"
            style={{ border: 'none', background: 'none', padding: 0, color: 'var(--accent-text)', fontSize: 13, cursor: 'pointer', marginRight: 'auto' }}
          >
            Add a note
          </button>
        )}
        {refresh ? (
          <>
            {/* The first press of Reject asks why; the second rejects. */}
            <button type="button" style={{ ...buttonStyle('danger', { disabled: busy }), ...big }} disabled={busy} onClick={showNote ? onReject : () => setNoteOpen(true)}>
              Reject
            </button>
            <button type="button" style={{ ...buttonStyle('primary', { disabled: busy }), ...big }} disabled={busy} onClick={onApprove}>
              Approve
            </button>
          </>
        ) : (
          <>
            <button type="button" style={buttonStyle('primary', { disabled: busy })} disabled={busy} onClick={onApprove}>
              Approve
            </button>
            <button type="button" style={buttonStyle('danger', { disabled: busy })} disabled={busy} onClick={onReject}>
              Reject
            </button>
          </>
        )}
      </div>
    </div>
  );
};
