import React from 'react';
import type { NavigateFunction } from 'react-router-dom';
import { baselineLabel } from '../../utils/baselines';
import { buttonStyle } from '../../components/ui';
import type { Artifact, Baseline, Setter } from './shared';

// The requirements toolbar: the baseline picker, compare, delete and capture,
// test-case drafting and the download wizard. A wide screen shows it in the
// document header; stacked, it folds into the phone actions sheet, which closes
// when a click lands inside a button, so its items stay native <button>s.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface ToolbarProps {
  activeBaselineId: string;
  handleBaselineChange: (baselineId: string) => void;
  baselines: Baseline[];
  navigate: NavigateFunction;
  projectId: string;
  handleDeleteBaseline: (baselineId: string) => void;
  stacked: boolean;
  handleCaptureBaseline: () => void;
  handleDraftTestCases: () => void;
  isBaselineView: boolean;
  draftingTests: boolean;
  requirementTargets: Artifact[];
  selectedArtifact: Artifact | undefined;
  setDownloadOpen: Setter<boolean>;
}

export const Toolbar: React.FC<ToolbarProps> = ({
  activeBaselineId,
  handleBaselineChange,
  baselines,
  navigate,
  projectId,
  handleDeleteBaseline,
  stacked,
  handleCaptureBaseline,
  handleDraftTestCases,
  isBaselineView,
  draftingTests,
  requirementTargets,
  selectedArtifact,
  setDownloadOpen,
}) => {
  // One primary action per area, and it is + New Artifact at the end of the
  // header: everything here is a quiet secondary button, the baseline delete
  // a red outline, and the agent action in the agent colour.
  const tall = stacked ? { minHeight: 44, height: 'auto' } : { height: 30, minHeight: 30 };
  const noBaselines = baselines.length === 0;
  const live = activeBaselineId === 'live';
  const cannotDraft = isBaselineView || draftingTests || requirementTargets.length === 0;
  return (
  <>
    <select
      value={activeBaselineId}
      onChange={(e) => handleBaselineChange(e.target.value)}
      title="Select baseline"
      data-help-key="baselines"
      style={{
        height: stacked ? 44 : 30,
        padding: '0 8px',
        borderRadius: 6,
        border: '1px solid var(--border)',
        fontSize: 13,
        backgroundColor: 'var(--surface)',
        color: 'var(--text)',
        cursor: 'pointer',
        width: 'auto',
        minWidth: 160,
        maxWidth: '100%',
      }}
    >
      <option value="live">Live Project</option>
      {baselines.map((baseline) => (
        <option key={baseline.id} value={baseline.id}>
          {baselineLabel(baseline)}
        </option>
      ))}
    </select>
    <button
      type="button"
      onClick={() => {
        // Compare the selected baseline (or the newest one when viewing
        // live) against the live project by default.
        const base = activeBaselineId !== 'live' ? activeBaselineId : baselines[0]?.id;
        if (base) navigate(`/projects/${projectId}/baselines/${base}/compare`);
      }}
      disabled={noBaselines}
      style={{ ...buttonStyle('secondary', { disabled: noBaselines }), ...tall }}
      title="Compare this baseline against another baseline or the live project"
      data-help-key="baselines"
    >
      Compare
    </button>
    <button
      type="button"
      onClick={() => handleDeleteBaseline(activeBaselineId)}
      disabled={live}
      style={{ ...buttonStyle('danger', { disabled: live }), ...tall }}
      title="Delete selected baseline"
      aria-label={stacked ? undefined : 'Delete baseline'}
      data-help-key="baselines"
    >
      {stacked ? 'Delete baseline' : '🗑'}
    </button>
    <button
      type="button"
      onClick={handleCaptureBaseline}
      data-help-key="baselines"
      style={{ ...buttonStyle('secondary'), ...tall }}
    >
      Capture Baseline
    </button>
    <button
      type="button"
      onClick={handleDraftTestCases}
      disabled={cannotDraft}
      style={{ ...buttonStyle('agent', { disabled: cannotDraft }), ...tall }}
      title={
        requirementTargets.length === 0
          ? 'No requirements to draft test cases for'
          : selectedArtifact && selectedArtifact.type === 'requirement'
            ? 'Draft test cases for the selected requirement (as proposals)'
            : `Draft test cases for all ${requirementTargets.length} requirements in view (as proposals)`
      }
    >
      {draftingTests ? 'Drafting…' : 'Draft test cases'}
    </button>
    {/* One way out of a project: the wizard asks what shape and how much,
        and every format reads the same narrowed snapshot. */}
    <button
      type="button"
      onClick={() => setDownloadOpen(true)}
      style={{ ...buttonStyle('secondary'), ...tall }}
      title="Download this project — choose a format, sections and attachments"
      data-help-key="projects.download"
    >
      ↓ Download
    </button>
  </>
  );
};
