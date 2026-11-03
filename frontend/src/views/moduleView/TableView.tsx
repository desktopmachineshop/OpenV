import React, { useMemo, useState } from 'react';
import type { DropZone } from '../../utils/artifactDrag';
import { documentOrder } from '../../utils/artifactSequence';
import { FactsIndex, QualityFacts, ago, priorityOf } from '../../utils/artifactFacts';
import { ArtifactActionsMenu, ArtifactContextAction, ArtifactMenuAnchor } from '../../components/ArtifactActionsMenu';
import { QualityChip, RefText, StatusChip, TraceCell, VVCell } from '../../components/artifactCells';
import { PhoneListRow } from './PhoneListRow';
import type { Artifact } from './shared';

/** The table's columns: ref, title, status, priority, quality, trace, V&V, updated, actions. */
export const TABLE_COLUMNS = '96px minmax(0, 1fr) 96px 60px 64px 76px 110px 48px 32px';

export interface TableGroup {
  key: string;
  label: string;
  rows: Artifact[];
}

/**
 * The rows in document order, grouped under the heading each sits in ("2 ·
 * Functional requirements · Spindle"); a heading is a group, not a row. What
 * no heading holds goes first, ungrouped. A list of nothing but headings (a
 * filter for them) shows them as rows rather than as empty groups.
 */
export const groupBySection = (artifacts: Artifact[]): TableGroup[] => {
  const order = documentOrder(artifacts).map((e) => e.artifact);
  const byId = new Map(order.map((a) => [a.id, a]));
  const content = order.filter((a) => a.type !== 'heading');
  if (content.length === 0) return order.length ? [{ key: '', label: '', rows: order }] : [];
  const sectionOf = (a: Artifact): Artifact | undefined => {
    let parent = a.parent_id ? byId.get(a.parent_id) : undefined;
    while (parent && parent.type !== 'heading') parent = parent.parent_id ? byId.get(parent.parent_id) : undefined;
    return parent;
  };
  const labelOf = (heading: Artifact): string => {
    const names: string[] = [];
    for (let h: Artifact | undefined = heading; h; h = h.parent_id ? byId.get(h.parent_id) : undefined) {
      if (h.type === 'heading') names.unshift(h.title);
    }
    return [heading.doc_number, ...names].filter(Boolean).join(' · ');
  };
  const groups: TableGroup[] = [];
  const index = new Map<string, TableGroup>();
  for (const a of content) {
    const section = sectionOf(a);
    const key = section?.id ?? '';
    let group = index.get(key);
    if (!group) {
      group = { key, label: section ? labelOf(section) : '', rows: [] };
      index.set(key, group);
      if (key === '') groups.unshift(group);
      else groups.push(group);
    }
    group.rows.push(a);
  }
  return groups;
};

interface TableViewProps {
  artifacts: Artifact[];
  allArtifacts: Artifact[];
  total: number;
  selectedId: string | null;
  onSelect: (id: string) => void;
  facts: FactsIndex;
  qualityScores?: Record<string, QualityFacts>;
  readOnly: boolean;
  canPaste: boolean;
  onReorder: (sourceId: string, targetId: string, mode: 'swap' | DropZone) => void;
  onContextMenuAction: (action: ArtifactContextAction, artifact: Artifact) => void;
  /** A phone: two-line rows instead of columns. */
  phone: boolean;
}

const headerCell: React.CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };

