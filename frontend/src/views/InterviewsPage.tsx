import React, { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  artifactAPI,
  interviewsAPI,
  Artifact,
  Interview,
  InterviewMessage,
  InterviewSession,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, useConfirm, usePrompt } from '../components/ui';

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

// Dedicated home of the user-interviews feature: create interviews, share
// invite links, browse sessions and transcripts, and link personas.
// Extracted from ProductOverview (issue #97) — Overview keeps a summary card.
export const InterviewsPage: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const confirm = useConfirm();
  const prompt = usePrompt();

  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

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
      const [interviewsRes, artifactsRes] = await Promise.all([
        interviewsAPI.list(projectId),
        artifactAPI.list(projectId).catch(() => ({ data: [] as Artifact[] })),
      ]);
      setInterviews(interviewsRes.data || []);
      setArtifacts(artifactsRes.data || []);
      setError('');
    } catch (err: any) {
      setError(`Failed to load interviews: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    load();
  }, [load]);

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
        // Fallback for non-secure contexts: show the link in a dialog so the
        // user can copy it manually (the input opens with its text selected).
        await prompt({
          title: 'Copy this invite link',
          defaultValue: url,
          confirmLabel: 'Done',
        });
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
    const ok = await confirm({
      title: 'Close interview',
      message: `Close interview "${interview.name}"? Invite links will stop working.`,
      confirmLabel: 'Close interview',
      danger: true,
    });
    if (!ok) return;
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
    return <div style={{ padding: 32, color: 'var(--text-muted)' }}>Loading interviews…</div>;
  }

  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: '0 auto' }}>
      <h2 style={{ color: 'var(--text)', marginBottom: 4 }}>Interviews</h2>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 20 }}>
        Create user interviews, share invite links, and review what an AI interviewer learned.
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />

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
              style={{ padding: '6px 14px', background: 'var(--accent)' }}
              onClick={() => setShowNewInterview(!showNewInterview)}
            >
              {showNewInterview ? 'Cancel' : '+ New interview'}
            </button>
          </div>
        </div>

        {showNewInterview && (
          <form onSubmit={createInterview} style={{ background: 'var(--surface-alt)', borderRadius: 4, padding: 14, marginBottom: 14 }}>
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
              <div style={{ fontSize: 12, color: 'var(--neutral)', marginTop: 4 }}>
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
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
            No interviews yet. Create one and share the invite link — an AI interviewer collects feedback for you.
          </div>
        ) : filteredInterviews.length === 0 ? (
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No interviews match this persona filter.</div>
        ) : (
          filteredInterviews.map((iv) => (
            <div key={iv.id} style={{ border: '1px solid var(--border-soft)', borderRadius: 4, marginBottom: 8 }}>
              <div
                style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 12px', cursor: 'pointer' }}
                onClick={() => toggleInterview(iv)}
              >
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{expandedInterviewId === iv.id ? '▾' : '▸'}</span>
                <div style={{ flex: 1, fontSize: 14, fontWeight: 600, color: 'var(--text)' }}>{iv.name}</div>
                {iv.persona_artifact_id && (
                  <span style={chip('var(--purple)')} title="Linked persona">
                    {personaTitle(iv.persona_artifact_id)}
                  </span>
                )}
                <span style={chip(iv.status === 'open' ? 'var(--success)' : 'var(--neutral)')}>{iv.status}</span>
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
                    style={{ padding: '5px 10px', fontSize: 12, background: 'var(--danger)' }}
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
                <div style={{ borderTop: '1px solid var(--border-soft)', padding: '10px 12px' }}>
                  {iv.brief && (
                    <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 8, whiteSpace: 'pre-wrap' }}>{iv.brief}</div>
                  )}
                  {personas.length > 0 && (
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                      <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)' }}>Persona:</span>
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
                    <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading sessions…</div>
                  ) : sessions.length === 0 ? (
                    <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No sessions yet — share the invite link to start.</div>
                  ) : (
                    sessions.map((s) => (
                      <div key={s.id} style={{ marginBottom: 6 }}>
                        <div
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 10,
                            padding: '6px 8px',
                            background: 'var(--surface-alt)',
                            borderRadius: 4,
                            cursor: 'pointer',
                            fontSize: 13,
                          }}
                          onClick={() => openTranscript(s)}
                        >
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{openSessionId === s.id ? '▾' : '▸'}</span>
                          <span style={{ flex: 1, color: 'var(--text)' }}>
                            {s.participant_name || 'Anonymous participant'}
                          </span>
                          <span style={chip(s.status === 'active' ? 'var(--warning)' : 'var(--neutral)')}>{s.status}</span>
                          <span style={{ color: 'var(--neutral)', fontSize: 11 }}>
                            {s.started_at ? new Date(s.started_at).toLocaleString() : ''}
                          </span>
                        </div>
                        {openSessionId === s.id && (
                          <div style={{ padding: '10px 8px', maxHeight: 360, overflowY: 'auto' }}>
                            {s.summary && (
                              <div
                                style={{
                                  fontSize: 12,
                                  color: 'var(--text)',
                                  background: 'var(--tint-yellow)',
                                  border: '1px solid var(--warning)',
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
                              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading transcript…</div>
                            ) : transcript.length === 0 ? (
                              <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No messages in this session.</div>
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
                                    <div style={{ fontSize: 11, fontStyle: 'italic', color: 'var(--neutral)' }}>{m.content}</div>
                                  ) : (
                                    <div
                                      style={{
                                        maxWidth: '75%',
                                        padding: '7px 11px',
                                        borderRadius: 10,
                                        fontSize: 13,
                                        whiteSpace: 'pre-wrap',
                                        background: m.role === 'participant' ? 'var(--accent)' : 'var(--neutral-soft)',
                                        color: m.role === 'participant' ? '#fff' : 'var(--text)',
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
