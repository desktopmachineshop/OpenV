import { PLANS, type AdminUsage, type AdminUser, type AdminWorkspace, type UsagePace, type UserUsage, type WorkspaceUsage } from '../../api/client';

// The usage view's model: the server's counts joined to the workspace and
// people listings, workspaces grouped by plan and then by kind, and the
// people list filtered by plan, workspace, pace and a search. Pure, so the
// grouping and the filters are tested without rendering.

/** Mirrors Go's usermetrics.PaceOf, for a group's summed series. */
export const PACE_THRESHOLD = 0.25;

export const paceOf = (weeks: number[], paceWeeks: number): UsagePace => {
  const n = weeks.length;
  const sum = (from: number, to: number) => weeks.slice(Math.max(0, from), Math.max(0, to)).reduce((a, b) => a + b, 0);
  const recent = sum(n - paceWeeks, n);
  const previous = sum(n - 2 * paceWeeks, n - paceWeeks);
  if (recent === 0 && previous === 0) return { recent, previous, change: 0, trend: 'idle' };
  if (recent === 0) return { recent, previous, change: -1, trend: 'dormant' };
  if (previous === 0) return { recent, previous, change: 0, trend: 'new' };
  const change = (recent - previous) / previous;
  const trend = change >= PACE_THRESHOLD ? 'accelerating' : change <= -PACE_THRESHOLD ? 'slowing' : 'steady';
  return { recent, previous, change, trend };
};

export const addSeries = (a: number[], b: number[]): number[] =>
  Array.from({ length: Math.max(a.length, b.length) }, (_, i) => (a[i] || 0) + (b[i] || 0));

const emptyUsage = (id: string, weeks: number): WorkspaceUsage => ({
  id,
  projects: 0,
  requirements: 0,
  test_cases: 0,
  other_artifacts: 0,
  links: 0,
  test_runs: 0,
  test_results: 0,
  comments: 0,
  members: 0,
  active_members_30d: 0,
  changes: Array(weeks).fill(0),
  pace: { recent: 0, previous: 0, change: 0, trend: 'idle' },
});

export interface WorkspaceRow {
  workspace: AdminWorkspace;
  usage: WorkspaceUsage;
}

export interface KindGroup {
  /** The workspace kind: company (a shared workspace) or personal. */
  kind: string;
  label: string;
  rows: WorkspaceRow[];
  changes: number[];
  pace: UsagePace;
}

export interface TierGroup {
  plan: string;
  label: string;
  kinds: KindGroup[];
  workspaces: number;
  members: number;
  changes: number[];
  pace: UsagePace;
}

const planOrder = (plan: string): number => {
  const i = PLANS.findIndex((p) => p.value === plan);
  return i < 0 ? PLANS.length : i;
};

export const planLabel = (plan: string): string => PLANS.find((p) => p.value === plan)?.label || plan;

const KIND_LABEL: Record<string, string> = { company: 'Company workspaces', personal: 'Personal workspaces' };
const KIND_ORDER = ['company', 'personal'];

/** Busiest first: recent changes, then everything it holds, then name. */
const byActivity = (a: WorkspaceRow, b: WorkspaceRow): number =>
  b.usage.pace.recent - a.usage.pace.recent ||
  b.usage.requirements + b.usage.test_cases - (a.usage.requirements + a.usage.test_cases) ||
  a.workspace.name.localeCompare(b.workspace.name);

/** Every workspace grouped by its plan (in plan order), then by kind. */
export const groupWorkspaces = (workspaces: AdminWorkspace[], usage: AdminUsage): TierGroup[] => {
  const byId = new Map(usage.workspaces.map((u) => [u.id, u]));
  const tiers = new Map<string, WorkspaceRow[]>();
  for (const ws of workspaces) {
    const row = { workspace: ws, usage: byId.get(ws.id) || emptyUsage(ws.id, usage.weeks.length) };
    tiers.set(ws.plan, [...(tiers.get(ws.plan) || []), row]);
  }
  const zero = Array(usage.weeks.length).fill(0);
  return [...tiers.entries()]
    .sort(([a], [b]) => planOrder(a) - planOrder(b) || a.localeCompare(b))
    .map(([plan, rows]) => {
      const kinds = [...new Set(rows.map((r) => r.workspace.type || 'company'))]
        .sort((a, b) => (KIND_ORDER.indexOf(a) + 1 || 99) - (KIND_ORDER.indexOf(b) + 1 || 99))
        .map((kind) => {
          const kindRows = rows.filter((r) => (r.workspace.type || 'company') === kind).sort(byActivity);
          const changes = kindRows.reduce((s, r) => addSeries(s, r.usage.changes), zero);
          return { kind, label: KIND_LABEL[kind] || kind, rows: kindRows, changes, pace: paceOf(changes, usage.pace_weeks) };
        });
      const changes = kinds.reduce((s, k) => addSeries(s, k.changes), zero);
      return {
        plan,
        label: planLabel(plan),
        kinds,
        workspaces: rows.length,
        members: rows.reduce((s, r) => s + r.workspace.members, 0),
        changes,
        pace: paceOf(changes, usage.pace_weeks),
      };
    });
};

export interface PersonRow {
  user: AdminUser;
  usage: UserUsage;
  /** The workspaces they belong to, with their role in each. */
  workspaces: { workspace: AdminWorkspace; role: string }[];
}

/** Everyone, joined to their usage and workspaces. */
export const joinPeople = (users: AdminUser[], workspaces: AdminWorkspace[], usage: AdminUsage): PersonRow[] => {
  const byId = new Map(usage.users.map((u) => [u.id, u]));
  const wsById = new Map(workspaces.map((w) => [w.id, w]));
  const zero = Array(usage.weeks.length).fill(0);
  return users.map((user) => {
    const u = byId.get(user.id) || {
      id: user.id,
      workspaces: [],
      changes: zero,
      active_days: zero,
      pace: { recent: 0, previous: 0, change: 0, trend: 'idle' as const },
    };
    const memberOf = u.workspaces
      .map((m) => ({ workspace: wsById.get(m.workspace_id), role: m.role }))
      .filter((m): m is { workspace: AdminWorkspace; role: string } => Boolean(m.workspace));
    return { user, usage: u, workspaces: memberOf };
  });
};

export interface PeopleFilter {
  plan: string;
  workspaceId: string;
  trend: string;
  search: string;
}

export const NO_FILTER: PeopleFilter = { plan: '', workspaceId: '', trend: '', search: '' };

export const filterPeople = (people: PersonRow[], f: PeopleFilter): PersonRow[] => {
  const q = f.search.trim().toLowerCase();
  return people
    .filter((p) => !f.plan || p.workspaces.some((m) => m.workspace.plan === f.plan))
    .filter((p) => !f.workspaceId || p.workspaces.some((m) => m.workspace.id === f.workspaceId))
    .filter((p) => !f.trend || p.usage.pace.trend === f.trend)
    .filter((p) => !q || p.user.name.toLowerCase().includes(q) || p.user.email.toLowerCase().includes(q))
    .sort(
      (a, b) =>
        b.usage.pace.recent - a.usage.pace.recent ||
        (b.usage.last_active || '').localeCompare(a.usage.last_active || '') ||
        (a.user.name || a.user.email).localeCompare(b.user.name || b.user.email)
    );
};

/** The people of one workspace, busiest first. */
export const membersOf = (people: PersonRow[], workspaceId: string): (PersonRow & { role: string })[] =>
  filterPeople(people, { ...NO_FILTER, workspaceId }).map((p) => ({
    ...p,
    role: p.workspaces.find((m) => m.workspace.id === workspaceId)?.role || '',
  }));
