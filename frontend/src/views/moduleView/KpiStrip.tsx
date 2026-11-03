import React from 'react';
import type { ModuleFigures } from '../../utils/artifactFacts';

// The figures over the list: how many artifacts are in view, how many are
// traced and verified, and what needs attention (suspect links, quality
// findings). Each counts what the search and filters leave in view.
export const KpiStrip: React.FC<{ figures: ModuleFigures }> = ({ figures }) => {
  const item = (value: string | number, label: string, color = 'var(--text)', title?: string) => (
    <span title={title}>
      <b style={{ fontSize: 18, fontWeight: 700, color }}>{value}</b>{' '}
      <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{label}</span>
    </span>
  );
  return (
    <div
      aria-label="Figures"
      role="group"
      data-help-key="requirements.views"
      style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 20px', alignItems: 'baseline', padding: '2px 2px 0', flexShrink: 0 }}
    >
      {item(figures.artifacts, figures.artifacts === 1 ? 'artifact' : 'artifacts')}
      {figures.tracedPct !== null &&
        item(`${figures.tracedPct}%`, 'traced', undefined, 'Artifacts (headings aside) with at least one traceability link')}
      {figures.verifiedPct !== null &&
        item(`${figures.verifiedPct}%`, 'verified', undefined, 'Requirements whose verification passes')}
      {item(
        figures.suspectLinks,
        figures.suspectLinks === 1 ? 'suspect link' : 'suspect links',
        figures.suspectLinks ? 'var(--warning-text)' : undefined
      )}
      {item(
        figures.qualityFindings,
        figures.qualityFindings === 1 ? 'quality finding' : 'quality findings',
        figures.qualityFindings ? 'var(--danger-strong)' : undefined
      )}
    </div>
  );
};
