// The types api/guided.ts sends and receives.
// The guided wizard's sessions and chat, and stakeholder interviews,
// including the public side an interviewee answers without a session.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

export interface GuidedSession {
  id: string;
  project_id: string;
  status: string;
  current_step: number;
  answers: Record<string, any>;
  draft_artifact_ids: string[];
  agent_run_id?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface GuidedChatMessage {
  id: string;
  session_id: string;
  role: 'assistant' | 'user' | 'system';
  content: string;
  created_at: string;
}

export interface Interview {
  id: string;
  project_id: string;
  persona_artifact_id?: string | null;
  name: string;
  brief: string;
  status: string;
  created_at: string;
}

export interface InterviewSession {
  id: string;
  interview_id: string;
  participant_name: string;
  status: string;
  summary: string;
  started_at: string;
  ended_at?: string | null;
}

export interface InterviewMessage {
  id: string;
  session_id: string;
  role: 'assistant' | 'participant' | 'system';
  content: string;
  created_at: string;
}
