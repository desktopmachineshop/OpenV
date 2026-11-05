import React from 'react';
import { buttonStyle } from '../../components/ui';
import { typeLabel } from '../../utils/artifactFacts';
import type { Setter } from './shared';
import type { RequirementsView } from './requirementsViews';

export { viewFromParam } from './requirementsViews';
export type { RequirementsView } from './requirementsViews';
export type QuickFilter = '' | 'in_review' | 'failing' | 'suspect';

interface ListToolbarProps {
  phone: boolean;
  view: RequirementsView;
  setView: (view: RequirementsView) => void;
  /** Artifact counts per type over the search and filters, for the type tabs. */
  typeCounts: Array<{ type: string; count: number }>;
  total: number;
  typeTab: string;
  setTypeTab: Setter<string>;
  quick: QuickFilter;
  setQuick: Setter<QuickFilter>;
  quickCounts: Record<Exclude<QuickFilter, ''>, number>;
  searchText: string;
  setSearchText: Setter<string>;
  filterSummary: string;
  filtersActive: boolean;
  clearFilters: () => void;
  showFilterPanel: boolean;
  setShowFilterPanel: Setter<boolean>;
  pickDepth: (depth: number) => void;
  /** The Table and Tree views (REQUIREMENTS_TREE_FEATURE): Table | Tree, and fold all for the depth picker. */
  treeViews?: boolean;
  /** Whether the view in front is folded, which the fold-all button turns the other way. */
  folded?: boolean;
  toggleFold?: () => void;
  /** The create button, which a phone shows beside the search box. */
  newButton?: React.ReactNode;
}

const segment = (on: boolean, first: boolean, phone: boolean): React.CSSProperties => ({
  minHeight: phone ? 36 : 26,
  padding: phone ? '0 12px' : '0 9px',
  border: 'none',
  borderLeft: first ? 'none' : '1px solid var(--border)',
  background: on ? 'var(--neutral-soft)' : 'var(--surface)',
  color: on ? 'var(--text)' : 'var(--text-muted)',
  fontSize: phone ? 13 : 12,
  fontWeight: on ? 600 : 500,
  cursor: 'pointer',
});

const pill = (on: boolean): React.CSSProperties => ({
  flex: 'none',
  minHeight: 36,
  padding: '0 12px',
  borderRadius: 999,
  border: on ? '1px solid var(--text)' : '1px solid var(--border)',
  background: on ? 'var(--text)' : 'var(--surface)',
  color: on ? 'var(--surface)' : 'var(--text-secondary)',
  fontSize: 13,
  fontWeight: on ? 600 : 500,
  cursor: 'pointer',
  whiteSpace: 'nowrap',
});

const VIEWS: Array<{ view: RequirementsView; label: string; phoneLabel: string }> = [
  { view: 'table', label: 'Table', phoneLabel: 'List' },
  { view: 'tree', label: 'Tree', phoneLabel: 'Outline' },
  { view: 'layers', label: 'Layers', phoneLabel: 'By level' },
];
// With the Table and Tree views: the table holds the hierarchy, the tree is
// the diagram, and a phone keeps its list and its outline.
const TREE_VIEWS = VIEWS.slice(0, 2);

/** Fold all / unfold all: one icon button that turns the view in front the other way. */
const FoldAllButton: React.FC<{ folded: boolean; onClick: () => void; phone: boolean }> = ({ folded, onClick, phone }) => {
  const label = folded ? 'Unfold all' : 'Fold all';
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={folded ? 'Unfold all: open every level' : 'Fold all: show the top level only'}
      data-help-key="requirements.navigate"
      style={{ ...buttonStyle('secondary', { compact: !phone }), minHeight: phone ? 36 : 26, padding: phone ? '0 10px' : '0 7px', flex: 'none' }}
    >
      <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {folded ? <path d="M6 7l4-4 4 4M6 13l4 4 4-4" /> : <path d="M6 3l4 4 4-4M6 17l4-4 4 4" />}
      </svg>
    </button>
  );
};

