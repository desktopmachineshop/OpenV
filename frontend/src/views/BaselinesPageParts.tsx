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
  /** Say how far the plan had got at each baseline that recorded it. */
  plan?: boolean;
}> = ({ baselines, liveCount, base, target, onPick, phone, plan }) => {
  const entries = [
    {
      id: LIVE,
      name: 'Working copy',
      meta: `${liveCount} artifacts · the project as it is now`,
    },
    ...baselines.map((b) => ({
      id: b.id,
      name: b.name,
      meta: [baselineDate(b.created_at), baselineAuthor(b), plan && b.plan ? `plan ${b.plan.progress}%` : ''].filter(Boolean).join(' · '),
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

/**
 * The change list's height on a computer: a long comparison scrolls inside
 * it, so the selected change's text below stays on screen (issue #729). A
 * short one is only as tall as its rows.
 */
export const CHANGE_LIST_MAX_HEIGHT = '42vh';

/** The text panel's height under the list; a long text scrolls inside it. */
export const TEXT_CHANGE_MAX_HEIGHT = '42vh';

/** A computer's change table: a sticky header, its own scroll, and ↑ / ↓ to walk it. */
const ChangeGrid: React.FC<{
  rows: ChangeRow[];
  selected: string;
  onSelect: (key: string) => void;
}> = ({ rows, selected, onSelect }) => {
  const refs = React.useRef(new Map<string, HTMLDivElement>());
  // A selection made anywhere (a click, a key) keeps its row in view.
  React.useEffect(() => {
    refs.current.get(selected)?.scrollIntoView?.({ block: 'nearest' });
  }, [selected]);
  const step = (from: string, delta: number) => {
    const at = rows.findIndex((r) => r.key === from);
    const next = rows[Math.min(rows.length - 1, Math.max(0, (at < 0 ? -1 : at) + delta))];
    if (!next) return;
    onSelect(next.key);
    refs.current.get(next.key)?.focus({ preventScroll: true });
  };
  return (
    <section
      aria-label="Changes"
      data-help-key="baselines.changes"
      style={{ ...panelStyle, overflow: 'auto', maxHeight: CHANGE_LIST_MAX_HEIGHT, flex: 'none' }}
    >
      <div role="table" aria-label="Changes" style={{ minWidth: 620 }}>
        <div style={{ position: 'sticky', top: 0, zIndex: 1 }}>
          <GridHead columns={COLUMNS} names={['Change', 'Ref', 'Title', 'Fields']} />
        </div>
        {rows.map((r) => (
          <div
            role="row"
            key={r.key}
            ref={(el) => {
              if (el) refs.current.set(r.key, el);
              else refs.current.delete(r.key);
            }}
            aria-selected={r.key === selected}
            tabIndex={r.key === selected || (!selected && r === rows[0]) ? 0 : -1}
            onClick={() => onSelect(r.key)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect(r.key);
              } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault();
                step(r.key, e.key === 'ArrowDown' ? 1 : -1);
              } else if (e.key === 'Home' || e.key === 'End') {
                e.preventDefault();
                step(r.key, e.key === 'Home' ? -rows.length : rows.length);
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
};

export const ChangeTable: React.FC<{
  rows: ChangeRow[];
  selected: string;
  onSelect: (key: string) => void;
  phone?: boolean;
}> = ({ rows, selected, onSelect, phone }) =>
  phone ? (
    <ChangeList rows={rows} selected={selected} onSelect={onSelect} />
  ) : (
    <ChangeGrid rows={rows} selected={selected} onSelect={onSelect} />
  );

const Diff = WordDiff;

/** The selected change's text, before and after, with the live links it left suspect. */
export const TextChange: React.FC<{
  row: ChangeRow;
  before?: Artifact;
  after?: Artifact;
  suspect: string[];
  /** On a computer, under the change list: at most TEXT_CHANGE_MAX_HEIGHT, scrolling inside. */
  bounded?: boolean;
}> = ({ row, before, after, suspect, bounded }) => {
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
        ...(bounded ? { maxHeight: TEXT_CHANGE_MAX_HEIGHT, overflowY: 'auto', flex: 'none' } : {}),
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
