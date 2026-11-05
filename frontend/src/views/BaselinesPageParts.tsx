// The Baselines page's panels: the timeline, the change table and the text of
// one change, word by word.
import React from 'react';
import type { Artifact, Baseline } from '../api/client';
import {
  Chip,
  GridHead,
  ellipsis,
  eyebrowStyle,
  gridRowStyle,
  mutedStyle,
  panelStyle,
  refStyle,
} from '../components/ui';
import { baselineAuthor, baselineDate } from '../utils/baselines';
import { WordDiff } from '../components/ArtifactWordDiff';
import { CHANGE_TONE, type ChangeRow } from './BaselinesPageData';

export const LIVE = 'live';

export const Timeline: React.FC<{
  baselines: Baseline[];
  liveCount: number;
  base: string;
  target: string;
  onPick: (id: string) => void;
  phone?: boolean;
}> = ({ baselines, liveCount, base, target, onPick, phone }) => {
  const entries = [
    {
      id: LIVE,
      name: 'Working copy',
      meta: `${liveCount} artifacts · the project as it is now`,
    },
    ...baselines.map((b) => ({
      id: b.id,
      name: b.name,
      meta: [baselineDate(b.created_at), baselineAuthor(b)].filter(Boolean).join(' · '),
    })),
  ];
  return (
    <aside
      aria-label="Baselines"
      data-help-key="baselines.timeline"
      style={
        phone
          ? panelStyle
          : {
              flex: '1 1 280px',
              maxWidth: 320,
              minWidth: 0,
              background: 'var(--surface)',
              borderRight: '1px solid var(--border)',
            }
      }
    >
      <h2
        style={{
          ...eyebrowStyle,
          padding: '10px 12px',
          borderBottom: '1px solid var(--border-soft)',
        }}
      >
        Timeline
      </h2>
      <ol style={{ listStyle: 'none', margin: 0, padding: 0 }}>
        {entries.map((e) => {
          const role = e.id === base ? 'from' : e.id === target ? 'to' : '';
          return (
            <li key={e.id} style={{ borderBottom: '1px solid var(--border-soft)' }}>
              <button
                type="button"
                aria-pressed={!!role}
                onClick={() => onPick(e.id)}
                style={{
                  display: 'flex',
                  gap: 10,
                  width: '100%',
                  padding: '10px 12px',
                  border: 'none',
                  background: role ? 'var(--tint-blue)' : 'var(--surface)',
                  color: 'var(--text)',
                  textAlign: 'left',
                  font: 'inherit',
                  cursor: 'pointer',
                }}
              >
                <span
                  style={{
                    flex: 'none',
                    width: 10,
                    height: 10,
                    marginTop: 4,
                    borderRadius: '50%',
                    boxSizing: 'border-box',
                    ...(e.id === LIVE
                      ? { background: 'var(--accent)' }
                      : {
                          border: '2px solid var(--neutral-mid)',
                          background: 'var(--surface)',
                        }),
                  }}
                />
                <span
                  style={{
                    flex: 1,
                    minWidth: 0,
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 2,
                  }}
                >
                  <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <b style={{ fontWeight: 600, ...ellipsis }}>{e.name}</b>
                    {e.id === LIVE && <Chip tone="info">live</Chip>}
                    <span style={{ flex: 1 }} />
                    {role && (
                      <span
                        style={{
                          fontSize: 12,
                          fontWeight: 700,
                          color: 'var(--accent-text)',
                        }}
                      >
                        {role}
                      </span>
                    )}
                  </span>
                  <span style={mutedStyle}>{e.meta}</span>
                </span>
              </button>
            </li>
          );
        })}
      </ol>
      {baselines.length === 0 && (
        <p style={{ ...mutedStyle, padding: '0 12px' }}>
          No baselines yet. Capture one from the top bar of the Requirements page.
        </p>
      )}
    </aside>
  );
};

const COLUMNS = '96px 84px minmax(0, 1fr) 120px';

