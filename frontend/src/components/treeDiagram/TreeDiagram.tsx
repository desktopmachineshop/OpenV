import React, { useEffect, useMemo, useRef, useState } from 'react';
import { DEFAULT_METRICS, DiagramMetrics, DiagramMove, DiagramNode, DiagramShape, PlacedNode, layoutDiagram, moveFocus } from './layout';
import { usePanZoom } from './usePanZoom';

// A tree drawn as boxes and elbow connectors, either flowing left to right
// from a forest of roots or branching both ways from one root in the middle
// (layout.ts). The caller owns what is folded, what is selected and what a
// box shows; the diagram owns the layout, the pan and zoom, the connectors
// with their ⊖ / ⊕ folds, and the keyboard.
//
// The boxes are a role="tree" of treeitems with aria-level, aria-posinset
// and aria-setsize, so the arrow keys and a screen reader walk the tree as
// they would a list-style one. The connectors and fold buttons are drawn for
// the eye and the mouse; the keyboard folds with ← and →. Boxes outside the
// viewport are not rendered (the focused and selected ones always are).

export interface TreeDiagramProps<T> {
  shape: DiagramShape<T>;
  folded: ReadonlySet<string>;
  onToggle: (key: string) => void;
  selectedKey?: string | null;
  onSelect?: (node: DiagramNode<T>) => void;
  /** What a box holds. */
  renderBox: (node: DiagramNode<T>, state: { selected: boolean }) => React.ReactNode;
  /** The box's frame beyond the defaults: its fill and border colour. */
  boxStyle?: (node: DiagramNode<T>) => React.CSSProperties;
  /** The box's accessible name. */
  labelOf: (node: DiagramNode<T>) => string;
  ariaLabel: string;
  metrics?: Partial<DiagramMetrics>;
  /** More controls for the bar over the diagram, before the zoom buttons. */
  controls?: React.ReactNode;
  /** Shown instead of the diagram when there is nothing to draw. */
  empty?: React.ReactNode;
}

const zoomButton: React.CSSProperties = {
  minWidth: 28,
  height: 28,
  padding: '0 8px',
  border: '1px solid var(--border)',
  borderRadius: 6,
  background: 'var(--surface)',
  color: 'var(--text-secondary)',
  fontSize: 13,
  cursor: 'pointer',
};

const KEY_MOVES: Record<string, DiagramMove> = { ArrowUp: 'up', ArrowDown: 'down', Home: 'first', End: 'last' };

