import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { CUSTOMERS_INTRO, CUSTOMER_STORY_INVITE_URL, CUSTOMER_STORY_PLACEHOLDERS } from './content';
import { Card, ExternalLink, Eyebrow, Grid, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// Customer stories: the page exists before the stories do, and says so.
// Nothing here is invented; the cards hold the room for real teams.

export const Customers: React.FC = () => {
  const { isCompact: compact } = useViewport();
  return (
    <SiteShell title="Customer stories">
      <Section compact={compact}>
        <Eyebrow>Customers</Eyebrow>
        <H1 compact={compact}>Stories from teams using OpenV</H1>
        <Lead>{CUSTOMERS_INTRO}</Lead>
        <Grid columns={3} compact={compact}>
          {CUSTOMER_STORY_PLACEHOLDERS.map((p) => (
            <Card key={p.sector} style={{ borderStyle: 'dashed', display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={{ fontSize: 12, fontWeight: 700, letterSpacing: 0.5, textTransform: 'uppercase', color: 'var(--accent-text)' }}>
                {p.sector}
              </div>
              <h3 style={{ margin: 0, fontSize: 17, color: 'var(--text)' }}>Your story here</h3>
              <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: 'var(--text-body)' }}>{p.prompt}</p>
            </Card>
          ))}
        </Grid>
      </Section>
      <Section alt compact={compact}>
        <H2 compact={compact}>Using OpenV? Tell us</H2>
        <Lead>A few lines about your team and what changed is all it takes. Stories are published with your approval, and only then.</Lead>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <ExternalLink href={CUSTOMER_STORY_INVITE_URL} style={primaryButton}>
            Share your story
          </ExternalLink>
          <Link to="/demos" style={secondaryButton}>
            Watch the demos
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
