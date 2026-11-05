// The Baselines page: the project's baselines as a timeline from the working
// copy back, one comparison between two of them (or a baseline and the working
// copy) with its added, changed and removed counts, the changes as a list, and
// the text of a selected change word by word. It reads the baselines, the
// baseline diff, the snapshots the diff compares and the live links. Gated by
// redesign-pages; the full comparison stays at baselines/:id/compare.
import React, { useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  artifactAPI,
  baselineAPI,
  linkAPI,
  type Artifact,
  type Baseline,
  type BaselineDiff,
  type Link as TraceLink,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Chip, ErrorBanner, FeatureGate, SegmentedControl, mutedStyle, pageType, panelStyle, plainLink, tapTarget } from '../components/ui';
import { REDESIGN_PAGES_FEATURE } from '../features';
import { useViewport } from '../hooks/useViewport';
import { ChangeTable, LIVE, TextChange, Timeline } from './BaselinesPageParts';
import { changeRows, filterRows, isStatusOnly, refMap, type ChangeFilter } from './BaselinesPageData';

interface Base {
  baselines: Baseline[];
  live: Artifact[];
  links: TraceLink[];
}

interface Comparison {
  diff: BaselineDiff;
  before: Artifact[];
  after: Artifact[];
}

const nameOf = (id: string, baselines: Baseline[]) => (id === LIVE ? 'Working copy' : baselines.find((b) => b.id === id)?.name || '');

const BaselinesContent: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { isPhone } = useViewport();
  const [data, setData] = useState<Base | null>(null);
  const [cmp, setCmp] = useState<Comparison | null>(null);
  const [error, setError] = useState('');
  const [base, setBase] = useState('');
  const [target, setTarget] = useState(LIVE);
  const [filter, setFilter] = useState<ChangeFilter>('all');
  const [hideStatusOnly, setHideStatusOnly] = useState(false);
  const [selected, setSelected] = useState('');

  useEffect(() => {
    let live = true;
    Promise.all([baselineAPI.list(projectId), artifactAPI.list(projectId), linkAPI.list(projectId)])
      .then(([b, a, l]) => {
        if (!live) return;
        const baselines = [...(b.data || [])].sort((x, y) => y.created_at.localeCompare(x.created_at));
        setData({ baselines, live: a.data, links: l.data || [] });
        setBase(baselines[0]?.id || '');
      })
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load the baselines')));
    return () => {
      live = false;
    };
  }, [projectId]);

  useEffect(() => {
    if (!base || !data) return;
    let live = true;
    setCmp(null);
    Promise.all([
      baselineAPI.diff(base, target),
      baselineAPI.get(base),
      target === LIVE ? Promise.resolve(null) : baselineAPI.get(target),
    ])
      .then(([d, before, after]) => {
        if (!live) return;
        setCmp({ diff: d.data, before: before.data.artifacts || [], after: after ? after.data.artifacts || [] : data.live });
        setSelected('');
      })
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to compare the baselines')));
    return () => {
      live = false;
    };
  }, [base, target, data]);

  const rows = useMemo(() => (cmp ? changeRows(cmp.diff, refMap(cmp.before, cmp.after)) : []), [cmp]);
  const shown = useMemo(() => filterRows(rows, filter, hideStatusOnly), [rows, filter, hideStatusOnly]);

  if (error) return <ErrorBanner message={error} />;
  if (!data) return <div style={{ padding: 16, color: 'var(--text-muted)' }}>Loading…</div>;

  const pick = (id: string) => {
    if (id === LIVE) {
      if (base) setTarget(LIVE);
      return;
    }
    if (id === target) setTarget(base || LIVE);
    setBase(id);
  };
  const row = shown.find((r) => r.key === selected) || shown.find((r) => r.kind === 'changed') || shown[0];
  const before = row && cmp?.before.find((a) => a.id === row.id);
  const after = row && cmp?.after.find((a) => a.id === row.id);
  const refs = refMap(data.live);
  const suspect =
    row && target === LIVE
      ? data.links
          .filter((l) => l.suspect && (l.from_id === row.id || l.to_id === row.id))
          .map((l) => refs.get(l.from_id === row.id ? l.to_id : l.from_id) || '')
          .filter(Boolean)
      : [];
  const counts = cmp ? { added: cmp.diff.added.length, changed: cmp.diff.modified.length, removed: cmp.diff.removed.length } : null;
  const linkCount = cmp ? cmp.diff.links_added.length + cmp.diff.links_removed.length : 0;
  const textCount = filterRows(rows, 'text').length;
  const statusOnlyCount = rows.filter(isStatusOnly).length;

  const timeline = (
    <Timeline baselines={data.baselines} liveCount={data.live.length} base={base} target={target} onPick={pick} phone={isPhone} />
  );
  const header = base && (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }} data-help-key="baselines.compare">
      <h1 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>
        {nameOf(base, data.baselines)} → {nameOf(target, data.baselines)}
      </h1>
      {counts && (
        <>
          <Chip tone="success">+{counts.added} added</Chip>
          <Chip tone="info">{counts.changed} changed</Chip>
          <Chip tone="danger">{counts.removed} removed</Chip>
        </>
      )}
      <span style={{ flexGrow: 1 }} />
      <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-muted)' }}>
        Compare to
        <select value={target} onChange={(e) => setTarget(e.target.value)} style={{ margin: 0, width: 'auto', fontSize: 12 }}>
          <option value={LIVE}>Working copy</option>
          {data.baselines
            .filter((b) => b.id !== base)
            .map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
        </select>
      </label>
      <SegmentedControl<ChangeFilter>
        aria-label="Show"
        value={filter}
        onChange={setFilter}
        options={[
          { value: 'all', label: `All ${rows.length}` },
          { value: 'text', label: `Text only ${textCount}` },
          { value: 'links', label: `Links ${linkCount}` },
        ]}
      />
      {statusOnlyCount > 0 && (
        <label
          title="Leave out the artifacts whose only change is their review status, such as a bulk approval"
          style={{ display: 'flex', alignItems: 'center', gap: 6, margin: 0, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}
        >
          <input
            type="checkbox"
            checked={hideStatusOnly}
            onChange={(e) => setHideStatusOnly(e.target.checked)}
            style={{ margin: 0, width: 'auto' }}
          />
          Hide status-only {statusOnlyCount}
        </label>
      )}
    </div>
  );
  const comparison = !base ? null : !cmp ? (
    <p style={mutedStyle}>Comparing…</p>
  ) : (
    <>
      <ChangeTable rows={shown} selected={row?.key || ''} onSelect={setSelected} phone={isPhone} />
      {row && <TextChange row={row} before={before} after={after} suspect={suspect} bounded={!isPhone} />}
      <Link to={`/projects/${projectId}/baselines/${base}/compare?against=${target}`} style={{ ...plainLink, ...tapTarget, alignSelf: 'flex-start', fontSize: 12 }}>
        Open the full comparison
      </Link>
    </>
  );

  if (isPhone) {
    return (
      <div data-help-pane="baselines" style={{ ...pageType(true), padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
        {base && <section style={{ ...panelStyle, padding: 12 }}>{header}</section>}
        {comparison}
        {timeline}
      </div>
    );
  }
  return (
    <div data-help-pane="baselines" style={{ ...pageType(false), display: 'flex', flexWrap: 'wrap', alignItems: 'stretch', minHeight: '100%' }}>
      {timeline}
      <section
        aria-label="Comparison"
        style={{ flex: '999 1 560px', minWidth: 0, padding: 16, display: 'flex', flexDirection: 'column', gap: 12 }}
      >
        {header || <p style={mutedStyle}>Capture a baseline to compare the project against it.</p>}
        {comparison}
      </section>
    </div>
  );
};

export const BaselinesPage: React.FC = () => (
  <FeatureGate feature={REDESIGN_PAGES_FEATURE} name="Baselines">
    <BaselinesContent />
  </FeatureGate>
);
