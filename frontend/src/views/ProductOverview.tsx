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
  InterviewMessage,
  InterviewSession,
  ProductProfile,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';

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
  color: '#2c3e50',
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

  // Interviews
  const [interviews, setInterviews] = useState<Interview[]>([]);
  const [showNewInterview, setShowNewInterview] = useState(false);
  const [newInterviewName, setNewInterviewName] = useState('');
  const [newInterviewBrief, setNewInterviewBrief] = useState('');
  const [newInterviewPersonaId, setNewInterviewPersonaId] = useState('');
  const [personaFilter, setPersonaFilter] = useState('');
  const [copiedInterviewId, setCopiedInterviewId] = useState('');
  const [expandedInterviewId, setExpandedInterviewId] = useState('');
  const [sessions, setSessions] = useState<InterviewSession[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [openSessionId, setOpenSessionId] = useState('');
  const [transcript, setTranscript] = useState<InterviewMessage[]>([]);
  const [transcriptLoading, setTranscriptLoading] = useState(false);

  const load = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const [profileRes, artifactsRes, interviewsRes, guidedRes] = await Promise.all([
        productProfileAPI.get(projectId),
        artifactAPI.list(projectId),
        interviewsAPI.list(projectId).catch(() => ({ data: [] as Interview[] })),
        guidedAPI.list(projectId).catch(() => ({ data: [] as GuidedSession[] })),
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

  const createInterview = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId || !newInterviewName.trim()) return;
    try {
      const res = await interviewsAPI.create(projectId, {
        name: newInterviewName.trim(),
        brief: newInterviewBrief.trim(),
        persona_artifact_id: newInterviewPersonaId || undefined,
      });
      setInterviews([res.data, ...interviews]);
      setNewInterviewName('');
      setNewInterviewBrief('');
      setNewInterviewPersonaId('');
      setShowNewInterview(false);
      setError('');
    } catch (err: any) {
      setError(`Failed to create interview: ${apiErrorMessage(err)}`);
    }
  };

  const copyInviteLink = async (interview: Interview) => {
    try {
      const res = await interviewsAPI.createInvite(interview.id);
      const url = `${window.location.origin}${res.data.path}`;
      try {
        await navigator.clipboard.writeText(url);
      } catch {
        // Fallback for non-secure contexts
        window.prompt('Copy this invite link:', url);
      }
      setCopiedInterviewId(interview.id);
      setTimeout(() => setCopiedInterviewId(''), 2500);
    } catch (err: any) {
      setError(`Failed to create invite: ${apiErrorMessage(err)}`);
    }
  };

  const toggleInterview = async (interview: Interview) => {
    if (expandedInterviewId === interview.id) {
      setExpandedInterviewId('');
      setSessions([]);
      setOpenSessionId('');
      setTranscript([]);
      return;
    }
    setExpandedInterviewId(interview.id);
    setOpenSessionId('');
    setTranscript([]);
    setSessionsLoading(true);
    try {
      const res = await interviewsAPI.listSessions(interview.id);
      setSessions(res.data || []);
    } catch (err: any) {
      setError(`Failed to load sessions: ${apiErrorMessage(err)}`);
      setSessions([]);
    } finally {
      setSessionsLoading(false);
    }
  };

  const openTranscript = async (session: InterviewSession) => {
    if (openSessionId === session.id) {
      setOpenSessionId('');
      setTranscript([]);
      return;
    }
    setOpenSessionId(session.id);
    setTranscriptLoading(true);
    try {
      const res = await interviewsAPI.transcript(session.id);
      setTranscript(res.data || []);
    } catch (err: any) {
      setError(`Failed to load transcript: ${apiErrorMessage(err)}`);
      setTranscript([]);
    } finally {
      setTranscriptLoading(false);
    }
  };

  const closeInterview = async (interview: Interview) => {
    if (!window.confirm(`Close interview "${interview.name}"? Invite links will stop working.`)) return;
    try {
      const res = await interviewsAPI.close(interview.id);
      setInterviews(interviews.map((iv) => (iv.id === interview.id ? res.data : iv)));
    } catch (err: any) {
      setError(`Failed to close interview: ${apiErrorMessage(err)}`);
    }
  };

  const setInterviewPersona = async (interview: Interview, personaArtifactId: string) => {
    try {
      const res = await interviewsAPI.setPersona(interview.id, personaArtifactId || null);
      setInterviews(interviews.map((iv) => (iv.id === interview.id ? res.data : iv)));
      setError('');
    } catch (err: any) {
      setError(`Failed to update persona link: ${apiErrorMessage(err)}`);
    }
  };

  const personas = artifacts.filter((a) => a.type === 'persona');
  const userNeeds = artifacts.filter((a) => a.type === 'user-need');

  const personaTitle = (id?: string | null) => {
    if (!id) return '';
    const persona = artifacts.find((a) => a.id === id);
    return persona ? persona.title : 'Unknown persona';
  };

  const filteredInterviews = interviews.filter((iv) => {
    if (!personaFilter) return true;
    if (personaFilter === 'none') return !iv.persona_artifact_id;
    return iv.persona_artifact_id === personaFilter;
  });

  if (loading) {
    return <div style={{ padding: 32, color: '#7f8c8d' }}>Loading product overview…</div>;
  }

  // Label the guided CTA by where the project actually is: a session in
  // progress resumes, a committed one reopens for modification.
  const guidedInProgress = guidedSessions.some(
    (s) => s.status === 'in-progress' || s.status === 'in_progress'
  );
  const guidedCommitted = guidedSessions.some((s) => s.status === 'committed');
  const guidedCtaLabel = guidedInProgress
    ? 'Resume guided definition'
    : guidedCommitted
    ? 'Modify guided definition'
    : 'Start guided definition';

  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: '0 auto' }}>
      <h2 style={{ color: '#2c3e50', marginBottom: 4 }}>Product Overview</h2>
      <p style={{ color: '#7f8c8d', fontSize: 13, marginBottom: 20 }}>
        The single source of truth for what this product is and how success is measured.
      </p>

      {error && (
        <div
          style={{
            background: '#fdecea',
            border: '1px solid #e74c3c',
            color: '#c0392b',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {error}{' '}
          <button
            onClick={() => setError('')}
            style={{ background: 'none', border: 'none', color: '#c0392b', cursor: 'pointer', width: 'auto', padding: 0 }}
          >
            ✕
          </button>
        </div>
      )}

      {/* Quick links */}
      <div style={{ display: 'flex', gap: 10, marginBottom: 20, flexWrap: 'wrap' }}>
        <Link to="guided" className="button" style={{ background: '#3498db', textDecoration: 'none' }}>
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
                <div style={{ fontSize: 12, fontWeight: 600, color: '#7f8c8d', textTransform: 'uppercase', marginBottom: 4 }}>
                  {f.label}
                </div>
                <div style={{ fontSize: 14, color: f.value ? '#2c3e50' : '#95a5a6', whiteSpace: 'pre-wrap' }}>
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
          <div style={{ color: '#95a5a6', fontSize: 13, marginBottom: 10 }}>No success metrics defined yet.</div>
        )}
        {metrics.length > 0 && (
          <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 10 }}>
            <thead>
              <tr>
                {['Metric', 'Target', 'Current', 'Unit', ''].map((h) => (
                  <th key={h} style={{ textAlign: 'left', fontSize: 12, color: '#7f8c8d', padding: '4px 6px' }}>
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
                      style={{ background: 'none', border: 'none', color: '#e74c3c', cursor: 'pointer', width: 'auto', padding: 4 }}
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
          <div style={{ color: '#95a5a6', fontSize: 13, marginBottom: 10 }}>No constraints recorded yet.</div>
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
              style={{ background: 'none', border: 'none', color: '#e74c3c', cursor: 'pointer', width: 'auto', padding: 4 }}
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
              <span style={chip(group.items.length > 0 ? '#3498db' : '#95a5a6')}>{group.items.length}</span>
            </div>
            {group.items.length === 0 ? (
              <div style={{ color: '#95a5a6', fontSize: 13 }}>
                None yet — the <Link to="guided">guided definition</Link> creates these.
              </div>
            ) : (
              <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                {group.items.slice(0, 5).map((a) => (
                  <li key={a.id} style={{ fontSize: 13, color: '#2c3e50', padding: '3px 0' }}>
                    • {a.title}
                  </li>
                ))}
                {group.items.length > 5 && (
                  <li style={{ fontSize: 12, color: '#7f8c8d', padding: '3px 0' }}>
                    …and {group.items.length - 5} more
                  </li>
                )}
              </ul>
            )}
            <div style={{ marginTop: 10 }}>
              <Link to="requirements" style={{ fontSize: 13, color: '#3498db' }}>
                View in Requirements →
              </Link>
            </div>
          </div>
        ))}
      </div>

      {/* Interviews */}
      <div className="card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12, gap: 10 }}>
          <div style={{ ...sectionTitle, marginBottom: 0 }}>User interviews</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {personas.length > 0 && interviews.length > 0 && (
              <select
                value={personaFilter}
                onChange={(e) => setPersonaFilter(e.target.value)}
                style={{ ...smallInput, width: 'auto' }}
                title="Filter interviews by linked persona"
              >
                <option value="">All personas</option>
                {personas.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title}
                  </option>
                ))}
                <option value="none">No persona</option>
              </select>
            )}
            <button
              className="button-secondary"
              style={{ padding: '6px 14px', background: '#3498db' }}
              onClick={() => setShowNewInterview(!showNewInterview)}
            >
              {showNewInterview ? 'Cancel' : '+ New interview'}
            </button>
          </div>
        </div>

        {showNewInterview && (
          <form onSubmit={createInterview} style={{ background: '#f8f9fa', borderRadius: 4, padding: 14, marginBottom: 14 }}>
            <div className="form-group">
              <label>Interview name *</label>
              <input
                value={newInterviewName}
                onChange={(e) => setNewInterviewName(e.target.value)}
                placeholder="e.g. Machinist onboarding feedback"
                autoFocus
              />
            </div>
            <div className="form-group">
              <label>Brief for the interviewer agent</label>
              <textarea
                rows={3}
                value={newInterviewBrief}
                onChange={(e) => setNewInterviewBrief(e.target.value)}
                placeholder="What should the interview find out? Topics, tone, questions to cover…"
              />
            </div>
            <div className="form-group">
              <label>Persona</label>
              <select value={newInterviewPersonaId} onChange={(e) => setNewInterviewPersonaId(e.target.value)}>
                <option value="">No persona</option>
                {personas.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title}
                  </option>
                ))}
              </select>
              <div style={{ fontSize: 12, color: '#95a5a6', marginTop: 4 }}>
                Link this interview to the persona the participant represents. Several interviews can share one
                persona, so you can compare how e.g. different design engineers describe the same needs.
              </div>
            </div>
            <button type="submit" className="button">
              Create interview
            </button>
          </form>
        )}

        {interviews.length === 0 ? (
          <div style={{ color: '#95a5a6', fontSize: 13 }}>
            No interviews yet. Create one and share the invite link — an AI interviewer collects feedback for you.
          </div>
        ) : filteredInterviews.length === 0 ? (
          <div style={{ color: '#95a5a6', fontSize: 13 }}>No interviews match this persona filter.</div>
        ) : (
          filteredInterviews.map((iv) => (
            <div key={iv.id} style={{ border: '1px solid #eee', borderRadius: 4, marginBottom: 8 }}>
              <div
                style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 12px', cursor: 'pointer' }}
                onClick={() => toggleInterview(iv)}
              >
                <span style={{ fontSize: 12, color: '#7f8c8d' }}>{expandedInterviewId === iv.id ? '▾' : '▸'}</span>
                <div style={{ flex: 1, fontSize: 14, fontWeight: 600, color: '#2c3e50' }}>{iv.name}</div>
                {iv.persona_artifact_id && (
                  <span style={chip('#8e44ad')} title="Linked persona">
                    {personaTitle(iv.persona_artifact_id)}
                  </span>
                )}
                <span style={chip(iv.status === 'open' ? '#27ae60' : '#95a5a6')}>{iv.status}</span>
                <button
                  className="button-secondary"
                  style={{ padding: '5px 10px', fontSize: 12 }}
                  onClick={(e) => {
                    e.stopPropagation();
                    copyInviteLink(iv);
                  }}
                >
                  {copiedInterviewId === iv.id ? '✓ Copied!' : 'Copy invite link'}
                </button>
                {iv.status !== 'closed' && (
                  <button
                    className="button-secondary"
                    style={{ padding: '5px 10px', fontSize: 12, background: '#e74c3c' }}
                    onClick={(e) => {
                      e.stopPropagation();
                      closeInterview(iv);
                    }}
                  >
                    Close
                  </button>
                )}
              </div>
              {expandedInterviewId === iv.id && (
                <div style={{ borderTop: '1px solid #eee', padding: '10px 12px' }}>
                  {iv.brief && (
                    <div style={{ fontSize: 12, color: '#7f8c8d', marginBottom: 8, whiteSpace: 'pre-wrap' }}>{iv.brief}</div>
                  )}
                  {personas.length > 0 && (
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                      <span style={{ fontSize: 12, fontWeight: 600, color: '#7f8c8d' }}>Persona:</span>
                      <select
                        value={iv.persona_artifact_id || ''}
                        onChange={(e) => setInterviewPersona(iv, e.target.value)}
                        onClick={(e) => e.stopPropagation()}
                        style={{ ...smallInput, width: 'auto' }}
                      >
                        <option value="">No persona</option>
                        {personas.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.title}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}
                  {sessionsLoading ? (
                    <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading sessions…</div>
                  ) : sessions.length === 0 ? (
                    <div style={{ color: '#95a5a6', fontSize: 13 }}>No sessions yet — share the invite link to start.</div>
                  ) : (
                    sessions.map((s) => (
                      <div key={s.id} style={{ marginBottom: 6 }}>
                        <div
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 10,
                            padding: '6px 8px',
                            background: '#f8f9fa',
                            borderRadius: 4,
                            cursor: 'pointer',
                            fontSize: 13,
                          }}
                          onClick={() => openTranscript(s)}
                        >
                          <span style={{ color: '#7f8c8d', fontSize: 11 }}>{openSessionId === s.id ? '▾' : '▸'}</span>
                          <span style={{ flex: 1, color: '#2c3e50' }}>
                            {s.participant_name || 'Anonymous participant'}
                          </span>
                          <span style={chip(s.status === 'active' ? '#f39c12' : '#95a5a6')}>{s.status}</span>
                          <span style={{ color: '#95a5a6', fontSize: 11 }}>
                            {s.started_at ? new Date(s.started_at).toLocaleString() : ''}
                          </span>
                        </div>
                        {openSessionId === s.id && (
                          <div style={{ padding: '10px 8px', maxHeight: 360, overflowY: 'auto' }}>
                            {s.summary && (
                              <div
                                style={{
                                  fontSize: 12,
                                  color: '#2c3e50',
                                  background: '#fef9e7',
                                  border: '1px solid #f39c12',
                                  borderRadius: 4,
                                  padding: 8,
                                  marginBottom: 10,
                                  whiteSpace: 'pre-wrap',
                                }}
                              >
                                <strong>Summary:</strong> {s.summary}
                              </div>
                            )}
                            {transcriptLoading ? (
                              <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading transcript…</div>
                            ) : transcript.length === 0 ? (
                              <div style={{ color: '#95a5a6', fontSize: 13 }}>No messages in this session.</div>
                            ) : (
                              transcript.map((m) => (
                                <div
                                  key={m.id}
                                  style={{
                                    display: 'flex',
                                    justifyContent:
                                      m.role === 'participant' ? 'flex-end' : m.role === 'system' ? 'center' : 'flex-start',
                                    marginBottom: 6,
                                  }}
                                >
                                  {m.role === 'system' ? (
                                    <div style={{ fontSize: 11, fontStyle: 'italic', color: '#95a5a6' }}>{m.content}</div>
                                  ) : (
                                    <div
                                      style={{
                                        maxWidth: '75%',
                                        padding: '7px 11px',
                                        borderRadius: 10,
                                        fontSize: 13,
                                        whiteSpace: 'pre-wrap',
                                        background: m.role === 'participant' ? '#3498db' : '#ecf0f1',
                                        color: m.role === 'participant' ? '#fff' : '#2c3e50',
                                      }}
                                    >
                                      {m.content}
                                    </div>
                                  )}
                                </div>
                              ))
                            )}
                          </div>
                        )}
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
};
