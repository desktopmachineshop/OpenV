import React, { useEffect, useMemo, useRef } from 'react';
import { Link } from 'react-router-dom';
import type { Artifact } from '../api/client';
import type { ImpactTreeLayout, TreeNode } from './ImpactViewLayout';

// Box and spacing of the tree, in px.
const NODE_W = 200;
const NODE_H = 52;
const ROW_H = 64;
const COL_GAP = 64;
const COL_W = NODE_W + COL_GAP;
const PAD_Y = 8;

// Each link type has a colour, used for the line that reaches a node and the
// edge of the node it enters, so a dependency's kind reads at a glance.
export const LINK_COLOURS: Record<string, string> = {
  'derives-from': 'var(--purple)',
  'decomposes-to': 'var(--warning)',
  refines: 'var(--warning-strong)',
  satisfies: 'var(--accent)',
  verifies: 'var(--success)',
  validates: 'var(--success-strong)',
  mitigates: 'var(--danger)',
  impacts: 'var(--danger-strong)',
  'relates-to': 'var(--neutral)',
};
const linkColour = (via: string) => LINK_COLOURS[via] || 'var(--neutral)';

const LINK_ORDER = Object.keys(LINK_COLOURS);

interface Props {
  layout: ImpactTreeLayout;
  seed: { id: string; title: string; ref?: string };
  projectId: string;
  byId: Record<string, Artifact>;
  typeLabel: (type: string) => string;
}

export const ImpactViewTree: React.FC<Props> = ({ layout, seed, projectId, byId, typeLabel }) => {
  const scroller = useRef<HTMLDivElement>(null);
  const { upstreamDepth, downstreamDepth, rows, seedRow, nodes } = layout;

  // Column 0 is the furthest upstream; the seed sits at column upstreamDepth.
  const colX = (side: TreeNode['side'] | 'seed', depth: number) => {
    const col =
      side === 'upstream' ? upstreamDepth - depth : side === 'downstream' ? upstreamDepth + depth : upstreamDepth;
    return col * COL_W;
  };
  const rowY = (row: number) => PAD_Y + row * ROW_H;
  const width = (upstreamDepth + downstreamDepth + 1) * COL_W - COL_GAP;
  const height = rows * ROW_H + PAD_Y * 2 - (ROW_H - NODE_H);

  const pos = useMemo(() => {
    const m: Record<string, { x: number; y: number }> = {
      [seed.id]: { x: colX('seed', 0), y: rowY(seedRow) },
    };
    nodes.forEach((n) => {
      // A node can be both up- and downstream (relates-to); key by side.
      m[`${n.side}:${n.id}`] = { x: colX(n.side, n.depth), y: rowY(n.row) };
    });
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout, seed.id]);

  // Open with the selected requirement in view, which on a phone means
  // scrolling past the upstream columns.
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    el.scrollLeft = Math.max(0, colX('seed', 0) - (el.clientWidth - NODE_W) / 2);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout]);

  const parentPos = (n: TreeNode) => (n.parentId === seed.id ? pos[seed.id] : pos[`${n.side}:${n.parentId}`]);

  const edges = nodes.map((n) => {
    const from = parentPos(n);
    const to = pos[`${n.side}:${n.id}`];
    if (!from || !to) return null;
    const down = n.side === 'downstream';
    const x1 = down ? from.x + NODE_W : from.x;
    const x2 = down ? to.x : to.x + NODE_W;
    const y1 = from.y + NODE_H / 2;
    const y2 = to.y + NODE_H / 2;
    const mid = (x1 + x2) / 2;
    return (
      <path
        key={`${n.side}:${n.id}`}
        d={`M ${x1} ${y1} C ${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`}
        fill="none"
        stroke={linkColour(n.via)}
        strokeWidth={2}
      />
    );
  });

  const usedLinks = LINK_ORDER.filter((t) => nodes.some((n) => n.via === t)).concat(
    Array.from(new Set(nodes.map((n) => n.via))).filter((t) => t && !LINK_COLOURS[t])
  );

  const box = (n: TreeNode) => {
    const p = pos[`${n.side}:${n.id}`];
    const colour = linkColour(n.via);
    const ref = byId[n.id]?.ref;
    return (
      <Link
        key={`${n.side}:${n.id}`}
        to={`/projects/${projectId}/requirements?artifact=${n.id}`}
        title={`${ref ? ref + ' ' : ''}${n.title} — ${typeLabel(n.type)}, ${n.via}, ${n.depth} hop${n.depth === 1 ? '' : 's'}`}
        style={{
          ...nodeStyle,
          left: p.x,
          top: p.y,
          [n.side === 'downstream' ? 'borderLeft' : 'borderRight']: `4px solid ${colour}`,
        }}
      >
        <span style={metaStyle}>
          {ref && <span style={refStyle}>{ref}</span>}
          <span>{typeLabel(n.type)}</span>
        </span>
        <span style={titleStyle}>{n.title}</span>
      </Link>
    );
  };

  const seedPos = pos[seed.id];
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>
        <span>← Upstream: what it depends on</span>
        <span style={{ textAlign: 'right' }}>Downstream: what depends on it →</span>
      </div>
      <div
        ref={scroller}
        data-testid="impact-tree"
        style={{ overflow: 'auto', border: '1px solid var(--border)', borderRadius: 6, background: 'var(--surface-alt)', padding: 12 }}
      >
        <div style={{ position: 'relative', width, height, margin: '0 auto' }}>
          <svg width={width} height={height} style={{ position: 'absolute', inset: 0 }} aria-hidden="true">
            {edges}
          </svg>
          <div
            style={{
              ...nodeStyle,
              left: seedPos.x,
              top: seedPos.y,
              background: 'var(--accent)',
              borderColor: 'var(--accent-strong)',
              color: 'var(--accent-fg)',
            }}
          >
            <span style={{ ...metaStyle, color: 'var(--accent-fg)' }}>
              {seed.ref && <span style={{ ...refStyle, color: 'var(--accent-fg)' }}>{seed.ref}</span>}
              <span>Selected requirement</span>
            </span>
            <span style={{ ...titleStyle, fontWeight: 600 }}>{seed.title}</span>
          </div>
          {nodes.map(box)}
        </div>
      </div>
      {usedLinks.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 16px', marginTop: 10, fontSize: 12, color: 'var(--text-muted)' }}>
          {usedLinks.map((t) => (
            <span key={t} style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              <span style={{ width: 18, height: 3, borderRadius: 2, background: linkColour(t) }} />
              {t}
            </span>
          ))}
        </div>
      )}
    </div>
  );
};

const nodeStyle: React.CSSProperties = {
  position: 'absolute',
  width: NODE_W,
  height: NODE_H,
  boxSizing: 'border-box',
  padding: '5px 9px',
  display: 'flex',
  flexDirection: 'column',
  justifyContent: 'center',
  gap: 2,
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  color: 'var(--text)',
  textDecoration: 'none',
  fontSize: 13,
  overflow: 'hidden',
};
const metaStyle: React.CSSProperties = { display: 'flex', gap: 6, fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' };
const refStyle: React.CSSProperties = {
  fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace',
  color: 'var(--accent-text)',
};
const titleStyle: React.CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };
