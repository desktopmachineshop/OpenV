import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { billingAPI, PublicPlans } from '../api/client';
import { useViewport } from '../hooks/useViewport';
import {
  ALPHA_NOTE,
  BUSINESS_LIMITS,
  BUSINESS_LITE_LIMITS,
  DATA_PROMISE,
  EXPORT_FORMATS,
  FEATURES,
  FEEDBACK_ISSUE_URL,
  HOSTED_LIMITS,
  HOSTED_TIERS,
  IMPORT_FORMATS,
  LICENSE_GLOSS,
  LICENSE_INTENT,
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
import { DEMOS } from '../site/content';
import {
  Card,
  ExternalLink,
  Eyebrow,
  H2,
  Lead,
  Section,
  SiteShell,
  primaryButton,
  secondaryButton,
} from '../site/SiteShell';

// The public landing page: what OpenV does, the hosting terms in force and
// the promise that data leaves freely. Rendered at "/" for visitors without a
// session (App.tsx sends signed-in users to /projects) and at "/pricing"
// scrolled to the pricing section. Copy lives in landing/content.ts; the
// frame and building blocks in site/SiteShell.tsx, shared with the other
// storefront pages.

interface LandingProps {
  section?: 'pricing';
}

/** A tier's live price, when the platform has one confirmed for it. */
interface LivePrice {
  text: string;
  perSeat: boolean;
  taxNote: string;
}

const formatAmount = (minor: number, currency: string): string =>
  new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: minor % 100 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(minor / 100);

/** The monthly price to show for a tier: the plan's month interval in GBP
 *  where offered, else the first currency it is offered in. No price in
 *  the repository — this reads what the provider confirmed. */
export function livePrice(plans: PublicPlans | null, tier: PricingTier): LivePrice | null {
  if (!plans?.billing_enabled || !tier.planKey) return null;
  const plan = plans.plans.find((p) => p.plan === tier.planKey);
  const month = plan?.intervals?.month;
  if (!plan || !month) return null;
  const currency = 'gbp' in month.amounts ? 'gbp' : Object.keys(month.amounts)[0];
  if (!currency) return null;
  return {
    text: formatAmount(month.amounts[currency], currency),
    perSeat: plan.per_seat,
    taxNote: month.tax_behavior === 'exclusive' ? 'excluding VAT' : '',
  };
}

const TierCard: React.FC<{ tier: PricingTier; compact: boolean; live?: LivePrice | null }> = ({ tier, compact, live }) => (
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
    ) : live ? (
      <div data-testid={`live-price-${tier.id}`}>
        <span style={{ fontSize: 28, fontWeight: 700, color: 'var(--text)' }}>{live.text}</span>
        <span style={{ fontSize: 14, color: 'var(--text-muted)' }}>
          {' '}
          / {live.perSeat ? 'member / ' : ''}month{live.taxNote ? `, ${live.taxNote}` : ''}
        </span>
      </div>
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
    {live ? (
      <Link to="/login?mode=register" style={{ ...secondaryButton, textAlign: 'center', fontSize: 14, padding: '10px 14px' }}>
        Start on {tier.name}
      </Link>
    ) : tier.cta.external ? (
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

  // Live prices, when the platform has some: the catalogue is served from
  // the platform's own cache, and a page with no answer shows its usual copy.
  const [plans, setPlans] = useState<PublicPlans | null>(null);
  useEffect(() => {
    let alive = true;
    billingAPI
      .publicPlans()
      .then((res) => {
        if (alive) setPlans(res.data);
      })
      .catch(() => {
        /* the page reads exactly as it does with no provider */
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (section === 'pricing') {
      document.getElementById('pricing')?.scrollIntoView({ block: 'start' });
    } else {
      window.scrollTo(0, 0);
    }
  }, [section]);

  return (
    <SiteShell>
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
              <Eyebrow>Open source · Free alpha · Bring your own AI</Eyebrow>
              <h1 style={{ margin: '0 0 16px', fontSize: compact ? 30 : 40, lineHeight: 1.15, color: 'var(--text)' }}>
                {TAGLINE}
              </h1>
              <p style={{ margin: '0 0 24px', fontSize: 17, lineHeight: 1.6, color: 'var(--text-body)' }}>{SUBLINE}</p>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                <Link to="/login?mode=register" style={primaryButton}>
                  Create free account
                </Link>
                <Link to="/demos" style={secondaryButton}>
                  Watch the demos
                </Link>
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

        <Section alt compact={compact} id="see-it">
          <H2 compact={compact}>See it working</H2>
          <Lead>
            Five narrated recordings on the platform’s own requirements project, three on a desktop and two on a phone.
            No slides, nothing staged.
          </Lead>
          <div style={{ display: 'grid', gridTemplateColumns: compact ? '1fr 1fr' : 'repeat(5, minmax(0, 1fr))', gap: 12 }}>
            {DEMOS.map((demo) => (
              <Link
                key={demo.id}
                to="/demos"
                style={{ textDecoration: 'none', color: 'inherit', minWidth: 0 }}
                aria-label={`Watch: ${demo.title}`}
              >
                <div
                  style={{
                    aspectRatio: '16 / 9',
                    background: 'var(--sidebar-bg)',
                    borderRadius: 8,
                    overflow: 'hidden',
                    border: '1px solid var(--border)',
                    display: 'flex',
                    justifyContent: 'center',
                  }}
                >
                  <img
                    src={`/videos/${demo.id}.jpg`}
                    alt=""
                    style={{ height: '100%', width: demo.vertical ? 'auto' : '100%', objectFit: 'cover', display: 'block' }}
                  />
                </div>
                <div style={{ fontSize: 13, lineHeight: 1.4, marginTop: 6, color: 'var(--text-body)' }}>{demo.title}</div>
              </Link>
            ))}
          </div>
        </Section>

        <Section compact={compact} id="features">
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
                  background: 'var(--surface)',
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

        <Section alt compact={compact} id="how">
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: compact ? '1fr' : 'minmax(0, 1fr) minmax(0, 1fr)',
              gap: compact ? 24 : 48,
              alignItems: 'center',
            }}
          >
            <div>
              <H2 compact={compact}>How it fits together</H2>
              <Lead>
                Needs, requirements, design and tests in one typed graph; agents that propose and people who approve;
                baselines, documents and share links to hand the result over. The diagrams are on one page.
              </Lead>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                <Link to="/how-it-works" style={primaryButton}>
                  How it works
                </Link>
                <Link to="/faq" style={secondaryButton}>
                  Security and FAQ
                </Link>
              </div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              {[
                ['Share a link', 'A public link opens the live project read only, no account needed; a reviewer link lets someone comment without editing.'],
                ['On a phone', 'Review, approve and comment with a thumb; install it and get notifications as pushes.'],
                ['Flow-down', 'Subsystems and suppliers work in their own projects, refining the requirements above them.'],
                ['Documents', 'PDF and Word with the sections and fields you choose, from the live project or a baseline.'],
              ].map(([title, body]) => (
                <Card key={title} style={{ background: 'var(--bg-app)', padding: 14 }}>
                  <div style={{ fontWeight: 600, fontSize: 15, color: 'var(--text)', marginBottom: 4 }}>{title}</div>
                  <div style={{ fontSize: 13, lineHeight: 1.5, color: 'var(--text-body)' }}>{body}</div>
                </Card>
              ))}
            </div>
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
              <TierCard key={tier.id} tier={tier} compact={compact} live={livePrice(plans, tier)} />
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
            <p style={{ margin: '14px 0 6px', fontSize: 15, color: 'var(--text-body)' }}>Business Lite will raise those to:</p>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 15, lineHeight: 1.7, color: 'var(--text-body)' }}>
              {BUSINESS_LITE_LIMITS.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            <p style={{ margin: '14px 0 6px', fontSize: 15, color: 'var(--text-body)' }}>And Business again, to:</p>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 15, lineHeight: 1.7, color: 'var(--text-body)' }}>
              {BUSINESS_LIMITS.map((line) => (
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

        <Section compact={compact} id="community">
          <div style={{ display: 'grid', gridTemplateColumns: compact ? '1fr' : 'repeat(3, minmax(0, 1fr))', gap: 16 }}>
            {[
              ['Open-source projects', 'Open-source teams host free, and their latest baselines are public for anyone to read.', '/open-source', 'See the projects'],
              ['Customer stories', 'Teams using OpenV, in their own words, as they come in.', '/customers', 'Read the stories'],
              ['White papers', 'Longer reads on agents in the audit trail, flow-down and leaving DOORS.', '/white-papers', 'Browse the papers'],
            ].map(([title, body, to, cta]) => (
              <Card key={title} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <h3 style={{ margin: 0, fontSize: 18, color: 'var(--text)' }}>{title}</h3>
                <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: 'var(--text-body)', flex: 1 }}>{body}</p>
                <Link to={to} style={{ ...secondaryButton, fontSize: 14, padding: '8px 14px', alignSelf: 'flex-start' }}>
                  {cta}
                </Link>
              </Card>
            ))}
          </div>
        </Section>

        <Section alt compact={compact} id="self-host">
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

        <Section compact={compact} id="licence">
          <H2 compact={compact}>Open source, and built in the open</H2>
          <Lead>{LICENSE_GLOSS}</Lead>
          <ul style={{ margin: '0 0 20px', paddingLeft: 20, fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)', maxWidth: 720 }}>
            {LICENSE_INTENT.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
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
    </SiteShell>
  );
};
