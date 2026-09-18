import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Artifact,
  Attachment,
  ArtifactStatus,
  artifactAPI,
  attachmentAPI,
  linkAPI,
  reviewAPI,
  SuspectLink,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, Modal, SegmentedControl, useConfirm } from '../components/ui';
import { NoteComposer, postNote } from '../components/NoteComposer';
import { NOTE_TAGGING_FEATURE } from '../components/noteTagging';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import {
  artifactLabel,
  attachmentLabel,
  groupAttachments,
  previewText,
} from './reviewArtifacts';

// The project-wide review round is gated until a stable release carries it.
export const REVIEW_ROUND_FEATURE = 'project-review-round';
// Deciding a review from the queue — previews, inline Approve / Send back,
// selection and bulk actions — is gated the same way. What it writes is the
// ordinary status change and the ordinary note, so a decision made on nightly
// reads correctly in a workspace that has not received the controls yet.
export const REVIEW_DECISIONS_FEATURE = 'review-queue-decisions';

type Section = 'all' | 'links' | 'artifacts';

// Small pill for an artifact type/kind, matching the V&V chip styling.
const typeChip = (label: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '1px 8px',
  borderRadius: 10,
  background: 'var(--surface-alt, var(--surface))',
  border: '1px solid var(--border)',
  color: 'var(--text-muted)',
  fontSize: 12,
  fontWeight: 600,
  whiteSpace: 'nowrap',
});

const cardStyle: React.CSSProperties = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  padding: 16,
  marginBottom: 24,
};

/**
 * One artifact as a reviewer reads it: what it is, what it is called, and
 * enough of what it says to judge without opening it. The title links to the
 * artifact for the times a preview is not enough.
 */
const ArtifactSummary: React.FC<{ artifact: Artifact; figures: Attachment[] }> = ({
  artifact,
  figures,
}) => {
  const preview = previewText(artifact.body || '');
  return (
    <div style={{ minWidth: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <span style={typeChip(artifact.type)}>{artifact.type}</span>
        {artifact.ref && (
          <span style={{ color: 'var(--text-muted)', fontSize: 12, fontWeight: 600 }}>
            {artifact.ref}
          </span>
        )}
        <Link
          to={`../requirements?artifact=${artifact.id}`}
          style={{ color: 'var(--accent)', textDecoration: 'none' }}
        >
          {artifactLabel(artifact)}
        </Link>
      </div>
      {preview ? (
        <p style={{ margin: '6px 0 0', color: 'var(--text-muted)', fontSize: 13, lineHeight: 1.45 }}>
          {preview}
        </p>
      ) : (
        <p style={{ margin: '6px 0 0', color: 'var(--text-muted)', fontSize: 13, fontStyle: 'italic' }}>
          No description.
        </p>
      )}
      {/* On a phone the figures ride along under the text; the table gives
          them a column of their own. */}
      {figures.length > 0 && (
        <div style={{ marginTop: 8 }}>
          <FigureStrip figures={figures} />
        </div>
      )}
    </div>
  );
};

/**
 * Small previews of an artifact's figures.
 *
 * A picture is shown as one; a PDF or a CAD file has nothing to show at this
 * size, so it gets a chip naming it instead of a broken thumbnail. Both link
 * to the file itself. Four at most: the point is to recognise the artifact,
 * not to review the drawings here.
 */
const FigureStrip: React.FC<{ figures: Attachment[] }> = ({ figures }) => {
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
          <a
            key={f.id}
            href={href}
            target="_blank"
            rel="noreferrer"
            title={label}
            style={{ ...typeChip(f.kind), textDecoration: 'none', padding: '4px 8px' }}
          >
            {f.figure_ref || f.kind}
          </a>
        );
      })}
      {rest > 0 && (
        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>+{rest}</span>
      )}
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
const RejectDialog: React.FC<{
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
          taggingEnabled
            ? 'What needs to change? @name, @@name for a to-do, #REQ-12'
            : 'What needs to change?'
        }
      />
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 12 }}>
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

