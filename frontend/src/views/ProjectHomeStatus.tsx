// Where the project stands, for the Overview of the refreshed interface
// (ui-refresh): the project's figures, what needs the team (the agent
// proposals among them) and the latest activity. It is the project Home's
// content in one column, loaded on its own so the Overview, which is in the
// first load, does not bring the Home page's code with it.
import React, { useEffect, useState } from 'react';
import { apiErrorMessage } from '../api/errors';
import { ErrorBanner, pageType } from '../components/ui';
import { useFeature } from '../hooks/useFeature';
import { useAppStore } from '../state/store';
import { COMMENT_READS_FEATURE } from '../components/noteReadState';
import { AuditTrail, NeedsYou, PhoneFigure, figure } from './ProjectHome';
import { loadHome, proposalLabel, type HomeData, type TodoItem } from './ProjectHomeData';

/** The agent proposals waiting, as rows of the needs list: each opens the runs page, where it is decided. */
const proposalNeeds = (data: HomeData, projectId: string): TodoItem[] =>
  data.proposals.map((p) => ({
    key: `proposal:${p.id}`,
    kind: 'proposal',
    ref: '',
    title: proposalLabel(p),
    meta: 'An agent proposes this change',
    action: 'Review',
    to: `/projects/${projectId}/agent-runs`,
    open: `/projects/${projectId}/agent-runs`,
  }));

/**
 * Where the project stands, beside what it is: the figures, what needs the
 * team (the agent proposals among them) and the latest activity. The
 * Overview of the refreshed interface (ui-refresh) draws it next to the
 * product's definition, so the one page answers both questions.
 */
const ProjectStatus: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [data, setData] = useState<HomeData | null>(null);
  const [error, setError] = useState('');
  const commentReadsOn = useFeature(COMMENT_READS_FEATURE);
  const meId = useAppStore((st) => st.currentUser?.id || null);
  useEffect(() => {
    let live = true;
    setData(null);
    loadHome(projectId, { userId: meId, commentReads: commentReadsOn })
      .then((d) => live && setData(d))
      .catch((err) => live && setError(apiErrorMessage(err, "Failed to load the project's status")));
    return () => {
      live = false;
    };
  }, [projectId, meId, commentReadsOn]);
  if (error) return <ErrorBanner message={error} />;
  if (!data) return <div style={{ padding: 12, color: 'var(--text-muted)' }}>Loading…</div>;
  const base = `/projects/${projectId}`;
  return (
    <div data-help-pane="home" style={{ ...pageType(true), display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', gap: 8 }} data-help-key="home.figures">
        <PhoneFigure value={figure(data.traced, '%')} label="traced" to={`${base}/matrix`} />
        <PhoneFigure value={figure(data.verified, '%')} label="verified" to={`${base}/vv`} />
        <PhoneFigure value={figure(data.failing)} label="failing" to={`${base}/tests`} danger={!!data.failing} />
        <PhoneFigure value={figure(data.suspect)} label="suspect" to={`${base}/review`} />
      </div>
      <NeedsYou items={[...data.todo, ...proposalNeeds(data, projectId)]} projectId={projectId} limit={6} flex="none" />
      <AuditTrail items={data.activity.slice(0, 5)} projectId={projectId} title="Recent activity" flex="none" />
    </div>
  );
};

export default ProjectStatus;
