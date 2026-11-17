import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Artifact, artifactAPI, ImpactReport, vvAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { SegmentedControl } from '../components/ui';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { useAppStore } from '../state/store';
import { ImpactDepth, impactShape, reportDepth } from './ImpactViewLayout';
import { ImpactViewPicker } from './ImpactViewPicker';
import { ImpactViewTable } from './ImpactViewTable';
import { ImpactViewTree } from './ImpactViewTree';
import type { ColourBy } from './moduleView/TreeDiagramView';
import type { ArtifactTypeValue, FeatureKey } from '../generated/contract';

/**
 * The picker pane, the result table and re-centring (issue #730). Without
 * it the page keeps the dropdown and the tree.
 */
export const IMPACT_PICKER_FEATURE: FeatureKey = 'impact-picker';

// Human labels for artifact type buckets; unknown types fall back to the raw
// value so the view never hides a group it can't name.
const TYPE_LABELS: Record<string, string> = {
  'user-need': 'User Needs',
  requirement: 'Requirements',
  'design-item': 'Design Items',
  'test-case': 'Test Cases',
  hazard: 'Hazards',
  persona: 'Personas',
  heading: 'Headings',
  description: 'Descriptions',
  other: 'Other',
} satisfies Record<ArtifactTypeValue, string>;

const typeLabel = (type: string): string => (TYPE_LABELS[type] || (type ? type : 'Unknown')).replace(/s$/, '');

type ResultView = 'tree' | 'table';