// The table: one dense row per artifact with its status, priority, quality,
// trace and verification, grouped by section. The tree is the place to
// rearrange the document; every row here still opens the same actions menu.
export const TableView: React.FC<TableViewProps> = ({
  artifacts,
  allArtifacts,
  total,
  selectedId,
  onSelect,
  facts,
  qualityScores,
  readOnly,
  canPaste,
  onReorder,
  onContextMenuAction,
  phone,
}) => {
  const groups = useMemo(() => groupBySection(artifacts), [artifacts]);
  const [closed, setClosed] = useState<Set<string>>(new Set());
  const [menu, setMenu] = useState<ArtifactMenuAnchor | null>(null);
  const shown = groups.reduce((n, g) => n + g.rows.length, 0);
  const toggle = (key: string) =>
    setClosed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const openMenu = (artifact: Artifact) => (event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    if (readOnly) return;
    if (event.type === 'contextmenu') {
      setMenu({ x: event.clientX, y: event.clientY, artifactId: artifact.id });
    } else {
      const box = (event.currentTarget as HTMLElement).getBoundingClientRect();
      setMenu({ x: Math.max(8, box.right - 200), y: box.bottom + 4, artifactId: artifact.id });
    }
  };

  if (artifacts.length === 0) {
    return <p style={{ color: 'var(--text-muted)' }}>No artifacts match. Create one, or clear the search and filters.</p>;
  }

  const groupHeader = (group: TableGroup) =>
    group.label ? (
      <button
        type="button"
        aria-expanded={!closed.has(group.key)}
        onClick={() => toggle(group.key)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          width: '100%',
          minHeight: phone ? 36 : 28,
          padding: phone ? '0 16px' : '0 12px',
          border: 'none',
          borderBottom: '1px solid var(--border-soft)',
          background: 'var(--surface-alt)',
          color: 'var(--text-secondary)',
          fontSize: 12,
          fontWeight: 600,
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ transform: closed.has(group.key) ? 'rotate(-90deg)' : undefined, flex: 'none' }}>
          <path d="M6 8l4 4 4-4" />
        </svg>
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{group.label}</span>
        <span style={{ color: 'var(--text-muted)', fontWeight: 500 }}>{group.rows.length}</span>
      </button>
    ) : null;

  const menuButton = (artifact: Artifact) =>
    readOnly ? (
      <span />
    ) : (
      <button
        type="button"
        aria-label={`Actions for ${artifact.title}`}
        title="Actions: create, move, copy (or right-click the row)"
        onClick={openMenu(artifact)}
        style={{ width: 28, height: 28, padding: 0, border: 'none', borderRadius: 4, background: 'transparent', color: 'var(--text-muted)', fontSize: 16, cursor: 'pointer', justifySelf: 'end' }}
      >
        ⋯
      </button>
    );

  const desktopRow = (a: Artifact) => {
    const f = facts.get(a.id);
    const selected = a.id === selectedId;
    return (
      <div
        key={a.id}
        role="row"
        aria-selected={selected}
        tabIndex={0}
        className="ov-row"
        onClick={() => onSelect(a.id)}
        onKeyDown={(e) => {
          if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
            e.preventDefault();
            onSelect(a.id);
          }
        }}
        onContextMenu={openMenu(a)}
        style={{
          display: 'grid',
          gridTemplateColumns: TABLE_COLUMNS,
          alignItems: 'center',
          minHeight: 34,
          padding: '0 8px 0 12px',
          borderBottom: '1px solid var(--border-soft)',
          background: selected ? 'var(--tint-blue)' : 'var(--surface)',
          cursor: 'pointer',
        }}
      >
        <span role="cell" style={headerCell}>
          <RefText artifact={a} />
        </span>
        <span role="cell" style={{ ...headerCell, paddingRight: 12, color: 'var(--text)', fontSize: 13 }}>
          {a.title}
        </span>
        <span role="cell">
          <StatusChip artifact={a} />
        </span>
        <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
          {priorityOf(a)}
        </span>
        <span role="cell">
          <QualityChip quality={qualityScores?.[a.id]} />
        </span>
        <span role="cell">
          <TraceCell facts={f} />
        </span>
        <span role="cell" style={{ minWidth: 0 }}>
          <VVCell vv={f.vv} />
        </span>
        <span role="cell" style={{ textAlign: 'right', fontSize: 12, color: 'var(--text-muted)' }}>
          {ago(a.updated_at)}
        </span>
        {menuButton(a)}
      </div>
    );
  };

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }} data-help-key="requirements.views">
      <div
        style={{
          flex: 1,
          minHeight: 0,
          overflow: 'auto',
          background: 'var(--surface)',
          border: phone ? 'none' : '1px solid var(--border)',
          borderTop: '1px solid var(--border)',
          borderRadius: phone ? 0 : 6,
        }}
      >
        <div role="table" aria-label="Requirements table" style={{ minWidth: phone ? undefined : 860 }}>
          {!phone && (
            <div
              role="row"
              style={{
                display: 'grid',
                gridTemplateColumns: TABLE_COLUMNS,
                alignItems: 'center',
                height: 30,
                padding: '0 8px 0 12px',
                background: 'var(--surface-inset)',
                borderBottom: '1px solid var(--border)',
                color: 'var(--text-muted)',
                fontSize: 12,
                fontWeight: 600,
                textTransform: 'uppercase',
                letterSpacing: '0.04em',
                position: 'sticky',
                top: 0,
                zIndex: 1,
              }}
            >
              {['Ref', 'Title', 'Status', 'Prio', 'Quality', 'Trace', 'V&V', 'Upd.'].map((h) => (
                <span key={h} role="columnheader" style={{ ...headerCell, textAlign: h === 'Upd.' ? 'right' : undefined }}>
                  {h}
                </span>
              ))}
              <span />
            </div>
          )}
          {groups.map((group) => (
            <div key={group.key || 'ungrouped'} role="rowgroup">
              {groupHeader(group)}
              {!closed.has(group.key) &&
                group.rows.map((a) =>
                  phone ? (
                    <PhoneListRow
                      key={a.id}
                      artifact={a}
                      facts={facts.get(a.id)}
                      quality={qualityScores?.[a.id]}
                      selected={a.id === selectedId}
                      onSelect={() => onSelect(a.id)}
                      onMenu={readOnly ? undefined : openMenu(a)}
                    />
                  ) : (
                    desktopRow(a)
                  )
                )}
            </div>
          ))}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', minHeight: 32, padding: '0 12px', color: 'var(--text-muted)', fontSize: 12 }}>
          <span>
            {shown} of {total}
            {groups.some((g) => g.label) ? ' · grouped by section' : ''}
          </span>
        </div>
      </div>
      {!readOnly && (
        <ArtifactActionsMenu
          menu={menu}
          onClose={() => setMenu(null)}
          allArtifacts={allArtifacts}
          canPaste={canPaste}
          onContextMenuAction={onContextMenuAction}
          onReorder={onReorder}
        />
      )}
    </div>
  );
};
