import { useState } from 'react';
import type { SetURLSearchParams } from 'react-router-dom';
import type { CoverageEntry, Link } from '../../api/client';
import { QualityFacts, buildFacts, moduleFigures, statusOf } from '../../utils/artifactFacts';
import { QuickFilter, RequirementsView, viewFromParam } from './ListToolbar';
import type { Artifact } from './shared';

export interface ListViewOptions {
  /** What the search box and the filter panel leave in view. */
  searched: Artifact[];
  /** The live project's or the baseline's artifacts, and their links. */
  artifacts: Artifact[];
  links: Link[];
  quality: Record<string, QualityFacts> | undefined;
  coverage: CoverageEntry[];
  searchParams: URLSearchParams;
  setSearchParams: SetURLSearchParams;
  /** The Table and Tree views are on: ?view=layers opens the Tree. */
  treeViews?: boolean;
}

/**
 * How the list pane shows the project: Table, Tree or Layers (?view=, so a
 * link and a reload keep it), the type tab and a phone's quick filter, which
 * narrow what the search and the filters leave, and the facts every row and
 * the figures above them read.
 */
export function useListView({ searched, artifacts, links, quality, coverage, searchParams, setSearchParams, treeViews }: ListViewOptions) {
  const [typeTab, setTypeTab] = useState<string>('');
  const [quick, setQuick] = useState<QuickFilter>('');

  const view: RequirementsView = viewFromParam(searchParams.get('view'), treeViews);
  const setView = (next: RequirementsView) => {
    setSearchParams(
      (prev) => {
        const out = new URLSearchParams(prev);
        if (next === 'table') out.delete('view');
        else out.set('view', next);
        return out;
      },
      { replace: true }
    );
  };

  const facts = buildFacts(artifacts, links, quality, coverage);
  const quickMatch = (artifact: Artifact, which: QuickFilter): boolean =>
    which === ''
      ? true
      : which === 'in_review'
        ? statusOf(artifact) === 'in_review'
        : which === 'failing'
          ? facts.get(artifact.id).vv.status === 'fail'
          : facts.get(artifact.id).suspect > 0;
  const filteredArtifacts = searched.filter(
    (artifact) => (!typeTab || artifact.type === typeTab) && quickMatch(artifact, quick)
  );
  const typeCounts = Array.from(
    searched.reduce((m, a) => m.set(a.type, (m.get(a.type) || 0) + 1), new Map<string, number>())
  )
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count);
  const inView = new Set(filteredArtifacts.map((a) => a.id));
  const figures = moduleFigures(
    filteredArtifacts,
    links.filter((l) => inView.has(l.from_id) || inView.has(l.to_id)),
    quality,
    coverage
  );
  const quickCounts = {
    in_review: searched.filter((a) => quickMatch(a, 'in_review')).length,
    failing: searched.filter((a) => quickMatch(a, 'failing')).length,
    suspect: searched.filter((a) => quickMatch(a, 'suspect')).length,
  };

  return { view, setView, typeTab, setTypeTab, quick, setQuick, facts, filteredArtifacts, typeCounts, figures, quickCounts };
}