export const ImpactView: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId || '';
  const navigate = useNavigate();
  const picker = useFeature(IMPACT_PICKER_FEATURE);
  const { isPhone } = useViewport();

  const [searchParams, setSearchParams] = useSearchParams();
  const artifactId = searchParams.get('artifact') || '';

  const [depth, setDepth] = useState<ImpactDepth>('all');
  const [colourBy, setColourBy] = useState<ColourBy>('type');
  const [view, setView] = useState<ResultView>('tree');
  // On a phone a preselected artifact opens with the list folded away.
  const [pickerOpen, setPickerOpen] = useState(() => !(isPhone && artifactId));
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [report, setReport] = useState<ImpactReport | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  // The artifact list, for the picker and for each box's ref, title and status.
  useEffect(() => {
    if (!projectId) return;
    artifactAPI
      .list(projectId)
      .then((res) => setArtifacts(res.data || []))
      .catch(() => setArtifacts([]));
  }, [projectId]);

  useEffect(() => {
    if (!projectId || !artifactId) {
      setReport(null);
      return;
    }
    setLoading(true);
    vvAPI
      .impact(projectId, artifactId, 'both')
      .then((res) => {
        setReport(res.data);
        setError('');
      })
      .catch((err: unknown) => {
        setReport(null);
        setError(apiErrorMessage(err, 'Failed to load impact'));
      })
      .finally(() => setLoading(false));
  }, [projectId, artifactId]);

  const selectArtifact = useCallback(
    (id: string) => {
      const next = new URLSearchParams(searchParams);
      if (id) next.set('artifact', id);
      else next.delete('artifact');
      setSearchParams(next);
    },
    [searchParams, setSearchParams]
  );

  const requirements = useMemo(() => artifacts.filter((a) => a.type === 'requirement'), [artifacts]);
  const byId = useMemo(() => {
    const m: Record<string, Artifact> = {};
    artifacts.forEach((a) => {
      m[a.id] = a;
    });
    return m;
  }, [artifacts]);
  const seed = byId[artifactId];
  const titleOf = useCallback((id: string) => byId[id]?.title || id, [byId]);

  const impact = useMemo(
    () =>
      report ? impactShape(report, depth, { title: seed?.title || artifactId, type: seed?.type || 'requirement' }, titleOf) : null,
    [report, depth, seed, artifactId, titleOf]
  );
  const deepest = report ? reportDepth(report) : 0;
  const shown = impact ? new Set(impact.boxes.map((b) => b.id)).size : 0;
  const open = (id: string) => navigate(`/projects/${projectId}/requirements?artifact=${id}`);
  const centre = picker ? selectArtifact : undefined;

  const depthOptions = ([1, 2, 3] as const)
    .filter((d) => d < deepest)
    .map((d) => ({ value: String(d), label: String(d), title: `${d} link${d === 1 ? '' : 's'} away` }))
    .concat([{ value: 'all', label: 'All', title: 'The whole chain' }]);

  const controls = (
    <>
      <span style={controlLabel}>Depth</span>
      <SegmentedControl<string>
        aria-label="Depth"
        value={depth === 'all' || depth >= deepest ? 'all' : String(depth)}
        onChange={(v) => setDepth(v === 'all' ? 'all' : (Number(v) as 1 | 2 | 3))}
        options={depthOptions}
      />
      <span style={controlLabel}>Colour by</span>
      <SegmentedControl<ColourBy>
        aria-label="Colour by"
        value={colourBy}
        onChange={setColourBy}
        options={[
          { value: 'type', label: 'Type' },
          { value: 'status', label: 'Status' },
        ]}
      />
      {picker && (
        <SegmentedControl<ResultView>
          aria-label="Show the impact as"
          value={view}
          onChange={setView}
          options={[
            { value: 'tree', label: 'Tree' },
            { value: 'table', label: 'Table' },
          ]}
        />
      )}
    </>
  );

  const pickerPane = picker && pickerOpen && (
    <ImpactViewPicker
      artifacts={artifacts}
      selectedId={artifactId}
      onPick={(id) => {
        selectArtifact(id);
        if (isPhone) setPickerOpen(false);
      }}
      onCollapse={artifactId ? () => setPickerOpen(false) : undefined}
      phone={isPhone}
    />
  );

  return (
    <div className="ov-gutter" style={{ padding: isPhone ? 12 : 24, display: 'flex', flexDirection: 'column', height: '100%', boxSizing: 'border-box' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 16, flexWrap: 'wrap' }} data-help-key="impact">
        <h2 className="ov-page-title" style={{ color: 'var(--text)', margin: 0 }}>Impact Analysis</h2>
        {!picker && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Requirement</label>
            <select value={artifactId} onChange={(e) => selectArtifact(e.target.value)} style={{ minWidth: 260, padding: '6px 10px' }}>
              <option value="">Select a requirement…</option>
              {requirements.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.ref ? `${a.ref} ` : ''}
                  {a.title}
                </option>
              ))}
            </select>
          </div>
        )}
        {picker && !pickerOpen && (
          <button type="button" onClick={() => setPickerOpen(true)} style={showListButton} title="Show the requirements to pick from">
            {isPhone ? 'Pick a requirement' : '› Show list'}
          </button>
        )}
      </div>

      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: isPhone ? 'column' : 'row', gap: 16 }}>
        {pickerPane}
        <section aria-label="Impact" style={{ flex: 1, minWidth: 0, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {error && <div style={{ color: 'var(--danger)', marginBottom: 10, fontSize: 13 }}>{error}</div>}
          {loading && <div style={{ color: 'var(--text-muted)', marginBottom: 10 }}>Loading…</div>}

          {!artifactId && (
            <div style={{ color: 'var(--text-muted)', fontSize: 14, marginTop: 8 }}>
              Pick a requirement {picker ? (isPhone ? 'from the list' : 'from the list on the left') : 'above'} — or open this view
              from a requirement's <em>Show impact</em> link — to trace everything a change to it would affect, down to its design,
              tests and hazards and up to its needs.
            </div>
          )}

          {artifactId && report && impact && (
            <>
              <div role="status" style={summaryStyle}>
                Impact of <strong>{seed ? `${seed.ref ? seed.ref + ' ' : ''}${seed.title}` : artifactId}</strong> —{' '}
                <strong>{shown}</strong> affected artifact{shown === 1 ? '' : 's'}: {impact.upstream} upstream, {impact.downstream} downstream
                {depth === 'all' || depth >= deepest ? ', through every link' : `, up to ${depth} link${depth === 1 ? '' : 's'} away`}.
              </div>
              {view === 'tree' ? (
                <ImpactViewTree impact={impact} byId={byId} colourBy={colourBy} typeLabel={typeLabel} onOpen={open} onCentre={centre} controls={controls} />
              ) : (
                <>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', paddingBottom: 8 }}>{controls}</div>
                  <ImpactViewTable boxes={impact.boxes} byId={byId} projectId={projectId} typeLabel={typeLabel} onCentre={centre} />
                </>
              )}
            </>
          )}
        </section>
      </div>
    </div>
  );
};

const controlLabel: React.CSSProperties = { fontSize: 12, color: 'var(--text-muted)' };
const summaryStyle: React.CSSProperties = {
  background: 'var(--surface-alt)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  padding: '10px 14px',
  marginBottom: 14,
  fontSize: 13,
  color: 'var(--text)',
};
const showListButton: React.CSSProperties = {
  padding: '4px 10px',
  fontSize: 13,
  border: '1px solid var(--border)',
  borderRadius: 6,
  background: 'var(--surface)',
  color: 'var(--text-secondary)',
  cursor: 'pointer',
};
