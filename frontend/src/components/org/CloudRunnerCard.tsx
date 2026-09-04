import React, { useCallback, useEffect, useRef, useState } from 'react';
import { RunnerSessionPayload, cloudRunnerAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner, useConfirm } from '../ui';

interface CloudRunnerCardProps {
  orgId: string;
  /** Called when a lease starts or ends, so sign-in cards can re-read state. */
  onChanged?: () => void;
}

// How long a countdown may run before the card re-reads the server. The
// deadline moves whenever the runner does work, so a purely local countdown
// would show a runner expiring while it is busy.
const REFRESH_MS = 20000;

/** Renders a remaining-time span as "12m 30s" / "45s". */
const formatRemaining = (seconds: number): string => {
  if (seconds <= 0) return 'expiring now';
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (mins >= 60) {
    const hours = Math.floor(mins / 60);
    return `${hours}h ${mins % 60}m`;
  }
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
};

// A transient runner: a pre-warmed runner in the cloud, leased to one member
// for a while. It is the no-install path to running agents — sign the vendor
// CLIs in from the browser and go — with the trade that the lease ends (on
// its own idle/expiry clock) and takes those sign-ins with it.
export const CloudRunnerCard: React.FC<CloudRunnerCardProps> = ({ orgId, onChanged }) => {
  const confirm = useConfirm();
  const [payload, setPayload] = useState<RunnerSessionPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [remaining, setRemaining] = useState(0);
  // The authoritative deadline from the last server read; the local ticker
  // counts down from it between reads.
  const deadlineRef = useRef<number>(0);

  const applyPayload = useCallback((next: RunnerSessionPayload) => {
    setPayload(next);
    deadlineRef.current = next.deadline ? new Date(next.deadline).getTime() : 0;
    setRemaining(next.seconds_remaining ?? 0);
  }, []);

  const load = useCallback(async () => {
    try {
      const res = await cloudRunnerAPI.get(orgId);
      applyPayload(res.data);
      setError('');
    } catch (err: any) {
      setError(`Failed to load your cloud runner: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [orgId, applyPayload]);

  useEffect(() => {
    load();
  }, [load]);

  const session = payload?.session ?? null;

  // Poll while a lease is live: the deadline slides forward as the runner
  // works, and the lease can end server-side without this tab doing anything.
  useEffect(() => {
    if (!session) return;
    const poll = setInterval(load, REFRESH_MS);
    const tick = setInterval(() => {
      if (!deadlineRef.current) return;
      setRemaining(Math.max(0, Math.round((deadlineRef.current - Date.now()) / 1000)));
    }, 1000);
    return () => {
      clearInterval(poll);
      clearInterval(tick);
    };
  }, [session, load]);

  const act = async (fn: () => Promise<{ data: RunnerSessionPayload }>, failure: string) => {
    setBusy(true);
    try {
      const res = await fn();
      applyPayload(res.data);
      setError('');
      onChanged?.();
    } catch (err: any) {
      // A full pool answers 503 with the same payload shape; show its counts
      // rather than a bare error.
      const data = err?.response?.data as RunnerSessionPayload | undefined;
      if (data && typeof data.enabled === 'boolean') {
        applyPayload(data);
        setError(
          data.pool && data.pool.total > 0
            ? `Every cloud runner is in use right now (${data.pool.leased} of ${data.pool.total}). Try again in a few minutes.`
            : 'No cloud runners are available on this deployment yet.'
        );
      } else {
        setError(`${failure}: ${apiErrorMessage(err)}`);
      }
    } finally {
      setBusy(false);
    }
  };

  const handleEnd = async () => {
    const ok = await confirm({
      title: 'End cloud runner',
      message:
        'End your cloud runner now? It is wiped when it stops, so your agent sign-ins on it are lost and you will sign in again next time.',
      confirmLabel: 'End runner',
      danger: true,
    });
    if (!ok) return;
    await act(() => cloudRunnerAPI.end(orgId), 'Failed to end your cloud runner');
  };

  if (!loading && payload && !payload.enabled) {
    // Nothing to offer on a deployment with no pool; the connector path is
    // still there, one card up.
    return null;
  }

  const pool = payload?.pool;

  return (
    <div className="card">
      <h3 style={{ marginBottom: 6 }}>Cloud runner</h3>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 12 }} />

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading your cloud runner…</div>
      ) : !session ? (
        <>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            Nothing to install: start a runner here, sign your agents into it from this browser,
            and launch runs. It is yours alone while it lasts, and it ends on its own once you
            stop using it — everything on it is wiped when it does, so the next one needs a fresh
            sign-in.
          </p>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
            <button
              className="button"
              style={{ width: 'auto' }}
              onClick={() => act(() => cloudRunnerAPI.start(orgId), 'Failed to start a cloud runner')}
              disabled={busy}
            >
              {busy ? 'Starting…' : 'Start a cloud runner'}
            </button>
            {pool && (
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                {pool.idle} of {pool.total} free
              </span>
            )}
          </div>
        </>
      ) : (
        <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 14, flexWrap: 'wrap' }}>
            <span style={{ fontSize: 13, color: 'var(--text)', fontWeight: 600 }}>
              <span
                style={{
                  color: session.status === 'active' ? 'var(--success)' : 'var(--warning)',
                  marginRight: 4,
                  fontSize: 11,
                }}
              >
                ●
              </span>
              {session.status === 'active' ? 'running' : 'starting…'}
            </span>
            <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
              ends in {formatRemaining(remaining)}
            </span>
            <div style={{ flex: 1 }} />
            <button
              className="button-secondary button"
              style={{ width: 'auto' }}
              onClick={() => act(() => cloudRunnerAPI.extend(orgId), 'Failed to extend your cloud runner')}
              disabled={busy}
              title="Reset the clock and keep this runner longer"
            >
              Extend
            </button>
            <button
              className="button-secondary button"
              style={{ width: 'auto' }}
              onClick={handleEnd}
              disabled={busy}
            >
              End now
            </button>
          </div>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 10, marginBottom: 0 }}>
            Sign your agents in below — the sign-in runs on this runner and its credentials live
            only as long as the runner does. The clock resets whenever the runner is working, and
            it stops {session.idle_minutes} minutes after you stop using it.
          </p>
        </>
      )}
    </div>
  );
};
