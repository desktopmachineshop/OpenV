import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { COMPARISONS, HUB, HUB_COLUMNS, HUB_ROWS, OPENV_LIMITS, OTHERS } from './compare';
import { Dated, body, cell, list } from './ComparePage';
import { ExternalLink, Eyebrow, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// /compare: the matrix of hosted tools, a page per head-to-head, the other
// tools in one sourced line each, and the rules every comparison keeps.

export const CompareHub: React.FC = () => {
  const { isCompact: compact } = useViewport();
  return (
    <SiteShell title={HUB.title}>
      <Section compact={compact}>
        <Eyebrow>Compare</Eyebrow>
        <H1 compact={compact}>{HUB.heading}</H1>
        <Lead>{HUB.answer}</Lead>
        <Dated updated={HUB.updated} page={HUB.path} />
      </Section>
      <Section compact={compact} alt>
        <H2 compact={compact}>At a glance</H2>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ borderCollapse: 'collapse', width: '100%', minWidth: 760 }}>
            <thead>
              <tr>
                <th style={{ ...cell, fontWeight: 700 }} scope="col">
                  &nbsp;
                </th>
                {HUB_COLUMNS.map((col) => (
                  <th key={col.key} style={{ ...cell, fontWeight: 700 }} scope="col">
                    {col.path ? <Link to={col.path}>{col.label}</Link> : col.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {HUB_ROWS.map((r) => (
                <tr key={r.dimension}>
                  <th style={{ ...cell, fontWeight: 600 }} scope="row">
                    {r.dimension}
                  </th>
                  {HUB_COLUMNS.map((col) => (
                    <td key={col.key} style={cell}>
                      {r.values[col.key]}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p style={{ ...body, fontSize: 14, marginTop: 12 }}>
          “Not stated” means the vendor pages we checked do not say either way. Each comparison page links every statement to the vendor
          page it comes from.
        </p>
      </Section>
      <Section compact={compact}>
        <H2 compact={compact}>Side-by-side comparisons</H2>
        <ul style={list}>
          {COMPARISONS.map((c) => (
            <li key={c.path} style={{ marginBottom: 8 }}>
              <Link to={c.path}>{c.title}</Link>: {c.summary}
            </li>
          ))}
        </ul>
      </Section>
      <Section compact={compact} alt>
        <H2 compact={compact}>Other hosted tools</H2>
        <ul style={list}>
          {OTHERS.map((o) => (
            <li key={o.name} style={{ marginBottom: 8 }}>
              <strong>{o.name}</strong>: {o.line} <ExternalLink href={o.source}>Vendor page</ExternalLink>
            </li>
          ))}
        </ul>
      </Section>
      <Section compact={compact} id="limits">
        <H2 compact={compact}>What OpenV does not do</H2>
        <ul style={list}>
          {[OPENV_LIMITS.signatures, OPENV_LIMITS.approver, OPENV_LIMITS.certification, OPENV_LIMITS.imports, OPENV_LIMITS.alpha].map(
            (l) => (
              <li key={l} style={{ marginBottom: 6 }}>
                {l}
              </li>
            ),
          )}
        </ul>
        <H2 compact={compact}>How these comparisons are made</H2>
        <p style={body}>
          Hosted services are compared with hosted services. Statements about another product say only what its vendor
          says, with a link; a price is shown only where the vendor publishes one. OpenV’s side states its limits as
          plainly as its strengths, and every page carries the date it was last checked.
        </p>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 24 }}>
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
