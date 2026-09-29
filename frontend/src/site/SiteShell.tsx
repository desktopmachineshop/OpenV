import React, { useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { ISSUES_URL, REPO_URL, TAGLINE } from '../landing/content';
import '../brand/tokens.css';
import '../brand/components.css';
import './site.css';

// SiteShell is the storefront's frame: the header, the footer and the
// building blocks every public page is laid out with. The pages (Landing,
// How it works, Demos, FAQ, Open source, Customers, White papers) share it
// so a visitor moving between them sees one site, and so the app's own
// chrome (Navbar, sidebar) never leaks into a page a visitor sees signed
// out.
//
// The site wears the OpenV brand (docs/brand): the .ov-brand root scopes the
// brand tokens, IBM Plex and the ov- component classes to these pages, so
// the signed-in app keeps theme.css until it migrates. Layout lives in
// site.css; fonts are self-hosted from /fonts, which the CSP's font-src
// 'self' allows. Pages must scroll, so the shell uses min-height, not
// .app-shell.

/** Button classes from the brand's components.css. */
export const buttonClass = {
  primary: 'ov-btn ov-btn--primary',
  secondary: 'ov-btn',
  primaryLarge: 'ov-btn ov-btn--primary ov-btn--lg',
  secondaryLarge: 'ov-btn ov-btn--lg',
  small: 'ov-btn ov-btn--sm',
};

export const ExternalLink: React.FC<{
  href: string;
  style?: React.CSSProperties;
  className?: string;
  children: React.ReactNode;
}> = ({ href, style, className, children }) => (
  <a href={href} target="_blank" rel="noreferrer" style={style} className={className}>
    {children}
  </a>
);

export const Section: React.FC<{
  id?: string;
  alt?: boolean;
  hero?: boolean;
  /** Kept for callers; spacing now follows the viewport in site.css. */
  compact?: boolean;
  children: React.ReactNode;
}> = ({ id, alt, hero, children }) => (
  <section id={id} className={['site-section', alt ? 'site-section--alt' : '', hero ? 'site-section--hero' : ''].join(' ').trim()}>
    <div className="site-wrap">{children}</div>
  </section>
);

export const H1: React.FC<{ children: React.ReactNode; compact?: boolean }> = ({ children }) => (
  <h1 className="site-h1">{children}</h1>
);

export const H2: React.FC<{ children: React.ReactNode; compact?: boolean; id?: string }> = ({ children, id }) => (
  <h2 id={id} className="site-h2">
    {children}
  </h2>
);

export const Lead: React.FC<{ children: React.ReactNode }> = ({ children }) => <p className="site-lead">{children}</p>;

/** A page's one label above its title: the page name, in plain words. */
export const Eyebrow: React.FC<{ children: React.ReactNode }> = ({ children }) => <p className="site-eyebrow">{children}</p>;

export const Card: React.FC<{ children: React.ReactNode; style?: React.CSSProperties }> = ({ children, style }) => (
  <div className="site-tile" style={style}>
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

interface SiteShellProps {
  /** The page's document title, after "OpenV". */
  title?: string;
  children: React.ReactNode;
}

export const SiteShell: React.FC<SiteShellProps> = ({ title, children }) => {
  const { pathname } = useLocation();

  useEffect(() => {
    const previous = document.title;
    document.title = title ? `${title} · OpenV` : 'OpenV: requirements, traceability and V&V with AI agents';
    return () => {
      document.title = previous;
    };
  }, [title]);

  const current = (to: string) => pathname === to || (to !== '/' && pathname.startsWith(to + '/'));
  const navLinks = (
    <>
      {SITE_NAV.map((item) => (
        <Link key={item.to} to={item.to} className="site-nav-link" aria-current={current(item.to) ? 'page' : undefined}>
          {item.label}
        </Link>
      ))}
      <ExternalLink href={REPO_URL} className="site-nav-link">
        GitHub
      </ExternalLink>
    </>
  );

  return (
    <div className="ov-brand site">
      <header className="site-header safe-area-top">
        <div className="site-wrap site-header-row">
          <Link to="/" aria-label="OpenV home" className="site-brand">
            <img src="/icon-192.png" alt="" />
            <span>OpenV</span>
          </Link>
          <nav aria-label="Site" className="site-nav">
            {navLinks}
          </nav>
          <div className="site-header-actions">
            <Link to="/login" className="ov-btn ov-btn--ghost ov-btn--sm">
              Sign in
            </Link>
            <Link to="/login?mode=register" className="ov-btn ov-btn--primary ov-btn--sm">
              Create free account
            </Link>
          </div>
        </div>
        <nav aria-label="Pages" className="site-nav--phone">
          {navLinks}
        </nav>
      </header>

      <main>{children}</main>

      <footer className="site-footer safe-area-bottom">
        <div className="site-wrap site-footer-grid">
          <div>
            <div className="site-brand" style={{ marginBottom: 12 }}>
              <img src="/icon-192.png" alt="" />
              <span>OpenV</span>
            </div>
            <p style={{ margin: '0 0 8px', maxWidth: '40ch' }}>{TAGLINE}</p>
            <p style={{ margin: 0 }}>Elastic License 2.0, built in the open.</p>
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
    <h2>{heading}</h2>
    <ul>
      {links.map((l) => (
        <li key={l.label}>
          {l.href ? <ExternalLink href={l.href}>{l.label}</ExternalLink> : <Link to={l.to || '/'}>{l.label}</Link>}
        </li>
      ))}
    </ul>
  </div>
);
