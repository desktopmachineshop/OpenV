import React, { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Navbar } from '../components/Navbar';
import { releaseAPI, ReleaseEntry, ReleaseInfo } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';

// WhatsNew is what changed for the people who use OpenV, newest release
// first, opened by the active workspace's own channel (REQ-140): a
// stable-channel workspace is told the stable release it is on and the one
// scheduled next, a nightly-channel workspace the release it runs.
//
// It renders the releases the API parsed, never the notes file: that file
// also holds what has not shipped yet and, until this page was rewritten,
// a block of instructions written for contributors that customers were
// being shown.

const SEMVER = /^\d+\.\d+\.\d+$/;

/**
 * The group name the parser gives bullets that sit under no heading. The
 * releases from before OpenV had version numbers are all like that: their
 * bullets simply are the release, so heading them adds a word and no meaning.
 */
const UNGROUPED = 'Changes';

/** The line that opens a release, phrased the way the announcement is. */
export const releaseHeading = (release: ReleaseEntry): string =>
  SEMVER.test(release.version)
    ? `OpenV version upgraded to ${release.version}`
    : `OpenV update ${release.version}`;

/**
 * A bullet, with its markdown rendered inline.
 *
 * Notes are written as a sentence with the odd `code span` or emphasis in
 * them, so they are markdown; but they are one line of a list, so the
 * paragraph react-markdown would wrap them in has to go.
 */
const Note: React.FC<{ text: string }> = ({ text }) => (
  <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ p: ({ children }) => <>{children}</> }}>
    {text}
  </ReactMarkdown>
);

const ReleaseSection: React.FC<{ release: ReleaseEntry; compact: boolean }> = ({
  release,
  compact,
}) => (
  <section className="card" style={{ padding: compact ? '16px 14px' : '20px 24px', marginBottom: 16 }}>
    <h2 style={{ margin: 0, fontSize: compact ? 17 : 19, color: 'var(--text-primary)' }}>
      {releaseHeading(release)}
    </h2>
    {(release.date || release.stable_since) && (
      <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
        {release.date}
        {release.stable_since && (
          <span
            className="badge"
            style={{ marginLeft: release.date ? 8 : 0 }}
            title={`Stable channel release since ${release.stable_since}`}
          >
            Stable release
          </span>
        )}
      </p>
    )}
    {(release.categories || []).map((category) => (
      <div key={category.name} style={{ marginTop: 16 }}>
        {category.name !== UNGROUPED && (
          <h3
            style={{
              margin: 0,
              fontSize: compact ? 14 : 15,
              color: 'var(--text-secondary, var(--text-muted))',
            }}
          >
            {category.name}
          </h3>
        )}
        <ul className="markdown-content" style={{ margin: '6px 0 0', paddingLeft: '1.3em', fontSize: 14 }}>
          {category.notes.map((note, i) => (
            <li key={i} style={{ marginBottom: 4 }}>
              <Note text={note} />
            </li>
          ))}
        </ul>
      </div>
    ))}
  </section>
);

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

  const releases = release?.releases || [];

  // The line that opens the page: which channel the active workspace is on
  // and what that means for what it runs. Empty when no workspace is active,
  // in which case the newest section speaks for itself.
  const channel = features?.channel || org?.release_channel || 'nightly';
  const stableOn = features?.stable_release || org?.stable_release || '';
  const nextAt = features?.next_stable_at ? new Date(features.next_stable_at) : null;
  const channelLine = (): string => {
    if (!org || !release) return '';
    if (channel === 'stable') {
      let line = `${org.name} is on the stable channel and ${
        stableOn ? `runs stable release ${stableOn}` : 'has no stable release yet'
      }`;
      if (features?.next_stable_release && nextAt) {
        line += `; stable release ${features.next_stable_release} turns on ${nextAt.toLocaleString()}`;
      }
      if (features?.preview) line += ' (you are previewing the next release)';
      return line + '.';
    }
    return `${org.name} is on the nightly channel${release.version ? ` and runs release ${release.version}` : ''}.`;
  };
  const opening = channelLine();

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)' }}>
      <Navbar title="What's new" showWorkspaceControls />
      <div style={{ maxWidth: 860, margin: '0 auto', padding: compact ? '16px 12px' : '24px 20px' }}>
        {error && <p style={{ fontSize: 13, color: 'var(--danger-text, #c0392b)' }}>{error}</p>}
        {release && (
          <>
            {/* The newest section below is the release this server runs, so
                without a workspace to speak for, saying so again here would
                only repeat its heading; the note is then for the build that
                names no release at all. */}
            {(opening || !release.version) && (
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
                {opening || 'This build does not name a release yet.'}
              </p>
            )}
            {releases.map((entry) => (
              <ReleaseSection key={entry.version} release={entry} compact={compact} />
            ))}
            {releases.length === 0 && (
              <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                There are no release notes for this build yet.
              </p>
            )}
          </>
        )}
        {!release && !error && <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>Loading…</p>}
      </div>
    </div>
  );
};
