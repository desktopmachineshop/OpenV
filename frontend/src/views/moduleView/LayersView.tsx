import React, { useMemo } from 'react';
import { buildHierarchy, ArtifactTreeNode } from '../../utils/artifactSequence';
import { FactsIndex, typeLabel, typeTag } from '../../utils/artifactFacts';
import { Chip, toneFor } from '../../components/ui/Chip';
import { RefText, StatusChip, TypeTag, VVBar } from '../../components/artifactCells';
import type { Artifact } from './shared';

/** The path from a root down to the selected artifact, through the tree as drawn. */
export const chainTo = (nodes: ArtifactTreeNode[], id: string | null): ArtifactTreeNode[] => {
  if (!id) return [];
  for (const node of nodes) {
    if (node.artifact.id === id) return [node];
    const below = chainTo(node.children, id);
    if (below.length) return [node, ...below];
  }
  return [];
};

/** What a level holds, named by its artifacts' type when they share one. */
export const levelName = (nodes: ArtifactTreeNode[], level: number): string => {
  const types = new Set(nodes.map((n) => n.artifact.type));
  return types.size === 1 ? typeLabel([...types][0]) : `Level ${level + 1}`;
};

/** The tests verifying any artifact of a chain, nearest first, with their latest result. */
export const testsForChain = (chain: Artifact[], facts: FactsIndex, byId: Map<string, Artifact>) =>
  [...chain]
    .reverse()
    .map((a) => ({
      artifact: a,
      tests: facts
        .linksOf(a.id)
        .filter((l) => l.type === 'verifies' && l.to_id === a.id)
        .map((l) => byId.get(l.from_id))
        .filter((t): t is Artifact => !!t)
        .map((t) => ({ test: t, result: facts.testResult(t.id) })),
    }))
    .filter((g) => g.tests.length > 0);

interface LayersViewProps {
  artifacts: Artifact[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  facts: FactsIndex;
}

const columnStyle: React.CSSProperties = {
  flex: '1 0 230px',
  maxWidth: 360,
  display: 'flex',
  flexDirection: 'column',
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  minHeight: 0,
};

const columnHead: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 1,
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-soft)',
  background: 'var(--surface-alt)',
  borderRadius: '6px 6px 0 0',
};

