import React, { useMemo, useState } from 'react';
import { buildHierarchy, ArtifactTreeNode } from '../../utils/artifactSequence';
import { FactsIndex, typeLabel, typeTag } from '../../utils/artifactFacts';
import { Chip, toneFor } from '../../components/ui/Chip';
import { RefText, StatusChip, TypeTag, VVBar, sectionHeadingStyle } from '../../components/artifactCells';
import { chainTo, levelName, testsForChain } from './LayersView';
import type { Artifact } from './shared';

interface LevelDrillProps {
  artifacts: Artifact[];
  facts: FactsIndex;
  /** Open an artifact's full document. */
  onOpen: (id: string) => void;
  /** Leave the levels for the list. */
  onBack: () => void;
}

const card: React.CSSProperties = { background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 8 };

const depthOf = (nodes: ArtifactTreeNode[]): number =>
  nodes.reduce((max, n) => Math.max(max, 1 + depthOf(n.children)), 0);

// A phone's way down the document, one level at a time: the artifact you are
// on, what it traces up to, what it decomposes into, and the tests that
// verify it. Tapping a child goes down a level; ‹ goes back up.
export const LevelDrill: React.FC<LevelDrillProps> = ({ artifacts, facts, onOpen, onBack }) => {
  const roots = useMemo(() => buildHierarchy(artifacts), [artifacts]);
  const byId = useMemo(() => new Map(artifacts.map((a) => [a.id, a])), [artifacts]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const chain = chainTo(roots, currentId);
  const current = chain[chain.length - 1];
  const parent = chain[chain.length - 2];
  const kids = current ? current.children : roots;
  const levels = Math.min(6, Math.max(1, depthOf(roots)));
  const here = chain.length; // 0 at the top
  const tally = current ? facts.verifiedBelow(current.artifact.id) : null;
  const tests = current ? testsForChain([current.artifact], facts, byId)[0]?.tests ?? [] : [];
  const up = () => setCurrentId(parent ? parent.artifact.id : null);

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }} data-help-key="requirements.layers">
      <div style={{ background: 'var(--sidebar-bg)', color: 'var(--sidebar-text)', padding: '4px 4px 12px', display: 'flex', flexDirection: 'column', gap: 8 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, minHeight: 44 }}>
          <button
            type="button"
            aria-label={current ? 'Up one level' : 'Back to the list'}
            onClick={current ? up : onBack}
            style={{ width: 44, height: 44, border: 'none', background: 'transparent', color: 'inherit', fontSize: 22, cursor: 'pointer' }}
          >
            ‹
          </button>
          <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--sidebar-text-dim)' }}>
              {current ? `${typeLabel(current.artifact.type)} · level ${here} of ${levels}` : 'Top of the project'}
            </span>
            <span style={{ fontSize: 16, fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {current ? current.artifact.ref || current.artifact.title : 'By level'}
            </span>
          </div>
        </div>
        <ol aria-label="Levels" style={{ listStyle: 'none', margin: '0 12px', padding: 0, display: 'grid', gridTemplateColumns: `repeat(${levels}, minmax(0, 1fr))`, gap: 4 }}>
          {Array.from({ length: levels }, (_, i) => (
            <li key={i} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <span style={{ height: 4, borderRadius: 2, background: i < here ? 'var(--accent)' : 'var(--sidebar-border)' }} />
              <button
                type="button"
                aria-current={i === here - 1 ? 'step' : undefined}
                disabled={i >= here}
                onClick={() => setCurrentId(chain[i].artifact.id)}
                style={{ minHeight: 44, padding: 0, border: 'none', background: 'transparent', textAlign: 'left', fontSize: 12, color: i === here - 1 ? 'var(--sidebar-text)' : 'var(--sidebar-text-dim)', fontWeight: i === here - 1 ? 700 : 500, cursor: i < here ? 'pointer' : 'default' }}
              >
                L{i + 1}
              </button>
            </li>
          ))}
        </ol>
      </div>

      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '12px 16px 16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
        {parent && (
          <button
            type="button"
            onClick={up}
            style={{ display: 'flex', alignItems: 'center', gap: 8, minHeight: 44, padding: '8px 12px', border: '1px dashed var(--neutral-mid)', borderRadius: 8, background: 'transparent', textAlign: 'left', color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer' }}
          >
            <span style={{ color: 'var(--text-muted)', flex: 'none' }}>Traces up to</span>
            <RefText artifact={parent.artifact} />
            <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{parent.artifact.title}</span>
          </button>
        )}

        {current && (
          <section style={{ ...card, display: 'flex', flexDirection: 'column', gap: 8, padding: 12 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
              <TypeTag tag={typeTag(current.artifact.type)} strong />
              <StatusChip artifact={current.artifact} />
              {facts.get(current.artifact.id).suspect > 0 && (
                <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--warning-text)' }}>suspect link</span>
              )}
            </div>
            <h2 style={{ margin: 0, fontSize: 19, fontWeight: 700, lineHeight: 1.25 }}>{current.artifact.title}</h2>
            {tally && tally.total > 0 && (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ flex: 1, display: 'flex' }}>
                  <VVBar pass={tally.passing} fail={tally.failing} blocked={tally.blocked} total={tally.total} height={6} />
                </span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {tally.passing}/{tally.total} verified below
                </span>
              </div>
            )}
            <button
              type="button"
              onClick={() => onOpen(current.artifact.id)}
              style={{ alignSelf: 'flex-start', minHeight: 44, padding: 0, border: 'none', background: 'transparent', color: 'var(--accent-text)', fontSize: 14, fontWeight: 600, cursor: 'pointer' }}
            >
              Open full artifact
            </button>
          </section>
        )}

        {kids.length > 0 && (
          <section aria-label="Decomposes into" style={card}>
            <h3 style={{ ...sectionHeadingStyle, padding: '10px 12px 6px', fontSize: 12 }}>
              {current ? 'Decomposes into' : 'Top level'} · {levelName(kids, here)}
            </h3>
            {kids.map((k) => (
              <button
                key={k.artifact.id}
                type="button"
                onClick={() => (k.children.length ? setCurrentId(k.artifact.id) : onOpen(k.artifact.id))}
                style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 10, minHeight: 60, padding: '8px 12px', border: 'none', borderTop: '1px solid var(--border-soft)', background: 'var(--surface)', textAlign: 'left', color: 'var(--text)', cursor: 'pointer' }}
              >
                <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 3 }}>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <TypeTag tag={typeTag(k.artifact.type)} />
                    <RefText artifact={k.artifact} />
                    <StatusChip artifact={k.artifact} />
                  </span>
                  <span style={{ fontSize: 15, fontWeight: 500 }}>{k.artifact.title}</span>
                  {k.children.length > 0 && (
                    <span style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginTop: 2 }}>
                      {k.children.slice(0, 6).map((g) => (
                        <span key={g.artifact.id} style={{ fontSize: 12, color: 'var(--text-secondary)', background: 'var(--surface-inset)', border: '1px solid var(--border-soft)', borderRadius: 3, padding: '0 4px' }}>
                          {g.artifact.ref || g.artifact.title}
                        </span>
                      ))}
                    </span>
                  )}
                </span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)', flex: 'none' }}>
                  {k.children.length ? `${k.children.length} ›` : facts.get(k.artifact.id).vv.label}
                </span>
              </button>
            ))}
          </section>
        )}

        {tests.length > 0 && (
          <section aria-label="Verified by" style={card}>
            <h3 style={{ ...sectionHeadingStyle, padding: '10px 12px 6px', fontSize: 12 }}>Verified by</h3>
            {tests.map(({ test, result }) => (
              <button
                key={test.id}
                type="button"
                onClick={() => onOpen(test.id)}
                style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 10, minHeight: 48, padding: '0 12px', border: 'none', borderTop: '1px solid var(--border-soft)', background: 'var(--surface)', textAlign: 'left', color: 'var(--text)', cursor: 'pointer' }}
              >
                <RefText artifact={test} />
                <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{test.title}</span>
                <Chip tone={toneFor(result)}>{result}</Chip>
              </button>
            ))}
          </section>
        )}
      </div>
    </div>
  );
};
