import { useEffect, useState } from 'react';
import { guidedAPI, productProfileAPI, GuidedSession, ProductProfile } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { REVIEW_STEP, STEP_LABELS } from './steps';
import { WizardAnswers, seedReopenedAnswers } from './wizardAnswers';

export interface GuidedSessionOptions {
  /** The open project: the route's, else the store's. */
  projectId: string | null | undefined;
  /**
   * Puts a session's saved answers into the wizard's form. Held by the
   * wizard, which owns the form; the load effect depends on it.
   */
  hydrateFromAnswers: (answers: Record<string, any>) => void;
  /** Fills step 1 from the product profile, for a session that has no step 1. */
  fillFraming: (profile: ProductProfile) => void;
}

/**
 * The guided wizard's session: the one being worked on (loaded on mount and
 * whenever the project changes), the newest committed one (what Modify
 * reopens), the step the person is on and the furthest one reached, and the
 * calls that start, reopen, save, abandon and commit a session, with the
 * loading, busy and error state they share. The form's own state stays with
 * the wizard, which hands in how to fill it.
 */
export function useGuidedSession({ projectId, hydrateFromAnswers, fillFraming }: GuidedSessionOptions) {
  const [session, setSession] = useState<GuidedSession | null>(null);
  const [latestCommitted, setLatestCommitted] = useState<GuidedSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [step, setStep] = useState(1);
  const [maxReached, setMaxReached] = useState(1);
  const [committed, setCommitted] = useState(false);

  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const res = await guidedAPI.list(projectId);
        if (cancelled) return;
        const sessions = res.data || [];
        const active = sessions.find((s) => s.status === 'in-progress' || s.status === 'in_progress');
        // Sessions arrive newest first, so this is the most recent commit.
        setLatestCommitted(sessions.find((s) => s.status === 'committed') || null);
        if (active) {
          setSession(active);
          const current = Math.min(Math.max(active.current_step || 1, 1), REVIEW_STEP);
          setStep(current);
          setMaxReached(current);
          hydrateFromAnswers(active.answers || {});
        }
        setError('');
      } catch (err: any) {
        if (!cancelled) setError(`Failed to load guided sessions: ${apiErrorMessage(err)}`);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [projectId, hydrateFromAnswers]);

  const startSession = async () => {
    if (!projectId) return;
    setBusy(true);
    try {
      const res = await guidedAPI.start(projectId);
      setSession(res.data);
      setStep(1);
      setMaxReached(1);
      hydrateFromAnswers(res.data.answers || {});
      // Pre-fill step 1 from the product profile if available.
      try {
        const profile = await productProfileAPI.get(projectId);
        if (!res.data.answers?.step_1) {
          fillFraming(profile.data);
        }
      } catch {
        // profile optional
      }
      setError('');
    } catch (err: any) {
      setError(`Failed to start session: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  // Reopen a committed definition: a new in-progress session seeded with the
  // committed answers. Entries that already materialized keep their artifact
  // links (shown locked); anything added becomes a fresh draft to commit.
  const modifySession = async (from: GuidedSession) => {
    if (!projectId) return;
    setBusy(true);
    try {
      const res = await guidedAPI.start(projectId);
      const seeded = await guidedAPI.saveStep(res.data.id, 1, seedReopenedAnswers(from.answers || {}));
      setSession(seeded.data);
      hydrateFromAnswers(seeded.data.answers || {});
      setStep(1);
      setMaxReached(STEP_LABELS.length);
      setError('');
    } catch (err: any) {
      setError(`Failed to reopen definition: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  // Saves the answers at a step. Never regresses the session's saved step:
  // clicking Next while revisiting an earlier step must not lose reload
  // progress (mirrors the copilot-apply path in the wizard).
  const saveStep = async (nextStep: number, answers: WizardAnswers) => {
    if (!session) return;
    const stepToSave = Math.max(session.current_step || 0, nextStep) || 1;
    const res = await guidedAPI.saveStep(session.id, stepToSave, answers);
    setSession(res.data);
  };

  const goTo = (nextStep: number) => {
    setStep(nextStep);
    setMaxReached((m) => Math.max(m, nextStep));
  };

  const goBack = () => {
    if (step > 1) setStep(step - 1);
  };

  // Drops the current in-progress session and returns to the Start/Modify
  // landing. The wizard asks first. Materialized drafts stay in the project.
  const abandonSession = async () => {
    if (!session) return;
    setBusy(true);
    try {
      await guidedAPI.abandon(session.id);
      setSession(null);
      setStep(1);
      setMaxReached(1);
      setError('');
    } catch (err: any) {
      setError(`Failed to abandon session: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const handleCommit = async () => {
    if (!session) return;
    setBusy(true);
    setError('');
    try {
      await guidedAPI.commit(session.id);
      setCommitted(true);
    } catch (err: any) {
      setError(`Failed to commit: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return {
    session,
    setSession,
    latestCommitted,
    setLatestCommitted,
    loading,
    error,
    setError,
    busy,
    setBusy,
    step,
    setStep,
    maxReached,
    committed,
    setCommitted,
    startSession,
    modifySession,
    saveStep,
    goTo,
    goBack,
    abandonSession,
    handleCommit,
  };
}
