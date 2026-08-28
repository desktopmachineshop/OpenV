import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Artifact,
  artifactAPI,
  Baseline,
  baselineAPI,
  CoverageReport,
  TestRun,
  vvAPI,
} from '../api/client';
import { useAppStore } from '../state/store';

// ---------------------------------------------------------------------------
// Shared color helpers for V&V rollup statuses
// ---------------------------------------------------------------------------

export const rollupColor = (status: string): string => {
  switch ((status || '').toLowerCase()) {
    case 'pass':
      return 'var(--success)';
    case 'fail':
      return 'var(--danger)';
    case 'blocked':
      return 'var(--warning)';
    default:
      // uncovered / unrun / method-missing / not-run
      return 'var(--neutral)';
  }
};

const chipStyle = (color: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 12,
  background: color,
  color: '#fff',
  fontSize: 12,
  fontWeight: 600,
  whiteSpace: 'nowrap',
});

const runStatusColor = (status: string): string => {
  switch (status) {
    case 'in-progress':
      return 'var(--accent)';
    case 'completed':
      return 'var(--success)';
    case 'aborted':
      return 'var(--danger)';
    default:
      return 'var(--neutral)';
  }
};

const SUMMARY_ORDER = ['pass', 'fail', 'blocked', 'unrun', 'uncovered', 'method-missing'];

