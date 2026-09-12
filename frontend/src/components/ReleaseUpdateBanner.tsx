import React, { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { releaseAPI } from '../api/client';

/** How often an open tab asks which release is behind it. */
export const RELEASE_POLL_MS = 10 * 60 * 1000;

// ReleaseUpdateBanner notices that the API behind an open tab has moved to a
// newer release than the one the tab loaded with, and offers a reload. The
// bell carries the release notification too, but the bell only reaches a
// tab that is still on speaking terms with the API: a frontend built for
// the previous release may not be, so this asks the smallest possible
// question — which release are you — on a timer and whenever the tab comes
// back into view.
export const ReleaseUpdateBanner: React.FC = () => {
  const loadedVersion = useRef<string | null>(null);
  const [newer, setNewer] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      try {
        const res = await releaseAPI.current();
        if (cancelled) return;
        const version = res.data.version || '';
        if (loadedVersion.current === null) {
          loadedVersion.current = version;
        } else if (version && version !== loadedVersion.current) {
          setNewer(version);
        }
      } catch {
        // A release check that fails is not news; the next one may succeed.
      }
    };
    void check();
    const timer = window.setInterval(check, RELEASE_POLL_MS);
    const onVisible = () => {
      if (document.visibilityState === 'visible') void check();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, []);

  if (!newer) return null;

  return (
    <div
      role="status"
      style={{
        position: 'fixed',
        left: '50%',
        bottom: 'calc(16px + env(safe-area-inset-bottom, 0px))',
        transform: 'translateX(-50%)',
        zIndex: 1000,
        maxWidth: 'calc(100vw - 32px)',
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        flexWrap: 'wrap',
        padding: '10px 14px',
        borderRadius: 8,
        background: 'var(--surface)',
        color: 'var(--text)',
        border: '1px solid var(--border)',
        boxShadow: '0 6px 24px rgba(0,0,0,0.18)',
        fontSize: 13,
      }}
    >
      <span>
        OpenV has been updated ({newer}).{' '}
        <Link to="/whats-new" style={{ color: 'var(--accent)' }}>
          See what's new
        </Link>
      </span>
      <button type="button" className="button-primary" onClick={() => window.location.reload()}>
        Reload
      </button>
    </div>
  );
};
