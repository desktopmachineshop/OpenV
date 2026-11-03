import React from 'react';
import type { DropZone } from '../utils/artifactDrag';
import type { ArtifactTreeNode } from '../utils/artifactSequence';
import { FactsIndex, QualityFacts, ago, priorityOf } from '../utils/artifactFacts';
import { QualityChip, RefText, StatusChip, SuspectMark, TraceCell, VVCell } from './artifactCells';

/** The tree's columns beside the title, the same as the table's. */
export const TREE_COLUMNS = 'minmax(0, 1fr) 96px 60px 64px 76px 110px 48px 32px';

const INDENT = 18;

type DragHandlers = Pick<
  React.HTMLAttributes<HTMLDivElement>,
  'onDragStart' | 'onDragOver' | 'onDragLeave' | 'onDrop' | 'onDragEnd'
>;

interface ArtifactTreeRowProps {
  node: ArtifactTreeNode;
  depth: number;
  /** For each level above this row: whether a guide line runs on past it. */
  guides: boolean[];
  collapsed: boolean;
  descendants: number;
  selected: boolean;
  drop: DropZone | null;
  dragging: boolean;
  readOnly: boolean;
  /** A touch screen: the caret and the ⋯ button get a fingertip's room. */
  touch: boolean;
  /** Draw the status, trace and verification columns (a wide pane). */
  columns: boolean;
  facts?: FactsIndex;
  quality?: QualityFacts;
  onSelect: () => void;
  onToggle: () => void;
  onKeyDown: (event: React.KeyboardEvent<HTMLDivElement>) => void;
  onMenu: (x: number, y: number) => void;
  dragHandlers: DragHandlers;
}

