import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  AgentDef,
  agentsAPI,
  Automation,
  automationsAPI,
  Crew,
  crewsAPI,
} from '../api/client';
import { useAppStore } from '../state/store';
import { ErrorBanner, Modal, SegmentedControl, useConfirm } from '../components/ui';

const EVENT_TYPES = [
  'artifact.created',
  'artifact.updated',
  'artifact.deleted',
  'link.created',
  'link.deleted',
  'baseline.captured',
  'testrun.recorded',
  'workitem.moved',
  'workitem.created',
  'agentrun.finished',
];

const CRON_PRESETS: { label: string; value: string }[] = [
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

const kindLabel = (kind: string): string => KIND_LABELS[kind] || kind;

const kindColor = (kind: string): string => {
  switch (kind) {
    case 'scheduled':
      return 'var(--accent)';
    case 'triggered':
      return 'var(--warning)';
    default:
      return 'var(--neutral)';
  }
};

interface FilterRow {
  key: string;
  value: string;
}

interface FormState {
  id: string | null;
  name: string;
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

const emptyForm = (): FormState => ({
  id: null,
  name: '',
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

const toForm = (a: Automation): FormState => ({
  id: a.id,
  name: a.name,
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

export const AutomationsPage: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const navigate = useNavigate();
  const confirm = useConfirm();

  const [automations, setAutomations] = useState<Automation[]>([]);
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [crews, setCrews] = useState<Crew[]>([]);
  const [error, setError] = useState('');
  const [form, setForm] = useState<FormState | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(() => {
    automationsAPI
      .list(projectId || undefined)
      .then((res) => setAutomations(res.data || []))
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load automations')
      );
    // activeOrgId: the list is scoped by the X-Org-ID header the API client
    // injects, so refetch when the active workspace changes (e.g.
    // ProjectLayout's cross-org deep-link sync, issue #99). The effect below
    // also re-runs via this callback, refreshing agents and crews too.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, activeOrgId]);

  useEffect(() => {
    load();
    agentsAPI.list().then((res) => setAgents(res.data || [])).catch(() => setAgents([]));
    crewsAPI.list(projectId || undefined).then((res) => setCrews(res.data || [])).catch(() => setCrews([]));
  }, [load, projectId]);

  const agentName = useMemo(() => {
    const m: Record<string, string> = {};
    agents.forEach((a) => {
      m[a.id] = a.name;
    });
    return (id: string) => m[id] || id;
  }, [agents]);

  const crewName = useMemo(() => {
    const m: Record<string, string> = {};
    crews.forEach((c) => {
      m[c.id] = c.name;
    });
    return (id: string) => m[id] || id;
  }, [crews]);

  const set = (update: Partial<FormState>) =>
    setForm((f) => (f ? { ...f, ...update } : f));

  const save = async () => {
    if (!form || !form.name.trim()) return;
    setSaving(true);
    setError('');
    const event_filter: Record<string, any> = {};
    form.filters.forEach((f) => {
      if (f.key.trim()) event_filter[f.key.trim()] = f.value;
    });
    const payload: Partial<Automation> = {
      name: form.name.trim(),
      project_id: projectId || null,
      agent_id: form.targetKind === 'agent' ? form.agent_id || null : null,
      team_id: form.targetKind === 'team' ? form.team_id || null : null,
      kind: form.kind,
      prompt_template: form.prompt_template,
      cron_expr: form.kind === 'scheduled' ? form.cron_expr : '',
      event_type: form.kind === 'triggered' ? form.event_type : '',
      event_filter: form.kind === 'triggered' ? event_filter : {},
      cooldown_seconds: form.kind === 'triggered' ? Number(form.cooldown_seconds) || 0 : 0,
      max_runs_per_hour: form.kind === 'triggered' ? Number(form.max_runs_per_hour) || 0 : 0,
    };
    try {
      if (form.id) {
        await automationsAPI.update(form.id, payload);
      } else {
        await automationsAPI.create({ ...payload, enabled: true });
      }
      setForm(null);
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save automation');
    } finally {
      setSaving(false);
    }
  };

  const toggleEnabled = async (a: Automation) => {
    try {
      await automationsAPI.update(a.id, { enabled: !a.enabled });
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to update automation');
    }
  };

  const runNow = async (a: Automation) => {
    try {
      const res = await automationsAPI.runNow(a.id);
      navigate(`/projects/${projectId}/agent-runs?run=${res.data.id}`);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to run automation');
    }
  };

  const remove = async (a: Automation) => {
    const ok = await confirm({
      title: 'Delete automation',
      message: `Delete automation "${a.name}"?`,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      await automationsAPI.remove(a.id);
      if (form?.id === a.id) setForm(null);
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to delete automation');
    }
  };

  const targetLabel = (a: Automation): string => {
    if (a.team_id) return `👥 ${crewName(a.team_id)}`;
    if (a.agent_id) return `🤖 ${agentName(a.agent_id)}`;
    return '—';
  };

  const scheduleLabel = (a: Automation): string => {
    if (a.kind === 'scheduled') return a.cron_expr || '—';
    if (a.kind === 'triggered') return a.event_type || '—';
    return 'manual';
  };

  return (
    <div style={{ padding: 24 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
        <h2 style={{ color: 'var(--text)', margin: 0, flex: 1 }}>Automations</h2>
        <button className="button" onClick={() => setForm(emptyForm())}>
          New automation
        </button>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} />

      <div className="table-container" style={{ marginBottom: 20 }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr>
              {['Name', 'Kind', 'Target', 'Schedule / Event', 'Enabled', 'Last run', 'Next run', ''].map(
                (h, i) => (
                  <th
                    key={i}
                    style={{
                      textAlign: 'left',
                      borderBottom: '2px solid var(--neutral-soft)',
                      padding: '10px 12px',
                      color: 'var(--text-muted)',
                      fontWeight: 600,
                      background: 'var(--surface)',
                    }}
                  >
                    {h}
                  </th>
                )
              )}
            </tr>
          </thead>
          <tbody>
            {automations.map((a) => (
              <tr key={a.id} style={{ background: 'var(--surface)' }}>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' }}>
                  <span
                    style={{ color: 'var(--accent)', cursor: 'pointer', fontWeight: 600 }}
                    onClick={() => setForm(toForm(a))}
                  >
                    {a.name}
                  </span>
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' }}>
                  <span
                    style={{
                      display: 'inline-block',
                      padding: '2px 10px',
                      borderRadius: 12,
                      background: kindColor(a.kind),
                      color: '#fff',
                      fontSize: 11.5,
                      fontWeight: 600,
                    }}
                  >
                    {kindLabel(a.kind)}
                  </span>
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text)' }}>
                  {targetLabel(a)}
                </td>
                <td
                  style={{
                    padding: '9px 12px',
                    borderBottom: '1px solid var(--neutral-soft)',
                    color: 'var(--text-body)',
                    fontFamily: a.kind === 'scheduled' ? 'monospace' : undefined,
                  }}
                >
                  {scheduleLabel(a)}
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' }}>
                  <label
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 6,
                      cursor: 'pointer',
                      margin: 0,
                      fontWeight: 400,
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={a.enabled}
                      onChange={() => toggleEnabled(a)}
                      style={{ width: 'auto' }}
                    />
                    <span style={{ fontSize: 12, color: a.enabled ? 'var(--success)' : 'var(--text-muted)' }}>
                      {a.enabled ? 'on' : 'off'}
                    </span>
                  </label>
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                  {a.last_run_at ? new Date(a.last_run_at).toLocaleString() : '—'}
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                  {a.next_run_at ? new Date(a.next_run_at).toLocaleString() : '—'}
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', whiteSpace: 'nowrap' }}>
                  <button
                    className="button"
                    style={{ padding: '4px 10px', fontSize: 12, marginRight: 6 }}
                    onClick={() => runNow(a)}
                  >
                    Run now
                  </button>
                  <button
                    style={{
                      padding: '4px 10px',
                      fontSize: 12,
                      background: 'var(--danger)',
                      color: '#fff',
                      border: 'none',
                      borderRadius: 4,
                      cursor: 'pointer',
                    }}
                    onClick={() => remove(a)}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {automations.length === 0 && (
              <tr>
                <td colSpan={8} style={{ padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>
                  No automations yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {form && (
        <Modal
          title={form.id ? 'Edit automation' : 'New automation'}
          width={720}
          onClose={() => setForm(null)}
        >
          <div className="form-group">
            <label>Name</label>
            <input value={form.name} onChange={(e) => set({ name: e.target.value })} />
          </div>

          <div className="form-group">
            <label>Target</label>
            <div style={{ marginBottom: 8 }}>
              <SegmentedControl
                aria-label="Automation target"
                options={[
                  { value: 'agent', label: 'Agent' },
                  { value: 'team', label: 'Crew' },
                ]}
                value={form.targetKind}
                onChange={(targetKind) => set({ targetKind })}
              />
            </div>
            {form.targetKind === 'agent' ? (
              <select value={form.agent_id} onChange={(e) => set({ agent_id: e.target.value })}>
                <option value="">Choose agent…</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            ) : (
              <select value={form.team_id} onChange={(e) => set({ team_id: e.target.value })}>
                <option value="">Choose crew…</option>
                {crews.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            )}
          </div>

          <div className="form-group">
            <label>Kind</label>
            <SegmentedControl
              aria-label="Automation kind"
              options={(['manual', 'scheduled', 'triggered'] as const).map((k) => ({
                value: k,
                label: kindLabel(k),
              }))}
              value={form.kind}
              onChange={(kind) => set({ kind })}
            />
          </div>

          {form.kind === 'scheduled' && (
            <div className="form-group">
              <label>Cron expression</label>
              <div style={{ display: 'flex', gap: 8 }}>
                <input
                  value={form.cron_expr}
                  onChange={(e) => set({ cron_expr: e.target.value })}
                  style={{ fontFamily: 'monospace' }}
                />
                <select
                  value=""
                  onChange={(e) => {
                    if (e.target.value) set({ cron_expr: e.target.value });
                  }}
                  style={{ width: 200 }}
                >
                  <option value="">Presets…</option>
                  {CRON_PRESETS.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
                Standard 5-field cron: minute hour day-of-month month day-of-week (e.g. "0 9 * * *"
                = daily at 9am).
              </div>
            </div>
          )}

          {form.kind === 'triggered' && (
            <>
              <div className="form-group">
                <label>Event type</label>
                <select value={form.event_type} onChange={(e) => set({ event_type: e.target.value })}>
                  {EVENT_TYPES.map((et) => (
                    <option key={et} value={et}>
                      {et}
                    </option>
                  ))}
                </select>
              </div>
              <div className="form-group">
                <label>Event filter (optional key/value matches)</label>
                {form.filters.map((f, idx) => (
                  <div key={idx} style={{ display: 'flex', gap: 8, marginBottom: 6 }}>
                    <input
                      value={f.key}
                      placeholder="key (e.g. artifact_type)"
                      onChange={(e) =>
                        set({
                          filters: form.filters.map((row, i) =>
                            i === idx ? { ...row, key: e.target.value } : row
                          ),
                        })
                      }
                    />
                    <input
                      value={f.value}
                      placeholder="value"
                      onChange={(e) =>
                        set({
                          filters: form.filters.map((row, i) =>
                            i === idx ? { ...row, value: e.target.value } : row
                          ),
                        })
                      }
                    />
                    <button
                      className="button-secondary"
                      style={{ padding: '6px 12px', fontSize: 13 }}
                      onClick={() => set({ filters: form.filters.filter((_, i) => i !== idx) })}
                    >
                      ×
                    </button>
                  </div>
                ))}
                <button
                  className="button-secondary"
                  style={{ padding: '6px 12px', fontSize: 13 }}
                  onClick={() => set({ filters: [...form.filters, { key: '', value: '' }] })}
                >
                  + Add filter
                </button>
              </div>
              <div style={{ display: 'flex', gap: 14 }}>
                <div className="form-group" style={{ flex: 1 }}>
                  <label>Cooldown (seconds)</label>
                  <input
                    type="number"
                    value={form.cooldown_seconds}
                    onChange={(e) => set({ cooldown_seconds: Number(e.target.value) })}
                  />
                </div>
                <div className="form-group" style={{ flex: 1 }}>
                  <label>Max runs per hour</label>
                  <input
                    type="number"
                    value={form.max_runs_per_hour}
                    onChange={(e) => set({ max_runs_per_hour: Number(e.target.value) })}
                  />
                </div>
              </div>
            </>
          )}

          <div className="form-group">
            <label>Prompt template</label>
            <textarea
              value={form.prompt_template}
              onChange={(e) => set({ prompt_template: e.target.value })}
              style={{ minHeight: 110, fontFamily: 'monospace', fontSize: 12.5 }}
              placeholder={'e.g. A {{event.type}} event occurred on {{event.entity_id}} — review it.'}
            />
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
              Placeholders: {'{{event.type}}'}, {'{{event.entity_id}}'}, {'{{project.id}}'},{' '}
              {'{{automation.name}}'}. Keep prompts lean — the agent fetches project context itself
              via its OpenV tools.
            </div>
          </div>

          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <button className="button-secondary" onClick={() => setForm(null)}>
              Cancel
            </button>
            <button className="button" onClick={save} disabled={saving || !form.name.trim()}>
              {saving ? 'Saving…' : form.id ? 'Save changes' : 'Create automation'}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};
