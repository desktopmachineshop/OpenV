import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { MANUAL_CHAPTERS } from '../manual';
import { Card, Eyebrow, Grid, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';
import { getTopic, type Topic } from './topics';

// One layout for every topic page (site/topics): the answer first, the
// detail, what OpenV does not do, and where to read more. Plain markup
// throughout, so the pre-rendered page reads the same to a crawler as to a
// person.

const body: React.CSSProperties = { margin: '0 0 12px', fontSize: 16, lineHeight: 1.65, color: 'var(--text-body)', maxWidth: 760 };
const list: React.CSSProperties = { ...body, paddingLeft: 22 };

const formatDate = (iso: string) =>
  new Date(`${iso}T00:00:00Z`).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' });

const Sections: React.FC<{ topic: Topic; compact: boolean }> = ({ topic, compact }) => {
  if (topic.kind === 'glossary') {
    return (
      <Section compact={compact} alt>
        <dl style={{ margin: 0, maxWidth: 820 }}>
          {topic.sections.map((s) => (
            <div key={s.heading} style={{ marginBottom: 18 }}>
              <dt style={{ fontWeight: 700, fontSize: 17, color: 'var(--text)' }}>{s.heading}</dt>
              <dd style={{ ...body, margin: '4px 0 0' }}>{(s.paragraphs ?? []).join(' ')}</dd>
            </div>
          ))}
        </dl>
      </Section>
    );
  }
  return (
    <>
      {topic.sections.map((s, i) => (
        <Section key={s.heading} compact={compact} alt={i % 2 === 0}>
          <H2 compact={compact}>{s.heading}</H2>
          {(s.paragraphs ?? []).map((p) => (
            <p key={p} style={body}>
              {p}
            </p>
          ))}
          {s.bullets && (
            <ul style={list}>
              {s.bullets.map((b) => (
                <li key={b} style={{ marginBottom: 6 }}>
                  {b}
                </li>
              ))}
            </ul>
          )}
        </Section>
      ))}
    </>
  );
};

export const TopicPage: React.FC<{ path: string }> = ({ path }) => {
  const { isCompact: compact } = useViewport();
  const topic = getTopic(path);
  if (!topic) return null;
  const chapters = topic.manual
    .map((slug) => MANUAL_CHAPTERS.find((c) => c.slug === slug))
    .filter((c): c is (typeof MANUAL_CHAPTERS)[number] => !!c);
  const related = topic.related.map(getTopic).filter((t): t is Topic => !!t);
  return (
    <SiteShell title={topic.title}>
      <Section compact={compact}>
        <Eyebrow>{topic.eyebrow}</Eyebrow>
        <H1 compact={compact}>{topic.heading}</H1>
        <Lead>{topic.answer}</Lead>
        <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
          Updated <time dateTime={topic.updated}>{formatDate(topic.updated)}</time> by Desktop Machine Shop, the makers
          of OpenV.
        </p>
      </Section>
      <Sections topic={topic} compact={compact} />
      {topic.limits.length > 0 && (
        <Section compact={compact} id="limits" alt={topic.sections.length % 2 === 0}>
          <H2 compact={compact}>What OpenV does not do</H2>
          <ul style={list}>
            {topic.limits.map((l) => (
              <li key={l} style={{ marginBottom: 6 }}>
                {l}
              </li>
            ))}
          </ul>
        </Section>
      )}
      <Section compact={compact}>
        <H2 compact={compact}>Read more</H2>
        <Grid columns={2} compact={compact}>
          <Card>
            <div style={{ fontWeight: 600, marginBottom: 8, color: 'var(--text)' }}>In the manual</div>
            <ul style={{ ...list, margin: 0 }}>
              {chapters.map((c) => (
                <li key={c.slug}>
                  <Link to={`/manual/${c.slug}`}>{c.title}</Link>
                </li>
              ))}
            </ul>
          </Card>
          <Card>
            <div style={{ fontWeight: 600, marginBottom: 8, color: 'var(--text)' }}>Related</div>
            <ul style={{ ...list, margin: 0 }}>
              {related.map((t) => (
                <li key={t.path}>
                  <Link to={t.path}>{t.title}</Link>
                </li>
              ))}
              <li>
                <Link to="/faq">Security and FAQ</Link>
              </li>
            </ul>
          </Card>
        </Grid>
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
