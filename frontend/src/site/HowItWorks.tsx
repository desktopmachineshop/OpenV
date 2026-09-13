import React from 'react';
import { Link } from 'react-router-dom';
import { useViewport } from '../hooks/useViewport';
import { Card, Eyebrow, Grid, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';

// How it works: the storefront page that explains OpenV with diagrams. The
// diagrams are inline SVG on the theme tokens, so they follow light and dark
// mode and need no image files.

const Diagram: React.FC<{ title: string; children: React.ReactNode; viewBox: string; caption: string }> = ({
  title,
  children,
  viewBox,
  caption,
}) => (
  <figure style={{ margin: 0 }}>
    <svg
      role="img"
      aria-label={title}
      viewBox={viewBox}
      style={{ width: '100%', height: 'auto', display: 'block', maxWidth: '100%' }}
      fontFamily="system-ui, sans-serif"
    >
      <title>{title}</title>
      {children}
    </svg>
    <figcaption style={{ marginTop: 8, fontSize: 13, color: 'var(--text-muted)' }}>{caption}</figcaption>
  </figure>
);

/** A rounded box with a centred label, in the diagrams below. */
const Box: React.FC<{
  x: number;
  y: number;
  w: number;
  h: number;
  label: string;
  sub?: string;
  accent?: boolean;
  dashed?: boolean;
}> = ({ x, y, w, h, label, sub, accent, dashed }) => (
  <g>
    <rect
      x={x}
      y={y}
      width={w}
      height={h}
      rx={8}
      fill={accent ? 'var(--accent)' : 'var(--surface)'}
      stroke={accent ? 'var(--accent)' : 'var(--border)'}
      strokeWidth={1.5}
      strokeDasharray={dashed ? '6 4' : undefined}
    />
    <text
      x={x + w / 2}
      y={y + h / 2 + (sub ? -4 : 5)}
      textAnchor="middle"
      fontSize={14}
      fontWeight={600}
      fill={accent ? 'var(--accent-fg)' : 'var(--text)'}
    >
      {label}
    </text>
    {sub && (
      <text x={x + w / 2} y={y + h / 2 + 14} textAnchor="middle" fontSize={11} fill={accent ? 'var(--accent-fg)' : 'var(--text-muted)'}>
        {sub}
      </text>
    )}
  </g>
);

const Arrow: React.FC<{ d: string; label?: string; lx?: number; ly?: number }> = ({ d, label, lx, ly }) => (
  <g>
    <path d={d} fill="none" stroke="var(--text-muted)" strokeWidth={1.5} markerEnd="url(#arrow)" />
    {label && (
      <text x={lx} y={ly} textAnchor="middle" fontSize={11} fill="var(--text-muted)">
        {label}
      </text>
    )}
  </g>
);

const Defs: React.FC = () => (
  <defs>
    <marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
      <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--text-muted)" />
    </marker>
  </defs>
);

const TraceDiagram: React.FC = () => (
  <Diagram
    title="The artifact graph: needs, requirements, design items, test cases and test runs, joined by typed links"
    viewBox="0 0 720 300"
    caption="Every artifact has a stable reference and a version; every link has a type. Coverage, gaps and impact are read off this graph, not maintained by hand."
  >
    <Defs />
    <Box x={20} y={120} w={130} h={56} label="User need" sub="NEED-6" />
    <Box x={210} y={120} w={140} h={56} label="Requirement" sub="REQ-16 · v5 · approved" accent />
    <Box x={410} y={40} w={130} h={56} label="Design item" sub="DES-7" />
    <Box x={410} y={200} w={130} h={56} label="Test case" sub="TC-1" />
    <Box x={580} y={200} w={120} h={56} label="Test run" sub="pass · evidence" dashed />
    <Arrow d="M 150 148 L 208 148" label="derives from" lx={180} ly={140} />
    <Arrow d="M 350 140 L 408 72" label="satisfied by" lx={392} ly={98} />
    <Arrow d="M 350 156 L 408 224" label="verified by" lx={392} ly={202} />
    <Arrow d="M 540 228 L 578 228" label="result" lx={559} ly={220} />
    <text x={280} y={270} textAnchor="middle" fontSize={12} fill="var(--text-muted)">
      Change REQ-16 and every link from it goes suspect until a reviewer confirms it.
    </text>
  </Diagram>
);

