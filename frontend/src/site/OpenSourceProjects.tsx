import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { OpenSourceProject, openSourceAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useViewport } from '../hooks/useViewport';
import { LICENSE_GLOSS, LICENSE_INTENT, REPO_URL } from '../landing/content';
import { OPENV_OWN_PROJECT_NOTE, OPENV_WORKSPACE, OPEN_SOURCE_CLAIM_URL, OPEN_SOURCE_INTRO } from './content';
import { Card, ExternalLink, Eyebrow, Grid, H1, H2, Lead, Section, SiteShell, primaryButton, secondaryButton } from './SiteShell';
import type { ArtifactTypeValue } from '../generated/contract';

// The open-source page (REQ-151): every project of a workspace on the
// open-source plan, as of its latest baseline. The list comes from a public
// endpoint; each entry opens in the read-only shared view. OpenV's own
// projects have a section of their own; the rest are grouped by workspace.

const countsLine = (counts: Record<string, number>): string => {
  const names: Record<string, [string, string]> = {
    requirement: ['requirement', 'requirements'],
    'user-need': ['need', 'needs'],
    'design-item': ['design item', 'design items'],
    'test-case': ['test case', 'test cases'],
    hazard: ['hazard', 'hazards'],
  } satisfies Partial<Record<ArtifactTypeValue, [string, string]>>;
  return Object.keys(names)
    .filter((k) => counts[k] > 0)
    .map((k) => `${counts[k]} ${names[k][counts[k] === 1 ? 0 : 1]}`)
    .join(' · ');
};

/** Groups projects by workspace, in the order the list first names each
 * one: the list is newest snapshot first, so is the grouping. */
export const groupByWorkspace = (projects: OpenSourceProject[]): [string, OpenSourceProject[]][] => {
  const groups = new Map<string, OpenSourceProject[]>();
  for (const p of projects) {
    const group = groups.get(p.workspace);
    if (group) group.push(p);
    else groups.set(p.workspace, [p]);
  }
  return [...groups.entries()];
};

// background contrasts the card with its section's.
const ProjectCard: React.FC<{ project: OpenSourceProject; background: string }> = ({ project: p, background }) => (
  <Card style={{ background, display: 'flex', flexDirection: 'column', gap: 8 }}>
    <h3 style={{ margin: 0, fontSize: 18, color: 'var(--text)' }}>
      <Link to={`/open-source/${p.project_id}`} style={{ color: 'inherit', textDecoration: 'none' }}>
        {p.name}
      </Link>
    </h3>
    {p.description && <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: 'var(--text-body)' }}>{p.description}</p>}
    <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 'auto' }}>
      {countsLine(p.counts)}
      {countsLine(p.counts) ? ' · ' : ''}snapshot “{p.baseline}” · {new Date(p.snapshot_at).toLocaleDateString()}
    </div>
    <Link to={`/open-source/${p.project_id}`} style={{ ...secondaryButton, fontSize: 14, padding: '8px 14px', alignSelf: 'flex-start' }}>
      Read the project
    </Link>
  </Card>
);

export const OpenSourceProjects: React.FC = () => {
  const { isCompact: compact } = useViewport();
  const [projects, setProjects] = useState<OpenSourceProject[] | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    openSourceAPI
      .list()
      .then((res) => {
        if (!cancelled) setProjects(res.data || []);
      })
      .catch((err) => {
        if (!cancelled) setError(apiErrorMessage(err, 'The list could not be loaded.'));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const openvProjects = (projects || []).filter((p) => p.workspace === OPENV_WORKSPACE);
  const communityGroups = groupByWorkspace((projects || []).filter((p) => p.workspace !== OPENV_WORKSPACE));
  const status = (
    <>
      {error && <p style={{ color: 'var(--danger-text, #c0392b)', fontSize: 14 }}>{error}</p>}
      {!error && projects === null && <p style={{ color: 'var(--text-muted)', fontSize: 14 }}>Loading…</p>}
    </>
  );

  return (
    <SiteShell title="Open-source projects">
      <Section compact={compact}>
        <Eyebrow>Open source</Eyebrow>
        <H1 compact={compact}>Open-source projects on OpenV</H1>
        <Lead>{OPEN_SOURCE_INTRO}</Lead>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <ExternalLink href={OPEN_SOURCE_CLAIM_URL} style={primaryButton}>
            Claim free hosting for your project
          </ExternalLink>
          <Link to="/faq#tiers" style={secondaryButton}>
            What the tier includes
          </Link>
        </div>
      </Section>

      <Section alt compact={compact} id="openv-projects">
        <Eyebrow>{OPENV_WORKSPACE}</Eyebrow>
        <H2 compact={compact}>OpenV&rsquo;s own projects</H2>
        <Lead>{OPENV_OWN_PROJECT_NOTE}</Lead>
        {status}
        {openvProjects.length > 0 && (
          <Grid columns={2} compact={compact} gap={16}>
            {openvProjects.map((p) => (
              <ProjectCard key={p.project_id} project={p} background="var(--bg-app)" />
            ))}
          </Grid>
        )}
      </Section>

      <Section compact={compact} id="projects">
        <H2 compact={compact}>Published projects</H2>
        {status}
        {projects && communityGroups.length === 0 && (
          <Card style={{ background: 'var(--surface)', maxWidth: 720 }}>
            <h3 style={{ margin: '0 0 6px', fontSize: 17, color: 'var(--text)' }}>No projects published yet</h3>
            <p style={{ margin: 0, fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)' }}>
              A project appears here as soon as its workspace is on the open-source tier and it has captured a baseline.
              Yours could be the first.
            </p>
          </Card>
        )}
        {communityGroups.map(([workspace, list]) => (
          <div key={workspace} style={{ marginTop: 28 }}>
            <h3 style={{ margin: '0 0 12px', fontSize: 20, color: 'var(--text)' }}>{workspace}</h3>
            <Grid columns={2} compact={compact} gap={16}>
              {list.map((p) => (
                <ProjectCard key={p.project_id} project={p} background="var(--surface)" />
              ))}
            </Grid>
          </div>
        ))}
      </Section>

      <Section alt compact={compact} id="openv">
        <H2 compact={compact}>OpenV itself is open source</H2>
        <Lead>{LICENSE_GLOSS}</Lead>
        <ul style={{ margin: '0 0 20px', paddingLeft: 20, fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)', maxWidth: 720 }}>
          {LICENSE_INTENT.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
        <p style={{ margin: '0 0 20px', fontSize: 15, lineHeight: 1.6, color: 'var(--text-body)', maxWidth: 720 }}>
          Contributions are welcome under the Developer Certificate of Origin.
        </p>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          <ExternalLink href={REPO_URL} style={primaryButton}>
            View on GitHub
          </ExternalLink>
          <Link to="/how-it-works" style={secondaryButton}>
            How it works
          </Link>
        </div>
      </Section>
    </SiteShell>
  );
};
