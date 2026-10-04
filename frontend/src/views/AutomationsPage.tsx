import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
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
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { EVENT_TYPES } from './AutomationsPageEvents';
import {
  AutomationScope,
  automationPayload,
  coversProject,
  CRON_PRESETS,
  crewsForScope,
  emptyForm,
  FormState,
  kindColor,
  kindLabel,
  toForm,
} from './AutomationsPageForm';
import { ScopeField, WorkspaceScopePill } from './AutomationsPageScope';
import { AutomationRunDetail } from './AutomationsPageRun';

export const AutomationsPage: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  // A phone lists name, kind, the switch and the actions; the target, the
  // schedule and the run times are in the editor a tap away.
  const { isPhone } = useViewport();
  const columns = isPhone
    ? ['Name', 'Kind', 'Enabled', '']
    : ['Name', 'Kind', 'Target', 'Schedule / Event', 'Enabled', 'Last run', 'Next run', ''];
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  // A workspace admin writes the whole workspace's automations and, with the
  // feature on, chooses an automation's scope; anyone else sees them listed.
  const isAdmin = useAppStore((s) =>
    Boolean(s.currentUser?.is_admin || s.orgs.find((o) => o.id === s.activeOrgId)?.role === 'admin')
  );
  const canChooseScope = useFeature('workspace-automations') && isAdmin;
  const navigate = useNavigate();
  const confirm = useConfirm();
  // The run Run now started with no project, shown beside the list (?run=).
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedRunId = searchParams.get('run');
  const selectRun = (runId: string | null) =>
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (runId) next.set('run', runId);
      else next.delete('run');
      return next;
    });

  const [automations, setAutomations] = useState<Automation[]>([]);
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [crews, setCrews] = useState<Crew[]>([]);
  const [error, setError] = useState('');
  const [form, setForm] = useState<FormState | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(() => {
    // The workspace's automations, of which the page lists this project's
    // and the whole workspace's.
    automationsAPI
      .list()
      .then((res) => setAutomations((res.data || []).filter((a) => coversProject(a, projectId))))
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
    const payload = automationPayload(form, projectId);
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
      const run = (await automationsAPI.runNow(a.id)).data;
      // A whole-workspace automation's run has no project, which no
      // project's Runs page lists (#379 bug 141): it opens here instead.
      if (run.project_id) navigate(`/projects/${run.project_id}/agent-runs?run=${run.id}`);
      else selectRun(run.id);
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

  // A whole-workspace automation is its workspace admins' to change; the
  // rest of the workspace sees it listed.
  const writable = (a: Automation): boolean => Boolean(a.project_id) || isAdmin;

  // A whole-workspace automation cannot keep a crew made in one project.
  const setScope = (scope: AutomationScope) =>
    setForm((f) => {
      if (!f) return f;
      const keeps = crewsForScope(crews, scope).some((c) => c.id === f.team_id);
      return { ...f, scope, team_id: keeps ? f.team_id : '' };
    });

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

      <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start', marginBottom: 20 }}>
      <div className="table-container" style={{ flex: 1, minWidth: 0 }}>
        <div className="table-scroll">
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr>
              {columns.map(
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
                  {writable(a) ? (
                    <span
                      style={{ color: 'var(--accent)', cursor: 'pointer', fontWeight: 600 }}
                      onClick={() => setForm(toForm(a))}
                    >
                      {a.name}
                    </span>
                  ) : (
                    <span style={{ color: 'var(--text)', fontWeight: 600 }}>{a.name}</span>
                  )}
                  {!a.project_id && <WorkspaceScopePill />}
                </td>
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)' }}>
                  <span
                    style={{
                      display: 'inline-block',
                      padding: '2px 10px',
                      borderRadius: 12,
                      background: kindColor(a.kind),
                      color: '#fff',
                      fontSize: 12,
                      fontWeight: 600,
                    }}
                  >
                    {kindLabel(a.kind)}
                  </span>
                </td>
                {!isPhone && (
                  <>
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
                  </>
                )}
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
                      disabled={!writable(a)}
                      onChange={() => toggleEnabled(a)}
                      style={{ width: 'auto' }}
                    />
                    <span style={{ fontSize: 12, color: a.enabled ? 'var(--success)' : 'var(--text-muted)' }}>
                      {a.enabled ? 'on' : 'off'}
                    </span>
                  </label>
                </td>
                {!isPhone && (
                  <>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                      {a.last_run_at ? new Date(a.last_run_at).toLocaleString() : '—'}
                    </td>
                    <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', color: 'var(--text-body)' }}>
                      {a.next_run_at ? new Date(a.next_run_at).toLocaleString() : '—'}
                    </td>
                  </>
                )}
                <td style={{ padding: '9px 12px', borderBottom: '1px solid var(--neutral-soft)', whiteSpace: 'nowrap' }}>
                  {writable(a) && (
                  <div style={{ display: 'flex', gap: 6, flexDirection: isPhone ? 'column' : 'row' }}>
                  <button
                    className="button"
                    style={{ padding: '4px 10px', fontSize: 12, minHeight: 36 }}
                    onClick={() => runNow(a)}
                  >
                    Run now
                  </button>
                  <button
                    style={{
                      padding: '4px 10px',
                      fontSize: 12,
                      minHeight: 36,
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
                  </div>
                  )}
                </td>
              </tr>
            ))}
            {automations.length === 0 && (
              <tr>
                <td colSpan={columns.length} style={{ padding: 16, color: 'var(--text-muted)', background: 'var(--surface)' }}>
                  No automations yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        </div>
      </div>
      {selectedRunId && (
        <AutomationRunDetail runId={selectedRunId} onSelectRun={selectRun} onClose={() => selectRun(null)} />
      )}
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

          <ScopeField scope={form.scope} canChoose={canChooseScope} onChange={setScope} />

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
                {crewsForScope(crews, form.scope).map((c) => (
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
