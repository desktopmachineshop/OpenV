import React, { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Navbar } from '../components/Navbar';
import { releaseAPI, ReleaseInfo } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';

// WhatsNew is the release notes the running server was built with, with
// the active workspace's own channel first (REQ-140): a stable-channel
// workspace sees the stable release it is on and the one scheduled next,
// a nightly-channel workspace the nightly it runs; then the whole history
// as the file is written.
export const WhatsNew: React.FC = () => {
  const { isCompact: compact } = useViewport();
  const { orgs, activeOrgId, features } = useAppStore();
  const org = orgs.find((o) => o.id === activeOrgId);
  const [release, setRelease] = useState<ReleaseInfo | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    releaseAPI
      .current()
      .then((res) => {
        if (!cancelled) setRelease(res.data);
      })
      .catch((err) => {
        if (!cancelled) setError(apiErrorMessage(err, 'Could not load the release notes'));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const channel = features?.channel || org?.release_channel || 'nightly';
  const stableOn = features?.stable_release || org?.stable_release || '';
  const nextAt = features?.next_stable_at ? new Date(features.next_stable_at) : null;

  const channelLine = () => {
    if (!org) return '';
    if (channel === 'stable') {
      const parts = [`${org.name} is on the stable channel`];
      parts.push(stableOn ? `and runs stable release ${stableOn}` : 'and has no stable release yet');
      if (features?.next_stable_release && nextAt) {
        parts.push(`; stable release ${features.next_stable_release} turns on ${nextAt.toLocaleString()}`);
      }
      if (features?.preview) parts.push(' (you are previewing the next release)');
      return parts.join(' ') + '.';
    }
    return `${org.name} is on the nightly channel${release?.version ? ` and runs release ${release.version}` : ''}.`;
  };

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)' }}>
      <Navbar title="What's new" showWorkspaceControls />
      <div style={{ maxWidth: 860, margin: '0 auto', padding: compact ? '16px 12px' : '24px 20px' }}>
        {error && <p style={{ fontSize: 13, color: 'var(--danger-text, #c0392b)' }}>{error}</p>}
        {release && (
          <>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
              {channelLine() || (release.version ? `This build is release ${release.version}.` : 'This build does not name a release yet.')}
            </p>
            {channel === 'stable' && release.stable && (
              <div className="card markdown-content" style={{ padding: compact ? '16px 14px' : '24px 32px' }}>
                <h2 style={{ marginTop: 0 }}>Stable release {release.stable.version}</h2>
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{release.stable.markdown}</ReactMarkdown>
              </div>
            )}
            <div className="card markdown-content" style={{ padding: compact ? '16px 14px' : '24px 32px' }}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{release.history}</ReactMarkdown>
            </div>
          </>
        )}
        {!release && !error && <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>Loading…</p>}
      </div>
    </div>
  );
};