// The bar over the list: the type tabs, the project search and its filters,
// the tree's depth, and the Table / Tree / Layers switch (with the Table and
// Tree views, Table / Tree and a fold-all button in place of the depth). A phone gets the
// search with the create button, then one scrolling row of chips: the views
// (List, Outline, By level) and the quick filters. The phone calls the tree
// its outline: its Tree pane holds every view of the list.
export const ListToolbar: React.FC<ListToolbarProps> = (p) => {
  const search = (
    <div style={{ position: 'relative', flex: p.phone ? 1 : '0 1 240px', minWidth: 140 }} data-help-key="requirements.search">
      <input
        type="text"
        aria-label="Search this project"
        value={p.searchText}
        onChange={(e) => p.setSearchText(e.target.value)}
        placeholder="Search..."
        style={{
          width: '100%',
          boxSizing: 'border-box',
          height: p.phone ? 40 : 30,
          padding: p.phone ? '0 12px' : '0 10px',
          border: '1px solid var(--border)',
          borderRadius: 6,
          background: 'var(--surface-alt)',
          color: 'var(--text)',
          fontSize: p.phone ? 16 : 13,
        }}
      />
    </div>
  );

  const filterButton = (
    <button
      type="button"
      onClick={() => p.setShowFilterPanel((prev) => !prev)}
      aria-expanded={p.showFilterPanel}
      title={`Toggle filters — ${p.filterSummary}`}
      data-help-key="requirements.search"
      style={{
        ...buttonStyle('secondary', { compact: !p.phone }),
        border: '1px dashed var(--neutral-mid)',
        minHeight: p.phone ? 40 : 26,
        flex: 'none',
      }}
    >
      <svg width="13" height="13" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
        <path d="M3 5h14M6 10h8M8 15h4" />
      </svg>
      Filter
    </button>
  );

  const activeChip = p.filtersActive ? (
    <button
      type="button"
      onClick={p.clearFilters}
      title={`${p.filterSummary} — clear`}
      aria-label={`Clear filters: ${p.filterSummary}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        maxWidth: p.phone ? '100%' : 240,
        minHeight: p.phone ? 36 : 26,
        padding: '0 8px',
        border: '1px solid var(--tint-blue-border)',
        borderRadius: 6,
        background: 'var(--tint-blue)',
        color: 'var(--accent-text)',
        fontSize: 12,
        fontWeight: 600,
        cursor: 'pointer',
      }}
    >
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.filterSummary}</span>
      <span aria-hidden="true">×</span>
    </button>
  ) : null;

  const views = p.treeViews ? TREE_VIEWS : VIEWS;
  // A phone's table is a flat list, so only its outline folds.
  const foldAll =
    p.treeViews && p.toggleFold && (p.phone ? p.view === 'tree' : true) ? (
      <FoldAllButton folded={!!p.folded} onClick={p.toggleFold} phone={p.phone} />
    ) : null;
  const depth = p.treeViews ? foldAll : p.view === 'tree' && (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }} data-help-key="requirements.navigate">
      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Depth</span>
      <span role="group" aria-label="Show down to level" style={{ display: 'flex', border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' }}>
        {[1, 2, 3, 4].map((d) => (
          <button
            key={d}
            type="button"
            onClick={() => p.pickDepth(d)}
            aria-label={d === 1 ? 'L1, collapse all' : undefined}
            title={d === 1 ? 'Collapse all: show the top level only' : `Show ${d} levels`}
            style={segment(false, d === 1, p.phone)}
          >
            L{d}
          </button>
        ))}
        <button type="button" onClick={() => p.pickDepth(Infinity)} aria-label="Expand all" title="Expand all" style={segment(false, false, p.phone)}>
          All
        </button>
      </span>
    </span>
  );

  if (p.phone) {
    const quick: Array<{ key: Exclude<QuickFilter, ''>; label: string }> = [
      { key: 'in_review', label: 'In review' },
      { key: 'failing', label: 'Failing' },
      { key: 'suspect', label: 'Suspect' },
    ];
    return (
      <div style={{ flexShrink: 0, background: 'var(--surface)', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', padding: '8px 12px 0' }}>
          {search}
          {filterButton}
          {p.newButton}
        </div>
        <div role="group" aria-label="Views and quick filters" style={{ display: 'flex', gap: 6, padding: '8px 12px', overflowX: 'auto' }} data-help-key="requirements.views">
          {views.map(({ view, phoneLabel }) => (
            <button key={view} type="button" aria-pressed={p.view === view} onClick={() => p.setView(view)} style={pill(p.view === view)}>
              {phoneLabel}
              {view === 'table' ? ` ${p.total}` : ''}
            </button>
          ))}
          <span aria-hidden="true" style={{ flex: 'none', width: 1, background: 'var(--border)', margin: '4px 2px' }} />
          {quick.map(({ key, label }) => (
            <button
              key={key}
              type="button"
              aria-pressed={p.quick === key}
              onClick={() => p.setQuick((prev) => (prev === key ? '' : key))}
              style={{ ...pill(false), ...(p.quick === key ? { background: 'var(--tint-blue)', borderColor: 'var(--tint-blue-border)', color: 'var(--accent-text)', fontWeight: 600 } : {}) }}
            >
              {label} {p.quickCounts[key]}
            </button>
          ))}
        </div>
        {(depth || activeChip) && (
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', padding: '0 12px 8px' }}>
            {depth}
            {activeChip}
          </div>
        )}
      </div>
    );
  }

  return (
    <div
      style={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: '4px 16px',
        padding: '0 20px',
        background: 'var(--surface)',
        borderBottom: '1px solid var(--border)',
        flexShrink: 0,
      }}
    >
      <div role="tablist" aria-label="Artifact type" style={{ display: 'flex', gap: 2, flexWrap: 'wrap' }} data-help-key="requirements.views">
        {[{ type: '', count: p.total }, ...p.typeCounts].map(({ type, count }) => {
          const on = p.typeTab === type;
          return (
            <button
              key={type || 'all'}
              type="button"
              role="tab"
              aria-selected={on}
              onClick={() => p.setTypeTab(type)}
              style={{
                height: 38,
                padding: '0 10px',
                border: 'none',
                borderBottom: `2px solid ${on ? 'var(--accent)' : 'transparent'}`,
                background: 'transparent',
                color: on ? 'var(--text)' : 'var(--text-muted)',
                fontSize: 13,
                fontWeight: on ? 600 : 500,
                cursor: 'pointer',
                whiteSpace: 'nowrap',
              }}
            >
              {type ? typeLabel(type) : 'All'} <span style={{ color: 'var(--text-muted)', fontWeight: 500 }}>{count}</span>
            </button>
          );
        })}
      </div>
      <div style={{ flexGrow: 1 }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 0', flex: '0 1 auto', minWidth: 0 }}>
        {search}
        {filterButton}
        {activeChip}
        {depth}
        <span aria-hidden="true" style={{ width: 1, height: 18, background: 'var(--border)', margin: '0 4px' }} />
        <div role="group" aria-label="View" style={{ display: 'flex', flex: 'none', border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' }} data-help-key="requirements.views">
          {views.map(({ view, label }, i) => (
            <button key={view} type="button" aria-pressed={p.view === view} onClick={() => p.setView(view)} style={segment(p.view === view, i === 0, false)}>
              {label}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};