export function TreeDiagram<T>(p: TreeDiagramProps<T>): React.ReactElement {
  const m = useMemo(() => ({ ...DEFAULT_METRICS, ...p.metrics }), [p.metrics]);
  const layout = useMemo(() => layoutDiagram(p.shape, p.folded, m), [p.shape, p.folded, m]);
  const pz = usePanZoom(layout.width, layout.height);
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const boxes = useRef(new Map<string, HTMLDivElement>());
  const wantFocus = useRef(false);
  const { reveal } = pz;

  const rectOf = (n: PlacedNode<T>) => ({ x: n.x, y: n.y, w: m.boxWidth, h: m.boxHeight });

  // A selection made anywhere (the list, a link, ‹ / ›) brings its box into view.
  const selected = p.selectedKey ? layout.byKey.get(p.selectedKey) : undefined;
  useEffect(() => {
    if (selected) reveal({ x: selected.x, y: selected.y, w: m.boxWidth, h: m.boxHeight });
    // Only a new selection pans; a re-layout does not drag the view back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.selectedKey, !!selected]);

  // The focus moves after the render that drew its box.
  useEffect(() => {
    if (!wantFocus.current || !focusKey) return;
    wantFocus.current = false;
    boxes.current.get(focusKey)?.focus({ preventScroll: true });
  });

  const tabKey = focusKey && layout.byKey.has(focusKey) ? focusKey : selected ? selected.key : (layout.nodes[0]?.key ?? null);

  const focusOn = (key: string | null) => {
    if (!key) return;
    const n = layout.byKey.get(key);
    if (n) reveal(rectOf(n));
    wantFocus.current = true;
    setFocusKey(key);
  };

  const onKey = (e: React.KeyboardEvent, n: PlacedNode<T>) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      p.onSelect?.(n.node);
      return;
    }
    // On the left of a two-sided tree, children lie to the left.
    const inKey = n.side === 'left' ? 'ArrowLeft' : 'ArrowRight';
    const outKey = n.side === 'left' ? 'ArrowRight' : 'ArrowLeft';
    let move: DiagramMove | undefined = KEY_MOVES[e.key];
    if (e.key === inKey) {
      if (n.folded) {
        e.preventDefault();
        p.onToggle(n.key);
        return;
      }
      move = 'in';
    } else if (e.key === outKey) {
      if (n.hasChildren && !n.folded && n.side !== 'root') {
        e.preventDefault();
        p.onToggle(n.key);
        return;
      }
      move = 'out';
    }
    if (!move) return;
    e.preventDefault();
    focusOn(moveFocus(layout, n.key, move));
  };

  if (layout.nodes.length === 0) return <>{p.empty ?? null}</>;

  // Culling: what lies within a screen's margin of the viewport. With no
  // measured viewport (a first render, a test) everything is drawn.
  const { view, size } = pz;
  const margin = 200;
  const vis = size.w
    ? { x: (-view.x - margin) / view.k, y: (-view.y - margin) / view.k, w: (size.w + margin * 2) / view.k, h: (size.h + margin * 2) / view.k }
    : null;
  const inView = (r: { x: number; y: number; w: number; h: number }) =>
    !vis || (r.x + r.w >= vis.x && r.x <= vis.x + vis.w && r.y + r.h >= vis.y && r.y <= vis.y + vis.h);
  const drawn = layout.nodes.filter((n) => inView(rectOf(n)) || n.key === tabKey || n.key === p.selectedKey);
  const edges = layout.edges.filter((e) => inView(e.box));

  return (
    <div style={{ position: 'relative', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', padding: '0 0 8px' }}>
        {p.controls}
        <span style={{ flex: 1 }} />
        <button type="button" style={zoomButton} onClick={() => pz.zoom(1 / 1.25)} aria-label="Zoom out" title="Zoom out">
          −
        </button>
        <span aria-live="polite" style={{ fontSize: 12, color: 'var(--text-muted)', minWidth: 40, textAlign: 'center' }}>
          {Math.round(view.k * 100)}%
        </span>
        <button type="button" style={zoomButton} onClick={() => pz.zoom(1.25)} aria-label="Zoom in" title="Zoom in">
          +
        </button>
        <button type="button" style={zoomButton} onClick={pz.fit} title="Fit the whole diagram on screen">
          Fit
        </button>
      </div>
      <div
        ref={pz.viewport}
        {...pz.handlers}
        style={{
          position: 'relative',
          flex: 1,
          minHeight: 240,
          overflow: 'hidden',
          background: 'var(--surface-inset)',
          border: '1px solid var(--border)',
          borderRadius: 6,
          cursor: 'grab',
          touchAction: 'none',
          // A drag pans: it must not select the boxes' text on its way.
          userSelect: 'none',
          WebkitUserSelect: 'none',
        }}
      >
        <div
          style={{
            position: 'absolute',
            left: 0,
            top: 0,
            width: layout.width,
            height: layout.height,
            transform: `translate(${view.x}px, ${view.y}px) scale(${view.k})`,
            transformOrigin: '0 0',
          }}
        >
          <svg width={layout.width} height={layout.height} aria-hidden="true" style={{ position: 'absolute', left: 0, top: 0, overflow: 'visible' }}>
            {edges.map((e) => (
              <g key={e.key}>
                <path
                  d={e.path}
                  fill="none"
                  stroke={e.style?.color ?? 'var(--neutral-mid)'}
                  strokeWidth={1.5}
                  strokeDasharray={e.style?.dashed ? '4 3' : undefined}
                />
                {e.style?.label && (
                  <text x={e.labelX} y={e.labelY} textAnchor="middle" fontSize={12} fill={e.style.color ?? 'var(--text-muted)'}>
                    {e.style.label}
                  </text>
                )}
              </g>
            ))}
          </svg>
          <div role="tree" aria-label={p.ariaLabel}>
            {drawn.map((n) => {
              const isSelected = n.key === p.selectedKey;
              return (
                <div
                  key={n.key}
                  ref={(el) => {
                    if (el) boxes.current.set(n.key, el);
                    else boxes.current.delete(n.key);
                  }}
                  role="treeitem"
                  aria-level={n.level}
                  aria-posinset={n.posInSet}
                  aria-setsize={n.setSize}
                  aria-expanded={n.hasChildren ? !n.folded : undefined}
                  aria-selected={isSelected}
                  aria-label={p.labelOf(n.node)}
                  tabIndex={n.key === tabKey ? 0 : -1}
                  onClick={() => {
                    setFocusKey(n.key);
                    p.onSelect?.(n.node);
                  }}
                  onKeyDown={(e) => onKey(e, n)}
                  style={{
                    position: 'absolute',
                    left: n.x,
                    top: n.y,
                    width: m.boxWidth,
                    height: m.boxHeight,
                    boxSizing: 'border-box',
                    padding: '6px 8px',
                    border: '1px solid var(--border)',
                    borderRadius: 6,
                    background: 'var(--surface)',
                    overflow: 'hidden',
                    cursor: 'pointer',
                    ...p.boxStyle?.(n.node),
                    ...(isSelected ? { outline: '2px solid var(--accent)', outlineOffset: 1 } : {}),
                  }}
                >
                  {p.renderBox(n.node, { selected: isSelected })}
                </div>
              );
            })}
          </div>
          <div aria-hidden="true">
            {drawn
              .filter((n) => n.hasChildren && n.side !== 'root')
              .map((n) => (
                <button
                  key={n.key}
                  type="button"
                  tabIndex={-1}
                  onClick={() => p.onToggle(n.key)}
                  title={n.folded ? `Unfold (${n.hiddenCount} below)` : 'Fold'}
                  style={{
                    position: 'absolute',
                    left: n.side === 'left' ? n.x - 20 : n.x + m.boxWidth + 2,
                    top: n.y + m.boxHeight / 2 - 9,
                    width: 18,
                    height: 18,
                    padding: 0,
                    border: '1px solid var(--neutral-mid)',
                    borderRadius: 999,
                    background: 'var(--surface)',
                    color: 'var(--text-secondary)',
                    fontSize: 12,
                    lineHeight: '16px',
                    cursor: 'pointer',
                  }}
                >
                  {n.folded ? '+' : '−'}
                </button>
              ))}
            {drawn
              .filter((n) => n.folded && n.side !== 'root')
              .map((n) => (
                <span
                  key={`${n.key}-count`}
                  style={{
                    position: 'absolute',
                    left: n.side === 'left' ? n.x - 20 - 40 : n.x + m.boxWidth + 23,
                    width: 36,
                    top: n.y + m.boxHeight / 2 - 8,
                    textAlign: n.side === 'left' ? 'right' : 'left',
                    fontSize: 12,
                    color: 'var(--text-muted)',
                    pointerEvents: 'none',
                  }}
                >
                  {n.hiddenCount}
                </span>
              ))}
          </div>
        </div>
      </div>
    </div>
  );
}
