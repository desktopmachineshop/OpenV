import { useCallback, useState } from 'react';
import { baselineAPI, Baseline, ProjectExport } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { useConfirm, usePrompt } from '../../components/ui';
import type { Setter } from './shared';

export interface BaselineViewOptions {
  projectId: string;
  /** The shell's error banner. */
  setError: Setter<string>;
  /**
   * What switching between the live project and a baseline does to the
   * shell's state, right after the switch is recorded: drop the selection and
   * leave any edit, create form or version preview.
   */
  leaveSelection: () => void;
}

/**
 * The project's baselines and the one the module shows in place of the live
 * project: the list (loaded by the shell's project load through
 * loadBaselines), the switch between live and a baseline with the snapshot it
 * fetches, and capturing and deleting a baseline. The shell's project load
 * puts the view back on live with setActiveBaselineId and setBaselineData.
 */
export function useBaselineView({ projectId, setError, leaveSelection }: BaselineViewOptions) {
  const confirm = useConfirm();
  const prompt = usePrompt();
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [activeBaselineId, setActiveBaselineId] = useState<string>('live');
  const [baselineData, setBaselineData] = useState<ProjectExport | null>(null);
  // A baseline snapshot is a whole project export — close to a megabyte of
  // JSON for a real project — so on a phone it can take seconds to arrive.
  // Without this the tree renders empty the moment a baseline is selected and
  // only fills in when the fetch lands, which looks exactly like a project
  // that lost all its requirements.
  const [baselineLoading, setBaselineLoading] = useState(false);

  const isBaselineView = activeBaselineId !== 'live';

  const loadBaselines = useCallback(async () => {
    if (!projectId) return;
    try {
      const response = await baselineAPI.list(projectId);
      setBaselines(response.data || []);
    } catch (error: any) {
      console.error('Failed to load baselines:', error);
      setBaselines([]);
    }
  }, [projectId]);

  const handleBaselineChange = async (baselineId: string) => {
    setActiveBaselineId(baselineId);
    leaveSelection();

    if (baselineId === 'live') {
      setBaselineData(null);
      setBaselineLoading(false);
      return;
    }

    setBaselineData(null);
    setBaselineLoading(true);
    try {
      const response = await baselineAPI.get(baselineId);
      setBaselineData(response.data);
      setError('');
    } catch (error: any) {
      console.error('Failed to load baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to load baseline: ${errorMsg}`);
      // Fall back to the live project rather than leaving the reader on a
      // baseline view with nothing in it, which they would read as a baseline
      // that captured nothing.
      setActiveBaselineId('live');
    } finally {
      setBaselineLoading(false);
    }
  };

  const handleCaptureBaseline = async () => {
    if (!projectId) {
      // A click that does nothing and says nothing is indistinguishable from
      // a broken button.
      setError('No project is open, so there is nothing to capture.');
      return;
    }
    const name = await prompt({
      title: 'Capture baseline',
      label: 'Baseline name',
      placeholder: 'e.g. Design freeze — rev A',
    });
    if (name === null) return;
    if (!name.trim()) {
      setError('Baseline name is required');
      return;
    }

    try {
      await baselineAPI.create(projectId, name.trim());
      await loadBaselines();
      setError('');
    } catch (error: any) {
      console.error('Failed to capture baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to capture baseline: ${errorMsg}`);
    }
  };

  const handleDeleteBaseline = async (baselineId: string) => {
    if (baselineId === 'live') return;
    const ok = await confirm({
      title: 'Delete baseline',
      message: 'Delete this baseline? This cannot be undone.',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) {
      return;
    }

    try {
      await baselineAPI.delete(baselineId);
      await loadBaselines();
      setActiveBaselineId('live');
      setBaselineData(null);
      setError('');
    } catch (error: any) {
      console.error('Failed to delete baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to delete baseline: ${errorMsg}`);
    }
  };

  return {
    baselines,
    activeBaselineId,
    setActiveBaselineId,
    baselineData,
    setBaselineData,
    baselineLoading,
    isBaselineView,
    loadBaselines,
    handleBaselineChange,
    handleCaptureBaseline,
    handleDeleteBaseline,
  };
}
