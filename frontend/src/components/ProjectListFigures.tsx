import React from 'react';
import { Meter, mutedStyle } from './ui';

// The project picker on the workspace Home (the redesign-pages feature key):
// each project's row shows its size and how much of it is traced and
// verified. The figures come with the rest of the Home (ProjectListHomeLoad),
// a few projects at a time, so a long list shows its names at once and
// fills in.

export interface ProjectFigures {
  count: number | null;
  traced: number | null;
  verified: number | null;
}

/** The row's figures: the artifact count beside the name, then the two meters. */
export const ProjectCount: React.FC<{ figures?: ProjectFigures }> = ({ figures }) =>
  figures && figures.count !== null ? <span className="project-count">{figures.count} artifacts</span> : null;

/** The Projects panel's header action (#720): opens the new-project form below the list and brings it into view. */
export const NewProjectAction: React.FC<{ onClick: () => void }> = ({ onClick }) => (
  <button
    type="button"
    className="project-panel-new"
    onClick={() => {
      onClick();
      requestAnimationFrame(() => document.querySelector('.create-project-form')?.scrollIntoView?.({ behavior: 'smooth', block: 'start' }));
    }}
  >
    + New project
  </button>
);

export const ProjectMeters: React.FC<{ figures?: ProjectFigures }> = ({ figures }) => {
  if (!figures) return <span style={mutedStyle} aria-busy="true">Loading figures…</span>;
  if (figures.traced === null) return <span style={mutedStyle}>No requirements to trace yet</span>;
  return (
    <div className="project-meters">
      <Meter label="Traced" value={figures.traced} color="var(--accent)" />
      {figures.verified !== null && <Meter label="Verified" value={figures.verified} color="var(--success)" />}
    </div>
  );
};
