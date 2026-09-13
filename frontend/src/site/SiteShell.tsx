import React, { useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { ISSUES_URL, REPO_URL } from '../landing/content';

// SiteShell is the storefront's frame: the header, the footer and the
// building blocks every public page is laid out with. The pages (Landing,
// How it works, Demos, FAQ, Open source, Customers, White papers) share it
// so a visitor moving between them sees one site, and so the app's own
// chrome (Navbar, sidebar) never leaks into a page a visitor sees signed
// out.
//
// Plain inline styles on the theme tokens, like the rest of the app: no
// webfonts and no third-party scripts, which the frontend's CSP would block
// anyway. Pages must scroll, so the shell uses min-height, not .app-shell.

export const maxWidth = 1080;

export const primaryButton: React.CSSProperties = {
  display: 'inline-block',
  background: 'var(--accent)',
  color: 'var(--accent-fg)',
  padding: '12px 22px',
  borderRadius: 6,
  fontSize: 16,
  fontWeight: 600,
  textDecoration: 'none',
  minHeight: 44,
  boxSizing: 'border-box',
};

export const secondaryButton: React.CSSProperties = {
  ...primaryButton,
  background: 'transparent',
  color: 'var(--text)',
  border: '1px solid var(--border)',
};

export const ExternalLink: React.FC<{ href: string; style?: React.CSSProperties; children: React.ReactNode }> = ({
  href,
  style,
  children,
}) => (
  <a href={href} target="_blank" rel="noreferrer" style={style}>
    {children}
  </a>
);

export const Section: React.FC<{
  id?: string;
  alt?: boolean;
  compact: boolean;
  children: React.ReactNode;
}> = ({ id, alt, compact, children }) => (
  <section
    id={id}
    style={{
      background: alt ? 'var(--surface)' : 'var(--bg-app)',
      borderTop: alt ? '1px solid var(--border-soft)' : 'none',
      borderBottom: alt ? '1px solid var(--border-soft)' : 'none',
      padding: compact ? '40px 16px' : '64px 24px',
    }}
  >
    <div style={{ maxWidth, margin: '0 auto' }}>{children}</div>
  </section>
);

export const H1: React.FC<{ children: React.ReactNode; compact: boolean }> = ({ children, compact }) => (
  <h1 style={{ margin: '0 0 16px', fontSize: compact ? 30 : 40, lineHeight: 1.15, color: 'var(--text)' }}>{children}</h1>
);

export const H2: React.FC<{ children: React.ReactNode; compact: boolean; id?: string }> = ({ children, compact, id }) => (
  <h2 id={id} style={{ margin: '0 0 12px', fontSize: compact ? 26 : 32, lineHeight: 1.2, color: 'var(--text)' }}>
    {children}
  </h2>
);

export const Lead: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <p style={{ margin: '0 0 28px', fontSize: 17, lineHeight: 1.6, color: 'var(--text-body)', maxWidth: 720 }}>{children}</p>
);

export const Eyebrow: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <p
    style={{
      margin: '0 0 14px',
      fontSize: 13,
      fontWeight: 700,
      letterSpacing: 1,
      textTransform: 'uppercase',
      color: 'var(--accent-text)',
    }}
  >
    {children}
  </p>
);

export const Card: React.FC<{ children: React.ReactNode; style?: React.CSSProperties }> = ({ children, style }) => (
  <div
    style={{
      background: 'var(--surface)',
      border: '1px solid var(--border)',
      borderRadius: 10,
      padding: 20,
      minWidth: 0,
      ...style,
    }}
  >
    {children}
  </div>
);

export const Grid: React.FC<{ columns: number; compact: boolean; children: React.ReactNode; gap?: number }> = ({
  columns,
  compact,
  children,
  gap = 16,
}) => (
  <div
    style={{
      display: 'grid',
      gridTemplateColumns: compact ? '1fr' : `repeat(${columns}, minmax(0, 1fr))`,
      gap,
      alignItems: 'stretch',
    }}
  >
    {children}
  </div>
);

/** The site's pages, in the order the header shows them. */
export const SITE_NAV: { to: string; label: string }[] = [
  { to: '/how-it-works', label: 'How it works' },
  { to: '/demos', label: 'Demos' },
  { to: '/pricing', label: 'Pricing' },
  { to: '/faq', label: 'FAQ' },
  { to: '/open-source', label: 'Open source' },
  { to: '/manual', label: 'Manual' },
];

const navLink: React.CSSProperties = {
  color: 'var(--text-secondary)',
  textDecoration: 'none',
  fontSize: 15,
  padding: '8px 4px',
  whiteSpace: 'nowrap',
};

interface SiteShellProps {
  /** The page's document title, after "OpenV". */
  title?: string;
  children: React.ReactNode;
}

