import React, { useEffect, useMemo, useRef, useState } from 'react';
import { TreeDiagram, parentKeys } from '../../components/treeDiagram';
import type { DiagramNode, DiagramShape } from '../../components/treeDiagram';
import { RefText, StatusChip, TypeTag } from '../../components/artifactCells';
import { ArtifactTreeNode, ancestorIds, buildHierarchy } from '../../utils/artifactSequence';
import { FactsIndex, STATUS_META, statusOf, typeLabel, typeTag } from '../../utils/artifactFacts';
import type { FoldTrigger } from './useFoldAll';
import type { Artifact } from './shared';

// The requirements Tree: the document's hierarchy (parent_id) drawn left to
// right as boxes and connectors, an org chart of the project. Each box shows
// its type, ref, status and title, and is coloured by its type or its status
// as the toggle says, with a key. It opens folded to two levels; ⊖ / ⊕ on a
// connector folds a branch and the toolbar's button folds or opens them all.
// A click opens the artifact in the detail panel, as the table does.

export type ColourBy = 'type' | 'status';

interface Swatch {
  fill: string;
  edge: string;
  label: string;
  dashed?: boolean;
}

// Theme tokens only, so both themes hold. The box keeps its type tag and its
// status chip whichever colouring is on: colour is never the only cue.
export const TYPE_COLOURS: Record<string, Swatch> = {
  heading: { fill: 'var(--surface-alt)', edge: 'var(--text-secondary)', label: 'Heading' },
  'user-need': { fill: 'var(--tint-yellow)', edge: 'var(--warning)', label: 'User need' },
  persona: { fill: 'var(--tint-yellow)', edge: 'var(--warning-strong)', label: 'Persona' },
  requirement: { fill: 'var(--tint-blue)', edge: 'var(--accent)', label: 'Requirement' },
  'design-item': { fill: 'var(--tint-purple)', edge: 'var(--purple)', label: 'Design item' },
  'test-case': { fill: 'var(--tint-green)', edge: 'var(--success)', label: 'Test case' },
  hazard: { fill: 'var(--tint-red)', edge: 'var(--danger)', label: 'Hazard' },
  description: { fill: 'var(--surface)', edge: 'var(--neutral-mid)', label: 'Description' },
};
const OTHER: Swatch = { fill: 'var(--surface)', edge: 'var(--neutral)', label: 'Other' };

export const STATUS_COLOURS: Record<string, Swatch> = {
  draft: { fill: 'var(--surface)', edge: 'var(--neutral-mid)', label: STATUS_META.draft.label },
  in_review: { fill: 'var(--tint-blue)', edge: 'var(--accent)', label: STATUS_META.in_review.label },
  approved: { fill: 'var(--tint-green)', edge: 'var(--success)', label: STATUS_META.approved.label },
  superseded: { fill: 'var(--surface-alt)', edge: 'var(--neutral)', label: STATUS_META.superseded.label, dashed: true },
};

export const swatchOf = (a: Artifact, by: ColourBy): Swatch =>
  by === 'type' ? (TYPE_COLOURS[a.type] ?? { ...OTHER, label: typeLabel(a.type) }) : STATUS_COLOURS[statusOf(a)];

const toDiagram = (nodes: ArtifactTreeNode[]): DiagramNode<Artifact>[] =>
  nodes.map((n) => ({ key: n.artifact.id, data: n.artifact, children: toDiagram(n.children) }));

const METRICS = { boxWidth: 232, boxHeight: 62 };

