import React from 'react';
import { useViewport } from '../hooks/useViewport';
import { Eyebrow, H1, H2, Lead, Section, SiteShell } from './SiteShell';

// The privacy policy of the hosted service at openv.app. Every statement is
// one the code bears out (the tables, cookies and services it names); a
// change to what OpenV stores or whom it sends data to changes this page in
// the same pull request. A self-hosted OpenV is its operator's service, and
// this policy says so rather than speaking for them.

export const PRIVACY_CONTACT = 'privacy@openv.app';
export const PRIVACY_UPDATED = '2026-10-16';

interface PrivacySection {
  id: string;
  heading: string;
  paragraphs?: string[];
  bullets?: string[];
  /** A paragraph after the bullets. */
  closing?: string;
}

export const PRIVACY_SECTIONS: PrivacySection[] = [
  {
    id: 'scope',
    heading: 'Who this covers',
    paragraphs: [
      'This policy covers the hosted OpenV service at openv.app, run by Desktop Machine Shop ("we"), and the AI apps you connect to it, such as the OpenV connector for Claude. If you use an OpenV that your organisation or someone else runs on its own servers, that operator decides what happens to your data, not us.',
    ],
  },
  {
    id: 'collect',
    heading: 'What we collect',
    bullets: [
      'Your account: your email address, your name, a profile picture if you upload one or your Google or single sign-on provider supplies one, and how you sign in. A password is stored only as a bcrypt hash.',
      'What you put in a workspace: projects, requirements and other artifacts, links, comments, test results, attachments and baselines, with who made each change and when.',
      'Sign-in sessions, stored as a hash of the session token with its expiry and when it was last used.',
      'Your settings: notification preferences, the workspace you open by default, and, if you turn on push notifications, each device’s push address and browser name.',
      'Keys you give us to run AI on your behalf, such as a workspace’s AI provider keys or a saved Claude sign-in for a runner. These are stored encrypted, and only their last four characters are shown back.',
      'Usage counts: one record per signed-in user per day, with the country Cloudflare reports. We do not keep your IP address.',
      'Site visits: a random visitor id, the day you first came, the page you landed on, the site you came from (its address only, not the page), any utm_source tag, and the country. We do not keep your IP address, browser, or the full address of the page that linked to us. We do not count browsers that send Global Privacy Control or Do Not Track.',
      'Billing, for a paid workspace: Stripe holds your card and payment details; we keep the subscription status and the name and email we gave Stripe.',
    ],
  },
  {
    id: 'connected-apps',
    heading: 'AI apps you connect',
    paragraphs: [
      'When you connect an AI app such as Claude to OpenV, you sign in to OpenV and approve it for one workspace, with read-only or read-and-change access. We keep a record of that approval: the app’s name, the workspace, the access you granted, and when you approved it, last used it and disconnected it. The app’s access tokens are stored only as hashes, last at most an hour (30 days for the token it renews them with), and are deleted when you disconnect the app in Settings → Connected apps.',
      'The app sees only what its tools return from that workspace, with your permissions. What the app itself does with that data, for example how Claude stores your conversations, is covered by that app provider’s own privacy policy.',
    ],
  },
  {
    id: 'use',
    heading: 'How we use it',
    bullets: [
      'To run the service: sign you in, show you your workspaces, run the agents and integrations you set up, and send the emails and notifications you asked for.',
      'To keep it secure: rate limiting, abuse prevention and the activity history of each project.',
      'To understand, in aggregate, how many people visit and use OpenV and where they come from.',
      'To bill a paid workspace.',
    ],
    closing: 'We do not sell your data, use it for advertising, or train AI models on it.',
  },
  {
    id: 'sharing',
    heading: 'Who we share it with',
    paragraphs: [
      'Only the services that run OpenV for us, each for its part of the job:',
    ],
    bullets: [
      'Railway, which hosts the service, its database and its files.',
      'Cloudflare, which carries traffic to openv.app and tells us the visitor’s country.',
      'Our email provider, to send sign-in, invitation and notification emails.',
      'Stripe, for payments on a paid workspace.',
      'Google, or your organisation’s single sign-on provider, if you sign in with them.',
      'The push service of your browser (Google, Apple, Microsoft or Mozilla), if you turn on push notifications.',
      'The AI provider your workspace chooses (for example Anthropic, OpenAI or Google), which receives the project content an agent run works on, under your workspace’s own key or subscription.',
    ],
  },
  {
    id: 'cookies',
    heading: 'Cookies and browser storage',
    bullets: [
      'openv_session: keeps you signed in. It lasts up to 30 days and ends after 7 days without use.',
      'openv_vid: the random visitor id used to count visits, kept for a year.',
      'openv_oidc_state and openv_oidc_nonce: used for ten minutes during single sign-on.',
      'Your browser’s local storage holds display preferences only, such as the theme, the last project you opened and column widths. No sign-in token is kept there.',
    ],
  },
  {
    id: 'retention',
    heading: 'How long we keep it',
    bullets: [
      'Your account and its workspaces: for as long as the account exists.',
      'A deleted workspace: restorable for 30 days, then deleted with its files.',
      'Expired sessions, sign-in and password-reset links, invitations and AI-app tokens are removed automatically.',
      'Daily usage counts and site-visit records: for as long as the service runs. If your account is deleted, its visit record is no longer linked to you.',
    ],
  },
  {
    id: 'rights',
    heading: 'Your choices and rights',
    paragraphs: [
      'You can change your name, picture and notification settings in your settings, export any project, disconnect any AI app, and delete a workspace you own. To get a copy of your personal data, correct it, or delete your account, email us at the address below; there is not yet a button for deleting an account in the app. Depending on where you live, you may also have the right to object to or restrict how we use your data, and to complain to your data protection authority.',
    ],
  },
  {
    id: 'security',
    heading: 'Security',
    paragraphs: [
      'The service is HTTPS only. The hosted database and files are encrypted at rest by the hosting provider. Passwords, session and other tokens are stored only as hashes, and AI keys are encrypted. Each workspace’s data is kept separate from every other workspace’s.',
    ],
  },
  {
    id: 'changes',
    heading: 'Changes and contact',
    paragraphs: [
      'When this policy changes, we update this page and the date on it, and announce material changes in the app. Questions, requests and complaints about privacy: ' +
        PRIVACY_CONTACT +
        '.',
    ],
  },
];

