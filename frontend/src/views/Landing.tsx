import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { billingAPI, PublicPlans } from '../api/client';
import {
  ALPHA_NOTE,
  BUSINESS_LIMITS,
  BUSINESS_LITE_LIMITS,
  DATA_PROMISE,
  EXPORT_FORMATS,
  FEATURES,
  FEEDBACK_ISSUE_URL,
  HERO_HEADLINE,
  HERO_LEAD,
  HOSTED_LIMITS,
  HOSTED_TIERS,
  IMPORT_FORMATS,
  LICENSE_GLOSS,
  LICENSE_INTENT,
  LICENSE_URL,
  NO_CARD_NOTE,
  OTHER_TIERS,
  PRICING_FOOTNOTE,
  PricingTier,
  QUICKSTART_URL,
  REPO_URL,
  SELF_HOST_COMMANDS,
} from '../landing/content';
import { DEMOS } from '../site/content';
import { ExternalLink, H2, Lead, Section, SiteShell, buttonClass } from '../site/SiteShell';

// The public landing page: what OpenV does, the hosting terms in force and
// the promise that data leaves freely. Rendered at "/" for visitors without a
// session (App.tsx sends signed-in users to /projects) and at "/pricing"
// scrolled to the pricing section. Copy lives in landing/content.ts; the
// frame and building blocks in site/SiteShell.tsx, shared with the other
// storefront pages; the layout classes in site/site.css, on the brand
// tokens (docs/brand/website.md).

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
 *  the repository: this reads what the provider confirmed. */
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

const TierCard: React.FC<{ tier: PricingTier; live?: LivePrice | null; featured?: boolean }> = ({ tier, live, featured }) => (
  <article aria-labelledby={`tier-${tier.id}`} className={featured ? 'site-tier site-tier--featured' : 'site-tier'}>
    <h3 id={`tier-${tier.id}`}>{tier.name}</h3>
    {tier.available ? (
      <div className="site-price">{tier.price}</div>
    ) : live ? (
      <div data-testid={`live-price-${tier.id}`}>
        <span className="site-price">{live.text}</span>
        <span style={{ fontSize: 14, color: 'var(--text-muted)' }}>
          {' '}
          / {live.perSeat ? 'member / ' : ''}month{live.taxNote ? `, ${live.taxNote}` : ''}
        </span>
      </div>
    ) : (
      <div style={{ minHeight: 40, display: 'flex', alignItems: 'center' }}>
        <span className="ov-chip">{tier.price}</span>
      </div>
    )}
    <p>{tier.summary}</p>
    <ul>
      {tier.points.map((p) => (
        <li key={p}>{p}</li>
      ))}
    </ul>
    {live ? (
      <Link to="/login?mode=register" className={buttonClass.secondary}>
        Start on {tier.name}
      </Link>
    ) : tier.cta.external ? (
      <ExternalLink href={tier.cta.href} className={buttonClass.secondary}>
        {tier.cta.label}
      </ExternalLink>
    ) : (
      <Link to={tier.cta.href} className={featured ? buttonClass.primary : buttonClass.secondary}>
        {tier.cta.label}
      </Link>
    )}
  </article>
);

/** A real traceability chain, drawn with the brand's own parts: the example
 *  the requirements feature tile shows. */
const TraceChain: React.FC = () => (
  <div className="site-chain" aria-label="Example: a user need, the requirement that satisfies it, and the passing test case that verifies it">
    <div className="site-chain-row">
      <span className="ov-ref">UN-3</span>
      <span className="site-chain-kind">User need</span>
      <span>Nothing runs without my consent</span>
      <span className="ov-chip ov-chip--success">Approved</span>
    </div>
    <div className="site-chain-link">satisfied by</div>
    <div className="site-chain-row">
      <span className="ov-ref">REQ-2</span>
      <span className="site-chain-kind">Requirement</span>
      <span>Role-based access control</span>
      <span className="ov-chip ov-chip--warning">In review</span>
    </div>
    <div className="site-chain-link">verified by</div>
    <div className="site-chain-row">
      <span className="ov-ref">TC-4</span>
      <span className="site-chain-kind">Test case</span>
      <span>Role checks on project endpoints</span>
      <span className="ov-chip ov-chip--success">Pass</span>
    </div>
  </div>
);

const featureBody = (title: string) => FEATURES.find((f) => f.title === title)?.body ?? '';

