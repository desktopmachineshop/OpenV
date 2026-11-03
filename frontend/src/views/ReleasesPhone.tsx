import React, { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { ReleaseProgress } from '../api/client';
import { Chip, PanelHeader, ellipsis, mutedStyle, pageType, panelStyle, refStyle } from '../components/ui';
import { BurnUpChart, REVIEW_LABEL, ROLLUP_LABEL, RollupBar, STATUS_LABEL, STATUS_TONE, percent, rangeLabel } from './ReleasesParts';

// The Releases page on a phone (TestsPage's lead): it opens on the current
// release's progress, its figures as tiles and what is failing in it first,
// then its burn-up and its requirements; the list of releases, with Add
// release and the controls, is the other tab. Choosing a release in the list
// opens its progress.

type Tab = 'progress' | 'list';

const tabStyle = (on: boolean): React.CSSProperties => ({
  minHeight: 40,
  padding: '0 12px',
  border: 'none',
  borderBottom: `2px solid ${on ? 'var(--accent)' : 'transparent'}`,
  background: 'transparent',
  color: on ? 'var(--text)' : 'var(--text-muted)',
  fontSize: 14,
  fontWeight: on ? 600 : 500,
  cursor: 'pointer',
});

const tile: React.CSSProperties = {
  ...panelStyle,
  display: 'flex',
  flexDirection: 'column',
  padding: 8,
};

const Tiles: React.FC<{ progress: ReleaseProgress }> = ({ progress: { summary: s } }) => (
  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(0, 1fr))', gap: 6 }}>
    <div style={tile}>
      <b style={{ fontSize: 18 }}>{s.total}</b>
      <span style={mutedStyle}>requirements</span>
    </div>
    <div style={tile}>
      <b style={{ fontSize: 18 }}>{percent(s.approved, s.total)}%</b>
      <span style={mutedStyle}>approved</span>
    </div>
    <div style={tile}>
      <b style={{ fontSize: 18 }}>{percent(s.verified, s.total)}%</b>
      <span style={mutedStyle}>verified</span>
    </div>
    <div style={tile}>
      <b style={{ fontSize: 18, color: 'var(--success-text)' }}>{percent(s.ready, s.total)}%</b>
      <span style={mutedStyle}>ready</span>
    </div>
  </div>
);

const Failing: React.FC<{ progress: ReleaseProgress; projectId: string }> = ({ progress, projectId }) => {
  const failing = progress.entries.filter((e) => e.rollup === 'fail' || e.rollup === 'blocked');
  return (
    <section aria-label="Failing or blocked" style={panelStyle}>
      <PanelHeader title="Failing or blocked" count={failing.length} />
      {failing.length === 0 ? (
        <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>Nothing in {progress.release.name} is failing or blocked.</p>
      ) : (
        failing.map((e) => (
          <Link
            key={e.requirement_id}
            to={`/projects/${projectId}/requirements?artifact=${e.requirement_id}`}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              minHeight: 48,
              padding: '6px 12px',
              borderBottom: '1px solid var(--border-soft)',
              color: 'var(--text)',
              textDecoration: 'none',
            }}
          >
            <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
              <span style={refStyle}>{e.ref}</span>
              <span style={ellipsis}>{e.title}</span>
            </span>
            <Chip>{e.rollup === 'fail' ? 'Fail' : 'Blocked'}</Chip>
          </Link>
        ))
      )}
    </section>
  );
};

/** The release's requirements as two-line rows: ref and title, then review, V&V and range. */
const Requirements: React.FC<{ progress: ReleaseProgress; projectId: string; names: Record<string, string> }> = ({ progress, projectId, names }) => (
  <section aria-label={`Requirements in ${progress.release.name}`} style={panelStyle}>
    <PanelHeader title={`Requirements in ${progress.release.name}`} count={progress.entries.length} />
    {progress.entries.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No requirement targets this release yet.</p>}
    {progress.entries.map((e) => (
      <Link
        key={e.requirement_id}
        to={`/projects/${projectId}/requirements?artifact=${e.requirement_id}`}
        style={{ display: 'flex', flexDirection: 'column', gap: 4, padding: '8px 12px', borderBottom: '1px solid var(--border-soft)', color: 'var(--text)', textDecoration: 'none' }}
      >
        <span style={ellipsis}>
          <span style={refStyle}>{e.ref}</span> {e.title}
        </span>
        <span style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
          <Chip>{REVIEW_LABEL[e.status] || e.status}</Chip>
          <Chip tone={e.rollup === 'verified-manually' ? 'success' : undefined}>{ROLLUP_LABEL[e.rollup] || e.rollup}</Chip>
          <span style={mutedStyle}>
            {rangeLabel(e.range, names)}
            {e.range.inherited_from ? ' (inherited)' : ''}
          </span>
        </span>
      </Link>
    ))}
  </section>
);

interface ReleasesPhoneProps {
  head: React.ReactNode;
  form: React.ReactNode;
  list: React.ReactNode;
  unassigned: React.ReactNode;
  error: React.ReactNode;
  loading: boolean;
  progress: ReleaseProgress | null;
  projectId: string;
  names: Record<string, string>;
}

export const ReleasesPhone: React.FC<ReleasesPhoneProps> = ({ head, form, list, unassigned, error, loading, progress, projectId, names }) => {
  const [tab, setTab] = useState<Tab>('progress');
  // Choosing another release in the list opens its progress.
  const shown = progress?.release.id || '';
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    if (shown) setTab('progress');
  }, [shown]);
  const tabs: [Tab, string][] = [
    ['progress', progress ? `${progress.release.name} progress` : 'Progress'],
    ['list', 'Releases'],
  ];
  return (
    <div data-help-key="releases.progress" style={{ ...pageType(true), padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div role="tablist" aria-label="Releases view" style={{ display: 'flex', gap: 2 }}>
        {tabs.map(([t, label]) => (
          <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)} style={tabStyle(tab === t)}>
            {label}
          </button>
        ))}
      </div>
      {error}
      {loading && <div style={mutedStyle}>Loading…</div>}
      {tab === 'list' ? (
        <>
          {head}
          {form}
          {list}
          {unassigned}
        </>
      ) : progress ? (
        <section aria-labelledby="release-detail" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <h2 id="release-detail" style={{ margin: 0, fontSize: 17, fontWeight: 700 }}>
              {progress.release.name}: progress
            </h2>
            <Chip tone={STATUS_TONE[progress.release.status]}>{STATUS_LABEL[progress.release.status]}</Chip>
            {progress.release.target_date && <span style={mutedStyle}>Target {progress.release.target_date}</span>}
          </div>
          <Tiles progress={progress} />
          <RollupBar counts={progress.summary.by_rollup} total={progress.summary.total} />
          <Failing progress={progress} projectId={projectId} />
          {progress.burn_up && progress.burn_up.length > 0 && (
            <section aria-label="Burn-up" style={panelStyle} data-help-key="releases.burnup">
              <PanelHeader title="Burn-up" />
              <div style={{ padding: 8 }}>
                <BurnUpChart points={progress.burn_up} targetDate={progress.release.target_date} />
              </div>
            </section>
          )}
          <Requirements progress={progress} projectId={projectId} names={names} />
        </section>
      ) : (
        !loading && (
          <p style={{ ...mutedStyle, margin: 0 }}>
            No releases yet. <button type="button" onClick={() => setTab('list')} style={{ background: 'none', border: 'none', padding: 0, font: 'inherit', color: 'var(--accent-text)', cursor: 'pointer', minHeight: 32 }}>Add one</button> such as MVP.
          </p>
        )
      )}
    </div>
  );
};
