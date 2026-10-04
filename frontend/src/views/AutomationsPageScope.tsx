import React from 'react';
import { SegmentedControl } from '../components/ui';
import type { AutomationScope } from './AutomationsPageForm';

// What an automation covers, on the Automations page: the pill that marks
// a whole-workspace automation in the table, and the form's choice between
// the project and the whole workspace, which only a workspace admin makes.

/** The table's mark for an automation that covers the whole workspace. */
export const WorkspaceScopePill: React.FC = () => (
  <span
    title="Covers every project of the workspace"
    style={{
      display: 'inline-block',
      marginLeft: 8,
      padding: '1px 8px',
      borderRadius: 10,
      background: 'var(--neutral-soft)',
      color: 'var(--text-muted)',
      fontSize: 11,
      fontWeight: 600,
      whiteSpace: 'nowrap',
    }}
  >
    Whole workspace
  </span>
);

const SCOPE_HINTS: Record<AutomationScope, string> = {
  project: 'Runs for this project alone: on its events, and in it.',
  workspace:
    'Runs for every project of the workspace, in the project of the event that fires it, and on ' +
    'workspace membership and invitation events. A crew made in one project cannot run it; the ' +
    "workspace's own crews, such as its default crew, can.",
};

interface ScopeFieldProps {
  scope: AutomationScope;
  /** Whether the viewer may choose: a workspace admin with the feature on. */
  canChoose: boolean;
  onChange: (scope: AutomationScope) => void;
}

/**
 * The form's scope. A workspace admin chooses it; anyone else keeps the
 * project, the only scope the page offered before, and sees nothing of it,
 * unless the automation already covers the whole workspace, which is said.
 */
export const ScopeField: React.FC<ScopeFieldProps> = ({ scope, canChoose, onChange }) => {
  if (!canChoose && scope === 'project') return null;
  return (
    <div className="form-group">
      <label>Covers</label>
      {canChoose ? (
        <SegmentedControl
          aria-label="Automation scope"
          options={[
            { value: 'project', label: 'This project' },
            { value: 'workspace', label: 'Whole workspace' },
          ]}
          value={scope}
          onChange={onChange}
        />
      ) : (
        <div style={{ color: 'var(--text)' }}>The whole workspace</div>
      )}
      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>{SCOPE_HINTS[scope]}</div>
    </div>
  );
};
