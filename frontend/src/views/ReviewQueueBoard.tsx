import React from 'react';
import { Link } from 'react-router-dom';
import { Artifact, Attachment, SuspectLink } from '../api/client';
import { Chip, buttonStyle, toneColors } from '../components/ui';
import { FigureStrip } from './ReviewQueueParts';
import { ageLabel, artifactLabel, previewText } from './reviewArtifacts';

// The review queue as a board (the Reviews design): a column per state of the
// work, each a stack of cards. The columns hold only what the queue really
// knows: the artifacts in review, the suspect links, and what this reviewer
// approved or sent back here, which stays on the board as a record of the
// session instead of vanishing on the click.

/** A decision made on this board, kept so its card moves to the right column. */
export interface ReviewDecision {
  artifact: Artifact;
  outcome: 'approved' | 'sent-back';
  /** The reason given, for a send-back. */
  comment?: string;
  at: string;
}

export type ColumnKey = 'in-review' | 'links' | 'changes' | 'approved';

/** Each column's name and the dot that marks it, in board order. */
export const COLUMNS: { key: ColumnKey; name: string; short: string; dot: string }[] = [
  { key: 'in-review', name: 'In review', short: 'In review', dot: 'var(--accent)' },
  { key: 'links', name: 'Suspect links', short: 'Suspect links', dot: 'var(--warning)' },
  { key: 'changes', name: 'Changes requested', short: 'Changes', dot: 'var(--warning-strong)' },
  { key: 'approved', name: 'Approved', short: 'Approved', dot: 'var(--success)' },
];

const approveStyle = (phone: boolean, disabled: boolean): React.CSSProperties => {
  const tone = toneColors('success');
  return {
    ...buttonStyle('secondary', { compact: !phone, disabled }),
    ...(disabled ? {} : { background: tone.bg, color: tone.fg, border: `1px solid ${tone.border}` }),
    ...(phone ? { flex: 1, minHeight: 40, fontSize: 14 } : {}),
  };
};

const actionStyle = (phone: boolean, disabled = false): React.CSSProperties => ({
  ...buttonStyle('secondary', { compact: !phone, disabled }),
  ...(phone ? { flex: 1, minHeight: 40, fontSize: 14 } : {}),
  textDecoration: 'none',
});

const checkboxStyle = (phone: boolean): React.CSSProperties =>
  phone ? { width: 22, height: 22, margin: 0, flexShrink: 0 } : { width: 'auto', margin: 0, flexShrink: 0 };

interface ColumnProps {
  column: (typeof COLUMNS)[number];
  count: number;
  /** The phone shows one column at a time, with no column chrome. */
  phone: boolean;
  /** Controls under the heading: select-all and the bulk actions. */
  tools?: React.ReactNode;
  empty: string;
  helpKey?: string;
  children: React.ReactNode;
}

export const BoardColumn: React.FC<ColumnProps> = ({ column, count, phone, tools, empty, helpKey, children }) => (
  <section aria-label={column.name} className={phone ? 'ov-board-col is-phone' : 'ov-board-col'} data-help-key={helpKey}>
    <div className="ov-board-col-head">
      <span className="ov-board-dot" style={{ background: column.dot }} aria-hidden="true" />
      <h3>{column.name}</h3>
      <span className="ov-board-count">{count}</span>
    </div>
    {tools}
    {count === 0 ? <div className="ov-board-empty">{empty}</div> : children}
  </section>
);

interface ArtifactCardProps {
  artifact: Artifact;
  figures: Attachment[];
  /** Inline decisions are on (the review-queue-decisions key). */
  decisions: boolean;
  selected: boolean;
  onSelect: (checked: boolean) => void;
  busy: boolean;
  onApprove: () => void;
  onSendBack: () => void;
  phone: boolean;
}

