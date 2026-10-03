import React from 'react';
import { QualityRulesEditor } from '../../components/QualityRulesEditor';

// The Quality rules tab of project settings. The editor keeps its own draft,
// so leaving the tab drops an unsaved change.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface QualityTabProps {
  projectId: string;
  canEditRules: boolean;
  flash: (msg: string) => void;
}

export const QualityTab: React.FC<QualityTabProps> = ({ projectId, canEditRules, flash }) => (
  <div className="card">
    <h3>Requirement quality rules</h3>
    <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
      How this project's requirements are worded and judged. Set nothing and the project
      follows its workspace's house style (Workspace settings → Quality rules); override only
      what this project needs to do differently. Agents read these rules before they draft,
      and the quality badge scores against them.
    </p>
    {projectId && (
      <QualityRulesEditor
        level="project"
        id={projectId}
        canEdit={canEditRules}
        onSaved={() => flash('Quality rules saved')}
      />
    )}
  </div>
);
