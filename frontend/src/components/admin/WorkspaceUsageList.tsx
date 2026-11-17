import React, { useState } from 'react';
import type { AdminWorkspace } from '../../api/client';
import { PlanSelect, linkButton } from './adminControls';
import { td, th } from './dashboardParts';
import { membersOf, type PersonRow, type TierGroup, type WorkspaceRow } from './usageModel';
import { PaceBadge, Sparkline, lastSeen } from './usageParts';

// The usage view by workspace: one block per plan, company workspaces
// before personal ones, each workspace with what it holds (counts only),
// its weekly changes and pace, and its members under it on demand.

interface Props {
  groups: TierGroup[];
  people: PersonRow[];
  paceWeeks: number;
  busy: string;
  onChangePlan: (ws: AdminWorkspace, plan: string) => void;
  /** Open the people view filtered to this workspace. */
  onShowPeople: (workspaceId: string) => void;
}

const num: React.CSSProperties = { ...td, textAlign: 'right' };
const muted: React.CSSProperties = { color: 'var(--text-muted)', fontSize: 12 };

const Holds: React.FC<{ row: WorkspaceRow }> = ({ row: { usage: u } }) => (
  <span style={{ fontSize: 12, whiteSpace: 'nowrap' }} title={`${u.projects} projects, ${u.other_artifacts} other artifacts, ${u.test_results} test results, ${u.comments} comments`}>
    {u.requirements} req · {u.test_cases} tests · {u.links} links · {u.test_runs} runs
  </span>
);

const Members: React.FC<{ people: (PersonRow & { role: string })[]; paceWeeks: number }> = ({ people, paceWeeks }) => (
  <tr>
    <td colSpan={7} style={{ ...td, background: 'var(--bg-app)', padding: '6px 8px 10px 28px' }}>
      {people.length === 0 ? (
        <span style={muted}>No members.</span>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <tbody>
            {people.map((p) => (
              <tr key={p.user.id}>
                <td style={td}>
                  {p.user.name || p.user.email} <span style={muted}>{p.role}</span>
                </td>
                <td style={{ ...num, width: 90 }} title="Days signed in, last 4 weeks">
                  {p.usage.active_days.slice(-paceWeeks).reduce((a, b) => a + b, 0)} days
                </td>
                <td style={{ ...td, width: 96 }}>
                  <Sparkline weeks={p.usage.changes} paceWeeks={paceWeeks} label={`Changes by ${p.user.name || p.user.email}`} />
                </td>
                <td style={{ ...td, width: 150 }}>
                  <PaceBadge pace={p.usage.pace} paceWeeks={paceWeeks} />
                </td>
                <td style={{ ...td, width: 110, ...muted }}>{lastSeen(p.usage.last_active)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </td>
  </tr>
);

const Row: React.FC<{ row: WorkspaceRow } & Omit<Props, 'groups'>> = ({ row, people, paceWeeks, busy, onChangePlan, onShowPeople }) => {
  const [open, setOpen] = useState(false);
  const { workspace: ws, usage: u } = row;
  return (
    <>
      <tr>
        <td style={td}>
          <button
            type="button"
            onClick={() => setOpen(!open)}
            aria-expanded={open}
            aria-label={`${open ? 'Hide' : 'Show'} the members of ${ws.name}`}
            style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', color: 'var(--text)', textAlign: 'left', width: 'auto' }}
          >
            <span aria-hidden style={{ display: 'inline-block', width: 14 }}>{open ? '▾' : '▸'}</span>
            <strong>{ws.name}</strong>
          </button>
          <div style={{ ...muted, paddingLeft: 14 }}>{ws.slug}</div>
        </td>
        <td style={num} title="Signed in in the last 30 days / members">
          {u.active_members_30d}/{ws.members}
        </td>
        <td style={td}>
          <Holds row={row} />
        </td>
        <td style={td}>
          <Sparkline weeks={u.changes} paceWeeks={paceWeeks} label={`Changes in ${ws.name}`} />
        </td>
        <td style={td}>
          <PaceBadge pace={u.pace} paceWeeks={paceWeeks} />
        </td>
        <td style={{ ...td, ...muted }}>{lastSeen(u.last_activity)}</td>
        <td style={td}>
          <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            <PlanSelect ws={ws} disabled={busy === ws.id} onChange={onChangePlan} />
            <button type="button" style={linkButton} onClick={() => onShowPeople(ws.id)} aria-label={`People in ${ws.name}`}>
              People
            </button>
          </div>
        </td>
      </tr>
      {open && <Members people={membersOf(people, ws.id)} paceWeeks={paceWeeks} />}
    </>
  );
};

export const WorkspaceUsageList: React.FC<Props> = (props) => {
  const { groups, paceWeeks } = props;
  if (groups.length === 0) return <div style={muted}>No workspaces yet.</div>;
  return (
    <div>
      {groups.map((tier) => (
        <section key={tier.plan} aria-label={`${tier.label} workspaces`} style={{ marginBottom: 18 }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'center', margin: '4px 0 6px' }}>
            <h3 style={{ margin: 0, fontSize: 15 }}>{tier.label}</h3>
            <span style={muted}>
              {tier.workspaces} {tier.workspaces === 1 ? 'workspace' : 'workspaces'} · {tier.members} members ·{' '}
              {tier.pace.recent} changes in {paceWeeks} weeks
            </span>
            <Sparkline weeks={tier.changes} paceWeeks={paceWeeks} label={`Changes on ${tier.label}`} />
            <PaceBadge pace={tier.pace} paceWeeks={paceWeeks} />
          </div>
          {tier.kinds.map((kind) => (
            <div key={kind.kind} className="table-scroll" style={{ marginBottom: 8 }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={th}>{kind.label}</th>
                    <th style={{ ...th, width: 70, textAlign: 'right' }}>Active</th>
                    <th style={th}>Holds</th>
                    <th style={{ ...th, width: 96 }}>{tier.changes.length} weeks</th>
                    <th style={{ ...th, width: 150 }}>Pace</th>
                    <th style={{ ...th, width: 100 }}>Last change</th>
                    <th style={{ ...th, width: 240 }}>Plan</th>
                  </tr>
                </thead>
                <tbody>
                  {kind.rows.map((row) => (
                    <Row key={row.workspace.id} row={row} {...props} />
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </section>
      ))}
    </div>
  );
};
