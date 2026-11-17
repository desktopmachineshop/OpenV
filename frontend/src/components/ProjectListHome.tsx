import React from 'react';
import { Link } from 'react-router-dom';
import { PanelHeader, mutedStyle, panelStyle, plainLink, tapTarget } from './ui';
import { ActivityRows, NeedsPanel, fitPanel, panelScroll } from './ProjectListHomePanels';
import type { WorkspaceHomeData, WorkspaceTodo } from './ProjectListHomeLoad';
import { useUiRefresh } from '../hooks/useUiRefresh';

// The workspace Home's upper half on the Projects page (the Home design):
// the workspace's figures, what needs the member across every project, the
// workspace's audit trail and the agent proposals waiting. The project picker
// (the Projects panel's rows) sits below it, in ProjectList.

const LIMIT = 8;

/**
 * Where Review all goes: there is no review page across projects, so it is
 * the review queue of the project with the most reviews and suspect links
 * waiting (the first with anything waiting, failing that), or none.
 */
export const reviewAllTarget = (todo: WorkspaceTodo[]): string => {
  const counts = new Map<string, number>();
  for (const t of todo) {
    const review = t.kind === 'approve' || t.kind === 'suspect' ? 1 : 0;
    counts.set(t.projectId, (counts.get(t.projectId) || 0) + review);
  }
  let best = '';
  let most = -1;
  for (const [id, n] of counts) if (n > most) [best, most] = [id, n];
  return best ? `/projects/${best}/review` : '';
};

const Figure: React.FC<{ value: number | null; suffix?: string; label: string; danger?: boolean }> = ({ value, suffix = '', label, danger }) => (
  <span style={{ whiteSpace: 'nowrap' }}>
    <b style={{ fontSize: 16, fontWeight: 700, color: danger ? 'var(--danger-strong)' : undefined }}>{value === null ? '–' : `${value}${suffix}`}</b>{' '}
    <span style={mutedStyle}>{label}</span>
  </span>
);

interface Props {
  workspace: string;
  projects: number;
  data: WorkspaceHomeData | null;
  phone: boolean;
  /**
   * The panels fill the height they are given (the page fits the window on a
   * wider screen) and each scrolls on its own, showing every row rather than
   * the first few.
   */
  fit?: boolean;
}

export const ProjectListHome: React.FC<Props> = ({ workspace, projects, data, phone, fit = false }) => {
  const todo = data?.todo || [];
  const activity = data?.activity || [];
  const proposals = data?.proposals || [];
  const reviewAll = reviewAllTarget(todo);
  // The refreshed Home (ui-refresh) gives an empty Activity panel no room:
  // what waits on you takes the width instead.
  const quiet = useUiRefresh() && !!data && activity.length === 0;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, ...(fit ? { height: '100%', minHeight: 0 } : {}) }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '6px 20px', flexWrap: 'wrap', flex: 'none' }}>
        <h1 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>{workspace}</h1>
        <span style={mutedStyle}>
          {projects} project{projects === 1 ? '' : 's'} in this workspace
        </span>
        {!phone && <span style={{ flex: 1 }} />}
        {data && (
          <>
            <Figure value={data.totals.artifacts} label="artifacts" />
            <Figure value={data.totals.traced} suffix="%" label="traced" />
            <Figure value={data.totals.verified} suffix="%" label="verified" />
            <Figure value={data.totals.failing} label="failing" danger={data.totals.failing > 0} />
          </>
        )}
      </div>
      <div
        style={
          fit
            ? { display: 'flex', gap: 12, flex: '1 1 0', minHeight: 0, alignItems: 'stretch' }
            : { display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-start' }
        }
      >
        <NeedsPanel
          items={data ? todo : null}
          limit={fit ? Infinity : LIMIT}
          action={reviewAll && <Link to={reviewAll} style={{ ...plainLink, fontWeight: 600 }}>Review all</Link>}
          emptyText="Nothing is waiting in any project: no reviews, suspect links, open runs, failing tests or new comments."
          more={(n) => `And ${n} more in the projects below.`}
          style={{ flex: '1.3 1 420px', minWidth: 0, ...(fit ? fitPanel : {}) }}
        />
        {!(quiet && proposals.length === 0) && <div style={{ flex: '1 1 340px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12, ...(fit ? { minHeight: 0 } : {}) }}>
          {!quiet && <section
            aria-labelledby="home-activity"
            data-help-key="home.activity"
            style={fit ? { ...panelStyle, ...fitPanel, flex: '1 1 0' } : panelStyle}
          >
            <PanelHeader id="home-activity" title="Activity" />
            <div style={panelScroll}>
              {data && activity.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No activity yet.</p>}
              <ActivityRows items={activity} />
            </div>
          </section>}
          {proposals.length > 0 && (
            <section
              aria-label="Agent proposals"
              style={{ ...panelStyle, flex: 'none', background: 'var(--tint-purple)', borderColor: 'var(--tint-purple-border)', padding: 12, display: 'flex', flexDirection: 'column', gap: 6 }}
            >
              <b style={{ fontSize: 13, color: 'var(--purple-strong)' }}>
                ✦ {proposals.length} agent proposal{proposals.length === 1 ? '' : 's'} waiting
              </b>
              <Link to={`/projects/${proposals[0].project_id}/agent-runs`} style={{ ...plainLink, ...tapTarget, alignSelf: 'flex-start', fontSize: 13, fontWeight: 600, color: 'var(--purple-strong)' }}>
                Review proposals
              </Link>
            </section>
          )}
        </div>}
      </div>
    </div>
  );
};
