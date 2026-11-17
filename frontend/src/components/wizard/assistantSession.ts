// Resolving the project's V&V Assistant conversation.
//
// The assistant's transcript is stored on a guided session, because that is
// where the conversation started life. One conversation runs through the whole
// project: the wizard writes into it, and so does every artifact's notes panel.
// So the notes panel does not start a conversation of its own — it finds the
// session already holding one, and only creates a session when the project has
// none at all.
//
// Which session: the newest, matching what the wizard itself resumes into, so
// a member reading notes sees the transcript the wizard is showing.
import { GuidedSession, guidedAPI } from '../../api/client';
import { isPlanReadOnlyError } from '../../api/errors';

/**
 * A session started only to host the assistant's chat, never opened in the
 * wizard: still on step one with nothing filled in.
 *
 * The project sidebar hides "Guided Definition" once a guided definition
 * exists, so without this distinction opening a notes chat would silently
 * remove the wizard from the nav of a project that had never run it.
 */
export const isUntouchedByWizard = (s: GuidedSession): boolean =>
  s.status === 'in-progress' &&
  (s.current_step ?? 1) <= 1 &&
  Object.keys(s.answers || {}).length === 0;

/** True when a project has a guided definition a person actually worked on. */
export const hasWizardProgress = (sessions: GuidedSession[]): boolean =>
  sessions.some((s) => !isUntouchedByWizard(s));

const createdAt = (s: GuidedSession): number => {
  const t = new Date(s.created_at || 0).getTime();
  return Number.isNaN(t) ? 0 : t;
};

/** The session whose transcript the assistant reads and writes. */
export const pickAssistantSession = (sessions: GuidedSession[]): GuidedSession | undefined =>
  sessions.slice().sort((a, b) => createdAt(b) - createdAt(a))[0];

/**
 * The id of the project's assistant conversation, creating the session that
 * holds it only when the project has none. Returns '' when the conversation
 * could not be reached, so the caller can say so rather than render a chat
 * that silently swallows messages.
 */
export const resolveAssistantSessionId = async (projectId: string): Promise<string> => {
  try {
    const res = await guidedAPI.list(projectId);
    const existing = pickAssistantSession(res.data || []);
    if (existing) return existing.id;
    const created = await guidedAPI.start(projectId);
    return created.data?.id || '';
  } catch {
    return '';
  }
};

const UNREACHABLE = 'The assistant conversation could not be opened. Try again in a moment.';

/** The conversation, or why the assistant cannot hold one here. */
export type AssistantSessionResult = { id: string } | { reason: string };

/**
 * Like resolveAssistantSessionId, but saying why when the conversation
 * cannot be opened (#726), so the floating panel explains rather than
 * fails. Starting a conversation needs editor access to the project, and a
 * workspace its plan has made read-only refuses it.
 */
export const openAssistantSession = async (projectId: string): Promise<AssistantSessionResult> => {
  try {
    const res = await guidedAPI.list(projectId);
    const existing = pickAssistantSession(res.data || []);
    if (existing) return { id: existing.id };
    const created = await guidedAPI.start(projectId);
    return created.data?.id ? { id: created.data.id } : { reason: UNREACHABLE };
  } catch (err) {
    if (isPlanReadOnlyError(err)) {
      return {
        reason:
          'This workspace is read-only on its current plan, so the V&V Assistant cannot start a conversation. A workspace admin can change the plan under Workspace Settings → Billing.',
      };
    }
    const status = (err as { response?: { status?: number } } | null)?.response?.status;
    if (status === 403 || status === 404) {
      return {
        reason:
          'The V&V Assistant needs editor access to this project. Ask a project owner to make you an editor in the project’s Settings → Members.',
      };
    }
    return { reason: UNREACHABLE };
  }
};