export const VVDashboard: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [baselineId, setBaselineId] = useState<string>('live');
  const [coverage, setCoverage] = useState<CoverageReport | null>(null);
  const [gaps, setGaps] = useState<Record<string, string[]>>({});
  const [runs, setRuns] = useState<TestRun[]>([]);
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // New run form state
  const [showNewRun, setShowNewRun] = useState(false);
  const [runName, setRunName] = useState('');
  const [runDescription, setRunDescription] = useState('');
  const [runBaselineId, setRunBaselineId] = useState('');
  const [creating, setCreating] = useState(false);

  const effectiveBaseline = baselineId === 'live' ? undefined : baselineId;

  // Load baselines + artifact map once
  useEffect(() => {
    if (!projectId) return;
    baselineAPI
      .list(projectId)
      .then((res) => setBaselines(res.data || []))
      .catch(() => setBaselines([]));
    artifactAPI
      .list(projectId)
      .then((res) => {
        const map: Record<string, Artifact> = {};
        (res.data || []).forEach((a) => {
          map[a.id] = a;
        });
        setArtifactMap(map);
      })
      .catch(() => setArtifactMap({}));
  }, [projectId]);

  const loadVV = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    setError('');
    Promise.all([
      vvAPI.coverage(projectId, effectiveBaseline),
      vvAPI.gaps(projectId, effectiveBaseline),
      vvAPI.listRuns(projectId),
    ])
      .then(([cov, gp, rn]) => {
        setCoverage(cov.data);
        setGaps(gp.data || {});
        setRuns(rn.data || []);
      })
      .catch((err: any) => {
        setError(err.response?.data?.error || err.message || 'Failed to load V&V data');
      })
      .finally(() => setLoading(false));
  }, [projectId, effectiveBaseline]);

  useEffect(() => {
    loadVV();
  }, [loadVV]);

  // Memoized so the object identity is stable between renders — a bare
  // `coverage?.summary || {}` would be a fresh object every render and make
  // the summaryKeys useMemo below recompute unnecessarily (and trips
  // react-hooks/exhaustive-deps).
  const summary = useMemo(() => coverage?.summary || {}, [coverage]);
  const summaryKeys = useMemo(() => {
    const keys = Object.keys(summary);
    keys.sort((a, b) => {
      const ia = SUMMARY_ORDER.indexOf(a);
      const ib = SUMMARY_ORDER.indexOf(b);
      return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib);
    });
    return keys;
  }, [summary]);
  const summaryTotal = summaryKeys.reduce((sum, k) => sum + (summary[k] || 0), 0);

  const artifactTitle = (id: string) => artifactMap[id]?.title || id;

  const handleCreateRun = async () => {
    if (!runName.trim() || !projectId) return;
    setCreating(true);
    try {
      await vvAPI.createRun(projectId, {
        name: runName.trim(),
        description: runDescription,
        baseline_id: runBaselineId || undefined,
      });
      setRunName('');
      setRunDescription('');
      setRunBaselineId('');
      setShowNewRun(false);
      loadVV();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to create run');
    } finally {
      setCreating(false);
    }
  };

  const handleRunStatus = async (runId: string, status: string) => {
    try {
      await vvAPI.updateRun(runId, status);
      loadVV();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to update run');
    }
  };

  const gapEntries = Object.entries(gaps).filter(([, ids]) => ids && ids.length > 0);

  return (
    <div style={{ padding: 24 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 16, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Verification &amp; Validation</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Baseline</label>
          <select
            value={baselineId}
            onChange={(e) => setBaselineId(e.target.value)}
            style={{ width: 220, padding: '6px 10px' }}
          >
            <option value="live">Live</option>
            {baselines.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </div>
        <div style={{ flex: 1 }} />
        <button
          className="button-secondary"
          onClick={() => vvAPI.report(projectId, effectiveBaseline)}
        >
          Download V&amp;V report (PDF)
        </button>
      </div>

      {baselineId !== 'live' && (
        <div
          style={{
            background: 'var(--tint-yellow)',
            border: '1px solid var(--warning)',
            color: 'var(--warning-text)',
            borderRadius: 4,
            padding: '10px 14px',
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          Viewing baseline "{baselines.find((b) => b.id === baselineId)?.name || baselineId}" — this
          is a read-only historical snapshot.
        </div>
      )}

      {error && (
        <div style={{ color: 'var(--danger)', marginBottom: 12, fontSize: 13 }}>{error}</div>
      )}
      {loading && <div style={{ color: 'var(--text-muted)', marginBottom: 12 }}>Loading…</div>}

      {/* Summary cards */}
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 12 }}>
        {summaryKeys.map((key) => (
          <div
            key={key}
            className="card"
            style={{ padding: '14px 20px', marginBottom: 0, minWidth: 120, textAlign: 'center' }}
          >
            <div style={{ fontSize: 26, fontWeight: 700, color: rollupColor(key) }}>
              {summary[key]}
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'capitalize' }}>{key}</div>
          </div>
        ))}
        {summaryKeys.length === 0 && !loading && (
          <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No coverage data yet.</div>
        )}
      </div>

      {/* Stacked bar */}
      {summaryTotal > 0 && (
        <div
          style={{
            display: 'flex',
            height: 16,
            borderRadius: 8,
            overflow: 'hidden',
            marginBottom: 24,
            maxWidth: 720,
            border: '1px solid var(--border)',
          }}
        >
          {summaryKeys.map((key) =>
            summary[key] > 0 ? (
              <div
                key={key}
                title={`${key}: ${summary[key]}`}
                style={{
                  width: `${(summary[key] / summaryTotal) * 100}%`,
                  background: rollupColor(key),
                }}
              />
            ) : null
          )}
        </div>
      )}

      {/* Coverage table */}
      <div className="card">
        <h3>Requirement coverage</h3>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>
                {['Requirement', 'Method', 'Rollup'].map((h) => (
                  <th
                    key={h}
                    style={{
                      textAlign: 'left',
                      borderBottom: '2px solid var(--neutral-soft)',
                      padding: '8px 10px',
                      color: 'var(--text-muted)',
                      fontWeight: 600,
                    }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(coverage?.entries || []).map((entry) => (
                <tr key={entry.requirement_id}>
                  <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)' }}>
                    {entry.title || artifactTitle(entry.requirement_id)}
                  </td>
                  <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                    {entry.verification_method || <span style={{ color: 'var(--danger)' }}>missing</span>}
                  </td>
                  <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)' }}>
                    <span style={chipStyle(rollupColor(entry.rollup))}>{entry.rollup}</span>
                  </td>
                </tr>
              ))}
              {(coverage?.entries || []).length === 0 && !loading && (
                <tr>
                  <td colSpan={3} style={{ padding: 12, color: 'var(--text-muted)' }}>
                    No requirements found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Gaps */}
      <h3 style={{ color: 'var(--text)', marginBottom: 10 }}>Gaps</h3>
      {gapEntries.length === 0 && !loading && (
        <div style={{ color: 'var(--success)', fontSize: 13, marginBottom: 20 }}>
          No gaps detected — nice work.
        </div>
      )}
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 8 }}>
        {gapEntries.map(([label, ids]) => (
          <div key={label} className="card" style={{ flex: '1 1 260px', marginBottom: 12 }}>
            <h3 style={{ fontSize: 14, textTransform: 'capitalize' }}>
              {label.replace(/[-_]/g, ' ')}{' '}
              <span style={{ color: 'var(--danger)', fontWeight: 700 }}>({ids.length})</span>
            </h3>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13, color: 'var(--text-body)' }}>
              {ids.map((id) => (
                <li key={id} style={{ marginBottom: 3 }}>
                  {artifactTitle(id)}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>

      {/* Test runs */}
      <div className="card">
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
          <h3 style={{ margin: 0, flex: 1 }}>Test runs</h3>
          <button className="button" onClick={() => setShowNewRun(!showNewRun)}>
            {showNewRun ? 'Cancel' : 'New Run'}
          </button>
        </div>

        {showNewRun && (
          <div
            style={{
              border: '1px solid var(--neutral-soft)',
              borderRadius: 4,
              padding: 16,
              marginBottom: 16,
              background: 'var(--surface-alt)',
            }}
          >
            <div className="form-group">
              <label>Name</label>
              <input
                value={runName}
                onChange={(e) => setRunName(e.target.value)}
                placeholder="e.g. Design verification — rev B"
              />
            </div>
            <div className="form-group">
              <label>Description</label>
              <textarea
                value={runDescription}
                onChange={(e) => setRunDescription(e.target.value)}
                style={{ minHeight: 60 }}
              />
            </div>
            <div className="form-group">
              <label>Baseline (optional)</label>
              <select value={runBaselineId} onChange={(e) => setRunBaselineId(e.target.value)}>
                <option value="">None (live artifacts)</option>
                {baselines.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.name}
                  </option>
                ))}
              </select>
            </div>
            <button className="button" onClick={handleCreateRun} disabled={creating || !runName.trim()}>
              {creating ? 'Creating…' : 'Create run'}
            </button>
          </div>
        )}

        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr>
              {['Name', 'Status', 'Started', 'Baseline', ''].map((h, i) => (
                <th
                  key={i}
                  style={{
                    textAlign: 'left',
                    borderBottom: '2px solid var(--neutral-soft)',
                    padding: '8px 10px',
                    color: 'var(--text-muted)',
                    fontWeight: 600,
                  }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {runs.map((run) => (
              <tr key={run.id}>
                <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)' }}>
                  <Link
                    to={`/projects/${projectId}/vv/runs/${run.id}`}
                    style={{ color: 'var(--accent)', textDecoration: 'none', fontWeight: 600 }}
                  >
                    {run.name}
                  </Link>
                  {run.description && (
                    <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>{run.description}</div>
                  )}
                </td>
                <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)' }}>
                  <span style={chipStyle(runStatusColor(run.status))}>{run.status}</span>
                </td>
                <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                  {run.started_at ? new Date(run.started_at).toLocaleString() : '—'}
                </td>
                <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                  {run.baseline_id
                    ? baselines.find((b) => b.id === run.baseline_id)?.name || run.baseline_id
                    : 'Live'}
                </td>
                <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--neutral-soft)', whiteSpace: 'nowrap' }}>
                  {run.status === 'in-progress' && (
                    <>
                      <button
                        className="button"
                        style={{ padding: '4px 10px', fontSize: 12, marginRight: 6 }}
                        onClick={() => handleRunStatus(run.id, 'completed')}
                      >
                        Complete
                      </button>
                      <button
                        style={{
                          padding: '4px 10px',
                          fontSize: 12,
                          background: 'var(--danger)',
                          color: '#fff',
                          border: 'none',
                          borderRadius: 4,
                          cursor: 'pointer',
                        }}
                        onClick={() => handleRunStatus(run.id, 'aborted')}
                      >
                        Abort
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ))}
            {runs.length === 0 && !loading && (
              <tr>
                <td colSpan={5} style={{ padding: 12, color: 'var(--text-muted)' }}>
                  No test runs yet. Create one to start recording results.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
