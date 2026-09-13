import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { WHITE_PAPERS, WHITE_PAPERS_INTRO, WHITE_PAPER_NOTIFY_URL } from './content';
import { Card, ExternalLink, Eyebrow, Grid, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// White papers: each is listed with its status, and linked once published.

export const WhitePapers: React.FC = () => {
  const { isCompact: compact } = useViewport();
  return (
    <SiteShell title="White papers">
      <Section compact={compact}>
        <Eyebrow>White papers</Eyebrow>
        <H1 compact={compact}>Longer reads</H1>
        <Lead>{WHITE_PAPERS_INTRO}</Lead>
        <Grid columns={2} compact={compact}>
          {WHITE_PAPERS.map((paper) => (
            <Card key={paper.title} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <span
                style={{
                  alignSelf: 'flex-start',
                  fontSize: 12,
                  fontWeight: 700,
                  letterSpacing: 0.5,
                  textTransform: 'uppercase',
                  color: paper.status === 'Published' ? 'var(--success-text)' : 'var(--accent-text)',
                  background: 'var(--tint-blue)',
                  borderRadius: 999,
                  padding: '4px 10px',
                }}
              >
                {paper.status}
              </span>
              <h3 style={{ margin: 0, fontSize: 18, color: 'var(--text)' }}>{paper.title}</h3>
              <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: 'var(--text-body)' }}>{paper.summary}</p>
              {paper.href && (
                <ExternalLink href={paper.href} style={{ ...secondaryButton, fontSize: 14, padding: '8px 14px', alignSelf: 'flex-start' }}>
                  Read the paper
                </ExternalLink>
              )}
            </Card>
          ))}
        </Grid>
      </Section>
      <Section alt compact={compact}>
        <H2 compact={compact}>Hear when they are out</H2>
        <Lead>Leave a note on GitHub and you will be told as each paper is published.</Lead>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <ExternalLink href={WHITE_PAPER_NOTIFY_URL} style={primaryButton}>
            Tell me when they are out
          </ExternalLink>
          <Link to="/how-it-works" style={secondaryButton}>
            How it works, meanwhile
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
