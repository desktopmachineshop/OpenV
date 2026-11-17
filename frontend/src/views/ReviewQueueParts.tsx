import React, { useState } from 'react';
import { Artifact, Attachment, attachmentAPI } from '../api/client';
import { Chip, Modal } from '../components/ui';
import { NoteComposer } from '../components/NoteComposer';
import { NOTE_TAGGING_FEATURE } from '../components/noteTagging';
import { useFeature } from '../hooks/useFeature';
import { artifactLabel, attachmentLabel } from './reviewArtifacts';

// The pieces of the review queue that are not the board itself: the figure
// previews a card carries and the dialog that asks why work is sent back.

/**
 * Small previews of an artifact's figures.
 *
 * A picture is shown as one; a PDF or a CAD file has nothing to show at this
 * size, so it gets a chip naming it instead of a broken thumbnail. Both link
 * to the file itself. Four at most: the point is to recognise the artifact,
 * not to review the drawings here.
 */
export const FigureStrip: React.FC<{ figures: Attachment[] }> = ({ figures }) => {
  if (figures.length === 0) return null;
  const shown = figures.slice(0, 4);
  const rest = figures.length - shown.length;

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
      {shown.map((f) => {
        const href = attachmentAPI.getDownloadUrl(f.id, f.version);
        const label = attachmentLabel(f);
        return f.kind === 'image' ? (
          <a key={f.id} href={href} target="_blank" rel="noreferrer" title={label}>
            <img
              src={href}
              alt={label}
              loading="lazy"
              style={{
                width: 40,
                height: 40,
                objectFit: 'cover',
                borderRadius: 4,
                border: '1px solid var(--border)',
                display: 'block',
                background: 'var(--surface-alt, var(--surface))',
              }}
            />
          </a>
        ) : (
          <a key={f.id} href={href} target="_blank" rel="noreferrer" title={label} style={{ textDecoration: 'none' }}>
            <Chip tone="neutral">{f.figure_ref || f.kind}</Chip>
          </a>
        );
      })}
      {rest > 0 && <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>+{rest}</span>}
    </div>
  );
};

/**
 * Ask for the reason before sending work back.
 *
 * A rejection with no reason is the worst thing a review queue can produce:
 * the author sees an artifact back in draft and has to come and ask what was
 * wrong with it. So the comment is required, and it is written in the SAME
 * composer the notes panel uses — "@name" reaches a person, "@@name" raises
 * them a to-do, "#REQ-12" cites — and posted through the same path, so the
 * reason lands in the artifact's feed as an ordinary note rather than as some
 * second-class rejection field only this screen knows how to read.
 */
export const RejectDialog: React.FC<{
  artifacts: Artifact[];
  projectId?: string;
  busy: boolean;
  onCancel: () => void;
  onSubmit: (comment: string) => void;
}> = ({ artifacts, projectId, busy, onCancel, onSubmit }) => {
  const [comment, setComment] = useState('');
  const taggingEnabled = useFeature(NOTE_TAGGING_FEATURE);
  const many = artifacts.length > 1;
  const submit = () => {
    if (comment.trim() && !busy) onSubmit(comment.trim());
  };

  return (
    <Modal
      title={many ? `Send ${artifacts.length} artifacts back` : 'Send back for changes'}
      width={560}
      onClose={onCancel}
    >
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 0 }}>
        {many
          ? 'Each of these goes back to draft, and your comment is posted on every one of them.'
          : 'This goes back to draft and your comment is posted on its feed.'}
      </p>
      {many && (
        <ul
          style={{
            margin: '0 0 12px',
            paddingLeft: 18,
            color: 'var(--text)',
            fontSize: 13,
            maxHeight: 120,
            overflowY: 'auto',
          }}
        >
          {artifacts.map((a) => (
            <li key={a.id}>{a.ref ? `${a.ref} — ${artifactLabel(a)}` : artifactLabel(a)}</li>
          ))}
        </ul>
      )}
      <NoteComposer
        projectId={projectId}
        artifactId={artifacts.length === 1 ? artifacts[0].id : undefined}
        value={comment}
        onChange={setComment}
        onSubmit={submit}
        autoFocus
        minHeight={96}
        ariaLabel="Reason for sending back"
        placeholder={
          taggingEnabled ? 'What needs to change? @name, @@name for a to-do, #REQ-12' : 'What needs to change?'
        }
      />
      {/* Modal takes no attributes: the dialog's buttons carry its pane. */}
      <div
        style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 12 }}
        data-help-pane={many ? 'review.bulk' : 'review.queue'}
      >
        <button className="button-secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button className="button-primary" onClick={submit} disabled={busy || !comment.trim()}>
          {busy ? 'Sending back…' : 'Send back'}
        </button>
      </div>
    </Modal>
  );
};