/** A phone's change list: the kind, reference and fields on one line, the title under them. */
const ChangeList: React.FC<{
  rows: ChangeRow[];
  selected: string;
  onSelect: (key: string) => void;
}> = ({ rows, selected, onSelect }) => (
  <section aria-label="Changes" data-help-key="baselines.changes" style={panelStyle}>
    <h2
      style={{
        margin: 0,
        padding: '10px 12px',
        fontSize: 14,
        borderBottom: '1px solid var(--border-soft)',
      }}
    >
      Changes
    </h2>
    {rows.map((r) => (
      <button
        key={r.key}
        type="button"
        aria-pressed={r.key === selected}
        onClick={() => onSelect(r.key)}
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 2,
          width: '100%',
          padding: '8px 12px',
          border: 'none',
          borderBottom: '1px solid var(--border-soft)',
          background: r.key === selected ? 'var(--tint-blue)' : 'var(--surface)',
          color: 'var(--text)',
          textAlign: 'left',
          font: 'inherit',
        }}
      >
        <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Chip tone={CHANGE_TONE[r.kind]}>{r.kind}</Chip>
          {r.ref && <span style={refStyle}>{r.ref}</span>}
          <span style={mutedStyle}>{r.fields}</span>
        </span>
        <span style={{ ...ellipsis, width: '100%' }}>{r.title}</span>
      </button>
    ))}
    {rows.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>Nothing changed.</p>}
  </section>
);

export const ChangeTable: React.FC<{
  rows: ChangeRow[];
  selected: string;
  onSelect: (key: string) => void;
  phone?: boolean;
}> = ({ rows, selected, onSelect, phone }) =>
  phone ? (
    <ChangeList rows={rows} selected={selected} onSelect={onSelect} />
  ) : (
    <section aria-label="Changes" data-help-key="baselines.changes" style={{ ...panelStyle, overflowX: 'auto' }}>
      <div role="table" aria-label="Changes" style={{ minWidth: 620 }}>
        <GridHead columns={COLUMNS} names={['Change', 'Ref', 'Title', 'Fields']} />
        {rows.map((r) => (
          <div
            role="row"
            key={r.key}
            aria-selected={r.key === selected}
            tabIndex={0}
            onClick={() => onSelect(r.key)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect(r.key);
              }
            }}
            style={{
              ...gridRowStyle(COLUMNS, r.key === selected),
              cursor: 'pointer',
            }}
          >
            <span role="cell">
              <Chip tone={CHANGE_TONE[r.kind]}>{r.kind}</Chip>
            </span>
            <span role="cell" style={refStyle}>
              {r.ref || '—'}
            </span>
            <span role="cell" style={ellipsis}>
              {r.title}
            </span>
            <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
              {r.fields}
            </span>
          </div>
        ))}
        {rows.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>Nothing changed.</p>}
      </div>
    </section>
  );

const Diff = WordDiff;

/** The selected change's text, before and after, with the live links it left suspect. */
export const TextChange: React.FC<{
  row: ChangeRow;
  before?: Artifact;
  after?: Artifact;
  suspect: string[];
}> = ({ row, before, after, suspect }) => {
  const m = row.modified;
  const textual = row.kind === 'added' || row.kind === 'removed' || (m && (m.title_changed || m.body_changed));
  return (
    <section
      aria-label={`Text change in ${row.ref || row.title}`}
      data-help-key="baselines.diff"
      style={{
        ...panelStyle,
        padding: 12,
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          flexWrap: 'wrap',
        }}
      >
        {row.ref && <span style={{ ...refStyle, fontWeight: 600 }}>{row.ref}</span>}
        <b style={{ fontWeight: 600 }}>{textual ? 'Text' : 'Change'}</b>
        <span style={mutedStyle}>
          {row.kind}
          {row.fields && row.fields !== '—' && row.fields !== 'new' ? ` · ${row.fields}` : ''}
          {after ? ` · version ${after.version}` : ''}
        </span>
      </div>
      {textual ? (
        <>
          {(row.kind !== 'changed' || m?.title_changed) && (
            <div style={{ fontWeight: 600 }}>
              <Diff before={before?.title || ''} after={after?.title || ''} />
            </div>
          )}
          <Diff before={before?.body || ''} after={after?.body || ''} />
        </>
      ) : (
        <p style={{ ...mutedStyle, margin: 0 }}>
          {row.kind.startsWith('link')
            ? `The link “${row.title}” was ${row.kind === 'link added' ? 'added' : 'removed'}.`
            : `Its ${row.fields} changed; the text is the same.`}
        </p>
      )}
      {suspect.length > 0 && (
        <span style={{ fontSize: 12, color: 'var(--warning-text)' }}>
          {suspect.length} {suspect.length === 1 ? 'link is' : 'links are'} suspect now: {suspect.join(', ')}.
        </span>
      )}
    </section>
  );
};
