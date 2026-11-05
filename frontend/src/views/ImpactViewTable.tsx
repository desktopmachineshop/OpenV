import React from 'react';
import { Link } from 'react-router-dom';
import type { Artifact } from '../api/client';
import { GridHead, ellipsis, gridRowStyle, mutedStyle, panelStyle, refStyle } from '../components/ui';
import { STATUS_META, statusOf } from '../utils/artifactFacts';
import { ImpactBox, linkColour } from './ImpactViewLayout';

// The impact as a table: the same artifacts the tree draws, one row each,
// upstream first, with the side, the hops, the link it came through and the
// artifact it hangs off. For a screen reader, for copying out, and for a
// result too wide to read as a picture.

const COLUMNS = '100px 52px 120px 84px minmax(0, 1fr) 110px 96px 72px';

interface Props {
  boxes: ImpactBox[];
  byId: Record<string, Artifact>;
  projectId: string;
  typeLabel: (type: string) => string;
  onCentre?: (id: string) => void;
}

export const ImpactViewTable: React.FC<Props> = ({ boxes, byId, projectId, typeLabel, onCentre }) => {
  if (boxes.length === 0) return <p style={mutedStyle}>Nothing is linked to it.</p>;
  const refOf = (id: string) => byId[id]?.ref || '';
  return (
    <section aria-label="Impact table" style={{ ...panelStyle, overflow: 'auto', flex: 1, minHeight: 0 }} data-testid="impact-table">
      <div role="table" aria-label="Impact" aria-rowcount={boxes.length + 1} style={{ minWidth: 820 }}>
        <div style={{ position: 'sticky', top: 0, zIndex: 1 }}>
          <GridHead columns={COLUMNS} names={['Direction', 'Hops', 'Link', 'Ref', 'Title', 'Type', 'Status', '']} />
        </div>
        {boxes.map((b) => {
          const a = byId[b.id];
          const from = b.depth > 1 ? refOf(b.parentId) : '';
          return (
            <div role="row" key={`${b.side}:${b.id}`} style={gridRowStyle(COLUMNS)}>
              <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                {b.side === 'upstream' ? '← Upstream' : 'Downstream →'}
              </span>
              <span role="cell" style={{ fontSize: 12 }}>
                {b.depth}
              </span>
              <span role="cell" style={{ fontSize: 12, display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
                <span aria-hidden="true" style={{ flex: 'none', width: 12, height: 3, borderRadius: 2, background: linkColour(b.via) }} />
                <span style={ellipsis} title={from ? `${b.via}, through ${from}` : b.via}>
                  {b.via}
                </span>
              </span>
              <span role="cell" style={refStyle}>
                {a?.ref || '—'}
              </span>
              <span role="cell" style={ellipsis}>
                <Link to={`/projects/${projectId}/requirements?artifact=${b.id}`} style={{ color: 'var(--text)' }}>
                  {b.title}
                </Link>
                {from && <span style={{ ...mutedStyle, marginLeft: 6 }}>via {from}</span>}
              </span>
              <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                {typeLabel(b.type)}
              </span>
              <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                {a ? STATUS_META[statusOf(a)].label : ''}
              </span>
              <span role="cell">
                {onCentre && (
                  <button
                    type="button"
                    onClick={() => onCentre(b.id)}
                    aria-label={`Centre the impact on ${a?.ref || b.title}`}
                    style={{ padding: '2px 8px', fontSize: 12, border: '1px solid var(--border)', borderRadius: 4, background: 'var(--surface)', color: 'var(--text-secondary)', cursor: 'pointer' }}
                  >
                    Centre
                  </button>
                )}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
};