export const Landing: React.FC<LandingProps> = ({ section }) => {
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

  const [lead, ...moreDemos] = DEMOS;

  return (
    <SiteShell>
      <Section hero>
        <div className="site-hero">
          <div>
            <h1 className="site-h1">{HERO_HEADLINE}</h1>
            <p className="site-lead">{HERO_LEAD}</p>
            <div className="site-actions">
              <Link to="/login?mode=register" className={buttonClass.primaryLarge}>
                Create free account
              </Link>
              <Link to="/demos" className={buttonClass.secondaryLarge}>
                Watch the demos
              </Link>
            </div>
          </div>
          <figure className="site-shot">
            <img
              src="/Images/screenshot-requirements.png"
              width={2160}
              height={1350}
              alt="The OpenV requirements module: a tree of requirements with stable refs beside the selected requirement's document and traceability links"
            />
          </figure>
        </div>
      </Section>

      <Section alt id="see-it">
        <H2>See it working</H2>
        <Lead>Five narrated recordings on the platform’s own requirements project. No slides, nothing staged.</Lead>
        <div className="site-demos">
          <Link to="/demos" className="site-card-link" aria-label={`Watch: ${lead.title}`}>
            <div className="site-thumb">
              <img src={`/videos/${lead.id}.jpg`} alt="" style={{ width: '100%' }} />
            </div>
            <div>
              <div className="site-card-title">{lead.title}</div>
              <div style={{ fontSize: 14, color: 'var(--text-muted)' }}>{lead.minutes} minutes</div>
            </div>
          </Link>
          <div className="site-demo-list">
            {moreDemos.map((demo) => (
              <Link key={demo.id} to="/demos" className="site-demo-row" aria-label={`Watch: ${demo.title}`}>
                <div className="site-thumb">
                  <img src={`/videos/${demo.id}.jpg`} alt="" style={{ width: demo.vertical ? 'auto' : '100%' }} />
                </div>
                <div>
                  <div className="site-card-title">{demo.title}</div>
                  <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                    {demo.vertical ? 'On a phone, ' : ''}
                    {demo.minutes} minutes
                  </div>
                </div>
              </Link>
            ))}
          </div>
        </div>
      </Section>

      <Section id="features">
        <H2>What it does</H2>
        <Lead>
          One place for what your product must do, why, and the proof that it does. Every artifact has a stable ref, a
          history and typed links.
        </Lead>
        <div className="site-bento">
          <div className="site-tile site-tile--wide">
            <h3 className="site-h3">{FEATURES[0].title}</h3>
            <p>{FEATURES[0].body}</p>
            <TraceChain />
          </div>
          <div className="site-tile">
            <h3 className="site-h3">V&amp;V evidence</h3>
            <p>{featureBody('V&V evidence')}</p>
          </div>
          <div className="site-tile site-tile--agent">
            <span className="ov-chip ov-chip--agent" style={{ alignSelf: 'flex-start' }}>
              Agent proposal
            </span>
            <h3 className="site-h3">Agents with human review</h3>
            <p>{featureBody('Agents with human review')}</p>
          </div>
          <div className="site-tile">
            <h3 className="site-h3">Stakeholder interviews</h3>
            <p>{featureBody('Stakeholder interviews')}</p>
          </div>
          <div className="site-tile">
            <h3 className="site-h3">Documents and hand-over</h3>
            <p>{featureBody('Documents and hand-over')}</p>
          </div>
          <div className="site-tile site-tile--full site-tile--info">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <h3 className="site-h3">Programmes, not just projects</h3>
              <p>{featureBody('Programmes, not just projects')}</p>
            </div>
            <div className="site-flow" aria-label="Example: requirements flowing down from a system to a subsystem to a supplier">
              <span className="site-flow-node">Aircraft</span>
              <span className="site-flow-arrow" aria-hidden="true">
                →
              </span>
              <span className="site-flow-node">Landing gear</span>
              <span className="site-flow-arrow" aria-hidden="true">
                →
              </span>
              <span className="site-flow-node">Actuator supplier</span>
            </div>
          </div>
        </div>
      </Section>

      <Section alt id="how">
        <div className="site-split">
          <div>
            <H2>How it fits together</H2>
            <Lead>
              Needs, requirements, design and tests in one typed graph. Agents propose, people approve, and baselines,
              documents and share links hand the result over.
            </Lead>
            <div className="site-actions">
              <Link to="/how-it-works" className={buttonClass.secondary}>
                How it works
              </Link>
              <Link to="/faq" className={buttonClass.secondary}>
                Security and FAQ
              </Link>
            </div>
          </div>
          <dl className="site-dl">
            {[
              ['Share a link', 'A public link opens the live project read only, no account needed; a reviewer link lets someone comment without editing.'],
              ['On a phone', 'Review, approve and comment with a thumb; install it and get notifications as pushes.'],
              ['Flow-down', 'Subsystems and suppliers work in their own projects, refining the requirements above them.'],
              ['Documents', 'PDF and Word with the sections and fields you choose, from the live project or a baseline.'],
            ].map(([title, body]) => (
              <div key={title}>
                <dt>{title}</dt>
                <dd>{body}</dd>
              </div>
            ))}
          </dl>
        </div>
      </Section>

      <Section id="data">
        <div className="site-split">
          <div>
            <H2>Your data is yours</H2>
            <Lead>{DATA_PROMISE}</Lead>
            <p className="site-body">
              Import reads {IMPORT_FORMATS}. Every download can snapshot a baseline instead of the live project, and the
              archive download carries your attachments with it.
            </p>
          </div>
          <div className="ov-panel" style={{ overflow: 'hidden' }}>
            <table className="ov-table">
              <thead>
                <tr>
                  <th scope="col" style={{ width: '38%' }}>
                    Export
                  </th>
                  <th scope="col">What you get</th>
                </tr>
              </thead>
              <tbody>
                {EXPORT_FORMATS.map((f) => (
                  <tr key={f.label}>
                    <td style={{ fontWeight: 500, padding: '10px 12px' }}>{f.label}</td>
                    <td style={{ color: 'var(--text-secondary)', padding: '10px 12px' }}>{f.body}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </Section>

      <Section alt id="pricing">
        <H2>Pricing</H2>
        <Lead>Free while in alpha, free forever to self-host, and a clear path when your team grows. {NO_CARD_NOTE}</Lead>
        <div role="note" className="ov-callout ov-callout--info" style={{ marginBottom: 32, fontSize: 15, lineHeight: '24px' }}>
          <div>{ALPHA_NOTE}</div>
        </div>
        <h3 className="site-h3" style={{ marginBottom: 16 }}>
          Hosted by us
        </h3>
        <div className="site-tiers site-tiers--4">
          {HOSTED_TIERS.map((tier) => (
            <TierCard key={tier.id} tier={tier} live={livePrice(plans, tier)} featured={tier.available} />
          ))}
        </div>

        <div className="ov-panel" style={{ marginTop: 24, padding: 24 }}>
          <h3 className="site-h3" style={{ marginBottom: 16 }}>
            What free means on the hosted service today
          </h3>
          <div className="site-limits">
            <div>
              <h4>Free, today</h4>
              <ul>
                {HOSTED_LIMITS.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
            </div>
            <div>
              <h4>Business Lite will raise those to</h4>
              <ul>
                {BUSINESS_LITE_LIMITS.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
            </div>
            <div>
              <h4>And Business again, to</h4>
              <ul>
                {BUSINESS_LIMITS.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
            </div>
          </div>
          <p style={{ margin: '20px 0 0', fontSize: 14, lineHeight: '22px', color: 'var(--text-muted)' }}>{PRICING_FOOTNOTE}</p>
        </div>

        <h3 className="site-h3" style={{ margin: '40px 0 16px' }}>
          Other ways to run OpenV
        </h3>
        <div className="site-tiers site-tiers--2">
          {OTHER_TIERS.map((tier) => (
            <TierCard key={tier.id} tier={tier} />
          ))}
        </div>
      </Section>

      <Section id="community">
        <div className="site-columns">
          {[
            ['Open-source projects', 'Open-source teams host free, and their latest baselines are public for anyone to read.', '/open-source', 'See the projects'],
            ['Customer stories', 'Teams using OpenV, in their own words, as they come in.', '/customers', 'Read the stories'],
            ['White papers', 'Longer reads on agents in the audit trail, flow-down and leaving DOORS.', '/white-papers', 'Browse the papers'],
          ].map(([title, body, to, cta]) => (
            <div key={title}>
              <h3 className="site-h3">{title}</h3>
              <p>{body}</p>
              <Link to={to}>{cta} →</Link>
            </div>
          ))}
        </div>
      </Section>

      <Section alt id="self-host">
        <div className="site-split">
          <div>
            <H2>Self-host in one command</H2>
            <Lead>
              Docker is the only prerequisite. The stack brings its own database, migrates itself on boot, and the first
              account to register becomes the admin.
            </Lead>
            <ExternalLink href={QUICKSTART_URL} className={buttonClass.secondary}>
              Read the quick start
            </ExternalLink>
          </div>
          <pre className="site-code">{SELF_HOST_COMMANDS.map((c) => `$ ${c}`).join('\n')}</pre>
        </div>
      </Section>

      <Section id="licence">
        <H2>Open source, and built in the open</H2>
        <Lead>{LICENSE_GLOSS}</Lead>
        <ul className="site-body" style={{ margin: '0 0 20px', paddingLeft: 20 }}>
          {LICENSE_INTENT.map((line) => (
            <li key={line} style={{ marginBottom: 6 }}>
              {line}
            </li>
          ))}
        </ul>
        <p className="site-body" style={{ marginBottom: 28 }}>
          OpenV manages its own requirements in OpenV: the live project holds every requirement the platform is built to,
          with traceability to the tests that prove it. Contributions are welcome under the Developer Certificate of
          Origin.
        </p>
        <div className="site-actions">
          <ExternalLink href={REPO_URL} className={buttonClass.primary}>
            View on GitHub
          </ExternalLink>
          <ExternalLink href={FEEDBACK_ISSUE_URL} className={buttonClass.secondary}>
            Send alpha feedback
          </ExternalLink>
          <ExternalLink href={LICENSE_URL} className={buttonClass.secondary}>
            Read the licence
          </ExternalLink>
        </div>
      </Section>
    </SiteShell>
  );
};
