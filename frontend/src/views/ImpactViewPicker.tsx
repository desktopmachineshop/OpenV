import React, { useMemo, useState } from 'react';
import type { Artifact } from '../api/client';
import { ArtifactList } from '../components/ArtifactList';
import { ChipSearch } from '../components/search/ChipSearch';
import { EMPTY_SEARCH, SearchSchema, SearchValue, filterItems } from '../components/search/searchValue';
import { useAppStore } from '../state/store';
import { buildFacts } from '../utils/artifactFacts';
import { ancestorIds } from '../utils/artifactSequence';
import { buildListSchema, listAccessors } from './moduleView/listSearch';

// Impact analysis's picker pane (issue #730): the requirements as the
// requirements page's folding Table, cut down to Ref and Title, under the
// chip search bar, so typing REQ-73 or part of a title finds the one to
// start from. The headings they sit under stay as the fold structure; a
// click on a requirement runs its impact, and the picked one is highlighted.

/** What impact analysis starts from. */
export const isPickable = (a: Artifact | undefined): boolean => a?.type === 'requirement';

/** The rows to show: the requirements the search leaves, with every artifact above them. */
export function pickerRows(artifacts: Artifact[], matched: Artifact[]): Artifact[] {
  const keep = new Set<string>();
  matched.forEach((a) => {
    keep.add(a.id);
    ancestorIds(artifacts, a.id).forEach((id) => keep.add(id));
  });
  return artifacts.filter((a) => keep.has(a.id));
}

interface Props {
  artifacts: Artifact[];
  selectedId: string;
  onPick: (id: string) => void;
  /** Folds the pane away; omitted, it offers no ‹ button. */
  onCollapse?: () => void;
  phone?: boolean;
}

const noop = () => undefined;

export const ImpactViewPicker: React.FC<Props> = ({ artifacts, selectedId, onPick, onCollapse, phone }) => {
  const currentUser = useAppStore((s) => s.currentUser);
  const [value, setValue] = useState<SearchValue>(EMPTY_SEARCH);
  const requirements = useMemo(() => artifacts.filter(isPickable), [artifacts]);
  // The list's schema over the requirements, without its grouping and saved
  // searches: a picker narrows, it does not regroup or keep searches.
  const schema: SearchSchema = useMemo(() => {
    const s = buildListSchema(requirements);
    return {
      fields: s.fields.filter((f) => f.key !== 'type'),
      quickFilters: (s.quickFilters || []).filter((q) => q.key === 'mine' || q.key === 'in_review'),
    };
  }, [requirements]);
  const accessors = useMemo(
    () => listAccessors(artifacts, buildFacts(artifacts, [], undefined, []), [currentUser?.name ?? '', currentUser?.email ?? '']),
    [artifacts, currentUser?.name, currentUser?.email]
  );
  const matched = useMemo(() => filterItems(requirements, value, schema, accessors), [requirements, value, schema, accessors]);
  const rows = useMemo(() => pickerRows(artifacts, matched), [artifacts, matched]);
  const byId = useMemo(() => new Map(artifacts.map((a) => [a.id, a])), [artifacts]);

  return (
    <aside
      aria-label="Pick a requirement"
      data-help-key="impact.picker"
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        minHeight: 0,
        ...(phone ? { maxHeight: '50vh' } : { width: 340, flex: 'none', alignSelf: 'stretch' }),
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
          Requirements · {matched.length}
        </span>
        <span style={{ flex: 1 }} />
        {onCollapse && (
          <button
            type="button"
            onClick={onCollapse}
            title="Hide the list to give the tree the width"
            style={{ padding: '2px 8px', fontSize: 12, border: '1px solid var(--border)', borderRadius: 6, background: 'var(--surface)', color: 'var(--text-secondary)', cursor: 'pointer' }}
          >
            {phone ? 'Hide list' : '‹ Hide list'}
          </button>
        )}
      </div>
      <div style={{ display: 'flex' }}>
        <ChipSearch schema={schema} value={value} onChange={setValue} ariaLabel="Search the requirements" placeholder="Search requirements…" phone={phone} helpKey="impact.picker" />
      </div>
      {matched.length === 0 ? (
        <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
          {requirements.length === 0 ? 'This project has no requirements yet.' : 'No requirement matches. Clear the search to see them all.'}
        </p>
      ) : (
        <ArtifactList
          artifacts={rows}
          allArtifacts={artifacts}
          selectedId={selectedId || undefined}
          onSelect={(id) => {
            if (isPickable(byId.get(id))) onPick(id);
          }}
          onReorder={noop}
          readOnly
          hideHeading
          compact
          ariaLabel="Requirements to pick from"
        />
      )}
    </aside>
  );
};
