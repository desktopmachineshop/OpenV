import React from 'react';
import { Link as RouterLink } from 'react-router-dom';
import type { Artifact } from '../api/client';
import { ArtifactBody } from './ArtifactBody';

interface ArtifactStatementProps {
  artifact: Artifact;
  onReferenceClick?: (ref: string) => void;
}

// The artifact's own text, for the refreshed pane (ui-refresh), where it sits
// straight under the title instead of below the artifact's facts, trace and
// verification. A requirement also offers the way to its impact analysis.
export const ArtifactStatement: React.FC<ArtifactStatementProps> = ({ artifact, onReferenceClick }) => (
  <div style={{ margin: '0 0 10px' }}>
    {/* Citations in the body (# figures, ## artifacts) open what they cite. */}
    <div style={{ fontSize: 15, lineHeight: 1.55, color: 'var(--text)' }} data-help-key="requirements.references">
      {artifact.body ? (
        <ArtifactBody body={artifact.body} onReferenceClick={onReferenceClick} />
      ) : (
        <p style={{ margin: 0, fontStyle: 'italic', color: 'var(--text-muted)' }}>(no description)</p>
      )}
    </div>
    {artifact.project_id && artifact.type === 'requirement' && (
      <RouterLink
        to={`/projects/${artifact.project_id}/impact?artifact=${artifact.id}`}
        title="Trace what a change to this requirement would affect"
        data-help-key="impact"
        className="compact-action" style={{ display: 'inline-flex', alignItems: 'center', padding: 0, fontSize: 12, color: 'var(--accent-text)', textDecoration: 'none' }}
      >
        Show impact →
      </RouterLink>
    )}
  </div>
);