// The layers: one column per level of the document, each holding what sits
// under the artifact picked in the column before it, so a whole chain — need,
// requirement, sub-requirement, design — reads left to right, with the tests
// that verify any link of it in the last column.
export const LayersView: React.FC<LayersViewProps> = ({ artifacts, selectedId, onSelect, facts }) => {
  const roots = useMemo(() => buildHierarchy(artifacts), [artifacts]);
  const byId = useMemo(() => new Map(artifacts.map((a) => [a.id, a])), [artifacts]);
  const chain = chainTo(roots, selectedId);
  const columns: ArtifactTreeNode[][] = [roots];
  chain.forEach((node) => {
    if (node.children.length) columns.push(node.children);
  });
  const groups = testsForChain(
    chain.map((n) => n.artifact),
    facts,
    byId
  );

  const card = (node: ArtifactTreeNode, level: number) => {
    const a = node.artifact;
    const on = chain[level]?.artifact.id === a.id;
    const f = facts.get(a.id);
    const tally = facts.verifiedBelow(a.id);
    return (
      <button
        key={a.id}
        type="button"
        aria-pressed={on}
        onClick={() => onSelect(a.id)}
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          width: '100%',
          padding: '8px 10px',
          borderRadius: 6,
          textAlign: 'left',
          color: 'var(--text)',
          cursor: 'pointer',
          background: on ? 'var(--tint-blue)' : 'var(--surface)',
          border: `1px solid ${on ? 'var(--accent)' : 'var(--border)'}`,
          boxShadow: on ? '0 0 0 1px var(--accent)' : undefined,
        }}
      >
        <span style={{ display: 'flex', alignItems: 'center', gap: 6, width: '100%' }}>
          <TypeTag tag={typeTag(a.type)} strong={level === 0} />
          <RefText artifact={a} />
          <span style={{ flex: 1 }} />
          <StatusChip artifact={a} />
        </span>
        <span style={{ fontSize: 13, fontWeight: 500, lineHeight: 1.35 }}>{a.title}</span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', fontSize: 12, color: 'var(--text-muted)' }}>
          <span>{node.children.length ? `${node.children.length} below` : 'leaf'}</span>
          {f.suspect > 0 && <span style={{ color: 'var(--warning-text)', fontWeight: 700 }}>suspect link</span>}
          <span style={{ flex: 1 }} />
          {tally.total > 0 && (
            <>
              <VVBar pass={tally.passing} fail={tally.failing} blocked={tally.blocked} total={tally.total} width={44} />
              <span>
                {tally.passing}/{tally.total}
              </span>
            </>
          )}
        </span>
      </button>
    );
  };

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', gap: 8 }} data-help-key="requirements.layers">
      <nav aria-label="Selected chain" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 4, fontSize: 12, minHeight: 20 }}>
        {chain.length === 0 ? (
          <span style={{ color: 'var(--text-muted)' }}>Pick an artifact to follow it down through the levels.</span>
        ) : (
          chain.map((n, i) => (
            <React.Fragment key={n.artifact.id}>
              {i > 0 && (
                <span aria-hidden="true" style={{ color: 'var(--neutral)' }}>
                  →
                </span>
              )}
              <button
                type="button"
                onClick={() => onSelect(n.artifact.id)}
                style={{ minHeight: 24, padding: '0 2px', border: 'none', background: 'transparent', cursor: 'pointer' }}
                title={n.artifact.title}
              >
                <RefText artifact={n.artifact} strong />
              </button>
            </React.Fragment>
          ))
        )}
      </nav>
      <div style={{ flex: 1, minHeight: 0, overflow: 'auto', display: 'flex', gap: 10, alignItems: 'flex-start' }}>
        {columns.map((nodes, level) => (
          <section key={level} aria-label={levelName(nodes, level)} style={columnStyle}>
            <div style={columnHead}>
              <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <TypeTag tag={`L${level + 1}`} strong={level < 2} />
                <b style={{ fontSize: 13, fontWeight: 600 }}>{levelName(nodes, level)}</b>
                <span style={{ flex: 1 }} />
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{nodes.length}</span>
              </span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                {level === 0 ? 'Everything at the top of the project' : `Under ${chain[level - 1]?.artifact.ref || chain[level - 1]?.artifact.title}`}
              </span>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: 8 }}>{nodes.map((n) => card(n, level))}</div>
          </section>
        ))}
        <section aria-label="Verification" style={columnStyle}>
          <div style={columnHead}>
            <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <Chip tone="success">V&amp;V</Chip>
              <b style={{ fontSize: 13, fontWeight: 600 }}>Verification</b>
            </span>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Tests for every level of the selected chain</span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '8px 10px 10px' }}>
            {groups.map((g) => (
              <div key={g.artifact.id} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  Verifies <RefText artifact={g.artifact} style={{ fontSize: 12 }} />
                </span>
                {g.tests.map(({ test, result }) => (
                  <button
                    key={test.id}
                    type="button"
                    onClick={() => onSelect(test.id)}
                    style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 8px', border: '1px solid var(--border-soft)', borderRadius: 6, background: 'var(--surface)', color: 'var(--text)', fontSize: 12, textAlign: 'left', cursor: 'pointer' }}
                  >
                    <RefText artifact={test} />
                    <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{test.title}</span>
                    <Chip tone={toneFor(result)}>{result}</Chip>
                  </button>
                ))}
              </div>
            ))}
            {groups.length === 0 && (
              <span style={{ padding: 8, border: '1px dashed var(--neutral-mid)', borderRadius: 6, fontSize: 12, color: 'var(--text-muted)' }}>
                {chain.length ? 'No test case verifies any level of this chain yet.' : 'Pick an artifact to see the tests below it.'}
              </span>
            )}
          </div>
        </section>
      </div>
    </div>
  );
};