/**
 * ReviewQueue is the reviewer's daily driver (issue #183): the suspect links
 * whose meaning may no longer hold and the artifacts sitting in review, in one
 * place. Suspect links can be cleared one at a time or in bulk (both reuse
 * linkAPI.confirm); in-review artifacts deep-link into the requirements module
 * via ?artifact= so the reviewer can open and sign them off.
 *
 * Send everything for review starts a round over the whole project instead of
 * submitting artifact by artifact. It is meant to be run again each cycle: an
 * approved requirement nobody has touched stays approved, and one edited since
 * it was approved is back in draft, so the re-run asks for exactly the
 * sign-offs that are missing.
 */
export const ReviewQueue: React.FC = () => {
  // Phones: a five-column table of links is unreadable at 390px; each link
  // becomes a card with the same checkbox and Confirm button (REQ-107).
  const phone = useViewport().isPhone;
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const confirm = useConfirm();

  const [suspectLinks, setSuspectLinks] = useState<SuspectLink[]>([]);
  const [inReview, setInReview] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [section, setSection] = useState<Section>('all');
  // Ids currently being confirmed, so their row buttons disable individually.
  const [confirming, setConfirming] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [startingRound, setStartingRound] = useState(false);
  // What the last round did, shown until the reviewer starts working.
  const [roundSummary, setRoundSummary] = useState('');
  const roundEnabled = useFeature(REVIEW_ROUND_FEATURE);
  const decisionsEnabled = useFeature(REVIEW_DECISIONS_FEATURE);
  const taggingEnabled = useFeature(NOTE_TAGGING_FEATURE);
  // The project's figures, so a reviewer can see what an artifact shows
  // without opening it. Grouped by artifact; a failure leaves the previews
  // out rather than the queue.
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [selectedArtifacts, setSelectedArtifacts] = useState<Record<string, boolean>>({});
  // Artifacts with a decision in flight, so their own buttons disable.
  const [deciding, setDeciding] = useState<Record<string, boolean>>({});
  // The artifacts a rejection is being written for; empty means no dialog.
  const [rejecting, setRejecting] = useState<Artifact[]>([]);
  const [rejectBusy, setRejectBusy] = useState(false);

  const load = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    reviewAPI
      .get(projectId)
      .then((res) => {
        setSuspectLinks(res.data.suspect_links || []);
        setInReview(res.data.in_review_artifacts || []);
        setError('');
      })
      .catch((err) => setError(apiErrorMessage(err, 'Failed to load the review queue')))
      .finally(() => setLoading(false));
    // Previews are an enrichment, not the queue: if the figures cannot be
    // read, the reviewer still gets the work.
    attachmentAPI
      .listByProject(projectId)
      .then((res) => setAttachments(res.data || []))
      .catch(() => setAttachments([]));
  }, [projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const selectedIds = useMemo(
    () => suspectLinks.filter((l) => selected[l.id]).map((l) => l.id),
    [suspectLinks, selected],
  );
  const allSelected = suspectLinks.length > 0 && selectedIds.length === suspectLinks.length;

  // Clear one link's suspect flag and drop it from the list on success.
  const confirmLink = useCallback(async (id: string) => {
    setConfirming((c) => ({ ...c, [id]: true }));
    try {
      await linkAPI.confirm(id);
      setSuspectLinks((links) => links.filter((l) => l.id !== id));
      setSelected((s) => {
        const next = { ...s };
        delete next[id];
        return next;
      });
      setError('');
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to confirm link'));
    } finally {
      setConfirming((c) => {
        const next = { ...c };
        delete next[id];
        return next;
      });
    }
  }, []);

  const confirmSelected = useCallback(async () => {
    if (selectedIds.length === 0) return;
    const ok = await confirm({
      title: 'Confirm selected links',
      message: `Clear the suspect flag on ${selectedIds.length} link${
        selectedIds.length === 1 ? '' : 's'
      }? This vouches that each still holds after its artifact changed.`,
      confirmLabel: 'Confirm links',
    });
    if (!ok) return;

    const results = await Promise.allSettled(selectedIds.map((id) => linkAPI.confirm(id)));
    const cleared = new Set<string>();
    let failures = 0;
    results.forEach((res, i) => {
      if (res.status === 'fulfilled') cleared.add(selectedIds[i]);
      else failures += 1;
    });
    if (cleared.size > 0) {
      setSuspectLinks((links) => links.filter((l) => !cleared.has(l.id)));
      setSelected((s) => {
        const next = { ...s };
        cleared.forEach((id) => delete next[id]);
        return next;
      });
    }
    setError(failures > 0 ? `${failures} link${failures === 1 ? '' : 's'} could not be confirmed.` : '');
  }, [confirm, selectedIds]);

  // Start a round over the whole project. The confirmation spells out the
  // re-run promise, because "send everything for review" reads like it would
  // undo sign-offs people already gave, and it does not.
  const startRound = useCallback(async () => {
    if (!projectId) return;
    const ok = await confirm({
      title: 'Send the project for review',
      message:
        'Move every requirement, need, test case and other artifact still in draft into review. ' +
        'Anything already approved stays approved — only something edited since it was approved comes back for a fresh sign-off.',
      confirmLabel: 'Send for review',
    });
    if (!ok) return;

    setStartingRound(true);
    try {
      const { data } = await reviewAPI.startRound(projectId);
      const moved = data.moved?.length ?? 0;
      setRoundSummary(
        moved === 0
          ? `Nothing new to review: ${data.already_in_review} already in review, ${data.approved} approved and unchanged.`
          : `Sent ${moved} artifact${moved === 1 ? '' : 's'} for review. ${data.approved} stayed approved, unchanged since sign-off.`,
      );
      setError('');
      load();
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to start the project review'));
    } finally {
      setStartingRound(false);
    }
  }, [confirm, load, projectId]);

  const attachmentsByArtifact = useMemo(() => groupAttachments(attachments), [attachments]);

  const selectedArtifactIds = useMemo(
    () => inReview.filter((a) => selectedArtifacts[a.id]).map((a) => a.id),
    [inReview, selectedArtifacts],
  );
  const allArtifactsSelected =
    inReview.length > 0 && selectedArtifactIds.length === inReview.length;

  const toggleAllArtifacts = () => {
    if (allArtifactsSelected) {
      setSelectedArtifacts({});
      return;
    }
    const next: Record<string, boolean> = {};
    inReview.forEach((a) => {
      next[a.id] = true;
    });
    setSelectedArtifacts(next);
  };

  // Drop the decided artifacts from the list and from the selection, so the
  // queue shows what is still waiting without a round trip.
  const settleDecided = useCallback((ids: string[]) => {
    const done = new Set(ids);
    setInReview((list) => list.filter((a) => !done.has(a.id)));
    setSelectedArtifacts((sel) => {
      const next = { ...sel };
      done.forEach((id) => delete next[id]);
      return next;
    });
  }, []);

  /**
   * Approve one or more artifacts.
   *
   * Each is its own state-machine transition, so one refusal (a viewer, a
   * status someone else already moved) costs only that artifact: the rest
   * still go through and the failures are counted rather than swallowed.
   */
  const approve = useCallback(
    async (ids: string[]) => {
      if (ids.length === 0) return;
      setDeciding((d) => ({ ...d, ...Object.fromEntries(ids.map((id) => [id, true])) }));
      const results = await Promise.allSettled(
        ids.map((id) => artifactAPI.changeStatus(id, 'approved' as ArtifactStatus)),
      );
      const approved = ids.filter((_, i) => results[i].status === 'fulfilled');
      const failed = results.length - approved.length;
      settleDecided(approved);
      setDeciding((d) => {
        const next = { ...d };
        ids.forEach((id) => delete next[id]);
        return next;
      });
      setError(
        failed > 0
          ? `${failed} artifact${failed === 1 ? '' : 's'} could not be approved.`
          : '',
      );
    },
    [settleDecided],
  );

  /**
   * Send artifacts back to draft with the reviewer's reason.
   *
   * The note is posted BEFORE the status moves, deliberately. If the note
   * fails, nothing has been rejected and the reviewer can try again; if the
   * order were reversed, a failure would leave the author with an artifact
   * back in draft and no word on what was wrong with it, which is the one
   * outcome a rejection must never produce.
   */
  const reject = useCallback(
    async (artifacts: Artifact[], comment: string) => {
      setRejectBusy(true);
      const rejected: string[] = [];
      let failures = 0;
      for (const artifact of artifacts) {
        try {
          await postNote({
            artifactId: artifact.id,
            message: comment,
            projectId,
            taggingEnabled,
            onTodoError: setError,
          });
          await artifactAPI.changeStatus(artifact.id, 'draft' as ArtifactStatus);
          rejected.push(artifact.id);
        } catch {
          failures += 1;
        }
      }
      settleDecided(rejected);
      setRejectBusy(false);
      setRejecting([]);
      if (failures > 0) {
        setError(
          `${failures} artifact${failures === 1 ? '' : 's'} could not be sent back; ` +
            'their comments may have been posted, so check before trying again.',
        );
      }
    },
    [projectId, settleDecided, taggingEnabled],
  );

  const selectedArtifactRows = useMemo(
    () => inReview.filter((a) => selectedArtifacts[a.id]),
    [inReview, selectedArtifacts],
  );

  const approveSelected = useCallback(async () => {
    if (selectedArtifactIds.length === 0) return;
    const ok = await confirm({
      title: 'Approve selected',
      message: `Approve ${selectedArtifactIds.length} artifact${
        selectedArtifactIds.length === 1 ? '' : 's'
      }? This signs off their current content, and clears the suspect flag on the links that touch them.`,
      confirmLabel: 'Approve',
    });
    if (!ok) return;
    await approve(selectedArtifactIds);
  }, [approve, confirm, selectedArtifactIds]);

  const rejectSelected = useCallback(() => {
    if (selectedArtifactRows.length > 0) setRejecting(selectedArtifactRows);
  }, [selectedArtifactRows]);

  const toggleAll = () => {
    if (allSelected) {
      setSelected({});
    } else {
      const next: Record<string, boolean> = {};
      suspectLinks.forEach((l) => {
        next[l.id] = true;
      });
      setSelected(next);
    }
  };

  const showLinks = section === 'all' || section === 'links';
  const showArtifacts = section === 'all' || section === 'artifacts';

  return (
    <div style={{ padding: 24 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 16, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Review Queue</h2>
        <SegmentedControl<Section>
          aria-label="Filter review queue"
          value={section}
          onChange={setSection}
          options={[
            { value: 'all', label: 'All' },
            { value: 'links', label: `Suspect links (${suspectLinks.length})` },
            { value: 'artifacts', label: `In review (${inReview.length})` },
          ]}
        />
        <div style={{ flex: 1 }} />
        {roundEnabled && (
          <button className="button-primary" onClick={startRound} disabled={startingRound || loading}>
            {startingRound ? 'Sending…' : 'Send project for review'}
          </button>
        )}
        <button className="button-secondary" onClick={load} disabled={loading}>
          Refresh
        </button>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} />

      {roundSummary && (
        <div
          role="status"
          style={{
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderLeft: '3px solid var(--accent)',
            borderRadius: 6,
            padding: '10px 14px',
            marginBottom: 16,
            color: 'var(--text)',
            fontSize: 14,
            display: 'flex',
            alignItems: 'center',
            gap: 12,
          }}
        >
          <span style={{ flex: 1 }}>{roundSummary}</span>
          <button className="button-secondary" onClick={() => setRoundSummary('')}>
            Dismiss
          </button>
        </div>
      )}

      {loading ? (
        <div style={{ color: 'var(--text-muted)' }}>Loading…</div>
      ) : (
        <>
          {showLinks && (
            <section style={cardStyle}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
                <h3 style={{ margin: 0, color: 'var(--text)', fontSize: 16 }}>
                  Suspect links
                </h3>
                <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                  Links flagged because a linked artifact changed. Confirm each that still holds.
                </span>
                <div style={{ flex: 1 }} />
                {suspectLinks.length > 0 && (
                  <button
                    className="button-secondary"
                    onClick={confirmSelected}
                    disabled={selectedIds.length === 0}
                  >
                    Confirm selected ({selectedIds.length})
                  </button>
                )}
              </div>

              {suspectLinks.length === 0 ? (
                <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>
                  No suspect links. Traceability is trusted.
                </div>
              ) : phone ? (
                <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                  {suspectLinks.map((l) => (
                    <li
                      key={l.id}
                      style={{
                        display: 'flex',
                        gap: 10,
                        alignItems: 'flex-start',
                        padding: '12px 0',
                        borderTop: '1px solid var(--border)',
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={!!selected[l.id]}
                        aria-label={`Select link ${l.from_title} to ${l.to_title}`}
                        onChange={(e) => setSelected((s) => ({ ...s, [l.id]: e.target.checked }))}
                        style={{ width: 22, height: 22, marginTop: 2, flexShrink: 0 }}
                      />
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ color: 'var(--text)' }}>{l.from_title}</div>
                        <div style={{ margin: '4px 0', display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                          <span style={typeChip(l.from_type)}>{l.from_type}</span>
                          <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{l.type} →</span>
                          <span style={typeChip(l.to_type)}>{l.to_type}</span>
                        </div>
                        <div style={{ color: 'var(--text)' }}>{l.to_title}</div>
                        <button
                          className="button-secondary"
                          onClick={() => confirmLink(l.id)}
                          disabled={!!confirming[l.id]}
                          style={{ marginTop: 8, minHeight: 44 }}
                        >
                          {confirming[l.id] ? 'Confirming…' : 'Confirm'}
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="table-scroll">
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
                  <thead>
                    <tr style={{ textAlign: 'left', color: 'var(--text-muted)', fontSize: 12 }}>
                      <th style={{ padding: '6px 8px', width: 28 }}>
                        <input
                          type="checkbox"
                          checked={allSelected}
                          aria-label="Select all suspect links"
                          onChange={toggleAll}
                        />
                      </th>
                      <th style={{ padding: '6px 8px' }}>From</th>
                      <th style={{ padding: '6px 8px' }}>Link</th>
                      <th style={{ padding: '6px 8px' }}>To</th>
                      <th style={{ padding: '6px 8px', width: 100 }} />
                    </tr>
                  </thead>
                  <tbody>
                    {suspectLinks.map((l) => (
                      <tr key={l.id} style={{ borderTop: '1px solid var(--border)' }}>
                        <td style={{ padding: '8px' }}>
                          <input
                            type="checkbox"
                            checked={!!selected[l.id]}
                            aria-label={`Select link ${l.from_title} to ${l.to_title}`}
                            onChange={(e) =>
                              setSelected((s) => ({ ...s, [l.id]: e.target.checked }))
                            }
                          />
                        </td>
                        <td style={{ padding: '8px' }}>
                          <div style={{ color: 'var(--text)' }}>{l.from_title}</div>
                          <span style={typeChip(l.from_type)}>{l.from_type}</span>
                        </td>
                        <td style={{ padding: '8px', color: 'var(--text-muted)' }}>{l.type}</td>
                        <td style={{ padding: '8px' }}>
                          <div style={{ color: 'var(--text)' }}>{l.to_title}</div>
                          <span style={typeChip(l.to_type)}>{l.to_type}</span>
                        </td>
                        <td style={{ padding: '8px', textAlign: 'right' }}>
                          <button
                            className="button-secondary"
                            onClick={() => confirmLink(l.id)}
                            disabled={!!confirming[l.id]}
                          >
                            {confirming[l.id] ? 'Confirming…' : 'Confirm'}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                </div>
              )}
            </section>
          )}

          {showArtifacts && (
            <section style={cardStyle}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
                <h3 style={{ margin: 0, color: 'var(--text)', fontSize: 16 }}>
                  In review
                </h3>
                <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                  {decisionsEnabled
                    ? 'Approve what reads right, or send it back with a reason.'
                    : 'Artifacts submitted for review. Open one to approve or send it back.'}
                </span>
                <div style={{ flex: 1 }} />
                {/* Bulk actions act on the selection and say how big it is,
                    so nobody approves forty artifacts thinking it was four. */}
                {decisionsEnabled && inReview.length > 0 && (
                  <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                    <button
                      className="button-secondary"
                      onClick={approveSelected}
                      disabled={selectedArtifactIds.length === 0}
                    >
                      Approve selected ({selectedArtifactIds.length})
                    </button>
                    <button
                      className="button-secondary"
                      onClick={rejectSelected}
                      disabled={selectedArtifactIds.length === 0}
                    >
                      Send back selected ({selectedArtifactIds.length})
                    </button>
                  </div>
                )}
              </div>

              {inReview.length === 0 ? (
                <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>
                  Nothing is in review right now.
                </div>
              ) : !decisionsEnabled ? (
                /* Until the controls reach this workspace the queue is what it
                   was: the list of what is waiting, each opening its artifact
                   where the same decisions have always been available. */
                <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                  {inReview.map((a) => (
                    <li
                      key={a.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 10,
                        padding: phone ? '12px 0' : '8px 0',
                        borderTop: '1px solid var(--border)',
                      }}
                    >
                      <span style={typeChip(a.type)}>{a.type}</span>
                      <Link
                        to={`../requirements?artifact=${a.id}`}
                        style={{ color: 'var(--accent)', textDecoration: 'none', flex: 1 }}
                      >
                        {artifactLabel(a)}
                      </Link>
                    </li>
                  ))}
                </ul>
              ) : phone ? (
                <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                  {inReview.map((a) => (
                    <li
                      key={a.id}
                      style={{
                        display: 'flex',
                        gap: 10,
                        alignItems: 'flex-start',
                        padding: '12px 0',
                        borderTop: '1px solid var(--border)',
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={!!selectedArtifacts[a.id]}
                        aria-label={`Select ${artifactLabel(a)}`}
                        onChange={(e) =>
                          setSelectedArtifacts((sel) => ({ ...sel, [a.id]: e.target.checked }))
                        }
                        style={{ width: 22, height: 22, marginTop: 2, flexShrink: 0 }}
                      />
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <ArtifactSummary artifact={a} figures={attachmentsByArtifact[a.id] || []} />
                        <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                          <button
                            className="button-secondary"
                            onClick={() => approve([a.id])}
                            disabled={!!deciding[a.id]}
                            style={{ minHeight: 44 }}
                          >
                            {deciding[a.id] ? 'Approving…' : 'Approve'}
                          </button>
                          <button
                            className="button-secondary"
                            onClick={() => setRejecting([a])}
                            disabled={!!deciding[a.id]}
                            style={{ minHeight: 44 }}
                          >
                            Send back
                          </button>
                        </div>
                      </div>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="table-scroll">
                  <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
                    <thead>
                      <tr style={{ textAlign: 'left', color: 'var(--text-muted)', fontSize: 12 }}>
                        <th style={{ padding: '6px 8px', width: 28 }}>
                          <input
                            type="checkbox"
                            checked={allArtifactsSelected}
                            aria-label="Select all artifacts in review"
                            onChange={toggleAllArtifacts}
                          />
                        </th>
                        <th style={{ padding: '6px 8px' }}>Artifact</th>
                        <th style={{ padding: '6px 8px', width: 190 }}>Figures</th>
                        <th style={{ padding: '6px 8px', width: 190 }} />
                      </tr>
                    </thead>
                    <tbody>
                      {inReview.map((a) => (
                        <tr key={a.id} style={{ borderTop: '1px solid var(--border)' }}>
                          <td style={{ padding: '8px', verticalAlign: 'top' }}>
                            <input
                              type="checkbox"
                              checked={!!selectedArtifacts[a.id]}
                              aria-label={`Select ${artifactLabel(a)}`}
                              onChange={(e) =>
                                setSelectedArtifacts((sel) => ({ ...sel, [a.id]: e.target.checked }))
                              }
                            />
                          </td>
                          <td style={{ padding: '8px', verticalAlign: 'top' }}>
                            <ArtifactSummary artifact={a} figures={[]} />
                          </td>
                          <td style={{ padding: '8px', verticalAlign: 'top' }}>
                            <FigureStrip figures={attachmentsByArtifact[a.id] || []} />
                          </td>
                          <td style={{ padding: '8px', textAlign: 'right', verticalAlign: 'top' }}>
                            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                              <button
                                className="button-secondary"
                                onClick={() => approve([a.id])}
                                disabled={!!deciding[a.id]}
                              >
                                {deciding[a.id] ? 'Approving…' : 'Approve'}
                              </button>
                              <button
                                className="button-secondary"
                                onClick={() => setRejecting([a])}
                                disabled={!!deciding[a.id]}
                              >
                                Send back
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          )}
        </>
      )}

      {rejecting.length > 0 && (
        <RejectDialog
          artifacts={rejecting}
          projectId={projectId}
          busy={rejectBusy}
          onCancel={() => setRejecting([])}
          onSubmit={(comment) => reject(rejecting, comment)}
        />
      )}
    </div>
  );
};

export default ReviewQueue;
