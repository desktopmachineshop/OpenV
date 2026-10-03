// The guided wizard's sessions and chat, and stakeholder interviews,
// including the public side an interviewee answers without a session.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client } from './http';
import type {
  GuidedChatMessage,
  GuidedSession,
  Interview,
  InterviewMessage,
  InterviewSession,
} from './types/guided';

export const guidedAPI = {
  start: (projectId: string) =>
    client.post<GuidedSession>('/api/v1/guided-sessions', { project_id: projectId }),
  list: (projectId: string) =>
    client.get<GuidedSession[]>('/api/v1/guided-sessions', { params: { project_id: projectId } }),
  get: (id: string) => client.get<GuidedSession>(`/api/v1/guided-sessions/${id}`),
  saveStep: (id: string, step: number, answers: Record<string, any>) =>
    client.put<GuidedSession>(`/api/v1/guided-sessions/${id}/step`, { step, answers }),
  materializeDrafts: (id: string, drafts: any[]) =>
    client.post<{ artifact_ids: string[] }>(`/api/v1/guided-sessions/${id}/drafts`, { drafts }),
  commit: (id: string) => client.post<GuidedSession>(`/api/v1/guided-sessions/${id}/commit`),
  abandon: (id: string) => client.post<GuidedSession>(`/api/v1/guided-sessions/${id}/abandon`),
  listMessages: (id: string) =>
    client.get<GuidedChatMessage[]>(`/api/v1/guided-sessions/${id}/messages`),
  // artifactId is set when the turn comes from an artifact's notes panel: the
  // assistant answers about the artifact on screen.
  sendMessage: (
    id: string,
    content: string,
    step: number,
    state: Record<string, any>,
    artifactId?: string
  ) =>
    client.post<{ message: GuidedChatMessage; runner_online?: boolean }>(
      `/api/v1/guided-sessions/${id}/messages`,
      { content, step, state, artifact_id: artifactId || '' }
    ),
  kickoffChat: (id: string, step: number, state: Record<string, any>, artifactId?: string) =>
    client.post<{ status: 'launched' | 'pending' | 'skipped' | 'unavailable'; runner_online?: boolean }>(
      `/api/v1/guided-sessions/${id}/chat/kickoff`,
      { step, state, artifact_id: artifactId || '' }
    ),
  nudgeChat: (id: string, step: number, state: Record<string, any>, event: string) =>
    client.post<{ status: 'launched' | 'pending' | 'unavailable'; runner_online?: boolean }>(
      `/api/v1/guided-sessions/${id}/chat/nudge`,
      { step, state, event }
    ),
  chatStreamUrl: (id: string) => `${API_BASE_URL}/api/v1/guided-sessions/${id}/chat/stream`,
};

export const interviewsAPI = {
  create: (
    projectId: string,
    payload: { name: string; brief: string; agent_slug?: string; persona_artifact_id?: string | null }
  ) => client.post<Interview>(`/api/v1/projects/${projectId}/interviews`, payload),
  list: (projectId: string) =>
    client.get<Interview[]>(`/api/v1/projects/${projectId}/interviews`),
  close: (id: string) => client.post<Interview>(`/api/v1/interviews/${id}/close`),
  setPersona: (id: string, personaArtifactId: string | null) =>
    client.put<Interview>(`/api/v1/interviews/${id}/persona`, { persona_artifact_id: personaArtifactId }),
  createInvite: (interviewId: string, inviteeLabel?: string) =>
    client.post<{ invite: any; token: string; path: string }>(
      `/api/v1/interviews/${interviewId}/invites`,
      { invitee_label: inviteeLabel || '' }
    ),
  listInvites: (interviewId: string) =>
    client.get<any[]>(`/api/v1/interviews/${interviewId}/invites`),
  revokeInvite: (inviteId: string) =>
    client.post(`/api/v1/interview-invites/${inviteId}/revoke`),
  listSessions: (interviewId: string) =>
    client.get<InterviewSession[]>(`/api/v1/interviews/${interviewId}/sessions`),
  listProjectSessions: (projectId: string, limit?: number) =>
    client.get<InterviewSession[]>(`/api/v1/projects/${projectId}/interview-sessions`, {
      params: limit ? { limit } : {},
    }),
  transcript: (sessionId: string) =>
    client.get<InterviewMessage[]>(`/api/v1/interview-sessions/${sessionId}/transcript`),
};

export const publicInterviewAPI = {
  intro: (token: string) =>
    client.get<{ interview_name: string; session: InterviewSession; transcript: InterviewMessage[] }>(
      `/api/v1/public/interviews/${token}`
    ),
  sendMessage: (token: string, content: string, participantName?: string) =>
    client.post<{ session: InterviewSession; message: InterviewMessage }>(
      `/api/v1/public/interviews/${token}/messages`,
      { content, participant_name: participantName || '' }
    ),
  streamUrl: (token: string) => `${API_BASE_URL}/api/v1/public/interviews/${token}/stream`,
  finish: (token: string) => client.post(`/api/v1/public/interviews/${token}/finish`),
};