const AgentDiagram: React.FC = () => (
  <Diagram
    title="The agent loop: an agent reads the project through typed tools, proposes changes, and a person approves them into the project"
    viewBox="0 0 720 260"
    caption="Agents never write to the project directly. Every change they make is a proposal with a diff, approved or rejected by a member, and recorded like any other version."
  >
    <Defs />
    <Box x={20} y={100} w={150} h={60} label="Your project" sub="artifacts · links · runs" accent />
    <Box x={285} y={30} w={150} h={60} label="Agent run" sub="your own AI, your keys" />
    <Box x={285} y={170} w={150} h={60} label="Proposal" sub="a diff, waiting for review" dashed />
    <Box x={550} y={170} w={150} h={60} label="A person" sub="approve · reject" />
    <Arrow d="M 170 120 C 230 120 230 60 283 60" label="reads (typed tools)" lx={220} ly={72} />
    <Arrow d="M 360 90 L 360 168" label="writes back as" lx={410} ly={135} />
    <Arrow d="M 435 200 L 548 200" label="reviewed by" lx={492} ly={192} />
    <Arrow d="M 550 190 C 400 120 250 130 172 132" label="approved changes land, versioned" lx={380} ly={150} />
  </Diagram>
);

const DeployDiagram: React.FC = () => (
  <Diagram
    title="Where things run: the browser talks to the API, the API to Postgres and files; agents run on a runner you control or lease"
    viewBox="0 0 720 280"
    caption="The hosted service is one API and one database per deployment, with every request checked against your workspace and project role. Self-hosting is the same stack, on your hardware."
  >
    <Defs />
    <Box x={20} y={40} w={130} h={56} label="Browser" sub="web app · phone" />
    <Box x={230} y={40} w={150} h={56} label="OpenV API" sub="Go · authz on every call" accent />
    <Box x={460} y={10} w={130} h={50} label="Postgres" sub="rows per workspace" />
    <Box x={460} y={80} w={130} h={50} label="Files" sub="attachments · figures" />
    <Box x={230} y={170} w={150} h={56} label="Runner" sub="your machine or a lease" />
    <Box x={460} y={170} w={200} h={56} label="AI provider you chose" sub="Claude · Codex · Gemini · own keys" dashed />
    <Arrow d="M 150 68 L 228 68" label="HTTPS" lx={189} ly={60} />
    <Arrow d="M 380 58 L 458 38" />
    <Arrow d="M 380 78 L 458 102" />
    <Arrow d="M 305 168 L 305 98" label="typed tools" lx={345} ly={140} />
    <Arrow d="M 380 198 L 458 198" label="prompts" lx={419} ly={190} />
    <text x={360} y={260} textAnchor="middle" fontSize={12} fill="var(--text-muted)">
      Your requirements go to the AI provider you chose, and nowhere else.
    </text>
  </Diagram>
);

const ShareDiagram: React.FC = () => (
  <Diagram
    title="Three levels of access: a public link to read, a reviewer link to comment, a membership to edit"
    viewBox="0 0 720 200"
    caption="Read and comment access travel as links because forwarding them is harmless. The right to change the specification is granted person by person."
  >
    <Defs />
    <Box x={20} y={70} w={150} h={60} label="Your project" accent />
    <Box x={290} y={10} w={170} h={50} label="Public link" sub="read, no account" />
    <Box x={290} y={75} w={170} h={50} label="Reviewer link" sub="sign in, comment, never edit" />
    <Box x={290} y={140} w={170} h={50} label="Membership" sub="editor · owner, by name" />
    <Arrow d="M 170 90 L 288 35" />
    <Arrow d="M 170 100 L 288 100" />
    <Arrow d="M 170 110 L 288 165" />
    <text x={590} y={40} textAnchor="middle" fontSize={12} fill="var(--text-muted)">customer · assessor</text>
    <text x={590} y={105} textAnchor="middle" fontSize={12} fill="var(--text-muted)">reviewer</text>
    <text x={590} y={170} textAnchor="middle" fontSize={12} fill="var(--text-muted)">contributor</text>
  </Diagram>
);

