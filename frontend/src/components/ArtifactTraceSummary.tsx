import React, { useState } from 'react';
import type { Artifact, LinkedArtifact } from '../api/client';
import { qualifiedRef } from '../api/client';
import { getLinkTypeLabel } from '../config/linkTypeRules';
import { FactsIndex, otherEnd, traceDirection } from '../utils/artifactFacts';
import { Chip, toneFor } from './ui/Chip';
import { MONO, RefText, SuspectMark, VVBar, sectionHeadingStyle } from './artifactCells';

interface ArtifactTraceSummaryProps {
  artifact: Artifact;
  artifacts: Artifact[];
  /** Far ends of links that cross into other projects (REQ-145). */
  linked?: LinkedArtifact[];
  facts: FactsIndex;
  onSelect: (artifactId: string) => void;
  /** A phone: rows a fingertip tall. */
  touch?: boolean;
}

const ARROW = { up: '↑', down: '↓', peer: '↔' } as const;
const ORDER = { up: 0, down: 1, peer: 2 } as const;
const SHOWN = 6;

// The selected artifact's place in the trace: what it answers to (↑) and what
// answers to it (↓), a suspect link marked where the far end changed since
// the link was made; then the tests that verify it, with their latest results
// and a pass / fail / blocked bar. The full list of links, with their types
// and Delete, is further down under Traceability links.
export const ArtifactTraceSummary: React.FC<ArtifactTraceSummaryProps> = ({
  artifact,
  artifacts,
  linked,
  facts,
  onSelect,
  touch = false,
}) => {
  const [showAll, setShowAll] = useState(false);
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const links = [...facts.linksOf(artifact.id)]
    .filter((l) => !(l.type === 'verifies' && l.to_id === artifact.id))
    .sort((a, b) => ORDER[traceDirection(a, artifact.id)] - ORDER[traceDirection(b, artifact.id)]);
  const up = links.filter((l) => traceDirection(l, artifact.id) === 'up').length;
  const down = links.filter((l) => traceDirection(l, artifact.id) === 'down').length;
  const suspect = links.filter((l) => l.suspect).length;

  const entry = facts.coverage(artifact.id);
  const testIds = entry
    ? entry.test_case_ids || []
    : facts
        .linksOf(artifact.id)
        .filter((l) => l.type === 'verifies' && l.to_id === artifact.id)
        .map((l) => l.from_id);
  const tests = testIds.map((id) => ({ id, test: byId.get(id), result: entry?.latest_results?.[id] || facts.testResult(id) }));
  const count = (r: string) => tests.filter((t) => t.result === r).length;
  const passing = count('pass');

  const rowStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    minHeight: touch ? 44 : 30,
    padding: '0 6px',
    border: 'none',
    borderRadius: 4,
    textAlign: 'left',
    color: 'var(--text)',
    fontSize: 13,
    cursor: 'pointer',
  };

  const far = (id: string) => {
    const local = byId.get(id);
    if (local) return { ref: local.ref || '', title: local.title };
    const remote = (linked || []).find((l) => l.id === id);
    return remote ? { ref: qualifiedRef(remote), title: remote.title } : { ref: id.slice(0, 8), title: '' };
  };

  if (links.length === 0 && tests.length === 0 && !entry) return null;

  return (
    <div className="card" style={{ display: 'flex', flexDirection: 'column', gap: 14, padding: '12px 14px' }} data-help-key="links.view">
      {links.length > 0 && (
        <section style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <h4 style={{ ...sectionHeadingStyle, display: 'flex', gap: 8 }}>
            <span>
              Trace · {up} up · {down} down
            </span>
            {suspect > 0 && <span style={{ color: 'var(--warning-text)' }}>{suspect} suspect</span>}
          </h4>
          {(showAll ? links : links.slice(0, SHOWN)).map((link) => {
            const dir = traceDirection(link, artifact.id);
            const end = far(otherEnd(link, artifact.id));
            return (
              <button
                key={link.id}
                type="button"
                onClick={() => onSelect(otherEnd(link, artifact.id))}
                title={`${getLinkTypeLabel(link.type, link.to_id === artifact.id)} ${end.ref} ${end.title}`}
                style={{ ...rowStyle, background: link.suspect ? 'var(--tint-yellow)' : 'transparent' }}
              >
                <span aria-hidden="true" style={{ color: 'var(--text-muted)', width: 12, flex: 'none' }}>
                  {ARROW[dir]}
                </span>
                <span style={{ fontFamily: MONO, fontSize: 12, color: 'var(--accent-text)', width: 72, flex: 'none', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {end.ref}
                </span>
                <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{end.title}</span>
                {link.suspect ? (
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 12, fontWeight: 700, color: 'var(--warning-text)' }}>
                    <SuspectMark />
                    suspect
                  </span>
                ) : (
                  <span style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                    {getLinkTypeLabel(link.type, link.to_id === artifact.id)}
                  </span>
                )}
              </button>
            );
          })}
          {links.length > SHOWN && (
            <button
              type="button"
              onClick={() => setShowAll((v) => !v)}
              style={{ ...rowStyle, justifyContent: 'center', background: 'transparent', color: 'var(--accent-text)', fontWeight: 600 }}
            >
              {showAll ? 'Show fewer' : `Show all ${links.length} links`}
            </button>
          )}
        </section>
      )}

      {(tests.length > 0 || entry) && (
        <section style={{ display: 'flex', flexDirection: 'column', gap: 4 }} data-help-key="vv">
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <h4 style={sectionHeadingStyle}>
              Verification · {passing} of {tests.length} passing
            </h4>
            {entry && <Chip tone={toneFor(entry.rollup)}>{entry.rollup}</Chip>}
          </div>
          {tests.length > 0 && (
            <span style={{ display: 'flex', marginBottom: 4 }}>
              <VVBar pass={passing} fail={count('fail')} blocked={count('blocked')} total={tests.length} />
            </span>
          )}
          {tests.map(({ id, test, result }) => (
            <button key={id} type="button" onClick={() => onSelect(id)} style={{ ...rowStyle, background: 'transparent' }}>
              {test ? <RefText artifact={test} style={{ width: 72, flex: 'none' }} /> : <span style={{ width: 72 }} />}
              <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {test?.title ?? id.slice(0, 8)}
              </span>
              <Chip tone={toneFor(result)}>{result}</Chip>
            </button>
          ))}
          {tests.length === 0 && entry && (
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              No test case verifies this yet
              {entry.verification_method ? ` (to be verified by ${entry.verification_method})` : ''}.
            </span>
          )}
        </section>
      )}
    </div>
  );
};
