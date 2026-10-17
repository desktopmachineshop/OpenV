import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { getComparison, type Comparison } from './compare';
import { ExternalLink, Eyebrow, Grid, Card, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// One layout for every comparison page (site/compare): the answer first,
// when to choose each tool, the side-by-side rows with a vendor link on
// every statement about the other tool, how to move, and what OpenV does not
// do. Plain markup, so the pre-rendered page reads the same to a crawler.

export const body: React.CSSProperties = { margin: '0 0 12px', fontSize: 16, lineHeight: 1.65, color: 'var(--text-body)', maxWidth: 760 };
export const list: React.CSSProperties = { ...body, paddingLeft: 22 };
export const cell: React.CSSProperties = {
  padding: '10px 12px',
  borderBottom: '1px solid var(--border)',
  verticalAlign: 'top',
  textAlign: 'left',
  fontSize: 14,
  lineHeight: 1.5,
  color: 'var(--text-body)',
};

export const formatDate = (iso: string) =>
  new Date(`${iso}T00:00:00Z`).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' });

/** Where a reader reports a statement that has gone out of date. */
export const CORRECTION_URL = (page: string) =>
  `https://github.com/desktopmachineshop/OpenV/issues/new?title=${encodeURIComponent(`Correction on ${page}`)}`;

export const Dated: React.FC<{ updated: string; page: string }> = ({ updated, page }) => (
  <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
    Checked <time dateTime={updated}>{formatDate(updated)}</time> by Desktop Machine Shop, the makers of OpenV, against
    each vendor’s own pages. Something out of date? <ExternalLink href={CORRECTION_URL(page)}>Tell us</ExternalLink>.
  </p>
);

const Bullets: React.FC<{ items: string[] }> = ({ items }) => (
  <ul style={{ ...list, margin: 0 }}>
    {items.map((i) => (
      <li key={i} style={{ marginBottom: 6 }}>
        {i}
      </li>
    ))}
  </ul>
);

const Rows: React.FC<{ c: Comparison }> = ({ c }) => (
  <div style={{ overflowX: 'auto' }}>
    <table style={{ borderCollapse: 'collapse', width: '100%', minWidth: 560 }}>
      <thead>
        <tr>
          <th style={{ ...cell, width: '18%', fontWeight: 700 }} scope="col">
            &nbsp;
          </th>
          <th style={{ ...cell, width: '41%', fontWeight: 700 }} scope="col">
            OpenV (hosted)
          </th>
          <th style={{ ...cell, width: '41%', fontWeight: 700 }} scope="col">
            {c.competitor}
          </th>
        </tr>
      </thead>
      <tbody>
        {c.rows.map((r) => (
          <tr key={r.dimension}>
            <th style={{ ...cell, fontWeight: 600 }} scope="row">
              {r.dimension}
            </th>
            <td style={cell}>{r.openv}</td>
            <td style={cell}>
              {r.them} <ExternalLink href={r.source}>{c.vendor}</ExternalLink>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

export const ComparePage: React.FC<{ path: string }> = ({ path }) => {
  const { isCompact: compact } = useViewport();
  const c = getComparison(path);
  if (!c) return null;
  return (
    <SiteShell title={c.title}>
      <Section compact={compact}>
        <Eyebrow>
          <Link to="/compare">Compare</Link> · {c.competitor}
        </Eyebrow>
        <H1 compact={compact}>{c.heading}</H1>
        <Lead>{c.answer}</Lead>
        <Dated updated={c.updated} page={c.path} />
      </Section>
      <Section compact={compact} alt>
        <H2 compact={compact}>Which to choose</H2>
        <Grid columns={2} compact={compact}>
          <Card>
            <div style={{ fontWeight: 600, marginBottom: 8, color: 'var(--text)' }}>Choose {c.competitor} if</div>
            <Bullets items={c.chooseThem} />
          </Card>
          <Card>
            <div style={{ fontWeight: 600, marginBottom: 8, color: 'var(--text)' }}>Choose OpenV if</div>
            <Bullets items={c.chooseOpenV} />
          </Card>
        </Grid>
      </Section>
      <Section compact={compact}>
        <H2 compact={compact}>Side by side</H2>
        <p style={body}>
          Each statement about {c.competitor} is what {c.vendor} says on its own pages, linked beside it.
        </p>
        <Rows c={c} />
      </Section>
      <Section compact={compact} alt>
        <H2 compact={compact}>Moving to OpenV</H2>
        {c.moving.map((p) => (
          <p key={p} style={body}>
            {p}
          </p>
        ))}
        <p style={body}>
          The <Link to="/reqif">ReqIF page</Link> says what an import and an export carry.
        </p>
      </Section>
      <Section compact={compact} id="limits">
        <H2 compact={compact}>What OpenV does not do</H2>
        <Bullets items={c.limits} />
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 24 }}>
          <Link to="/login?mode=register" style={primaryButton}>
            Create a free account
          </Link>
          <Link to="/compare" style={secondaryButton}>
            All comparisons
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
