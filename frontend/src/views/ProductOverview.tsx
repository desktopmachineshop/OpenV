import React, { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  artifactAPI,
  guidedAPI,
  interviewsAPI,
  productProfileAPI,
  Artifact,
  GuidedSession,
  Interview,
  InterviewSession,
  ProductProfile,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { isUntouchedByWizard } from '../components/wizard/assistantSession';
import { ErrorBanner } from '../components/ui';

interface MetricRow {
  name: string;
  target: string;
  current: string;
  unit: string;
}

interface ConstraintRow {
  title: string;
  description: string;
}

const sectionTitle: React.CSSProperties = {
  fontSize: 16,
  fontWeight: 600,
  color: 'var(--text)',
  marginBottom: 12,
};

const smallInput: React.CSSProperties = { padding: '6px 8px', fontSize: 13 };

const chip = (bg: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 8px',
  borderRadius: 10,
  fontSize: 11,
  fontWeight: 600,
  color: '#fff',
  background: bg,
});

export const ProductOverview: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const [profile, setProfile] = useState<ProductProfile | null>(null);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Product definition edit state
  const [editingDef, setEditingDef] = useState(false);
  const [vision, setVision] = useState('');
  const [problem, setProblem] = useState('');
  const [targetUsers, setTargetUsers] = useState('');
  const [savingDef, setSavingDef] = useState(false);

  // Metrics / constraints edit state
  const [metrics, setMetrics] = useState<MetricRow[]>([]);
  const [constraints, setConstraints] = useState<ConstraintRow[]>([]);
  const [savingMetrics, setSavingMetrics] = useState(false);
  const [savingConstraints, setSavingConstraints] = useState(false);

  // Guided definition sessions (drives the guided CTA label)
  const [guidedSessions, setGuidedSessions] = useState<GuidedSession[]>([]);

  // Interviews summary (the full feature lives on the Interviews page)
  const [interviews, setInterviews] = useState<Interview[]>([]);
  const [recentSessions, setRecentSessions] = useState<InterviewSession[]>([]);

  const load = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const [profileRes, artifactsRes, interviewsRes, guidedRes, sessionsRes] = await Promise.all([
        productProfileAPI.get(projectId),
        artifactAPI.list(projectId),
        interviewsAPI.list(projectId).catch(() => ({ data: [] as Interview[] })),
        guidedAPI.list(projectId).catch(() => ({ data: [] as GuidedSession[] })),
        // Best-effort peek at the latest sessions across all interviews —
        // purely informational, so failures are swallowed.
        interviewsAPI
          .listProjectSessions(projectId, 3)
          .catch(() => ({ data: [] as InterviewSession[] })),
      ]);
      const p = profileRes.data;
      setProfile(p);
      setVision(p.vision || '');
      setProblem(p.problem_statement || '');
      setTargetUsers(p.target_users || '');
      setMetrics(
        (p.success_metrics || []).map((m) => ({
          name: String(m.name ?? ''),
          target: String(m.target ?? ''),
          current: String(m.current ?? ''),
          unit: String(m.unit ?? ''),
        }))
      );
      setConstraints(
        (p.constraints || []).map((c) => ({
          title: String(c.title ?? ''),
          description: String(c.description ?? ''),
        }))
      );
      setArtifacts(artifactsRes.data || []);
      setInterviews(interviewsRes.data || []);
      setGuidedSessions(guidedRes.data || []);
      setRecentSessions(sessionsRes.data || []);
      setError('');
    } catch (err: any) {
      setError(`Failed to load product overview: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const saveDefinition = async () => {
    if (!projectId) return;
    setSavingDef(true);
    try {
      const res = await productProfileAPI.update(projectId, {
        vision,
        problem_statement: problem,
        target_users: targetUsers,
      });
      setProfile(res.data);
      setEditingDef(false);
      setError('');
    } catch (err: any) {
      setError(`Failed to save product definition: ${apiErrorMessage(err)}`);
    } finally {
      setSavingDef(false);
    }
  };

  const saveMetrics = async () => {
    if (!projectId) return;
    setSavingMetrics(true);
    try {
      const res = await productProfileAPI.update(projectId, {
        success_metrics: metrics as unknown as Record<string, any>[],
      });
      setProfile(res.data);
      setError('');
    } catch (err: any) {
      setError(`Failed to save metrics: ${apiErrorMessage(err)}`);
    } finally {
      setSavingMetrics(false);
    }
  };

  const saveConstraints = async () => {
    if (!projectId) return;
    setSavingConstraints(true);
    try {
      const res = await productProfileAPI.update(projectId, {
        constraints: constraints as unknown as Record<string, any>[],
      });
      setProfile(res.data);
      setError('');
    } catch (err: any) {
      setError(`Failed to save constraints: ${apiErrorMessage(err)}`);
    } finally {
      setSavingConstraints(false);
    }
  };

  const personas = artifacts.filter((a) => a.type === 'persona');
  const userNeeds = artifacts.filter((a) => a.type === 'user-need');

  const interviewName = (id: string) => interviews.find((iv) => iv.id === id)?.name || 'Interview';

  if (loading) {
    return <div style={{ padding: 32, color: 'var(--text-muted)' }}>Loading product overview…</div>;
  }

  // Label the guided CTA by where the project actually is: a session in
  // progress resumes, a committed one reopens for modification.
  //
  // A session the assistant created to hold its conversation, never opened in
  // the wizard, is not progress — offering to "resume" it would send the user
  // into an empty step one they never started.
  const guidedInProgress = guidedSessions.some(
    (s) =>
      (s.status === 'in-progress' || s.status === 'in_progress') && !isUntouchedByWizard(s)
  );
  const guidedCommitted = guidedSessions.some((s) => s.status === 'committed');
  const guidedCtaLabel = guidedInProgress
    ? 'Resume guided definition'
    : guidedCommitted
    ? 'Modify guided definition'
    : 'Start guided definition';

  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: '0 auto' }}>
      <h2 style={{ color: 'var(--text)', marginBottom: 4 }}>Product Overview</h2>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 20 }}>
        The single source of truth for what this product is and how success is measured.
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />

      {/* Quick links */}
      <div style={{ display: 'flex', gap: 10, marginBottom: 20, flexWrap: 'wrap' }}>
        <Link to="guided" className="button" style={{ background: 'var(--accent)', textDecoration: 'none' }}>
          {guidedCtaLabel}
        </Link>
        <Link to="vv" className="button-secondary" style={{ textDecoration: 'none' }}>
          V&amp;V dashboard
        </Link>
        <Link to="board" className="button-secondary" style={{ textDecoration: 'none' }}>
          Open board
        </Link>
      </div>

      {/* Product definition */}
      <div className="card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <div style={{ ...sectionTitle, marginBottom: 0 }}>Product definition</div>
          {!editingDef && (
            <button className="button-secondary" style={{ padding: '6px 14px' }} onClick={() => setEditingDef(true)}>
              Edit
            </button>
          )}
        </div>
        {editingDef ? (
          <>
            <div className="form-group">
              <label>Vision</label>
              <textarea rows={3} value={vision} onChange={(e) => setVision(e.target.value)} placeholder="What does the world look like once this product succeeds?" />
            </div>
            <div className="form-group">
              <label>Problem statement</label>
              <textarea rows={3} value={problem} onChange={(e) => setProblem(e.target.value)} placeholder="What problem are we solving, for whom, and why now?" />
            </div>
            <div className="form-group">
              <label>Target users</label>
              <textarea rows={3} value={targetUsers} onChange={(e) => setTargetUsers(e.target.value)} placeholder="Who are the primary users and buyers?" />
            </div>
            <div style={{ display: 'flex', gap: 10 }}>
              <button className="button" onClick={saveDefinition} disabled={savingDef}>
                {savingDef ? 'Saving…' : 'Save'}
              </button>
              <button
                className="button-secondary"
                onClick={() => {
                  setEditingDef(false);
                  setVision(profile?.vision || '');
                  setProblem(profile?.problem_statement || '');
                  setTargetUsers(profile?.target_users || '');
                }}
              >
                Cancel
              </button>
            </div>
          </>
        ) : (
          <div style={{ display: 'grid', gap: 14 }}>
            {[
              { label: 'Vision', value: profile?.vision },
              { label: 'Problem statement', value: profile?.problem_statement },
              { label: 'Target users', value: profile?.target_users },
            ].map((f) => (
              <div key={f.label}>
                <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: 4 }}>
                  {f.label}
                </div>
                <div style={{ fontSize: 14, color: f.value ? 'var(--text)' : 'var(--neutral)', whiteSpace: 'pre-wrap' }}>
                  {f.value || 'Not defined yet — click Edit or run the guided definition.'}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Success metrics */}
      <div className="card">
        <div style={sectionTitle}>Success metrics</div>
        {metrics.length === 0 && (
          <div style={{ color: 'var(--neutral)', fontSize: 13, marginBottom: 10 }}>No success metrics defined yet.</div>
        )}
        {metrics.length > 0 && (
          <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 10 }}>
            <thead>
              <tr>
                {['Metric', 'Target', 'Current', 'Unit', ''].map((h) => (
                  <th key={h} style={{ textAlign: 'left', fontSize: 12, color: 'var(--text-muted)', padding: '4px 6px' }}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {metrics.map((m, i) => (
                <tr key={i}>
                  <td style={{ padding: 4 }}>
                    <input style={smallInput} value={m.name} placeholder="e.g. Weekly active users" onChange={(e) => setMetrics(metrics.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
                  </td>
                  <td style={{ padding: 4, width: 120 }}>
                    <input style={smallInput} value={m.target} placeholder="Target" onChange={(e) => setMetrics(metrics.map((x, j) => (j === i ? { ...x, target: e.target.value } : x)))} />
                  </td>
                  <td style={{ padding: 4, width: 120 }}>
                    <input style={smallInput} value={m.current} placeholder="Current" onChange={(e) => setMetrics(metrics.map((x, j) => (j === i ? { ...x, current: e.target.value } : x)))} />
                  </td>
                  <td style={{ padding: 4, width: 100 }}>
                    <input style={smallInput} value={m.unit} placeholder="Unit" onChange={(e) => setMetrics(metrics.map((x, j) => (j === i ? { ...x, unit: e.target.value } : x)))} />
                  </td>
                  <td style={{ padding: 4, width: 30 }}>
                    <button
                      onClick={() => setMetrics(metrics.filter((_, j) => j !== i))}
                      style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                      title="Remove metric"
                    >
                      ✕
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div style={{ display: 'flex', gap: 10 }}>
          <button
            className="button-secondary"
            style={{ padding: '6px 14px' }}
            onClick={() => setMetrics([...metrics, { name: '', target: '', current: '', unit: '' }])}
          >
            + Add metric
          </button>
          <button className="button" style={{ padding: '6px 14px' }} onClick={saveMetrics} disabled={savingMetrics}>
            {savingMetrics ? 'Saving…' : 'Save metrics'}
          </button>
        </div>
      </div>

      {/* Constraints */}
      <div className="card">
        <div style={sectionTitle}>Constraints</div>
        {constraints.length === 0 && (
          <div style={{ color: 'var(--neutral)', fontSize: 13, marginBottom: 10 }}>No constraints recorded yet.</div>
        )}
        {constraints.map((c, i) => (
          <div key={i} style={{ display: 'flex', gap: 8, marginBottom: 8, alignItems: 'flex-start' }}>
            <input
              style={{ ...smallInput, width: 240 }}
              value={c.title}
              placeholder="Constraint title"
              onChange={(e) => setConstraints(constraints.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)))}
            />
            <input
              style={smallInput}
              value={c.description}
              placeholder="Description"
              onChange={(e) => setConstraints(constraints.map((x, j) => (j === i ? { ...x, description: e.target.value } : x)))}
            />
            <button
              onClick={() => setConstraints(constraints.filter((_, j) => j !== i))}
              style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
              title="Remove constraint"
            >
              ✕
            </button>
          </div>
        ))}
        <div style={{ display: 'flex', gap: 10 }}>
          <button
            className="button-secondary"
            style={{ padding: '6px 14px' }}
            onClick={() => setConstraints([...constraints, { title: '', description: '' }])}
          >
            + Add constraint
          </button>
          <button className="button" style={{ padding: '6px 14px' }} onClick={saveConstraints} disabled={savingConstraints}>
            {savingConstraints ? 'Saving…' : 'Save constraints'}
          </button>
        </div>
      </div>

      {/* Personas & user needs */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
        {[
          { label: 'Personas', items: personas },
          { label: 'User needs', items: userNeeds },
        ].map((group) => (
          <div key={group.label} className="card" style={{ marginBottom: 20 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 10 }}>
              <div style={{ ...sectionTitle, marginBottom: 0 }}>{group.label}</div>
              <span style={chip(group.items.length > 0 ? 'var(--accent)' : 'var(--neutral)')}>{group.items.length}</span>
            </div>
            {group.items.length === 0 ? (
              <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
                None yet — the <Link to="guided">guided definition</Link> creates these.
              </div>
            ) : (
              <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                {group.items.slice(0, 5).map((a) => (
                  <li key={a.id} style={{ fontSize: 13, color: 'var(--text)', padding: '3px 0' }}>
                    •{' '}
                    <Link
                      to={`requirements?artifact=${a.id}`}
                      title="Open in Requirements"
                      style={{ color: 'var(--accent)', textDecoration: 'none' }}
                    >
                      {a.title}
                    </Link>
                  </li>
                ))}
                {group.items.length > 5 && (
                  <li style={{ fontSize: 12, color: 'var(--text-muted)', padding: '3px 0' }}>
                    …and {group.items.length - 5} more
                  </li>
                )}
              </ul>
            )}
            <div style={{ marginTop: 10 }}>
              <Link to="requirements" style={{ fontSize: 13, color: 'var(--accent)' }}>
                View in Requirements →
              </Link>
            </div>
          </div>
        ))}
      </div>

      {/* Interviews summary — the full feature lives on the Interviews page */}
      <div className="card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 10 }}>
          <div style={{ ...sectionTitle, marginBottom: 0 }}>User interviews</div>
          <span style={chip(interviews.length > 0 ? 'var(--accent)' : 'var(--neutral)')}>{interviews.length}</span>
        </div>
        {interviews.length === 0 ? (
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
            No interviews yet. Create one and share the invite link — an AI interviewer collects feedback for you.
          </div>
        ) : recentSessions.length === 0 ? (
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
            {interviews.length} interview{interviews.length === 1 ? '' : 's'} — no sessions recorded yet.
          </div>
        ) : (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
            {recentSessions.map((s) => (
              <li
                key={s.id}
                style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, color: 'var(--text)', padding: '3px 0' }}
              >
                <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {s.participant_name || 'Anonymous participant'} · {interviewName(s.interview_id)}
                </span>
                <span style={chip(s.status === 'active' ? 'var(--warning)' : 'var(--neutral)')}>{s.status}</span>
                <span style={{ color: 'var(--neutral)', fontSize: 11 }}>
                  {s.started_at ? new Date(s.started_at).toLocaleString() : ''}
                </span>
              </li>
            ))}
          </ul>
        )}
        <div style={{ marginTop: 10 }}>
          <Link to="interviews" style={{ fontSize: 13, color: 'var(--accent)' }}>
            Open Interviews →
          </Link>
        </div>
      </div>
    </div>
  );
};
