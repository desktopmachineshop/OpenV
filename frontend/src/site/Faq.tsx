import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { FAQ_GROUPS } from './content';
import { Eyebrow, H1, H2, Lead, Section, SiteShell, secondaryButton } from './SiteShell';

// Security and subscription FAQ. Plain <details> elements: they open
// without JavaScript, are keyboard-accessible, and search finds the text.

export const Faq: React.FC = () => {
  const { isCompact: compact } = useViewport();
  return (
    <SiteShell title="FAQ">
      <Section compact={compact}>
        <Eyebrow>FAQ</Eyebrow>
        <H1 compact={compact}>Security, tiers and the practical questions</H1>
        <Lead>
          Short answers on who can see what, where data lives, what each tier includes and how work leaves OpenV. For
          the full picture, the manual and the source are both public.
        </Lead>
        <nav aria-label="FAQ sections" style={{ display: 'flex', gap: 10, flexWrap: 'wrap', marginBottom: 8 }}>
          {FAQ_GROUPS.map((g) => (
            <a key={g.id} href={`#${g.id}`} style={{ ...secondaryButton, padding: '8px 14px', fontSize: 14 }}>
              {g.title}
            </a>
          ))}
        </nav>
      </Section>
      {FAQ_GROUPS.map((group, i) => (
        <Section key={group.id} id={group.id} alt={i % 2 === 0} compact={compact}>
          <H2 compact={compact}>{group.title}</H2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, maxWidth: 820 }}>
            {group.entries.map((entry) => (
              <details
                key={entry.q}
                style={{
                  background: i % 2 === 0 ? 'var(--bg-app)' : 'var(--surface)',
                  border: '1px solid var(--border)',
                  borderRadius: 8,
                  padding: '12px 16px',
                }}
              >
                <summary style={{ cursor: 'pointer', fontWeight: 600, fontSize: 16, color: 'var(--text)' }}>{entry.q}</summary>
                <p style={{ margin: '10px 0 0', fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)' }}>{entry.a}</p>
              </details>
            ))}
          </div>
        </Section>
      ))}
      <Section compact={compact}>
        <H2 compact={compact}>Something else?</H2>
        <Lead>Ask on GitHub, or read how the pieces fit together.</Lead>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <Link to="/how-it-works" style={secondaryButton}>
            How it works
          </Link>
          <Link to="/pricing" style={secondaryButton}>
            Pricing
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
