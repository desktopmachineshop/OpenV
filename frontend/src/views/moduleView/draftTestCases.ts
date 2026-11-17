import type { NavigateFunction } from 'react-router-dom';
import { agentsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import type { Artifact, Setter } from './shared';

// Drafting test cases with an agent: for the selected requirement when one
// is selected, otherwise for every requirement in view. The run starts and
// the reader is taken to the runs page, where its proposals arrive.
// Not a hook: the shell holds the "drafting" flag with its other state and
// calls this where the artifacts in view are known.
export const draftTestCases = (
  projectId: string | undefined,
  selectedArtifact: Artifact | undefined,
  activeArtifacts: Artifact[],
  [draftingTests, setDraftingTests]: [boolean, Setter<boolean>],
  setError: Setter<string>,
  navigate: NavigateFunction
) => {
  const requirementTargets =
    selectedArtifact && selectedArtifact.type === 'requirement'
      ? [selectedArtifact]
      : activeArtifacts.filter((a) => a.type === 'requirement');

  const handleDraftTestCases = async () => {
    if (!projectId || requirementTargets.length === 0 || draftingTests) return;
    setDraftingTests(true);
    try {
      await agentsAPI.draftTestCases(
        projectId,
        requirementTargets.map((a) => a.id)
      );
      setError('');
      navigate(`/projects/${projectId}/agent-runs`);
    } catch (error: any) {
      setError(`Failed to launch test-case drafting: ${apiErrorMessage(error, 'Unknown error')}`);
    } finally {
      setDraftingTests(false);
    }
  };

  return { requirementTargets, draftingTests, handleDraftTestCases };
};
