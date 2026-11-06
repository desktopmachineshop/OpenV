// The project Home page: the project's figures, what needs the team (reviews
// waiting, suspect links, test runs in progress, failing tests, comments), the
// audit trail, the workspace's projects with their traced and verified share,
// and the agent proposals waiting for review. Gated by redesign-pages.
import React, { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { apiErrorMessage } from '../api/errors';
import {
  ErrorBanner,
  FeatureGate,
  Meter,
  PanelHeader,
  ellipsis,
  mutedStyle,
  pageType,
  plainLink,
  tapTarget,
  panelStyle,
} from '../components/ui';
import { REDESIGN_PAGES_FEATURE } from '../features';
import { useViewport } from '../hooks/useViewport';
import { baselineDate } from '../utils/baselines';
import { ActivityRows, NeedsPanel, fitPanel, panelScroll } from '../components/ProjectListHomePanels';
import { loadHome, proposalLabel, type ActivityItem, type HomeData, type TodoItem } from './ProjectHomeData';

const figure = (value: number | null, suffix = '') => (value === null ? '–' : `${value}${suffix}`);

const Figure: React.FC<{ value: string; label: string; danger?: boolean }> = ({ value, label, danger }) => (
  <span>
    <b style={{ fontSize: 16, fontWeight: 700, color: danger ? 'var(--danger-strong)' : undefined }}>{value}</b>{' '}
    <span style={mutedStyle}>{label}</span>
  </span>
);

const PhoneFigure: React.FC<{ value: string; label: string; to: string; danger?: boolean }> = ({ value, label, to, danger }) => (
  <Link
    to={to}
    style={{
      ...panelStyle,
      flex: '1 1 0',
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      padding: '8px 4px',
      color: 'var(--text)',
      textDecoration: 'none',
    }}
  >
    <b style={{ fontSize: 18, color: danger ? 'var(--danger-strong)' : undefined }}>{value}</b>
    <span style={mutedStyle}>{label}</span>
  </Link>
);

// On a wider screen the page fits the window: the figures on top, then the
// panels share the height left, each scrolling on its own (fit). A phone
// scrolls the page as a whole.

const NeedsYou: React.FC<{ items: TodoItem[]; projectId: string; limit: number; flex?: string; fit?: boolean }> = ({ items, projectId, limit, flex = '1.3 1 420px', fit = false }) => (
  <NeedsPanel
    items={items}
    limit={limit}
    action={<Link to={`/projects/${projectId}/review`} style={{ ...plainLink, fontWeight: 600 }}>Review all</Link>}
    emptyText="Nothing is waiting: no reviews, suspect links, open runs, failing tests or new comments."
    style={{ flex, ...(fit ? fitPanel : {}) }}
  />
);

const AuditTrail: React.FC<{ items: ActivityItem[]; projectId: string; title: string; flex?: string; fit?: boolean }> = ({
  items,
  projectId,
  title,
  flex = '1 1 340px',
  fit = false,
}) => (
  <section aria-labelledby="home-activity" data-help-key="home.activity" style={{ ...panelStyle, flex, ...(fit ? fitPanel : {}) }}>
    <PanelHeader id="home-activity" title={title} action={<Link to={`/projects/${projectId}/activity`} style={plainLink}>Full history</Link>} />
    <div style={panelScroll}>
      {items.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No activity yet.</p>}
      <ActivityRows items={items} />
    </div>
  </section>
);

const Projects: React.FC<{ data: HomeData; projectId: string; fit?: boolean }> = ({ data, projectId, fit = false }) => (
  <section aria-labelledby="home-projects" data-help-key="home.projects" style={fit ? { ...panelStyle, ...fitPanel, flex: '1 1 0' } : panelStyle}>
    <PanelHeader id="home-projects" title="Projects" action={<Link to="/projects" style={plainLink}>All projects</Link>} />
    <div style={panelScroll}>
    {data.projects.map((p) => (
      <Link
        key={p.id}
        to={`/projects/${p.id}/home`}
        aria-current={p.id === projectId ? 'page' : undefined}
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 5,
          padding: '9px 12px',
          borderBottom: '1px solid var(--border-soft)',
          color: 'var(--text)',
          textDecoration: 'none',
          background: p.id === projectId ? 'var(--tint-blue)' : 'var(--surface)',
        }}
      >
        <span style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
          <b style={{ fontWeight: 600, ...ellipsis }}>{p.name}</b>
          {p.count !== null && <span style={{ ...mutedStyle, flex: 'none' }}>{p.count} artifacts</span>}
        </span>
        {p.traced !== null && <Meter label="Traced" value={p.traced} color="var(--accent)" />}
        {p.verified !== null && <Meter label="Verified" value={p.verified} color="var(--success)" />}
        {p.traced === null && <span style={mutedStyle}>No requirements to trace yet</span>}
      </Link>
    ))}
    </div>
  </section>
);

