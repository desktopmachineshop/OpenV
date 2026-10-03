import React from 'react';
import type { NavigateFunction } from 'react-router-dom';
import { baselineLabel } from '../../utils/baselines';
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
}) => (
  <>
    <select
      value={activeBaselineId}
      onChange={(e) => handleBaselineChange(e.target.value)}
      title="Select baseline"
      style={{
        height: '36px',
        padding: '0 10px',
        borderRadius: '4px',
        border: '1px solid var(--neutral-mid)',
        fontSize: '12px',
        backgroundColor: 'var(--surface)',
        cursor: 'pointer',
        width: 'auto',
        minWidth: '180px',
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
      onClick={() => {
        // Compare the selected baseline (or the newest one when viewing
        // live) against the live project by default.
        const base = activeBaselineId !== 'live' ? activeBaselineId : baselines[0]?.id;
        if (base) navigate(`/projects/${projectId}/baselines/${base}/compare`);
      }}
      disabled={baselines.length === 0}
      style={{
        height: '36px',
        padding: '0 12px',
        backgroundColor: baselines.length === 0 ? 'var(--neutral-mid)' : 'var(--accent)',
        color: 'var(--accent-fg)',
        border: 'none',
        borderRadius: '4px',
        cursor: baselines.length === 0 ? 'not-allowed' : 'pointer',
        fontSize: '12px',
      }}
      title="Compare this baseline against another baseline or the live project"
    >
      Compare
    </button>
    <button
      onClick={() => handleDeleteBaseline(activeBaselineId)}
      disabled={activeBaselineId === 'live'}
      style={{
        height: '36px',
        padding: '0 10px',
        backgroundColor: activeBaselineId === 'live' ? 'var(--neutral-mid)' : 'var(--danger)',
        color: 'white',
        border: 'none',
        borderRadius: '4px',
        cursor: activeBaselineId === 'live' ? 'not-allowed' : 'pointer',
        fontSize: '12px',
      }}
      title="Delete selected baseline"
    >
      {stacked ? '🗑 Delete baseline' : '🗑'}
    </button>
    <button
      onClick={handleCaptureBaseline}
      style={{
        height: '36px',
        padding: '0 12px',
        backgroundColor: 'var(--success-bright)',
        color: 'white',
        border: 'none',
        borderRadius: '4px',
        cursor: 'pointer',
        fontSize: '12px',
      }}
    >
      Capture Baseline
    </button>
    <button
      onClick={handleDraftTestCases}
      disabled={isBaselineView || draftingTests || requirementTargets.length === 0}
      style={{
        height: '36px',
        padding: '0 12px',
        backgroundColor:
          isBaselineView || requirementTargets.length === 0 ? 'var(--neutral-mid)' : 'var(--success-bright)',
        color: 'white',
        border: 'none',
        borderRadius: '4px',
        cursor:
          isBaselineView || draftingTests || requirementTargets.length === 0 ? 'not-allowed' : 'pointer',
        fontSize: '12px',
      }}
      title={
        requirementTargets.length === 0
          ? 'No requirements to draft test cases for'
          : selectedArtifact && selectedArtifact.type === 'requirement'
            ? 'Draft test cases for the selected requirement (as proposals)'
            : `Draft test cases for all ${requirementTargets.length} requirements in view (as proposals)`
      }
    >
      {draftingTests ? 'Drafting…' : '🧪 Draft test cases'}
    </button>
    {/* One way out of a project: the wizard asks what shape and how much,
        and every format reads the same narrowed snapshot. */}
    <button
      onClick={() => setDownloadOpen(true)}
      style={{
        height: '36px',
        padding: '0 12px',
        backgroundColor: 'var(--accent-alt)',
        color: 'white',
        border: 'none',
        borderRadius: '4px',
        cursor: 'pointer',
        fontSize: '12px',
      }}
      title="Download this project — choose a format, sections and attachments"
    >
      ↓ Download
    </button>
  </>
);
