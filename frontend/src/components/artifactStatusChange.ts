import { useState } from 'react';
import { Artifact, ArtifactStatus, artifactAPI } from '../api/client';
import { useAlert, useConfirm } from './ui';
import { apiErrorMessage } from '../api/errors';

// Review state machine (mirrors internal/domain/artifacts/status.go):
// draft <-> in_review -> approved -> superseded. The server is authoritative;
// this map only decides which transition buttons to offer.
export const NEXT_STATUSES: Record<ArtifactStatus, ArtifactStatus[]> = {
  draft: ['in_review'],
  in_review: ['draft', 'approved'],
  approved: ['superseded'],
  superseded: [],
};

export const TRANSITION_LABELS: Record<ArtifactStatus, string> = {
  draft: 'Return to draft',
  in_review: 'Submit for review',
  approved: 'Approve',
  superseded: 'Mark superseded',
};

/** The transition a step forward takes; Return to draft is the step back. */
export const isForward = (next: ArtifactStatus): boolean => next !== 'draft';

/**
 * Moving an artifact through review: Approve and Mark superseded ask first,
 * a failure is reported in a dialog, and `onChanged` receives the new
 * version the change creates. The header's buttons and the phone's action
 * bar both go through here.
 */
export function useStatusChange(artifactId: string, onChanged: (artifact: Artifact) => void) {
  const confirm = useConfirm();
  const alertDialog = useAlert();
  const [changing, setChanging] = useState(false);

  const change = async (next: ArtifactStatus) => {
    if (next === 'approved' || next === 'superseded') {
      const ok = await confirm({
        title: TRANSITION_LABELS[next],
        message:
          next === 'approved'
            ? 'Approve this artifact? Editing it later will return the new version to draft.'
            : 'Mark this artifact as superseded? This is a terminal state.',
        confirmLabel: TRANSITION_LABELS[next],
      });
      if (!ok) return;
    }
    try {
      setChanging(true);
      const response = await artifactAPI.changeStatus(artifactId, next);
      onChanged(response.data);
    } catch (error) {
      console.error('Failed to change artifact status', error);
      await alertDialog({
        title: 'Change status',
        message: apiErrorMessage(error, 'Failed to change status.'),
      });
    } finally {
      setChanging(false);
    }
  };

  return { change, changing };
}
