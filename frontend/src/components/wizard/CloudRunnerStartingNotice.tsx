import React from 'react';
import { Link } from 'react-router-dom';
import type { FeatureKey } from '../../generated/contract';

// The gate on leasing a cloud runner for a chat turn. The server applies it:
// it sends runner_starting only once the workspace has the feature, so the
// panel reads no flag of its own.
export const AUTO_CLOUD_RUNNER_FEATURE: FeatureKey = 'auto-cloud-runner';

// CloudRunnerStartingNotice stands in the chat where "not connected" would
// be, when no runner was online and OpenV leased a cloud runner for the turn
// the member just sent. The reply arrives once the runner is up.
export const CloudRunnerStartingNotice: React.FC = () => (
  <div
    data-testid="cloud-runner-starting"
    style={{
      border: '1px solid var(--border)',
      background: 'var(--surface-alt)',
      borderRadius: 6,
      padding: '10px 12px',
      marginBottom: 8,
      fontSize: 12,
      color: 'var(--text-muted)',
      lineHeight: 1.5,
    }}
  >
    No runner was online, so OpenV is starting a cloud runner for you. The reply follows as soon as it is up,
    usually within a minute. It counts against your workspace&apos;s cloud runner minutes and stops on its own
    when idle.
  </div>
);

// RunnerOfflineNotice is what the chat says when no runner is online to
// answer: turns are saved and wait, and this says how to connect one.
export const RunnerOfflineNotice: React.FC = () => (
  <div
    style={{
      border: '1px solid var(--tint-yellow-border)',
      background: 'var(--tint-yellow)',
      borderRadius: 6,
      padding: '10px 12px',
      marginBottom: 8,
      fontSize: 12,
      color: 'var(--warning-text)',
      lineHeight: 1.5,
    }}
  >
    <div style={{ fontWeight: 700, marginBottom: 4 }}>⚠ V&amp;V Assistant not connected</div>
    No runner is online to answer, so assistant replies are paused. To connect one, open{' '}
    <Link to="/org/settings" style={{ color: 'var(--warning-text)', fontWeight: 600 }}>
      Workspace Settings → Runners
    </Link>{' '}
    and launch the Agent Connector on your machine — or restart it if it was already running.
    Messages you send are saved and will be answered as soon as a runner connects.
  </div>
);
