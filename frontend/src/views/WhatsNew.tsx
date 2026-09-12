import React, { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Navbar } from '../components/Navbar';
import { releaseAPI, ReleaseInfo } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useViewport } from '../hooks/useViewport';

// WhatsNew is the release notes the running server was built with: the
// current release first, then the whole history, as the file is written.
export const WhatsNew: React.FC = () => {
  const { isCompact: compact } = useViewport();
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

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)' }}>
      <Navbar title="What's new" showWorkspaceControls />
      <div style={{ maxWidth: 860, margin: '0 auto', padding: compact ? '16px 12px' : '24px 20px' }}>
        {error && (
          <p style={{ fontSize: 13, color: 'var(--danger-text, #c0392b)' }}>{error}</p>
        )}
        {release && (
          <>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
              {release.version
                ? `You are on release ${release.version}.`
                : 'This build does not name a release yet.'}
            </p>
            <div className="card markdown-content" style={{ padding: compact ? '16px 14px' : '24px 32px' }}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{release.history}</ReactMarkdown>
            </div>
          </>
        )}
        {!release && !error && (
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>Loading…</p>
        )}
      </div>
    </div>
  );
};