const STEPS: { title: string; body: string }[] = [
  {
    title: '1. Capture what the product must do',
    body: 'Write requirements by hand, run a guided definition with the V&V Assistant, or send stakeholders an interview link and let an interviewer agent file candidate needs as drafts. Every requirement gets a stable reference on creation and a quality score against the wording convention you chose.',
  },
  {
    title: '2. Link it to why, and to how',
    body: 'Typed links join needs to requirements, requirements to design items and test cases, hazards to mitigations, and a child project’s requirements to the parent’s they refine. Change the wording and the links go suspect until somebody confirms them.',
  },
  {
    title: '3. Prove it',
    body: 'Test runs record a result per test case, with evidence attached. Coverage, gaps and the traceability matrix update as links change; verification rolls up from subsystems to the system.',
  },
  {
    title: '4. Let agents do the legwork, and approve their work',
    body: 'Agents draft test cases, check consistency, find gaps and answer questions, on your own AI subscription. They write back as proposals; a person approves each one, and the approval is in the history.',
  },
  {
    title: '5. Freeze it and hand it over',
    body: 'Capture a baseline before a review, a release or a contract. Produce the PDF or Word document with exactly the sections and fields you need, share a read-only link, or export ReqIF for the tool on the other side.',
  },
];

export const HowItWorks: React.FC = () => {
  const { isCompact: compact } = useViewport();
  return (
    <SiteShell title="How it works">
      <Section compact={compact}>
        <Eyebrow>How it works</Eyebrow>
        <H1 compact={compact}>One graph from need to evidence</H1>
        <Lead>
          OpenV keeps a product’s requirements, the reasons behind them, the design that meets them and the tests that
          prove it in one typed graph, with AI agents that work inside the same audit trail as the people.
        </Lead>
        <Grid columns={2} compact={compact} gap={20}>
          <Card>
            <TraceDiagram />
          </Card>
          <Card>
            <AgentDiagram />
          </Card>
        </Grid>
      </Section>

      <Section alt compact={compact} id="steps">
        <H2 compact={compact}>From a blank project to a signed-off specification</H2>
        <div style={{ display: 'grid', gridTemplateColumns: compact ? '1fr' : 'repeat(auto-fit, minmax(300px, 1fr))', gap: 16 }}>
          {STEPS.map((s) => (
            <Card key={s.title} style={{ background: 'var(--bg-app)' }}>
              <h3 style={{ margin: '0 0 8px', fontSize: 17, color: 'var(--text)' }}>{s.title}</h3>
              <p style={{ margin: 0, fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)' }}>{s.body}</p>
            </Card>
          ))}
        </div>
      </Section>

      <Section compact={compact} id="architecture">
        <H2 compact={compact}>Where things run</H2>
        <Lead>
          The hosted service and a self-hosted OpenV are the same stack. Agents run wherever you put a runner: the
          Agent Connector on your own machine, or a leased cloud runner.
        </Lead>
        <Grid columns={2} compact={compact} gap={20}>
          <Card>
            <DeployDiagram />
          </Card>
          <Card>
            <ShareDiagram />
          </Card>
        </Grid>
      </Section>

      <Section alt compact={compact}>
        <H2 compact={compact}>See it working</H2>
        <Lead>Five narrated recordings on the platform’s own requirements project, three on a desktop and two on a phone.</Lead>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <Link to="/demos" style={primaryButton}>
            Watch the demos
          </Link>
          <Link to="/login?mode=register" style={secondaryButton}>
            Create free account
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
