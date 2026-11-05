import { useEffect, useMemo, useRef, useState } from 'react';
import type { SetURLSearchParams } from 'react-router-dom';
import type { CoverageEntry, Link } from '../../api/client';
import { QualityFacts, buildFacts, moduleFigures, statusOf } from '../../utils/artifactFacts';
import { documentOrder } from '../../utils/artifactSequence';
import { useFeature } from '../../hooks/useFeature';
import { useAppStore } from '../../state/store';
import { CHIP_SEARCH_FEATURE, SearchValue, filterItems, groupItems, groupKeys, readSearchParams, writeSearchParams } from '../../components/search/searchValue';
import { QuickFilter, RequirementsView, viewFromParam } from './ListToolbar';
import { buildListSchema, listAccessors } from './listSearch';
import type { TableGroup } from './TableView';
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
}

/**
 * How the list pane shows the project: Table, Tree or Layers (?view=, so a
 * link and a reload keep it), the type tab and a phone's quick filter, or
 * the chip search bar's chips (also in the URL), which narrow what the search
 * and the filters leave, and the facts every row and the figures above them
 * read.
 */
export function useListView({ searched, artifacts, links, quality, coverage, searchParams, setSearchParams }: ListViewOptions) {
  // The chip search bar (issue #722): its chips, in the URL, narrow and
  // group what the search leaves, in place of the phone's quick filter. The
  // Mine chip matches owners against the member's name and email.
  const chipSearch = useFeature(CHIP_SEARCH_FEATURE);
  const currentUser = useAppStore((s) => s.currentUser);
  const me = [currentUser?.name ?? '', currentUser?.email ?? ''];
  const [typeTab, setTypeTab] = useState<string>('');
  const [quick, setQuick] = useState<QuickFilter>('');

  const view: RequirementsView = viewFromParam(searchParams.get('view'));
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

  // The chip search: its schema over the project's artifacts, its value (held
  // here, so typing never waits on a navigation, and mirrored into the URL so
  // a filtered view can be shared or bookmarked), and what it leaves of the
  // searched rows. The URL is read without the schema: a custom attribute's
  // chip must survive a load that has not brought the artifacts yet.
  const searchSchema = useMemo(() => buildListSchema(artifacts), [artifacts]);
  const fromUrl = writeSearchParams(new URLSearchParams(), readSearchParams(searchParams)).toString();
  const [searchValue, setSearchValueState] = useState<SearchValue>(() => readSearchParams(searchParams));
  const written = useRef(fromUrl);
  useEffect(() => {
    // Back, forward or a link changed the chips: follow the URL.
    if (fromUrl !== written.current) {
      written.current = fromUrl;
      setSearchValueState(readSearchParams(new URLSearchParams(fromUrl)));
    }
  }, [fromUrl]);
  const setSearchValue = (next: SearchValue) => {
    setSearchValueState(next);
    written.current = writeSearchParams(new URLSearchParams(), next).toString();
    setSearchParams((prev) => writeSearchParams(new URLSearchParams(prev), next), { replace: true });
  };
  const accessors = listAccessors(artifacts, facts, me);
  const chipSearched = chipSearch ? filterItems(searched, searchValue, searchSchema, accessors) : searched;

  const quickMatch = (artifact: Artifact, which: QuickFilter): boolean =>
    which === ''
      ? true
      : which === 'in_review'
        ? statusOf(artifact) === 'in_review'
        : which === 'failing'
          ? facts.get(artifact.id).vv.status === 'fail'
          : facts.get(artifact.id).suspect > 0;
  const filteredArtifacts = chipSearched.filter(
    (artifact) => (!typeTab || artifact.type === typeTab) && (chipSearch || quickMatch(artifact, quick))
  );
  const typeCounts = Array.from(
    chipSearched.reduce((m, a) => m.set(a.type, (m.get(a.type) || 0) + 1), new Map<string, number>())
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
  const chipQuickCounts: Record<string, number> = chipSearch
    ? { ...quickCounts, mine: searched.filter((a) => accessors.quickMatch!(a, 'mine')).length }
    : quickCounts;

  // Group by: the rows in document order under one heading per value of the
  // group chips. Undefined with no group chip, and the table then groups by
  // section as it always has.
  // Headings are the document's structure, not rows, as in the section
  // grouping; a list of nothing but headings shows them.
  const keys = chipSearch ? groupKeys(searchValue) : [];
  const ordered = keys.length ? documentOrder(filteredArtifacts).map((e) => e.artifact) : [];
  const content = ordered.some((a) => a.type !== 'heading') ? ordered.filter((a) => a.type !== 'heading') : ordered;
  const tableGroups: TableGroup[] | undefined =
    keys.length > 0
      ? groupItems(
          content,
          keys,
          searchSchema,
          accessors.groupValue
        )
      : undefined;
  const groupedBy = keys.length
    ? keys.map((k) => [...(searchSchema.groupBy ?? []), ...(searchSchema.customGroups ?? [])].find((g) => g.key === k)?.label ?? k).join(' · ')
    : '';

  return {
    view,
    setView,
    typeTab,
    setTypeTab,
    quick,
    setQuick,
    facts,
    filteredArtifacts,
    typeCounts,
    figures,
    quickCounts,
    chipSearch,
    userId: currentUser?.id,
    /** The rows the chip search leaves, before the type tab: the tabs' "All". */
    chipSearched,
    searchSchema,
    searchValue,
    setSearchValue,
    chipQuickCounts,
    tableGroups,
    groupedBy,
  };
}
