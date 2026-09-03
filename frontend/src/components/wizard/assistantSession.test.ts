import { GuidedSession } from '../../api/client';
import {
  hasWizardProgress,
  isUntouchedByWizard,
  pickAssistantSession,
} from './assistantSession';

const session = (over: Partial<GuidedSession> = {}): GuidedSession => ({
  id: 'sess-1',
  project_id: 'proj-1',
  status: 'in-progress',
  current_step: 1,
  answers: {},
  draft_artifact_ids: [],
  ...over,
});

// The assistant's conversation lives on a guided session, so opening the notes
// chat in a project that never ran the wizard creates one. Everything that
// keys off "is there a guided definition" has to tell that session apart from
// a wizard someone actually started.
describe('isUntouchedByWizard', () => {
  it('counts a session created only to hold the conversation', () => {
    expect(isUntouchedByWizard(session())).toBe(true);
  });

  it('does not count a session someone has entered anything into', () => {
    expect(isUntouchedByWizard(session({ answers: { step_1: { vision: 'x' } } }))).toBe(false);
  });

  it('does not count a session past the first step', () => {
    expect(isUntouchedByWizard(session({ current_step: 3 }))).toBe(false);
  });

  it('does not count a committed or abandoned session', () => {
    expect(isUntouchedByWizard(session({ status: 'committed' }))).toBe(false);
    expect(isUntouchedByWizard(session({ status: 'abandoned' }))).toBe(false);
  });
});

describe('hasWizardProgress', () => {
  it('is false for no sessions and for chat-only ones', () => {
    expect(hasWizardProgress([])).toBe(false);
    expect(hasWizardProgress([session(), session({ id: 'sess-2' })])).toBe(false);
  });

  it('is true as soon as one session shows real wizard use', () => {
    expect(hasWizardProgress([session(), session({ id: 'sess-2', current_step: 2 })])).toBe(true);
  });
});

describe('pickAssistantSession', () => {
  it('returns nothing when the project has no sessions', () => {
    expect(pickAssistantSession([])).toBeUndefined();
  });

  it('picks the newest, so notes show the transcript the wizard resumes into', () => {
    const older = session({ id: 'older', created_at: '2026-09-01T10:00:00Z' });
    const newer = session({ id: 'newer', created_at: '2026-09-02T10:00:00Z' });
    expect(pickAssistantSession([older, newer])?.id).toBe('newer');
    expect(pickAssistantSession([newer, older])?.id).toBe('newer');
  });

  it('still returns a session when timestamps are missing', () => {
    expect(pickAssistantSession([session({ id: 'only' })])?.id).toBe('only');
  });
});
