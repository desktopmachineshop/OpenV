import React from 'react';
import { PLANS, type AdminWorkspace } from '../../api/client';
import { AdminToggle, ResetLinkButton, type PersonActions } from './adminControls';
import { td, th } from './dashboardParts';
import { NO_FILTER, filterPeople, type PeopleFilter, type PersonRow } from './usageModel';
import { PaceBadge, Sparkline, TREND_OPTIONS, lastSeen } from './usageParts';

// The usage view by person: everyone, filtered by plan, workspace, pace or
// a search, with their workspaces, their weekly changes and signed-in days,
// their pace, and the platform-admin controls.

interface Props extends PersonActions {
  people: PersonRow[];
  workspaces: AdminWorkspace[];
  paceWeeks: number;
  filter: PeopleFilter;
  onFilter: (f: PeopleFilter) => void;
}

const muted: React.CSSProperties = { color: 'var(--text-muted)', fontSize: 12 };
const control: React.CSSProperties = { padding: '5px 8px', fontSize: 13, width: 'auto', minWidth: 130 };

export const PeopleUsageList: React.FC<Props> = ({ people, workspaces, paceWeeks, filter, onFilter, ...actions }) => {
  const shown = filterPeople(people, filter);
  // The workspace list narrows to the chosen plan.
  const choices = workspaces
    .filter((w) => !filter.plan || w.plan === filter.plan)
    .sort((a, b) => a.name.localeCompare(b.name));
  const plans = PLANS.filter((p) => workspaces.some((w) => w.plan === p.value));
  const set = (patch: Partial<PeopleFilter>) => onFilter({ ...filter, ...patch });
  const filtered = filter.plan || filter.workspaceId || filter.trend || filter.search;
  return (
    <div>
      <div role="search" style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', marginBottom: 10 }}>
        <select aria-label="Filter people by plan" value={filter.plan} style={control}
          onChange={(e) => set({ plan: e.target.value, workspaceId: '' })}>
          <option value="">Every plan</option>
          {plans.map((p) => (
            <option key={p.value} value={p.value}>
              {p.label}
            </option>
          ))}
        </select>
        <select aria-label="Filter people by workspace" value={filter.workspaceId} style={control}
          onChange={(e) => set({ workspaceId: e.target.value })}>
          <option value="">Every workspace</option>
          {choices.map((w) => (
            <option key={w.id} value={w.id}>
              {w.name}
            </option>
          ))}
        </select>
        <select aria-label="Filter people by pace" value={filter.trend} style={control} onChange={(e) => set({ trend: e.target.value })}>
          <option value="">Any pace</option>
          {TREND_OPTIONS.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
        <input type="search" aria-label="Search people" placeholder="Name or email" value={filter.search}
          onChange={(e) => set({ search: e.target.value })} style={{ ...control, flex: '1 1 160px' }} />
        <span style={muted}>
          {shown.length} of {people.length}
        </span>
        {filtered && (
          <button type="button" onClick={() => onFilter(NO_FILTER)} style={{ ...control, minWidth: 0, cursor: 'pointer' }}>
            Clear
          </button>
        )}
      </div>
      <div className="table-scroll">
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>Person</th>
              <th style={th}>Workspaces</th>
              <th style={{ ...th, width: 80, textAlign: 'right' }}>Days in</th>
              <th style={{ ...th, width: 96 }}>Changes</th>
              <th style={{ ...th, width: 150 }}>Pace</th>
              <th style={{ ...th, width: 100 }}>Last seen</th>
              <th style={{ ...th, width: 150 }}>Platform admin</th>
              <th style={{ ...th, width: 110 }}>Password</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((p) => (
              <tr key={p.user.id}>
                <td style={td}>
                  <div>
                    {p.user.name || '—'}
                    {actions.currentUserId === p.user.id && <span style={muted}> (you)</span>}
                  </div>
                  <div style={muted}>{p.user.email}</div>
                </td>
                <td style={{ ...td, fontSize: 12 }}>
                  {p.workspaces.length === 0 ? (
                    <span style={muted}>none</span>
                  ) : (
                    p.workspaces.map((m) => (
                      <div key={m.workspace.id}>
                        {m.workspace.name} <span style={muted}>{m.role}</span>
                      </div>
                    ))
                  )}
                </td>
                <td style={{ ...td, textAlign: 'right' }} title={`Days signed in, last ${paceWeeks} weeks`}>
                  {p.usage.active_days.slice(-paceWeeks).reduce((a, b) => a + b, 0)}
                </td>
                <td style={td}>
                  <Sparkline weeks={p.usage.changes} paceWeeks={paceWeeks} label={`Changes by ${p.user.name || p.user.email}`} />
                </td>
                <td style={td}>
                  <PaceBadge pace={p.usage.pace} paceWeeks={paceWeeks} />
                </td>
                <td style={{ ...td, ...muted }}>{lastSeen(p.usage.last_active)}</td>
                <td style={td}>
                  <AdminToggle u={p.user} {...actions} />
                </td>
                <td style={td}>
                  <ResetLinkButton u={p.user} {...actions} />
                </td>
              </tr>
            ))}
            {shown.length === 0 && (
              <tr>
                <td style={td} colSpan={8}>
                  Nobody matches.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