/** A card the reviewer decides on: what it is, what it says, and the decision. */
export const ArtifactCard: React.FC<ArtifactCardProps> = ({
  artifact: a,
  figures,
  decisions,
  selected,
  onSelect,
  busy,
  onApprove,
  onSendBack,
  phone,
}) => {
  const preview = previewText(a.body || '');
  const to = `../requirements?artifact=${a.id}`;
  return (
    <article className={phone ? 'ov-board-card is-phone' : 'ov-board-card'}>
      <div className="ov-board-card-top">
        {decisions && (
          <input
            type="checkbox"
            checked={selected}
            aria-label={`Select ${artifactLabel(a)}`}
            data-help-key="review.bulk"
            onChange={(e) => onSelect(e.target.checked)}
            style={checkboxStyle(phone)}
          />
        )}
        {a.ref && <span className="ov-ref">{a.ref}</span>}
        <span className="ov-board-meta">{a.type}</span>
        <span style={{ flex: 1 }} />
        <span className="ov-board-meta" title={a.updated_at}>
          {ageLabel(a.updated_at)}
        </span>
      </div>
      <Link to={to} className="ov-board-title">
        {artifactLabel(a)}
      </Link>
      {preview ? (
        <span className="ov-board-note">{preview}</span>
      ) : (
        <span className="ov-board-note" style={{ fontStyle: 'italic' }}>
          No description.
        </span>
      )}
      <FigureStrip figures={figures} />
      <div className="ov-board-actions">
        <Link to={to} style={actionStyle(phone)}>
          Open
        </Link>
        {decisions && (
          <>
            <button type="button" onClick={onApprove} disabled={busy} style={approveStyle(phone, busy)}>
              {busy ? 'Approving…' : 'Approve'}
            </button>
            <button type="button" onClick={onSendBack} disabled={busy} style={actionStyle(phone, busy)}>
              Send back
            </button>
          </>
        )}
      </div>
    </article>
  );
};

interface LinkCardProps {
  link: SuspectLink;
  selected: boolean;
  onSelect: (checked: boolean) => void;
  busy: boolean;
  onConfirm: () => void;
  phone: boolean;
}

/** A suspect link: both ends, the link between them, and Confirm. */
export const LinkCard: React.FC<LinkCardProps> = ({ link: l, selected, onSelect, busy, onConfirm, phone }) => (
  <article className={phone ? 'ov-board-card is-phone' : 'ov-board-card'}>
    <div className="ov-board-card-top">
      <input
        type="checkbox"
        checked={selected}
        aria-label={`Select link ${l.from_title} to ${l.to_title}`}
        data-help-key="review.bulk"
        onChange={(e) => onSelect(e.target.checked)}
        style={checkboxStyle(phone)}
      />
      <Chip tone="warning">suspect</Chip>
      <span style={{ flex: 1 }} />
      <span className="ov-board-meta" title={l.updated_at}>
        {ageLabel(l.updated_at)}
      </span>
    </div>
    <div className="ov-board-link-end">
      <Chip tone="neutral">{l.from_type}</Chip>
      <span className="ov-board-title">{l.from_title}</span>
    </div>
    <span className="ov-board-meta">{l.type} →</span>
    <div className="ov-board-link-end">
      <Chip tone="neutral">{l.to_type}</Chip>
      <span className="ov-board-title">{l.to_title}</span>
    </div>
    <div className="ov-board-actions">
      <button type="button" onClick={onConfirm} disabled={busy} style={actionStyle(phone, busy)}>
        {busy ? 'Confirming…' : 'Confirm'}
      </button>
    </div>
  </article>
);

/** A decision made here: approved, or sent back with its reason. */
export const DecidedCard: React.FC<{ decision: ReviewDecision; phone: boolean }> = ({ decision, phone }) => {
  const a = decision.artifact;
  return (
    <article className={phone ? 'ov-board-card is-phone' : 'ov-board-card'}>
      <div className="ov-board-card-top">
        {a.ref && <span className="ov-ref">{a.ref}</span>}
        <span className="ov-board-meta">{a.type}</span>
        <span style={{ flex: 1 }} />
        <span className="ov-board-meta">{ageLabel(decision.at)}</span>
      </div>
      <Link to={`../requirements?artifact=${a.id}`} className="ov-board-title">
        {artifactLabel(a)}
      </Link>
      <span className="ov-board-note">
        {decision.outcome === 'approved'
          ? 'Approved by you on this board.'
          : `Sent back to draft: ${decision.comment || ''}`}
      </span>
    </article>
  );
};