export const Privacy: React.FC = () => {
  const { isCompact: compact } = useViewport();
  const body: React.CSSProperties = { margin: '0 0 12px', fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)' };
  return (
    <SiteShell title="Privacy policy">
      <Section compact={compact}>
        <Eyebrow>Privacy</Eyebrow>
        <H1 compact={compact}>Privacy policy</H1>
        <Lead>
          What the hosted OpenV service collects, why, who it goes to and how long it stays. Last updated{' '}
          {PRIVACY_UPDATED}.
        </Lead>
      </Section>
      {PRIVACY_SECTIONS.map((s, i) => (
        <Section key={s.id} id={s.id} alt={i % 2 === 0} compact={compact}>
          <div style={{ maxWidth: 820 }}>
            <H2 compact={compact}>{s.heading}</H2>
            {(s.paragraphs ?? []).map((p) => (
              <p key={p} style={body}>
                {p}
              </p>
            ))}
            {s.bullets && (
              <ul style={{ ...body, paddingLeft: 20 }}>
                {s.bullets.map((b) => (
                  <li key={b} style={{ marginBottom: 6 }}>
                    {b}
                  </li>
                ))}
              </ul>
            )}
            {s.closing && <p style={body}>{s.closing}</p>}
          </div>
        </Section>
      ))}
    </SiteShell>
  );
};
