import React, { useEffect, useState } from 'react';
import { artifactAPI, vvAPI } from '../api/client';
import { Meter, mutedStyle } from './ui';

// The projects list drawn as the Home design's Projects panel (the
// redesign-pages feature key): each project as a row with its size and how
// much of it is traced and verified, the same figures Home shows for the
// project you are in. The figures load after the list, a project at a time
// in small batches, so a long list shows its names at once and fills in.

export interface ProjectFigures {
  count: number | null;
  traced: number | null;
  verified: number | null;
}

const percent = (part: number | undefined, total: number | undefined): number | null =>
  total ? Math.round(((part ?? 0) / total) * 100) : null;

const BATCH = 4;

const figuresFor = async (projectId: string): Promise<ProjectFigures> => {
  const [cov, page] = await Promise.allSettled([vvAPI.coverage(projectId), artifactAPI.listPage(projectId, { limit: 1 })]);
  const summary = cov.status === 'fulfilled' ? cov.value.data.summary : undefined;
  const total = page.status === 'fulfilled' ? parseInt(page.value.headers?.['x-total-count'] ?? '', 10) : NaN;
  return {
    count: Number.isFinite(total) ? total : null,
    traced: percent(summary?.covered, summary?.total),
    verified: percent(summary?.verified, summary?.total),
  };
};

/** Each listed project's figures by id, filled in as they load; none while `ids` is empty. */
export const useProjectFigures = (ids: string[]): Record<string, ProjectFigures> => {
  const [figures, setFigures] = useState<Record<string, ProjectFigures>>({});
  const key = ids.join(',');
  useEffect(() => {
    let live = true;
    setFigures({});
    (async () => {
      for (let i = 0; i < ids.length && live; i += BATCH) {
        const batch = ids.slice(i, i + BATCH);
        const loaded = await Promise.all(batch.map(figuresFor));
        if (!live) return;
        setFigures((prev) => {
          const next = { ...prev };
          batch.forEach((id, n) => {
            next[id] = loaded[n];
          });
          return next;
        });
      }
    })();
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return figures;
};

/** The row's figures: the artifact count beside the name, then the two meters. */
export const ProjectCount: React.FC<{ figures?: ProjectFigures }> = ({ figures }) =>
  figures && figures.count !== null ? <span className="project-count">{figures.count} artifacts</span> : null;

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
