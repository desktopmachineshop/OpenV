import React, { useEffect, useMemo, useState } from 'react';
import { adminAPI, type AdminUsage, type AdminUser, type AdminWorkspace } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import type { PersonActions } from './adminControls';
import { PeopleUsageList } from './PeopleUsageList';
import { WorkspaceUsageList } from './WorkspaceUsageList';
import { NO_FILTER, groupWorkspaces, joinPeople, type PeopleFilter } from './usageModel';

// UsageOverview is the Platform admin page's usage view (FeatureAdminUsage):
// how much each workspace and each person uses OpenV, in counts only — what
// a workspace holds, its changes per week and whether that pace is rising
// or falling — and never what any of it says. Two ways in: by workspace,
// grouped by plan and then company or personal, with each workspace's
// members under it; or by person, filtered by plan, workspace or pace. The
// plan, platform-admin and reset-link controls live in these lists.

interface Props extends PersonActions {
  compact: boolean;
  workspaces: AdminWorkspace[] | null;
  users: AdminUser[] | null;
  onChangePlan: (ws: AdminWorkspace, plan: string) => void;
  /** Bumped by the page after a change, so the counts are read again. */
  reload: number;
}

type View = 'workspaces' | 'people';

const tab = (active: boolean): React.CSSProperties => ({
  background: active ? 'var(--accent)' : 'none',
  color: active ? 'var(--on-accent, #fff)' : 'var(--text)',
  border: '1px solid var(--border)',
  borderRadius: 4,
  padding: '5px 12px',
  fontSize: 13,
  cursor: 'pointer',
  width: 'auto',
  minHeight: 32,
});

export const UsageOverview: React.FC<Props> = ({ compact, workspaces, users, onChangePlan, reload, ...actions }) => {
  const [usage, setUsage] = useState<AdminUsage | null>(null);
  const [error, setError] = useState('');
  const [view, setView] = useState<View>('workspaces');
  const [filter, setFilter] = useState<PeopleFilter>(NO_FILTER);

  useEffect(() => {
    // The page bumps reload once its listings are in, and after each change.
    if (!reload) return;
    adminAPI
      .usage()
      .then((r) => setUsage(r.data))
      .catch((err) => setError(apiErrorMessage(err, 'Could not load usage')));
  }, [reload]);

  const groups = useMemo(() => (usage && workspaces ? groupWorkspaces(workspaces, usage) : []), [usage, workspaces]);
  const people = useMemo(() => (usage && workspaces && users ? joinPeople(users, workspaces, usage) : []), [usage, workspaces, users]);
  const ready = usage && workspaces && users;

  return (
    <section className="card" id="usage" style={{ padding: compact ? 14 : 20, marginBottom: 16 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', marginBottom: 6 }}>
        <h2 style={{ margin: 0, fontSize: 18, flex: '1 1 auto' }}>Usage</h2>
        <div role="tablist" aria-label="Usage by" style={{ display: 'flex', gap: 6 }}>
          <button type="button" role="tab" aria-selected={view === 'workspaces'} style={tab(view === 'workspaces')} onClick={() => setView('workspaces')}>
            By workspace
          </button>
          <button type="button" role="tab" aria-selected={view === 'people'} style={tab(view === 'people')} onClick={() => setView('people')}>
            By person
          </button>
        </div>
      </div>
      <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
        Counts only: what each workspace holds and how many changes its members and agents make each week, never
        what any of it says. Pace compares the last {usage?.pace_weeks ?? 4} weeks with the{' '}
        {usage?.pace_weeks ?? 4} before; a change of a quarter either way is accelerating or slowing. A person who
        only reads is paced on the days they signed in.
      </p>
      {error ? (
        <div style={{ color: 'var(--danger-text)', fontSize: 13 }}>{error}</div>
      ) : !ready ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
      ) : view === 'workspaces' ? (
        <WorkspaceUsageList
          groups={groups}
          people={people}
          paceWeeks={usage.pace_weeks}
          busy={actions.busy}
          onChangePlan={onChangePlan}
          onShowPeople={(workspaceId) => {
            setFilter({ ...NO_FILTER, workspaceId });
            setView('people');
          }}
        />
      ) : (
        <PeopleUsageList people={people} workspaces={workspaces} paceWeeks={usage.pace_weeks} filter={filter} onFilter={setFilter} {...actions} />
      )}
    </section>
  );
};
