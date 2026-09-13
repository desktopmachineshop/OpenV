import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { DEMOS, DEMOS_INTRO, Demo } from './content';
import { Card, Eyebrow, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// The demo videos: five narrated recordings served from public/videos, so
// they play under the frontend's same-origin content-security policy with
// no third-party player. The poster frame is the video's own first frame.

const DemoCard: React.FC<{ demo: Demo; index: number; compact: boolean }> = ({ demo, index, compact }) => (
  <Card style={{ display: 'flex', flexDirection: 'column', gap: 12, padding: compact ? 14 : 18 }}>
    <div
      style={{
        // A phone recording is portrait; giving it the same box as a
        // desktop one, centred, keeps the grid even.
        aspectRatio: '16 / 9',
        background: 'var(--sidebar-bg)',
        borderRadius: 8,
        overflow: 'hidden',
        display: 'flex',
        justifyContent: 'center',
        maxWidth: '100%',
      }}
    >
      <video
        controls
        preload="metadata"
        playsInline
        poster={`/videos/${demo.id}.jpg`}
        aria-label={demo.title}
        style={{ height: '100%', width: demo.vertical ? 'auto' : '100%', maxWidth: '100%', display: 'block' }}
      >
        <source src={`/videos/${demo.id}.mp4`} type="video/mp4" />
        Your browser cannot play this video.
      </video>
    </div>
    <div>
      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>
        {index + 1} of {DEMOS.length} · {demo.vertical ? 'on a phone' : 'on a desktop'} · about {demo.minutes} min
      </div>
      <h3 style={{ margin: '0 0 6px', fontSize: 17, color: 'var(--text)' }}>{demo.title}</h3>
      <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: 'var(--text-body)' }}>{demo.summary}</p>
    </div>
  </Card>
);

export const Demos: React.FC = () => {
  const { isCompact: compact } = useViewport();
  const desktop = DEMOS.filter((d) => !d.vertical);
  const phone = DEMOS.filter((d) => d.vertical);
  return (
    <SiteShell title="Demo videos">
      <Section compact={compact}>
        <Eyebrow>Demos</Eyebrow>
        <H1 compact={compact}>Watch it work</H1>
        <Lead>{DEMOS_INTRO}</Lead>
        <div style={{ display: 'grid', gridTemplateColumns: compact ? '1fr' : 'repeat(auto-fit, minmax(320px, 1fr))', gap: 20 }}>
          {desktop.map((d) => (
            <DemoCard key={d.id} demo={d} index={DEMOS.indexOf(d)} compact={compact} />
          ))}
        </div>
      </Section>
      <Section alt compact={compact} id="phone">
        <H2 compact={compact}>On a phone</H2>
        <Lead>The same project on an iPhone: reviewing, approving, planning and running agents with a thumb.</Lead>
        <div style={{ display: 'grid', gridTemplateColumns: compact ? '1fr' : 'repeat(2, minmax(0, 1fr))', gap: 20 }}>
          {phone.map((d) => (
            <DemoCard key={d.id} demo={d} index={DEMOS.indexOf(d)} compact={compact} />
          ))}
        </div>
      </Section>
      <Section compact={compact}>
        <H2 compact={compact}>Try it on your own project</H2>
        <Lead>A free account takes a minute. Import a ReqIF or JSON export, or start from a template and let the guided definition ask the questions.</Lead>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <Link to="/login?mode=register" style={primaryButton}>
            Create free account
          </Link>
          <Link to="/how-it-works" style={secondaryButton}>
            How it works
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