interface TreeDiagramViewProps {
  artifacts: Artifact[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  facts: FactsIndex;
  foldTrigger: FoldTrigger;
}

const segment = (on: boolean, first: boolean): React.CSSProperties => ({
  minHeight: 26,
  padding: '0 9px',
  border: 'none',
  borderLeft: first ? 'none' : '1px solid var(--border)',
  background: on ? 'var(--neutral-soft)' : 'var(--surface)',
  color: on ? 'var(--text)' : 'var(--text-muted)',
  fontSize: 12,
  fontWeight: on ? 600 : 500,
  cursor: 'pointer',
});

export const TreeDiagramView: React.FC<TreeDiagramViewProps> = ({ artifacts, selectedId, onSelect, facts, foldTrigger }) => {
  const roots = useMemo(() => toDiagram(buildHierarchy(artifacts)), [artifacts]);
  const shape: DiagramShape<Artifact> = useMemo(() => ({ kind: 'forest', roots }), [roots]);
  const [colourBy, setColourBy] = useState<ColourBy>('type');
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const started = useRef(false);

  // Open to two levels the first time there is something to draw.
  useEffect(() => {
    if (started.current || roots.length === 0) return;
    started.current = true;
    setFolded(new Set(parentKeys(roots, 1)));
  }, [roots]);

  // Fold all / unfold all from the toolbar, once per press.
  useEffect(() => {
    if (foldTrigger.token === 0) return;
    started.current = true;
    setFolded(foldTrigger.folded ? new Set(parentKeys(roots)) : new Set());
    // A refiltered tree keeps what is open; only a new press re-applies.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [foldTrigger.token]);

  // A selection from outside (a link, ‹ / ›, a search) opens the branch it sits in.
  const reveal = ancestorIds(artifacts, selectedId).join(' ');
  useEffect(() => {
    if (!reveal) return;
    setFolded((prev) => {
      const shut = reveal.split(' ').filter((id) => prev.has(id));
      if (shut.length === 0) return prev;
      const next = new Set(prev);
      shut.forEach((id) => next.delete(id));
      return next;
    });
  }, [reveal]);

  const toggle = (key: string) =>
    setFolded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  if (artifacts.length === 0) {
    return <p style={{ color: 'var(--text-muted)' }}>No artifacts match. Create one, or clear the search and filters.</p>;
  }

  const present = new Map<string, Swatch>();
  artifacts.forEach((a) => {
    const key = colourBy === 'type' ? a.type : statusOf(a);
    if (!present.has(key)) present.set(key, swatchOf(a, colourBy));
  });

  const controls = (
    <>
      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Colour by</span>
      <span role="group" aria-label="Colour by" style={{ display: 'flex', border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' }}>
        {(['type', 'status'] as const).map((by, i) => (
          <button key={by} type="button" aria-pressed={colourBy === by} onClick={() => setColourBy(by)} style={segment(colourBy === by, i === 0)}>
            {by === 'type' ? 'Type' : 'Status'}
          </button>
        ))}
      </span>
      <ul aria-label="Colour key" style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 12px', margin: 0, padding: 0, listStyle: 'none' }}>
        {Array.from(present.values()).map((s) => (
          <li key={s.label} style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 12, color: 'var(--text-secondary)' }}>
            <span
              aria-hidden="true"
              style={{ width: 12, height: 12, borderRadius: 3, background: s.fill, border: `1px ${s.dashed ? 'dashed' : 'solid'} var(--border)`, borderLeft: `3px solid ${s.edge}` }}
            />
            {s.label}
          </li>
        ))}
      </ul>
    </>
  );

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }} data-help-key="requirements.tree">
      <TreeDiagram
        shape={shape}
        folded={folded}
        onToggle={toggle}
        selectedKey={selectedId}
        onSelect={(node) => onSelect(node.key)}
        ariaLabel="Requirements tree diagram"
        controls={controls}
        metrics={METRICS}
        labelOf={(node) => {
          const a = node.data;
          const f = facts.get(a.id);
          return [a.ref || a.doc_number, a.title, typeLabel(a.type), STATUS_META[statusOf(a)].label, f.suspect ? `${f.suspect} suspect` : '']
            .filter(Boolean)
            .join(', ');
        }}
        boxStyle={(node) => {
          const s = swatchOf(node.data, colourBy);
          return { background: s.fill, borderLeft: `4px solid ${s.edge}`, borderStyle: s.dashed ? 'dashed' : 'solid' };
        }}
        renderBox={(node) => <TreeBox artifact={node.data} />}
      />
    </div>
  );
};

const TreeBox: React.FC<{ artifact: Artifact }> = ({ artifact: a }) => (
  <>
    <span style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
      <TypeTag tag={typeTag(a.type)} />
      <RefText artifact={a} strong style={{ overflow: 'hidden', textOverflow: 'ellipsis' }} />
      <span style={{ flex: 1 }} />
      <StatusChip artifact={a} size="sm" />
    </span>
    <span
      style={{
        display: '-webkit-box',
        WebkitLineClamp: 2,
        WebkitBoxOrient: 'vertical',
        overflow: 'hidden',
        marginTop: 3,
        fontSize: 12,
        lineHeight: '15px',
        color: 'var(--text)',
      }}
    >
      {a.title}
    </span>
  </>
);
