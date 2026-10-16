import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { RELEASES, UNGROUPED, isNumbered, releaseAnchor, type ChangelogRelease } from './changelog';
import { Eyebrow, ExternalLink, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// The public changelog: every release OpenV has shipped, newest first, from
// the same notes the app announces to members on What's new. In-app
// /whats-new stays behind sign-in (it follows the workspace's channel);
// this page is the one crawlers and buyers read, and it is pre-rendered, so
// the history is in the HTML rather than fetched after load.

const REPO_BLOB = 'https://github.com/desktopmachineshop/OpenV/blob/master/';
const NOTES_URL = `${REPO_BLOB}RELEASE_NOTES.md`;

const body: React.CSSProperties = { margin: '0 0 12px', fontSize: 16, lineHeight: 1.65, color: 'var(--text-body)', maxWidth: 760 };

const formatDate = (iso: string) =>
  iso
    ? new Date(`${iso}T00:00:00Z`).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' })
    : '';

/**
 * A note links to repository files by relative path (`docs/extending.md`),
 * which on this site would be a 404; point those at the file on GitHub.
 */
export const noteHref = (href: string): string =>
  /^(https?:|mailto:|#|\/)/.test(href) ? href : REPO_BLOB + href.replace(/^\.\//, '');

const Note: React.FC<{ text: string }> = ({ text }) => (
  <ReactMarkdown
    remarkPlugins={[remarkGfm]}
    urlTransform={noteHref}
    components={{ p: ({ children }) => <>{children}</> }}
  >
    {text}
  </ReactMarkdown>
);

/** The release's heading: its version, or for an early dated one, the day. */
export const releaseTitle = (r: ChangelogRelease): string =>
  isNumbered(r) ? `OpenV ${r.version}` : `Update of ${formatDate(r.date)}`;

const Release: React.FC<{ release: ChangelogRelease; compact: boolean }> = ({ release, compact }) => (
  <article id={releaseAnchor(release)} style={{ padding: compact ? '18px 0' : '22px 0', borderTop: '1px solid var(--border)' }}>
    <h2 style={{ margin: 0, fontSize: compact ? 19 : 21, color: 'var(--text)' }}>
      <a href={`#${releaseAnchor(release)}`} style={{ color: 'inherit', textDecoration: 'none' }}>
        {releaseTitle(release)}
      </a>
    </h2>
    <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
      {isNumbered(release) && release.date && <time dateTime={release.date}>{formatDate(release.date)}</time>}
      {release.stableSince && (
        <span>
          {isNumbered(release) && release.date ? ' · ' : ''}stable release since{' '}
          <time dateTime={release.stableSince}>{formatDate(release.stableSince)}</time>
        </span>
      )}
    </p>
    {release.groups.map((g) => (
      <div key={g.name} style={{ marginTop: 14 }}>
        {g.name !== UNGROUPED && <h3 style={{ margin: 0, fontSize: 15, color: 'var(--text-muted)' }}>{g.name}</h3>}
        <ul style={{ ...body, margin: '6px 0 0', paddingLeft: 22, fontSize: 15 }}>
          {g.notes.map((n, i) => (
            <li key={i} style={{ marginBottom: 8 }}>
              <Note text={n} />
            </li>
          ))}
        </ul>
      </div>
    ))}
  </article>
);

export const Changelog: React.FC = () => {
  const { isCompact: compact } = useViewport();
  const latest = RELEASES[0];
  const first = RELEASES[RELEASES.length - 1];
  return (
    <SiteShell title="Changelog">
      <Section compact={compact}>
        <Eyebrow>Changelog</Eyebrow>
        <H1 compact={compact}>What changed in OpenV, release by release</H1>
        {latest && first && (
          <Lead>
            OpenV has shipped {RELEASES.length} releases since {formatDate(first.date)}; the latest is{' '}
            {releaseTitle(latest)}, released on {formatDate(latest.date)}. Every release below is listed with what a
            workspace member will notice: new features, security fixes, maintenance updates and bug fixes.
          </Lead>
        )}
        <p style={body}>
          These are the notes the app shows its members on What&rsquo;s new, and the same notes are in{' '}
          <ExternalLink href={NOTES_URL}>RELEASE_NOTES.md</ExternalLink> in the source repository. A new feature
          raises the minor version and fixes alone raise the patch. Workspaces on the nightly channel get each release
          as it ships. On the stable channel, a workspace gets a release once it has run on nightly for at least seven
          days, and a security fix at once.
        </p>
      </Section>
      <Section compact={compact} alt>
        <H2 compact={compact} id="releases">
          Releases, newest first
        </H2>
        {RELEASES.map((r) => (
          <Release key={r.version} release={r} compact={compact} />
        ))}
      </Section>
      <Section compact={compact}>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <Link to="/login?mode=register" style={primaryButton}>
            Create a free account
          </Link>
          <Link to="/how-it-works" style={secondaryButton}>
            How it works
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
