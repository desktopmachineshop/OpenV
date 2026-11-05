import { useCallback, useState } from 'react';
import { Artifact, artifactAPI } from '../../api/client';
import { useFeature } from '../../hooks/useFeature';
import {
  ASSISTANT_EDITS_FEATURE,
  GATED_REASON,
  applySuggestionsToProject,
  isProjectEditKind,
} from './applySuggestion';
import type { CopilotSuggestion } from './GuidedChatPanel';

/**
 * Add what the assistant suggested to the project, for a chat beside the
 * project rather than the wizard (the Notes panel's assistant tab and the
 * floating assistant).
 *
 * The wizard applies a suggestion into the form somebody is filling in;
 * here there is no form, so it goes straight into the project as a draft
 * artifact under the heading the wizard would have used. The project's
 * artifacts are read at apply time rather than held in state: a heading
 * created a minute ago by somebody else should be found, not duplicated.
 *
 * `applied` is which suggestions this chat has applied. The wizard persists
 * its own in the session answers; here the project is the record, so
 * remembering for the life of the chat is what stops a second click adding
 * twice.
 */
export const useProjectSuggestions = (projectId: string | undefined, onArtifactsChanged?: () => void) => {
  const [applied, setApplied] = useState<Record<string, boolean>>({});
  const editsEnabled = useFeature(ASSISTANT_EDITS_FEATURE);

  const applySuggestions = useCallback(
    async (items: { suggestion: CopilotSuggestion; key: string }[]): Promise<(string | null)[]> => {
      if (!projectId) return items.map(() => 'No project is open.');
      let artifacts: Artifact[] = [];
      try {
        const res = await artifactAPI.list(projectId);
        artifacts = res.data || [];
      } catch {
        // Without the list, headings cannot be matched and would be created
        // again. Refusing is better than quietly growing a second set.
        return items.map(() => 'The project could not be read, so nothing was added.');
      }
      // A gated kind is answered, not applied: the server does not offer
      // these shapes to a gated workspace, but a transcript can carry one
      // from before the gate closed, or from another member's channel.
      const allowed = editsEnabled ? items : items.filter((i) => !isProjectEditKind(i.suggestion?.kind));
      const applyResults = await applySuggestionsToProject(
        { projectId, artifacts, onChanged: onArtifactsChanged },
        allowed
      );
      const byKey = new Map(allowed.map((i, n) => [i.key, applyResults[n]]));
      const results = items.map((i) => (byKey.has(i.key) ? byKey.get(i.key)! : GATED_REASON));
      setApplied((prev) => {
        const next = { ...prev };
        items.forEach((i, n) => {
          if (results[n] === null) next[i.key] = true;
        });
        return next;
      });
      return results;
    },
    [projectId, onArtifactsChanged, editsEnabled]
  );

  return { applied, applySuggestions };
};
