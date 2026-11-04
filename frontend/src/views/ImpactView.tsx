import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import { Artifact, artifactAPI, ImpactReport, vvAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { ImpactDepth, layoutImpactTree } from './ImpactViewLayout';
import { ImpactViewTree } from './ImpactViewTree';
import type { ArtifactTypeValue } from '../generated/contract';

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

const typeLabel = (type: string): string =>
  TYPE_LABELS[type] || (type ? type : 'Unknown');

const DEPTHS: { value: ImpactDepth; label: string }[] = [
  { value: 'one', label: '1 level' },
  { value: 'all', label: 'All levels' },
];

export const ImpactView: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const [searchParams, setSearchParams] = useSearchParams();
  const artifactId = searchParams.get('artifact') || '';

  const [depth, setDepth] = useState<ImpactDepth>('all');
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [report, setReport] = useState<ImpactReport | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  // Load the artifact list once for the picker and for resolving titles/types
  // of nodes the report may reference.
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
      .catch((err: any) => {
        setReport(null);
        setError(err.response?.data?.error || err.message || 'Failed to load impact');
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

  // Impact analysis starts from a requirement; the walk from it reaches
  // every artifact type its links lead to.
  const requirements = useMemo(
    () => artifacts.filter((a) => a.type === 'requirement'),
    [artifacts]
  );

  const seed = useMemo(
    () => artifacts.find((a) => a.id === artifactId),
    [artifacts, artifactId]
  );

  const byId = useMemo(() => {
    const m: Record<string, Artifact> = {};
    artifacts.forEach((a) => {
      m[a.id] = a;
    });
    return m;
  }, [artifacts]);

  const titleOf = useCallback((id: string) => byId[id]?.title || id, [byId]);

  const layout = useMemo(
    () => (report ? layoutImpactTree(report, depth, titleOf) : null),
    [report, depth, titleOf]
  );
  const shown = useMemo(
    () => (layout ? new Set(layout.nodes.map((n) => n.id)).size : 0),
    [layout]
  );

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 16, flexWrap: 'wrap' }} data-help-key="impact">
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Impact Analysis</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Requirement</label>
          <select
            value={artifactId}
            onChange={(e) => selectArtifact(e.target.value)}
            style={{ minWidth: 260, padding: '6px 10px' }}
          >
            <option value="">Select a requirement…</option>
            {requirements.map((a) => (
              <option key={a.id} value={a.id}>
                {a.ref ? `${a.ref} ` : ''}{a.title}
              </option>
            ))}
          </select>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Depth</label>
          <select
            value={depth}
            onChange={(e) => setDepth(e.target.value as ImpactDepth)}
            style={{ minWidth: 120, padding: '6px 10px' }}
          >
            {DEPTHS.map((d) => (
              <option key={d.value} value={d.value}>
                {d.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      {error && <div style={{ color: 'var(--danger)', marginBottom: 10, fontSize: 13 }}>{error}</div>}
      {loading && <div style={{ color: 'var(--text-muted)', marginBottom: 10 }}>Loading…</div>}

      {!artifactId && (
        <div style={{ color: 'var(--text-muted)', fontSize: 14, marginTop: 8 }}>
          Pick a requirement above — or open this view from a requirement's{' '}
          <em>Show impact</em> link — to trace everything a change to it would
          affect, down to its design, tests and hazards and up to its needs.
        </div>
      )}

      {artifactId && report && layout && (
        <>
          <div
            style={{
              background: 'var(--surface-alt)',
              border: '1px solid var(--border)',
              borderRadius: 6,
              padding: '10px 14px',
              marginBottom: 14,
              fontSize: 13,
              color: 'var(--text)',
            }}
          >
            Impact of <strong>{seed?.title || artifactId}</strong> —{' '}
            <strong>{shown}</strong> affected artifact{shown === 1 ? '' : 's'}
            {depth === 'one' ? ' one link away' : ' through every link'}.
          </div>
          <ImpactViewTree
            layout={layout}
            seed={{ id: artifactId, title: seed?.title || artifactId, ref: seed?.ref }}
            projectId={projectId || ''}
            byId={byId}
            typeLabel={(t) => typeLabel(t).replace(/s$/, '')}
          />
        </>
      )}
    </div>
  );
};