// One row of the requirements tree: indentation with a guide line per level,
// the expand caret, the reference and the title, and — in a wide pane — the
// same columns as the table. One ⋯ button holds every action, moving up and
// down included; a right click opens the same menu, and a drag still moves
// the artifact before, after or inside another.
export const ArtifactTreeRow: React.FC<ArtifactTreeRowProps> = ({
  node,
  depth,
  guides,
  collapsed,
  descendants,
  selected,
  drop,
  dragging,
  readOnly,
  touch,
  columns,
  facts,
  quality,
  onSelect,
  onToggle,
  onKeyDown,
  onMenu,
  dragHandlers,
}) => {
  const { artifact } = node;
  const hasChildren = node.children.length > 0;
  const rowFacts = facts?.get(artifact.id);
  const parent = hasChildren;

  const caret = hasChildren ? (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation();
        onToggle();
      }}
      aria-label={collapsed ? 'Expand children' : 'Collapse children'}
      title={collapsed ? `Expand (${descendants} below)` : 'Collapse'}
      style={{
        flex: 'none',
        width: touch ? 32 : 24,
        height: touch ? 40 : 26,
        margin: touch ? 0 : '0 -3px',
        padding: 0,
        border: 'none',
        borderRadius: 4,
        background: 'transparent',
        color: 'var(--text-secondary)',
        cursor: 'pointer',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <svg
        width="12"
        height="12"
        viewBox="0 0 20 20"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        style={{ transform: collapsed ? 'rotate(-90deg)' : undefined }}
      >
        <path d="M6 8l4 4 4-4" />
      </svg>
    </button>
  ) : (
    <span aria-hidden="true" style={{ flex: 'none', width: INDENT }} />
  );

  const title = (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0, height: '100%' }}>
      {guides.map((runsOn, i) => (
        <span
          key={i}
          aria-hidden="true"
          style={{
            flex: 'none',
            alignSelf: 'stretch',
            width: INDENT,
            backgroundImage: runsOn || i === guides.length - 1 ? 'linear-gradient(var(--border), var(--border))' : undefined,
            backgroundSize: '1px 100%',
            backgroundRepeat: 'no-repeat',
            backgroundPosition: 'center',
          }}
        />
      ))}
      {caret}
      {/* A section shows where it sits ("1.2") beside its ref; everything
          else shows the address a reader cites ("REQ-12"). */}
      {artifact.doc_number && (
        <span style={{ flex: 'none', color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums', fontSize: 12 }}>
          {artifact.doc_number}
        </span>
      )}
      {artifact.ref && <RefText artifact={{ ref: artifact.ref }} strong={parent} style={{ flex: 'none' }} />}
      <span
        style={{
          minWidth: 0,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          color: 'var(--text)',
          fontSize: 13,
          fontWeight: parent ? 600 : 400,
        }}
      >
        {artifact.title}
      </span>
      {hasChildren && (
        <span style={{ flex: 'none', color: 'var(--text-muted)', fontSize: 12 }} title={`${descendants} below`}>
          {collapsed ? descendants : node.children.length}
        </span>
      )}
      {!columns && rowFacts && rowFacts.suspect > 0 && <SuspectMark count={rowFacts.suspect} />}
    </span>
  );

  const menuButton = !readOnly ? (
    <button
      type="button"
      aria-label={`Actions for ${artifact.title}`}
      title="Actions: create, move up or down, move to, copy (or right-click the row)"
      data-help-key="requirements.create"
      onClick={(event) => {
        event.stopPropagation();
        const box = event.currentTarget.getBoundingClientRect();
        onMenu(Math.max(8, box.right - 200), box.bottom + 4);
      }}
      style={{
        width: touch ? 44 : 28,
        height: touch ? 44 : 28,
        border: '1px solid transparent',
        borderRadius: 4,
        background: 'transparent',
        color: 'var(--text-muted)',
        fontSize: 16,
        lineHeight: 1,
        cursor: 'pointer',
        padding: 0,
        justifySelf: 'end',
      }}
    >
      ⋯
    </button>
  ) : (
    <span />
  );

  return (
    <div
      role="treeitem"
      aria-level={depth + 1}
      aria-selected={selected}
      aria-expanded={hasChildren ? !collapsed : undefined}
      tabIndex={0}
      className="ov-row"
      onClick={onSelect}
      onKeyDown={onKeyDown}
      onContextMenu={(event) => {
        event.preventDefault();
        if (!readOnly) onMenu(event.clientX, event.clientY);
      }}
      draggable={!readOnly}
      {...dragHandlers}
      style={{
        display: 'grid',
        gridTemplateColumns: columns ? TREE_COLUMNS : 'minmax(0, 1fr) auto auto',
        alignItems: 'center',
        columnGap: columns ? 0 : 8,
        minHeight: columns ? 32 : 40,
        padding: '0 8px 0 12px',
        borderBottom: '1px solid var(--border-soft)',
        cursor: 'pointer',
        backgroundColor: selected || drop === 'child' ? 'var(--tint-blue)' : parent ? 'var(--surface-alt)' : 'var(--surface)',
        // A sibling drop shows a line on the edge it would land against; a
        // child drop outlines the row it would go inside.
        boxShadow: drop === 'before' ? 'inset 0 2px 0 0 var(--accent)' : drop === 'after' ? 'inset 0 -2px 0 0 var(--accent)' : 'none',
        outline: drop === 'child' ? '2px solid var(--accent)' : undefined,
        outlineOffset: drop === 'child' ? '-2px' : undefined,
        opacity: dragging ? 0.6 : 1,
      }}
    >
      {title}
      {columns && rowFacts ? (
        <>
          <span>
            <StatusChip artifact={artifact} />
          </span>
          <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{priorityOf(artifact)}</span>
          <span>
            <QualityChip quality={quality} />
          </span>
          <TraceCell facts={rowFacts} />
          <VVCell vv={rowFacts.vv} />
          <span style={{ textAlign: 'right', fontSize: 12, color: 'var(--text-muted)' }}>{ago(artifact.updated_at)}</span>
        </>
      ) : (
        <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}>
          {facts && <StatusChip artifact={artifact} />}
          <QualityChip quality={quality} />
        </span>
      )}
      {menuButton}
    </div>
  );
};
