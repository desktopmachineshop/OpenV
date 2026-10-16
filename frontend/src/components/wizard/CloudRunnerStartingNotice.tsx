import React from 'react';

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
