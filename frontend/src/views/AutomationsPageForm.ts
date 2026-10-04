import type { Automation, Crew } from '../api/client';
import { EVENT_TYPES } from './AutomationsPageEvents';

// The Automations page's form: its state, built from an automation and
// turned back into the body the API takes, and the copy for an automation's
// kind and scope.

export const CRON_PRESETS: { label: string; value: string }[] = [
  { label: 'Hourly', value: '0 * * * *' },
  { label: 'Daily at 9am', value: '0 9 * * *' },
  { label: 'Every 15 minutes', value: '*/15 * * * *' },
];

// Display copy for automation kinds. The domain constant is 'scheduled'
// (DB index + backend code) — never 'cron'; the cron expression is only the
// schedule field of a Scheduled automation.
const KIND_LABELS: Record<string, string> = {
  manual: 'Manual',
  scheduled: 'Scheduled',
  triggered: 'Triggered',
};

export const kindLabel = (kind: string): string => KIND_LABELS[kind] || kind;

export const kindColor = (kind: string): string => {
  switch (kind) {
    case 'scheduled':
      return 'var(--accent)';
    case 'triggered':
      return 'var(--warning)';
    default:
      return 'var(--neutral)';
  }
};

/**
 * What an automation covers: the project whose page it is made on, or the
 * whole workspace (stored with no project), which fires on events in every
 * project of the workspace and on the workspace's own membership events.
 */
export type AutomationScope = 'project' | 'workspace';

export const scopeOf = (a: Automation): AutomationScope => (a.project_id ? 'project' : 'workspace');

/** The automations a project's page lists: the project's own and the whole workspace's. */
export const coversProject = (a: Automation, projectId: string | undefined): boolean =>
  !a.project_id || a.project_id === projectId;

/**
 * The crews a scope's automation may run. A whole-workspace automation runs
 * in the project of each event that fires it, so only a crew pinned to no
 * project can serve it; a project's automation may also have a crew pinned
 * to that project, which the list for a project already holds.
 */
export const crewsForScope = (crews: Crew[], scope: AutomationScope): Crew[] =>
  scope === 'workspace' ? crews.filter((c) => !c.project_id) : crews;

interface FilterRow {
  key: string;
  value: string;
}

export interface FormState {
  id: string | null;
  name: string;
  scope: AutomationScope;
  /** The scope the automation is saved with; null for a new one. */
  savedScope: AutomationScope | null;
  targetKind: 'agent' | 'team';
  agent_id: string;
  team_id: string;
  kind: 'manual' | 'scheduled' | 'triggered';
  cron_expr: string;
  event_type: string;
  filters: FilterRow[];
  cooldown_seconds: number;
  max_runs_per_hour: number;
  prompt_template: string;
}

export const emptyForm = (): FormState => ({
  id: null,
  name: '',
  scope: 'project',
  savedScope: null,
  targetKind: 'agent',
  agent_id: '',
  team_id: '',
  kind: 'manual',
  cron_expr: '0 9 * * *',
  event_type: EVENT_TYPES[0],
  filters: [],
  cooldown_seconds: 0,
  max_runs_per_hour: 0,
  prompt_template: '',
});

export const toForm = (a: Automation): FormState => ({
  id: a.id,
  name: a.name,
  scope: scopeOf(a),
  savedScope: scopeOf(a),
  targetKind: a.team_id ? 'team' : 'agent',
  agent_id: a.agent_id || '',
  team_id: a.team_id || '',
  kind: a.kind,
  cron_expr: a.cron_expr || '0 9 * * *',
  event_type: a.event_type || EVENT_TYPES[0],
  filters: Object.entries(a.event_filter || {}).map(([key, value]) => ({
    key,
    value: String(value),
  })),
  cooldown_seconds: a.cooldown_seconds || 0,
  max_runs_per_hour: a.max_runs_per_hour || 0,
  prompt_template: a.prompt_template || '',
});

/**
 * The body that saves the form. A new automation names its scope: the
 * page's project, or none for the whole workspace. An update names a scope
 * only to move the automation, "" being the whole workspace, and clears the
 * target it no longer has with "" too, since the API leaves a field it gets
 * as null as it was.
 */
export const automationPayload = (form: FormState, projectId: string | undefined): Partial<Automation> => {
  const event_filter: Record<string, any> = {};
  form.filters.forEach((f) => {
    if (f.key.trim()) event_filter[f.key.trim()] = f.value;
  });
  const cleared = form.id ? '' : null;
  const payload: Partial<Automation> = {
    name: form.name.trim(),
    agent_id: form.targetKind === 'agent' ? form.agent_id || null : cleared,
    team_id: form.targetKind === 'team' ? form.team_id || null : cleared,
    kind: form.kind,
    prompt_template: form.prompt_template,
    cron_expr: form.kind === 'scheduled' ? form.cron_expr : '',
    event_type: form.kind === 'triggered' ? form.event_type : '',
    event_filter: form.kind === 'triggered' ? event_filter : {},
    cooldown_seconds: form.kind === 'triggered' ? Number(form.cooldown_seconds) || 0 : 0,
    max_runs_per_hour: form.kind === 'triggered' ? Number(form.max_runs_per_hour) || 0 : 0,
  };
  if (!form.id) {
    payload.project_id = form.scope === 'workspace' ? null : projectId || null;
  } else if (form.scope !== form.savedScope) {
    payload.project_id = form.scope === 'workspace' ? '' : projectId || null;
  }
  return payload;
};
