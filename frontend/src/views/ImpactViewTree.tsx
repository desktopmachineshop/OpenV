import React, { useEffect, useState } from 'react';
import type { Artifact } from '../api/client';
import { TreeDiagram } from '../components/treeDiagram';
import { refStyle } from '../components/ui';
import { STATUS_META, statusOf } from '../utils/artifactFacts';
import { STATUS_COLOURS, TYPE_COLOURS, type ColourBy } from './moduleView/TreeDiagramView';
import { ImpactBox, ImpactShape, linkColour, usedLinkTypes } from './ImpactViewLayout';

// The impact drawn with the shared tree diagram (components/treeDiagram):
// the selected artifact in the middle, upstream to the left and downstream
// to the right, each connector coloured and labelled by its link type, with
// a key; the boxes coloured by type or status as the Colour by switch says,
// as the requirements Tree is. Folds per box, pan, zoom and fit are the
// diagram's. A click opens the artifact; ⌖ on a box re-centres the impact.

const METRICS = { boxWidth: 220, boxHeight: 60, gapX: 72 };

interface Props {
  impact: ImpactShape;
  byId: Record<string, Artifact>;
  colourBy: ColourBy;
  typeLabel: (type: string) => string;
  onOpen: (id: string) => void;
  /** Re-centre the impact on an artifact; omitted, the boxes offer no ⌖. */
  onCentre?: (id: string) => void;
  controls?: React.ReactNode;
}

const swatch = (b: ImpactBox, a: Artifact | undefined, by: ColourBy) => {
  if (by === 'status') return STATUS_COLOURS[a ? statusOf(a) : 'draft'];
  return TYPE_COLOURS[b.type] ?? { fill: 'var(--surface)', edge: 'var(--neutral)', label: b.type };
};

export const ImpactViewTree: React.FC<Props> = ({ impact, byId, colourBy, typeLabel, onOpen, onCentre, controls }) => {
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const rootKey = impact.shape.kind === 'two-sided' ? impact.shape.root.key : '';
  // A new centre opens unfolded.
  useEffect(() => setFolded(new Set()), [rootKey]);

  const toggle = (key: string) =>
    setFolded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const links = usedLinkTypes(impact.boxes);
  const all = impact.shape.kind === 'two-sided' ? [impact.shape.root.data, ...impact.boxes] : impact.boxes;
  const colours = new Map<string, { fill: string; edge: string; label: string }>();
  all.forEach((b) => {
    const s = swatch(b, byId[b.id], colourBy);
    if (!colours.has(s.label)) colours.set(s.label, s);
  });

  return (
    <div style={{ flex: 1, minHeight: 360, display: 'flex', flexDirection: 'column' }} data-testid="impact-tree">
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>
        <span>← Upstream: what it depends on</span>
        <span style={{ textAlign: 'right' }}>Downstream: what depends on it →</span>
      </div>
      <TreeDiagram<ImpactBox>
        shape={impact.shape}
        folded={folded}
        onToggle={toggle}
        selectedKey={rootKey}
        onSelect={(node) => onOpen(node.data.id)}
        ariaLabel="Impact tree"
        metrics={METRICS}
        controls={controls}
        labelOf={(node) => {
          const b = node.data;
          const a = byId[b.id];
          const where = b.side === 'seed' ? 'centre' : `${b.side}, ${b.via}, ${b.depth} hop${b.depth === 1 ? '' : 's'}`;
          return [a?.ref, b.title, typeLabel(b.type), a ? STATUS_META[statusOf(a)].label : '', where].filter(Boolean).join(', ');
        }}
        boxStyle={(node) => {
          const s = swatch(node.data, byId[node.data.id], colourBy);
          const seed = node.data.side === 'seed';
          return {
            background: s.fill,
            [node.data.side === 'upstream' ? 'borderRight' : 'borderLeft']: `4px solid ${s.edge}`,
            ...(seed ? { borderWidth: 2, borderColor: 'var(--accent)' } : {}),
          };
        }}
        renderBox={(node) => <Box box={node.data} artifact={byId[node.data.id]} typeLabel={typeLabel} onCentre={onCentre} />}
      />
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 16px', marginTop: 10, fontSize: 12, color: 'var(--text-muted)' }}>
        {links.length > 0 && (
          <ul aria-label="Link key" style={keyList}>
            {links.map((t) => (
              <li key={t} style={keyItem}>
                <span aria-hidden="true" style={{ width: 18, height: 3, borderRadius: 2, background: linkColour(t) }} />
                {t}
              </li>
            ))}
          </ul>
        )}
        <ul aria-label="Colour key" style={keyList}>
          {Array.from(colours.values()).map((s) => (
            <li key={s.label} style={keyItem}>
              <span aria-hidden="true" style={{ width: 12, height: 12, borderRadius: 3, background: s.fill, border: '1px solid var(--border)', borderLeft: `3px solid ${s.edge}` }} />
              {s.label}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
};

const Box: React.FC<{ box: ImpactBox; artifact?: Artifact; typeLabel: (t: string) => string; onCentre?: (id: string) => void }> = ({
  box,
  artifact,
  typeLabel,
  onCentre,
}) => (
  <>
    <span style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
      {artifact?.ref && <span style={{ ...refStyle, fontWeight: 600 }}>{artifact.ref}</span>}
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{box.side === 'seed' ? `Centre · ${typeLabel(box.type)}` : typeLabel(box.type)}</span>
      <span style={{ flex: 1 }} />
      {onCentre && box.side !== 'seed' && (
        <button
          type="button"
          tabIndex={-1}
          title="Centre the impact on this artifact"
          aria-label={`Centre the impact on ${artifact?.ref || box.title}`}
          onClick={(e) => {
            e.stopPropagation();
            onCentre(box.id);
          }}
          style={{ padding: '0 5px', minHeight: 20, border: '1px solid var(--border)', borderRadius: 4, background: 'var(--surface)', color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer' }}
        >
          ⌖
        </button>
      )}
    </span>
    <span
      style={{
        display: '-webkit-box',
        WebkitLineClamp: 2,
        WebkitBoxOrient: 'vertical',
        overflow: 'hidden',
        marginTop: 2,
        fontSize: 12,
        lineHeight: '15px',
        color: 'var(--text)',
        fontWeight: box.side === 'seed' ? 600 : 400,
      }}
    >
      {box.title}
    </span>
  </>
);

const keyList: React.CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: '4px 12px', margin: 0, padding: 0, listStyle: 'none' };
const keyItem: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 6 };
