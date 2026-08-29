import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import {
  Artifact,
  artifactAPI,
  ImpactDirection,
  ImpactGroup,
  ImpactReport,
  vvAPI,
} from '../api/client';
import { useAppStore } from '../state/store';

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
};

const typeLabel = (type: string): string =>
  TYPE_LABELS[type] || (type ? type : 'Unknown');

const DIRECTIONS: { value: ImpactDirection; label: string }[] = [
  { value: 'both', label: 'Both' },
  { value: 'downstream', label: 'Downstream (dependents)' },
  { value: 'upstream', label: 'Upstream (dependencies)' },
];

const badge = (background: string, color = 'var(--surface)'): React.CSSProperties => ({
  display: 'inline-block',
  padding: '1px 7px',
  borderRadius: 8,
  background,
  color,
  fontSize: 11,
  fontWeight: 600,
});

export const ImpactView: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const [searchParams, setSearchParams] = useSearchParams();
  const artifactId = searchParams.get('artifact') || '';

  const [direction, setDirection] = useState<ImpactDirection>('both');
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
      .impact(projectId, artifactId, direction)
      .then((res) => {
        setReport(res.data);
        setError('');
      })
      .catch((err: any) => {
        setReport(null);
        setError(err.response?.data?.error || err.message || 'Failed to load impact');
      })
      .finally(() => setLoading(false));
  }, [projectId, artifactId, direction]);

  const selectArtifact = useCallback(
    (id: string) => {
      const next = new URLSearchParams(searchParams);
      if (id) next.set('artifact', id);
      else next.delete('artifact');
      setSearchParams(next);
    },
    [searchParams, setSearchParams]
  );

  const seed = useMemo(
    () => artifacts.find((a) => a.id === artifactId),
    [artifacts, artifactId]
  );

  const titleOf = useCallback(
    (id: string) => artifacts.find((a) => a.id === id)?.title || id,
    [artifacts]
  );

  const renderGroups = (label: string, hint: string, groups: ImpactGroup[]) => {
    const total = groups.reduce((n, g) => n + g.count, 0);
    return (
      <div style={{ flex: 1, minWidth: 320 }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 4 }}>
          <h3 style={{ margin: 0, color: 'var(--text)' }}>{label}</h3>
          <span style={badge('var(--accent)')}>{total}</span>
        </div>
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>{hint}</div>
        {total === 0 ? (
          <div style={{ color: 'var(--neutral-mid)', fontSize: 13 }}>
            No {label.toLowerCase()} artifacts.
          </div>
        ) : (
          groups.map((group) => (
            <div key={group.type} style={{ marginBottom: 18 }}>
              <div
                style={{
                  fontSize: 12,
                  fontWeight: 700,
                  textTransform: 'uppercase',
                  letterSpacing: 0.4,
                  color: 'var(--text-muted)',
                  marginBottom: 6,
                }}
              >
                {typeLabel(group.type)} ({group.count})
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {group.nodes.map((node) => (
                  <Link
                    key={node.artifact_id}
                    to={`/projects/${projectId}/requirements?artifact=${node.artifact_id}`}
                    title={
                      'Path: ' +
                      node.path.map((id) => titleOf(id)).join('  →  ')
                    }
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                      padding: '8px 10px',
                      background: 'var(--surface-alt)',
                      border: '1px solid var(--border)',
                      borderRadius: 6,
                      textDecoration: 'none',
                      color: 'var(--text)',
                      fontSize: 13,
                    }}
                  >
                    <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {node.title || node.artifact_id}
                    </span>
                    {node.via && (
                      <span style={badge('var(--neutral-soft)', 'var(--text)')}>{node.via}</span>
                    )}
                    <span
                      title={`${node.distance} hop${node.distance === 1 ? '' : 's'} away`}
                      style={badge('var(--tint-blue, var(--surface-inset))', 'var(--text-muted)')}
                    >
                      {node.distance} hop{node.distance === 1 ? '' : 's'}
                    </span>
                  </Link>
                ))}
              </div>
            </div>
          ))
        )}
      </div>
    );
  };

  const showDownstream = direction === 'both' || direction === 'downstream';
  const showUpstream = direction === 'both' || direction === 'upstream';

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 16, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Impact Analysis</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Artifact</label>
          <select
            value={artifactId}
            onChange={(e) => selectArtifact(e.target.value)}
            style={{ minWidth: 260, padding: '6px 10px' }}
          >
            <option value="">Select an artifact…</option>
            {artifacts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.title} ({a.type})
              </option>
            ))}
          </select>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Direction</label>
          <select
            value={direction}
            onChange={(e) => setDirection(e.target.value as ImpactDirection)}
            style={{ minWidth: 200, padding: '6px 10px' }}
          >
            {DIRECTIONS.map((d) => (
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
          Pick an artifact above — or open this view from an artifact's{' '}
          <em>Show impact</em> link — to trace what a change to it would affect.
        </div>
      )}

      {artifactId && report && (
        <>
          <div
            style={{
              background: 'var(--surface-alt)',
              border: '1px solid var(--border)',
              borderRadius: 6,
              padding: '10px 14px',
              marginBottom: 18,
              fontSize: 13,
              color: 'var(--text)',
            }}
          >
            Impact of <strong>{seed?.title || artifactId}</strong>
            {seed && <span style={{ color: 'var(--text-muted)' }}> ({seed.type})</span>} —{' '}
            <strong>{report.total}</strong> affected artifact{report.total === 1 ? '' : 's'}.
          </div>

          <div style={{ display: 'flex', gap: 32, flexWrap: 'wrap', alignItems: 'flex-start' }}>
            {showDownstream &&
              renderGroups(
                'Downstream',
                'Artifacts that depend on this one — what a change here could affect.',
                report.downstream
              )}
            {showUpstream &&
              renderGroups(
                'Upstream',
                'Artifacts this one depends on — its sources and rationale.',
                report.upstream
              )}
          </div>
        </>
      )}
    </div>
  );
};
