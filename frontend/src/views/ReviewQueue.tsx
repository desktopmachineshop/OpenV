import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
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
import { ErrorBanner, SegmentedControl, buttonStyle, useConfirm } from '../components/ui';
import { postNote } from '../components/NoteComposer';
import { NOTE_TAGGING_FEATURE } from '../components/noteTagging';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { groupAttachments } from './reviewArtifacts';
import { RejectDialog } from './ReviewQueueParts';
import {
  ArtifactCard,
  BoardColumn,
  COLUMNS,
  ColumnKey,
  DecidedCard,
  LinkCard,
  ReviewDecision,
} from './ReviewQueueBoard';
import type { FeatureKey } from '../generated/contract';

// The project-wide review round is gated until a stable release carries it.
export const REVIEW_ROUND_FEATURE: FeatureKey = 'project-review-round';
// Deciding a review from the queue — previews, inline Approve / Send back,
// selection and bulk actions — is gated the same way. What it writes is the
// ordinary status change and the ordinary note, so a decision made on nightly
// reads correctly in a workspace that has not received the controls yet.
export const REVIEW_DECISIONS_FEATURE: FeatureKey = 'review-queue-decisions';

type Section = 'all' | 'links' | 'artifacts';

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
  // What was decided on this board, newest first: the Approved and Changes
  // requested columns. Kept for the visit; a reload starts them empty.
  const [decided, setDecided] = useState<ReviewDecision[]>([]);
  // The one column a phone shows at a time.
  const [phoneColumn, setPhoneColumn] = useState<ColumnKey>('in-review');
  // The decisions read the queue as it is when they land, not as it was
  // when the button was pressed.
  const inReviewRef = useRef<Artifact[]>([]);
  inReviewRef.current = inReview;

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
  const settleDecided = useCallback((ids: string[], outcome: ReviewDecision['outcome'], comment?: string) => {
    const done = new Set(ids);
    const at = new Date().toISOString();
    const moved = inReviewRef.current
      .filter((a) => done.has(a.id))
      .map((artifact) => ({ artifact, outcome, comment, at }));
    if (moved.length > 0) setDecided((prev) => [...moved, ...prev]);
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
      settleDecided(approved, 'approved');
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
      settleDecided(rejected, 'sent-back', comment);
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

  const sentBack = decided.filter((d) => d.outcome === 'sent-back');
  const approvedHere = decided.filter((d) => d.outcome === 'approved');
  const counts: Record<ColumnKey, number> = {
    'in-review': inReview.length,
    links: suspectLinks.length,
    changes: sentBack.length,
    approved: approvedHere.length,
  };
  // Before the decision controls reach a workspace nothing is decided here,
  // so the two columns of decisions would only ever be empty.
  const columns = COLUMNS.filter((c) => decisionsEnabled || (c.key !== 'changes' && c.key !== 'approved'));
  const shown = (key: ColumnKey) =>
    phone
      ? phoneColumn === key
      : key === 'links'
        ? showLinks
        : showArtifacts;
  // Bulk actions appear once something is ticked, and say how much, so
  // nobody approves forty artifacts thinking it was four.
  const bulk = buttonStyle('secondary', { compact: !phone });

  const renderColumn = (key: ColumnKey) => {
    const column = COLUMNS.find((c) => c.key === key)!;
    if (key === 'links') {
      return (
        <BoardColumn
          key={key}
          column={column}
          count={counts.links}
          phone={phone}
          helpKey="review.queue"
          empty="No suspect links. Traceability is trusted."
          tools={
            suspectLinks.length > 0 && (
              <div className="ov-board-tools" data-help-key="review.bulk">
                <label className="ov-board-selectall">
                  <input
                    type="checkbox"
                    checked={allSelected}
                    aria-label="Select all suspect links"
                    data-help-key="review.bulk"
                    onChange={toggleAll}
                    style={{ width: 'auto', margin: 0 }}
                  />
                  All
                </label>
                {selectedIds.length === 0 ? (
                  <span className="ov-board-hint">Tick links to confirm them together</span>
                ) : (
                  <button type="button" style={bulk} onClick={confirmSelected}>
                    Confirm selected ({selectedIds.length})
                  </button>
                )}
              </div>
            )
          }
        >
          {suspectLinks.map((l) => (
            <LinkCard
              key={l.id}
              link={l}
              phone={phone}
              selected={!!selected[l.id]}
              onSelect={(checked) => setSelected((s) => ({ ...s, [l.id]: checked }))}
              busy={!!confirming[l.id]}
              onConfirm={() => confirmLink(l.id)}
            />
          ))}
        </BoardColumn>
      );
    }
    if (key === 'in-review') {
      return (
        <BoardColumn
          key={key}
          column={column}
          count={counts['in-review']}
          phone={phone}
          helpKey="review.queue"
          empty="Nothing is in review right now."
          tools={
            // Bulk actions act on the selection and say how big it is, so
            // nobody approves forty artifacts thinking it was four.
            decisionsEnabled &&
            inReview.length > 0 && (
              <div className="ov-board-tools" data-help-key="review.bulk">
                <label className="ov-board-selectall">
                  <input
                    type="checkbox"
                    checked={allArtifactsSelected}
                    aria-label="Select all artifacts in review"
                    data-help-key="review.bulk"
                    onChange={toggleAllArtifacts}
                    style={{ width: 'auto', margin: 0 }}
                  />
                  All
                </label>
                {selectedArtifactIds.length === 0 ? (
                  <span className="ov-board-hint">Tick cards to decide them together</span>
                ) : (
                  <>
                    <button type="button" style={bulk} onClick={approveSelected}>
                      Approve selected ({selectedArtifactIds.length})
                    </button>
                    <button type="button" style={bulk} onClick={rejectSelected}>
                      Send back selected ({selectedArtifactIds.length})
                    </button>
                  </>
                )}
              </div>
            )
          }
        >
          {inReview.map((a) => (
            <ArtifactCard
              key={a.id}
              artifact={a}
              figures={attachmentsByArtifact[a.id] || []}
              decisions={decisionsEnabled}
              phone={phone}
              selected={!!selectedArtifacts[a.id]}
              onSelect={(checked) => setSelectedArtifacts((sel) => ({ ...sel, [a.id]: checked }))}
              busy={!!deciding[a.id]}
              onApprove={() => approve([a.id])}
              onSendBack={() => setRejecting([a])}
            />
          ))}
        </BoardColumn>
      );
    }
    const list = key === 'changes' ? sentBack : approvedHere;
    return (
      <BoardColumn
        key={key}
        column={column}
        count={list.length}
        phone={phone}
        helpKey="review.queue"
        empty={
          key === 'changes'
            ? 'What you send back from this board lands here, with your reason.'
            : 'What you approve from this board lands here.'
        }
      >
        {list.map((d) => (
          <DecidedCard key={`${d.outcome}-${d.artifact.id}-${d.at}`} decision={d} phone={phone} />
        ))}
      </BoardColumn>
    );
  };

  return (
    <div className={phone ? 'ov-page ov-review is-phone' : 'ov-page ov-review'}>
      <div className="ov-page-head">
        {/* On a phone the top bar names the page; the heading stays for a
            screen reader. */}
        <h2 className={phone ? 'sr-only' : undefined}>Review Queue</h2>
        <span className="ov-page-sub">
          {inReview.length} in review · {suspectLinks.length} suspect link{suspectLinks.length === 1 ? '' : 's'}
        </span>
        {!phone && (
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
        )}
        <div style={{ flex: 1 }} />
        {roundEnabled && (
          <button
            type="button"
            style={buttonStyle('primary', { disabled: startingRound || loading })}
            onClick={startRound}
            disabled={startingRound || loading}
            data-help-key="review.round"
          >
            {startingRound ? 'Sending…' : 'Send project for review'}
          </button>
        )}
        <button type="button" style={buttonStyle('secondary', { disabled: loading })} onClick={load} disabled={loading}>
          Refresh
        </button>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} />

      {roundSummary && (
        <div role="status" className="ov-review-summary">
          <span style={{ flex: 1 }}>{roundSummary}</span>
          <button type="button" style={buttonStyle('secondary', { compact: true })} onClick={() => setRoundSummary('')}>
            Dismiss
          </button>
        </div>
      )}

      {phone && (
        <div role="tablist" aria-label="Review state" className="ov-chip-tabs">
          {columns.map((c) => (
            <button
              key={c.key}
              type="button"
              role="tab"
              aria-selected={phoneColumn === c.key}
              className="ov-chip-tab"
              onClick={() => setPhoneColumn(c.key)}
            >
              {c.short} {counts[c.key]}
            </button>
          ))}
        </div>
      )}

      {loading ? (
        <div style={{ color: 'var(--text-muted)' }}>Loading…</div>
      ) : (
        <div className="ov-board" style={{ ['--ov-board-cols' as string]: phone ? 1 : columns.filter((c) => shown(c.key)).length }}>
          {columns.filter((c) => shown(c.key)).map((c) => renderColumn(c.key))}
        </div>
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