export const SiteShell: React.FC<SiteShellProps> = ({ title, children }) => {
  const { isCompact: compact, isPhone: phone } = useViewport();
  const { pathname } = useLocation();

  useEffect(() => {
    const previous = document.title;
    document.title = title ? `${title} · OpenV` : 'OpenV — requirements, traceability and V&V with AI agents';
    return () => {
      document.title = previous;
    };
  }, [title]);

  const current = (to: string) => pathname === to || (to !== '/' && pathname.startsWith(to + '/'));

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)', color: 'var(--text)' }}>
      <header
        className="safe-area-top"
        style={{
          background: 'var(--surface)',
          borderBottom: '1px solid var(--border)',
          position: 'sticky',
          top: 0,
          zIndex: 10,
        }}
      >
        <div
          style={{
            maxWidth,
            margin: '0 auto',
            padding: compact ? '10px 16px' : '12px 24px',
            display: 'flex',
            alignItems: 'center',
            gap: compact ? 12 : 24,
          }}
        >
          <Link to="/" aria-label="OpenV home" style={{ display: 'flex', alignItems: 'center', flexShrink: 0 }}>
            <img src="/Images/logo.png" alt="OpenV" className="app-logo" style={{ height: 36 }} />
          </Link>
          {!phone && (
            <nav aria-label="Site" style={{ display: 'flex', gap: 16, alignItems: 'center', overflowX: 'auto' }}>
              {SITE_NAV.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  aria-current={current(item.to) ? 'page' : undefined}
                  style={{ ...navLink, color: current(item.to) ? 'var(--text)' : navLink.color, fontWeight: current(item.to) ? 600 : 400 }}
                >
                  {item.label}
                </Link>
              ))}
              <ExternalLink href={REPO_URL} style={navLink}>
                GitHub
              </ExternalLink>
            </nav>
          )}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 10, alignItems: 'center', flexShrink: 0 }}>
            <Link to="/login" style={{ ...secondaryButton, padding: '9px 16px', fontSize: 14 }}>
              Sign in
            </Link>
            {!phone && (
              <Link to="/login?mode=register" style={{ ...primaryButton, padding: '9px 16px', fontSize: 14 }}>
                Create free account
              </Link>
            )}
          </div>
        </div>
        {phone && (
          <nav
            aria-label="Site"
            style={{
              display: 'flex',
              gap: 4,
              padding: '0 8px 6px',
              overflowX: 'auto',
              WebkitOverflowScrolling: 'touch',
            }}
          >
            {SITE_NAV.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                aria-current={current(item.to) ? 'page' : undefined}
                style={{ ...navLink, fontSize: 14, padding: '6px 8px', color: current(item.to) ? 'var(--text)' : navLink.color, fontWeight: current(item.to) ? 600 : 400 }}
              >
                {item.label}
              </Link>
            ))}
          </nav>
        )}
      </header>

      <main>{children}</main>

      <footer
        className="safe-area-bottom"
        style={{
          padding: compact ? '24px 16px' : '32px 24px',
          borderTop: '1px solid var(--border)',
          background: 'var(--bg-app)',
        }}
      >
        <div
          style={{
            maxWidth,
            margin: '0 auto',
            display: 'grid',
            gridTemplateColumns: compact ? '1fr' : 'repeat(4, minmax(0, 1fr))',
            gap: compact ? 18 : 24,
            fontSize: 14,
            color: 'var(--text-muted)',
          }}
        >
          <div>
            <div style={{ fontWeight: 700, color: 'var(--text)', marginBottom: 6 }}>OpenV</div>
            <div>Requirements, traceability and V&amp;V evidence, with AI agents that work inside the audit trail.</div>
            <div style={{ marginTop: 8 }}>AGPL-3.0 · built in the open</div>
          </div>
          <FooterColumn
            heading="Product"
            links={[
              { to: '/how-it-works', label: 'How it works' },
              { to: '/demos', label: 'Demo videos' },
              { to: '/pricing', label: 'Pricing' },
              { to: '/faq', label: 'Security and FAQ' },
            ]}
          />
          <FooterColumn
            heading="Community"
            links={[
              { to: '/open-source', label: 'Open-source projects' },
              { to: '/customers', label: 'Customer stories' },
              { to: '/white-papers', label: 'White papers' },
              { href: REPO_URL, label: 'GitHub' },
              { href: ISSUES_URL, label: 'Issues' },
            ]}
          />
          <FooterColumn
            heading="Use it"
            links={[
              { to: '/login', label: 'Sign in' },
              { to: '/login?mode=register', label: 'Create free account' },
              { to: '/manual', label: 'Manual' },
            ]}
          />
        </div>
      </footer>
    </div>
  );
};

const FooterColumn: React.FC<{
  heading: string;
  links: { to?: string; href?: string; label: string }[];
}> = ({ heading, links }) => (
  <div>
    <div style={{ fontWeight: 600, color: 'var(--text)', marginBottom: 6 }}>{heading}</div>
    <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
      {links.map((l) => (
        <li key={l.label}>
          {l.href ? (
            <ExternalLink href={l.href} style={{ ...navLink, padding: 0 }}>
              {l.label}
            </ExternalLink>
          ) : (
            <Link to={l.to || '/'} style={{ ...navLink, padding: 0 }}>
              {l.label}
            </Link>
          )}
        </li>
      ))}
    </ul>
  </div>
);
