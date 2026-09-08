import React, { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import {
  ALPHA_NOTE,
  BUSINESS_LIFE_LIMITS,
  DATA_PROMISE,
  EXPORT_FORMATS,
  FEATURES,
  FEEDBACK_ISSUE_URL,
  HOSTED_LIMITS,
  HOSTED_TIERS,
  IMPORT_FORMATS,
  ISSUES_URL,
  LICENSE_GLOSS,
  LICENSE_URL,
  OTHER_TIERS,
  PRICING_FOOTNOTE,
  PricingTier,
  QUICKSTART_URL,
  REPO_URL,
  SELF_HOST_COMMANDS,
  SUBLINE,
  TAGLINE,
} from '../landing/content';

// The public landing page: what OpenV does, the hosting terms in force and
// the promise that data leaves freely. Rendered at "/" for visitors without a
// session (App.tsx sends signed-in users to /projects) and at "/pricing"
// scrolled to the pricing section. Copy lives in landing/content.ts.
//
// Plain inline styles on the theme tokens, like the rest of the app: no
// webfonts and no third-party scripts, which the frontend's CSP would block
// anyway. The page must scroll, so it uses min-height rather than .app-shell.

interface LandingProps {
  section?: 'pricing';
}

const maxWidth = 1080;

const Section: React.FC<{
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

const H2: React.FC<{ children: React.ReactNode; compact: boolean }> = ({ children, compact }) => (
  <h2 style={{ margin: '0 0 12px', fontSize: compact ? 26 : 32, lineHeight: 1.2, color: 'var(--text)' }}>{children}</h2>
);

const Lead: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <p style={{ margin: '0 0 28px', fontSize: 17, lineHeight: 1.6, color: 'var(--text-body)', maxWidth: 720 }}>{children}</p>
);

const primaryButton: React.CSSProperties = {
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

const secondaryButton: React.CSSProperties = {
  ...primaryButton,
  background: 'transparent',
  color: 'var(--text)',
  border: '1px solid var(--border)',
};

const ExternalLink: React.FC<{ href: string; style?: React.CSSProperties; children: React.ReactNode }> = ({
  href,
  style,
  children,
}) => (
  <a href={href} target="_blank" rel="noreferrer" style={style}>
    {children}
  </a>
);

const TierCard: React.FC<{ tier: PricingTier; compact: boolean }> = ({ tier, compact }) => (
  <article
    aria-labelledby={`tier-${tier.id}`}
    style={{
      background: 'var(--bg-app)',
      border: `1px solid ${tier.available ? 'var(--border)' : 'var(--border-soft)'}`,
      borderRadius: 10,
      padding: compact ? 20 : 22,
      display: 'flex',
      flexDirection: 'column',
      gap: 10,
      minWidth: 0,
    }}
  >
    <h3 id={`tier-${tier.id}`} style={{ margin: 0, fontSize: 19, color: 'var(--text)' }}>
      {tier.name}
    </h3>
    {tier.available ? (
      <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--success-text)' }}>{tier.price}</div>
    ) : (
      <div>
        <span
          style={{
            display: 'inline-block',
            fontSize: 12,
            fontWeight: 700,
            letterSpacing: 0.5,
            textTransform: 'uppercase',
            color: 'var(--accent-text)',
            background: 'var(--tint-blue)',
            borderRadius: 999,
            padding: '5px 10px',
          }}
        >
          {tier.price}
        </span>
      </div>
    )}
    <p style={{ margin: 0, fontSize: 14, lineHeight: 1.5, color: 'var(--text-body)' }}>{tier.summary}</p>
    <ul style={{ margin: 0, paddingLeft: 18, fontSize: 14, lineHeight: 1.6, color: 'var(--text-body)', flex: 1 }}>
      {tier.points.map((p) => (
        <li key={p}>{p}</li>
      ))}
    </ul>
    {tier.cta.external ? (
      <ExternalLink href={tier.cta.href} style={{ ...secondaryButton, textAlign: 'center', fontSize: 14, padding: '10px 14px' }}>
        {tier.cta.label}
      </ExternalLink>
    ) : (
      <Link to={tier.cta.href} style={{ ...primaryButton, textAlign: 'center', fontSize: 14, padding: '10px 14px' }}>
        {tier.cta.label}
      </Link>
    )}
  </article>
);

export const Landing: React.FC<LandingProps> = ({ section }) => {
  const viewport = useViewport();
  const compact = viewport.isCompact;
  const phone = viewport.isPhone;

  useEffect(() => {
    if (section === 'pricing') {
      document.getElementById('pricing')?.scrollIntoView({ block: 'start' });
    } else {
      window.scrollTo(0, 0);
    }
  }, [section]);

  const navLink: React.CSSProperties = {
    color: 'var(--text-secondary)',
    textDecoration: 'none',
    fontSize: 15,
    padding: '8px 4px',
  };

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
            flexWrap: 'wrap',
          }}
        >
          <Link to="/" aria-label="OpenV home" style={{ display: 'flex', alignItems: 'center' }}>
            <img src="/Images/logo.png" alt="OpenV" className="app-logo" style={{ height: 36 }} />
          </Link>
          {!phone && (
            <nav aria-label="Site" style={{ display: 'flex', gap: 18, alignItems: 'center' }}>
              <a href="#pricing" style={navLink}>
                Pricing
              </a>
              <Link to="/manual" style={navLink}>
                Manual
              </Link>
              <ExternalLink href={REPO_URL} style={navLink}>
                GitHub
              </ExternalLink>
            </nav>
          )}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 10, alignItems: 'center' }}>
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
      </header>

      <main>
        <Section compact={compact}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: compact ? '1fr' : 'minmax(0, 5fr) minmax(0, 6fr)',
              gap: compact ? 28 : 48,
              alignItems: 'center',
            }}
          >
            <div>
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
                Open source · Free alpha · Bring your own AI
              </p>
              <h1 style={{ margin: '0 0 16px', fontSize: compact ? 30 : 40, lineHeight: 1.15, color: 'var(--text)' }}>
                {TAGLINE}
              </h1>
              <p style={{ margin: '0 0 24px', fontSize: 17, lineHeight: 1.6, color: 'var(--text-body)' }}>{SUBLINE}</p>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                <Link to="/login?mode=register" style={primaryButton}>
                  Create free account
                </Link>
                <a href="#pricing" style={secondaryButton}>
                  See what free means
                </a>
              </div>
              <p style={{ margin: '18px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                No card. No trial clock. Export your data whenever you like.
              </p>
            </div>
            <figure style={{ margin: 0, minWidth: 0 }}>
              <img
                src="/Images/screenshot-requirements.png"
                alt="The OpenV requirements module: a tree of requirements with stable refs beside the selected requirement's document and traceability links"
                style={{
                  width: '100%',
                  height: 'auto',
                  borderRadius: 8,
                  border: '1px solid var(--border)',
                  boxShadow: '0 12px 32px rgba(0,0,0,0.18)',
                  display: 'block',
                }}
              />
              <figcaption style={{ marginTop: 8, fontSize: 12, color: 'var(--text-muted)', textAlign: 'center' }}>
                The requirements module: tree, document and traceability side by side.
              </figcaption>
            </figure>
          </div>
        </Section>

        <Section alt compact={compact} id="features">
          <H2 compact={compact}>What it does</H2>
          <Lead>
            One place for what your product must do, why, and the proof that it does. Every artifact has a stable ref, a
            history and typed links, so the answer to “what changed and what does it affect” is a click, not a meeting.
          </Lead>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: compact ? '1fr' : 'repeat(2, minmax(0, 1fr))',
              gap: 20,
            }}
          >
            {FEATURES.map((f) => (
              <div
                key={f.title}
                style={{
                  background: 'var(--bg-app)',
                  border: '1px solid var(--border)',
                  borderRadius: 8,
                  padding: 20,
                }}
              >
                <h3 style={{ margin: '0 0 8px', fontSize: 18, color: 'var(--text)' }}>{f.title}</h3>
                <p style={{ margin: 0, fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)' }}>{f.body}</p>
              </div>
            ))}
          </div>
        </Section>

        <Section compact={compact} id="data">
          <H2 compact={compact}>Your data is yours</H2>
          <Lead>{DATA_PROMISE}</Lead>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: compact ? '1fr' : 'repeat(auto-fit, minmax(190px, 1fr))',
              gap: 14,
            }}
          >
            {EXPORT_FORMATS.map((f) => (
              <div key={f.label} style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 8, padding: 16 }}>
                <div style={{ fontWeight: 600, marginBottom: 6, color: 'var(--text)' }}>{f.label}</div>
                <div style={{ fontSize: 14, lineHeight: 1.5, color: 'var(--text-body)' }}>{f.body}</div>
              </div>
            ))}
          </div>
          <p style={{ margin: '18px 0 0', fontSize: 15, color: 'var(--text-body)' }}>
            Import reads {IMPORT_FORMATS}. Every download can snapshot a baseline instead of the live project, and the
            archive download carries your attachments with it.
          </p>
        </Section>

        <Section alt compact={compact} id="pricing">
          <H2 compact={compact}>Pricing</H2>
          <Lead>Free while in alpha, free forever to self-host, and a clear path when your team grows.</Lead>
          <div
            role="note"
            style={{
              marginBottom: 24,
              padding: '14px 18px',
              borderRadius: 8,
              background: 'var(--tint-blue)',
              border: '1px solid var(--tint-blue-border)',
              color: 'var(--text)',
              fontSize: 15,
              lineHeight: 1.6,
            }}
          >
            {ALPHA_NOTE}
          </div>
          <h3 style={{ margin: '0 0 14px', fontSize: 20, color: 'var(--text)' }}>Hosted by us</h3>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: phone ? '1fr' : compact ? 'repeat(2, minmax(0, 1fr))' : 'repeat(4, minmax(0, 1fr))',
              gap: 16,
              alignItems: 'stretch',
            }}
          >
            {HOSTED_TIERS.map((tier) => (
              <TierCard key={tier.id} tier={tier} compact={compact} />
            ))}
          </div>

          <div
            style={{
              marginTop: 24,
              background: 'var(--bg-app)',
              border: '1px solid var(--border)',
              borderRadius: 10,
              padding: compact ? 18 : 24,
            }}
          >
            <h3 style={{ margin: '0 0 10px', fontSize: 18, color: 'var(--text)' }}>What free means on the hosted service today</h3>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 15, lineHeight: 1.7, color: 'var(--text-body)' }}>
              {HOSTED_LIMITS.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            <p style={{ margin: '14px 0 6px', fontSize: 15, color: 'var(--text-body)' }}>Business Life will raise the cloud runner to:</p>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 15, lineHeight: 1.7, color: 'var(--text-body)' }}>
              {BUSINESS_LIFE_LIMITS.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            <p style={{ margin: '14px 0 0', fontSize: 14, lineHeight: 1.6, color: 'var(--text-muted)' }}>{PRICING_FOOTNOTE}</p>
          </div>

          <h3 style={{ margin: '32px 0 14px', fontSize: 20, color: 'var(--text)' }}>Other ways to run OpenV</h3>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: compact ? '1fr' : 'repeat(2, minmax(0, 1fr))',
              gap: 16,
              alignItems: 'stretch',
            }}
          >
            {OTHER_TIERS.map((tier) => (
              <TierCard key={tier.id} tier={tier} compact={compact} />
            ))}
          </div>
        </Section>

        <Section compact={compact} id="self-host">
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: compact ? '1fr' : 'minmax(0, 1fr) minmax(0, 1fr)',
              gap: compact ? 24 : 48,
              alignItems: 'start',
            }}
          >
            <div>
              <H2 compact={compact}>Self-host in one command</H2>
              <Lead>
                Docker is the only prerequisite. The stack brings its own database, migrates itself on boot, and the first
                account to register becomes the admin.
              </Lead>
              <ExternalLink href={QUICKSTART_URL} style={secondaryButton}>
                Read the quick start
              </ExternalLink>
            </div>
            <pre
              style={{
                margin: 0,
                background: 'var(--sidebar-bg)',
                color: '#ecf0f1',
                padding: 20,
                borderRadius: 8,
                fontSize: 14,
                lineHeight: 1.7,
                overflowX: 'auto',
              }}
            >
              {SELF_HOST_COMMANDS.map((c) => `$ ${c}`).join('\n')}
            </pre>
          </div>
        </Section>

        <Section alt compact={compact} id="open-source">
          <H2 compact={compact}>Open source, and built in the open</H2>
          <Lead>{LICENSE_GLOSS}</Lead>
          <p style={{ margin: '0 0 20px', fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)', maxWidth: 720 }}>
            OpenV manages its own requirements in OpenV: the live project holds every requirement the platform is built
            to, with traceability to the tests that prove it. Contributions are welcome under the Developer Certificate of
            Origin.
          </p>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <ExternalLink href={REPO_URL} style={primaryButton}>
              View on GitHub
            </ExternalLink>
            <ExternalLink href={FEEDBACK_ISSUE_URL} style={secondaryButton}>
              Send alpha feedback
            </ExternalLink>
            <ExternalLink href={LICENSE_URL} style={secondaryButton}>
              Read the licence
            </ExternalLink>
          </div>
        </Section>
      </main>

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
            display: 'flex',
            flexWrap: 'wrap',
            gap: 18,
            alignItems: 'center',
            fontSize: 14,
            color: 'var(--text-muted)',
          }}
        >
          <span>OpenV · AGPL-3.0</span>
          <Link to="/manual" style={navLink}>
            Manual
          </Link>
          <a href="#pricing" style={navLink}>
            Pricing
          </a>
          <ExternalLink href={REPO_URL} style={navLink}>
            GitHub
          </ExternalLink>
          <ExternalLink href={ISSUES_URL} style={navLink}>
            Issues
          </ExternalLink>
          <Link to="/login" style={navLink}>
            Sign in
          </Link>
        </div>
      </footer>
    </div>
  );
};