const Proposals: React.FC<{ data: HomeData; projectId: string }> = ({ data, projectId }) => {
  const n = data.proposals.length;
  if (n === 0) return null;
  return (
    <section
      aria-label="Agent proposals"
      data-help-key="home.proposals"
      style={{
        background: 'var(--tint-purple)',
        border: '1px solid var(--tint-purple-border)',
        borderRadius: 6,
        padding: '10px 12px',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        flex: 'none',
      }}
    >
      <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--purple-strong)' }}>
        ✦ {n} agent {n === 1 ? 'proposal' : 'proposals'} waiting
      </span>
      <span style={{ fontSize: 13 }}>
        {data.proposals
          .slice(0, 3)
          .map((p) => proposalLabel(p))
          .join(' · ')}
        {n > 3 ? ` · and ${n - 3} more` : ''}
      </span>
      <Link to={`/projects/${projectId}/agent-runs`} style={{ ...plainLink, ...tapTarget, alignSelf: 'flex-start', fontSize: 13, fontWeight: 600, color: 'var(--purple-strong)' }}>
        Review proposals
      </Link>
    </section>
  );
};

const HomeBody: React.FC<{ data: HomeData; projectId: string; phone: boolean }> = ({ data, projectId, phone }) => {
  const base = `/projects/${projectId}`;
  if (phone) {
    return (
      <div style={{ padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
        <h1 style={{ margin: 0, fontSize: 18 }}>{data.project?.name || 'Home'}</h1>
        <div style={{ display: 'flex', gap: 8 }} data-help-key="home.figures">
          <PhoneFigure value={figure(data.traced, '%')} label="traced" to={`${base}/matrix`} />
          <PhoneFigure value={figure(data.verified, '%')} label="verified" to={`${base}/vv`} />
          <PhoneFigure value={figure(data.failing)} label="failing" to={`${base}/tests`} danger={!!data.failing} />
          <PhoneFigure value={figure(data.suspect)} label="suspect" to={`${base}/review`} />
        </div>
        <NeedsYou items={data.todo} projectId={projectId} limit={5} flex="none" />
        <Proposals data={data} projectId={projectId} />
        <AuditTrail items={data.activity.slice(0, 4)} projectId={projectId} title="Recent activity" flex="none" />
      </div>
    );
  }
  const b = data.lastBaseline;
  const sub = [
    b ? `Last baseline ${b.name} · ${baselineDate(b.created_at)}` : 'No baseline yet',
    data.members !== null ? `${data.members} ${data.members === 1 ? 'member' : 'members'}` : '',
  ].filter(Boolean);
  return (
    <div style={{ padding: 16, display: 'flex', flexDirection: 'column', gap: 12, boxSizing: 'border-box', height: '100%', minHeight: 520 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: '6px 20px', flex: 'none' }} data-help-key="home.figures">
        <h1 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>{data.project?.name || 'Home'}</h1>
        <span style={mutedStyle}>{sub.join(' · ')}</span>
        <span style={{ flexGrow: 1 }} />
        <Figure value={figure(data.artifacts)} label="artifacts" />
        <Figure value={figure(data.traced, '%')} label="traced" />
        <Figure value={figure(data.verified, '%')} label="verified" />
        <Figure value={figure(data.failing)} label="failing" danger={!!data.failing} />
      </div>
      <div style={{ display: 'flex', gap: 12, alignItems: 'stretch', flex: '1 1 0', minHeight: 0 }}>
        <NeedsYou items={data.todo} projectId={projectId} limit={Infinity} fit />
        <AuditTrail items={data.activity} projectId={projectId} title="Audit trail" fit />
        <div style={{ flex: '1 1 280px', maxWidth: 360, minWidth: 0, minHeight: 0, display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Projects data={data} projectId={projectId} fit />
          <Proposals data={data} projectId={projectId} />
        </div>
      </div>
    </div>
  );
};

const HomeContent: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { isPhone } = useViewport();
  const [data, setData] = useState<HomeData | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let live = true;
    setData(null);
    loadHome(projectId)
      .then((d) => live && setData(d))
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load the project home')));
    return () => {
      live = false;
    };
  }, [projectId]);
  if (error) return <ErrorBanner message={error} />;
  if (!data) return <div style={{ padding: 16, color: 'var(--text-muted)' }}>Loading…</div>;
  return (
    <div data-help-pane="home" style={{ ...pageType(isPhone), ...(isPhone ? {} : { height: '100%' }) }}>
      <HomeBody data={data} projectId={projectId} phone={isPhone} />
    </div>
  );
};

export const ProjectHome: React.FC = () => (
  <FeatureGate feature={REDESIGN_PAGES_FEATURE} name="Home">
    <HomeContent />
  </FeatureGate>
);
