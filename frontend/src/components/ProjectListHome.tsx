import React from 'react';
import { Link } from 'react-router-dom';
import { Chip, PanelHeader, ellipsis, mutedStyle, panelStyle, plainLink, refStyle, tapTarget, type ChipTone } from './ui';
import type { ActivityTone, TodoKind } from './ProjectListHomeData';
import type { WorkspaceHomeData } from './ProjectListHomeLoad';

// The workspace Home's upper half on the Projects page (the Home design):
// the workspace's figures, what needs the member across every project, the
// workspace's audit trail and the agent proposals waiting. The project picker
// (the Projects panel's rows) sits below it, in ProjectList.

const KIND_TONE: Record<TodoKind, ChipTone> = {
  approve: 'info',
  suspect: 'warning',
  'test run': 'neutral',
  failing: 'danger',
  comment: 'neutral',
};

const DOT: Record<ActivityTone, string> = {
  ok: 'var(--success)',
  bad: 'var(--danger)',
  warn: 'var(--warning)',
  agent: 'var(--purple)',
  info: 'var(--accent)',
};

const REF = /^[A-Z][A-Z0-9]*-\d+$/;
const LIMIT = 8;

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
}

export const ProjectListHome: React.FC<Props> = ({ workspace, projects, data, phone }) => {
  const todo = data?.todo || [];
  const activity = data?.activity || [];
  const proposals = data?.proposals || [];
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '6px 20px', flexWrap: 'wrap' }}>
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
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-start' }}>
        <section aria-labelledby="home-needs" data-help-key="home.needs" style={{ ...panelStyle, flex: '1.3 1 420px', minWidth: 0 }}>
          <PanelHeader id="home-needs" title="Needs you" count={data ? todo.length : undefined} />
          {!data && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>Gathering what waits on you…</p>}
          {data && todo.length === 0 && (
            <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>
              Nothing is waiting in any project: no reviews, suspect links, open runs, failing tests or new comments.
            </p>
          )}
          <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
            {todo.slice(0, LIMIT).map((t) => (
              <li key={`${t.project}-${t.key}`} style={{ display: 'flex', alignItems: 'center', gap: 10, minHeight: 40, padding: '4px 12px', borderBottom: '1px solid var(--border-soft)' }}>
                <span style={{ flex: 'none', width: 76 }}>
                  <Chip tone={KIND_TONE[t.kind]}>{t.kind}</Chip>
                </span>
                <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                  <span style={ellipsis}>
                    {t.ref && <span style={refStyle}>{t.ref}</span>} {t.title}
                  </span>
                  <span style={{ ...mutedStyle, ...ellipsis }}>
                    <b style={{ fontWeight: 600 }}>{t.project}</b> · {t.meta}
                  </span>
                </span>
                <Link to={t.to} style={{ ...plainLink, ...tapTarget, flex: 'none', fontSize: 12, fontWeight: 600 }}>
                  {t.action}
                </Link>
              </li>
            ))}
          </ul>
          {todo.length > LIMIT && <p style={{ ...mutedStyle, margin: 0, padding: '8px 12px' }}>And {todo.length - LIMIT} more in the projects below.</p>}
        </section>
        <div style={{ flex: '1 1 340px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12 }}>
          <section aria-labelledby="home-activity" data-help-key="home.activity" style={panelStyle}>
            <PanelHeader id="home-activity" title="Activity" />
            {data && activity.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No activity yet.</p>}
            <ol style={{ listStyle: 'none', margin: 0, padding: activity.length ? '6px 12px 10px' : 0 }}>
              {activity.map((a) => (
                <li key={a.id} style={{ display: 'flex', gap: 10, padding: '6px 0' }}>
                  <span style={{ flex: 'none', width: 8, height: 8, marginTop: 6, borderRadius: '50%', background: DOT[a.tone] }} />
                  <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                    <span>
                      <b style={{ fontWeight: 600 }}>{a.who}</b> {a.what}{' '}
                      {a.ref && <span style={REF.test(a.ref) ? refStyle : { fontWeight: 600 }}>{a.ref}</span>}
                    </span>
                    <span style={mutedStyle}>
                      {a.when}
                      {a.project ? ` · ${a.project}` : ''}
                    </span>
                  </span>
                </li>
              ))}
            </ol>
          </section>
          {proposals.length > 0 && (
            <section
              aria-label="Agent proposals"
              style={{ ...panelStyle, background: 'var(--tint-purple)', borderColor: 'var(--tint-purple-border)', padding: 12, display: 'flex', flexDirection: 'column', gap: 6 }}
            >
              <b style={{ fontSize: 13, color: 'var(--purple-strong)' }}>
                ✦ {proposals.length} agent proposal{proposals.length === 1 ? '' : 's'} waiting
              </b>
              <Link to={`/projects/${proposals[0].project_id}/agent-runs`} style={{ ...plainLink, ...tapTarget, alignSelf: 'flex-start', fontSize: 13, fontWeight: 600, color: 'var(--purple-strong)' }}>
                Review proposals
              </Link>
            </section>
          )}
        </div>
      </div>
    </div>
  );
};
