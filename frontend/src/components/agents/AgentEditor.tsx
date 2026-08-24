import React, { useCallback, useEffect, useState } from 'react';
import { AgentDef, agentsAPI, ProviderSetting, providerSettingsAPI } from '../../api/client';
import { ModelSelect } from './ModelSelect';

// Used until the provider settings load (or if they fail to) — the server
// returns the same list, in the same order, with each provider's models.
const FALLBACK_PROVIDERS = [
  'claude-code',
  'codex-cli',
  'gemini-cli',
  'anthropic-api',
  'openai-api',
  'google-api',
];

interface AgentEditorProps {
  agent: AgentDef | null; // null = creating a new agent
  onSaved: () => void;
  onCancel: () => void;
}

interface FormState {
  slug: string;
  name: string;
  description: string;
  provider: string;
  model: string;
  write_mode: 'proposal' | 'direct';
  repo_access: boolean;
  max_turns: number;
  timeout_seconds: number;
  allowed_tools: string;
  system_prompt: string;
}

const toForm = (agent: AgentDef | null): FormState => ({
  slug: agent?.slug || '',
  name: agent?.name || '',
  description: agent?.description || '',
  provider: agent?.provider || 'claude-code',
  model: agent?.model || '',
  write_mode: agent?.write_mode || 'proposal',
  repo_access: agent?.repo_access || false,
  max_turns: agent?.max_turns ?? 30,
  timeout_seconds: agent?.timeout_seconds ?? 600,
  allowed_tools: (agent?.allowed_tools || []).join(', '),
  system_prompt: agent?.system_prompt || '',
});

export const AgentEditor: React.FC<AgentEditorProps> = ({ agent, onSaved, onCancel }) => {
  const isNew = !agent;
  const [mode, setMode] = useState<'form' | 'raw'>('form');
  const [form, setForm] = useState<FormState>(toForm(agent));
  const [raw, setRaw] = useState('');
  const [rawLoaded, setRawLoaded] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [providerSettings, setProviderSettings] = useState<ProviderSetting[]>([]);

  // Provider settings carry each provider's available models; a failure here
  // is not worth an error banner — the form falls back to free-text entry.
  useEffect(() => {
    providerSettingsAPI
      .list()
      .then((res) => setProviderSettings(res.data || []))
      .catch(() => setProviderSettings([]));
  }, []);

  useEffect(() => {
    setForm(toForm(agent));
    setMode('form');
    setRaw('');
    setRawLoaded(false);
    setError('');
  }, [agent]);

  const loadRaw = useCallback(() => {
    if (!agent) return;
    agentsAPI
      .raw(agent.slug)
      .then((res) => {
        setRaw(res.data.content);
        setRawLoaded(true);
      })
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load raw markdown')
      );
  }, [agent]);

  useEffect(() => {
    if (mode === 'raw' && !rawLoaded) loadRaw();
  }, [mode, rawLoaded, loadRaw]);

  const set = (update: Partial<FormState>) => setForm((f) => ({ ...f, ...update }));

  const saveForm = async () => {
    setSaving(true);
    setError('');
    const payload: Partial<AgentDef> = {
      slug: form.slug.trim(),
      name: form.name.trim(),
      description: form.description,
      provider: form.provider,
      model: form.model,
      write_mode: form.write_mode,
      repo_access: form.repo_access,
      max_turns: Number(form.max_turns) || 0,
      timeout_seconds: Number(form.timeout_seconds) || 0,
      allowed_tools: form.allowed_tools
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      system_prompt: form.system_prompt,
    };
    try {
      if (isNew) {
        await agentsAPI.create(payload);
      } else {
        await agentsAPI.update(agent!.slug, payload);
      }
      onSaved();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save agent');
    } finally {
      setSaving(false);
    }
  };

  const saveRawContent = async () => {
    if (!agent) return;
    setSaving(true);
    setError('');
    try {
      await agentsAPI.saveRaw(agent.slug, raw);
      onSaved();
    } catch (err: any) {
      // Show API validation errors verbatim.
      const data = err.response?.data;
      setError(
        typeof data === 'string' ? data : data?.error || JSON.stringify(data) || err.message
      );
    } finally {
      setSaving(false);
    }
  };

  const activeProvider = providerSettings.find((p) => p.provider === form.provider);
  const providerNames = providerSettings.length
    ? providerSettings.map((p) => p.provider)
    : FALLBACK_PROVIDERS;

  return (
    <div className="card" style={{ marginBottom: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
        <h3 style={{ margin: 0, flex: 1 }}>{isNew ? 'New agent' : `Edit: ${agent?.name}`}</h3>
        {!isNew && (
          <div style={{ display: 'inline-flex', border: '1px solid #ddd', borderRadius: 4, overflow: 'hidden' }}>
            {(['form', 'raw'] as const).map((m) => (
              <button
                key={m}
                onClick={() => setMode(m)}
                style={{
                  padding: '5px 12px',
                  border: 'none',
                  cursor: 'pointer',
                  fontSize: 12,
                  background: mode === m ? '#3498db' : '#fff',
                  color: mode === m ? '#fff' : '#2c3e50',
                }}
              >
                {m === 'form' ? 'Form' : 'Raw markdown'}
              </button>
            ))}
          </div>
        )}
        <button
          onClick={onCancel}
          style={{ background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: '#7f8c8d' }}
          title="Close editor"
        >
          ×
        </button>
      </div>

      {error && (
        <pre
          style={{
            background: '#fdedec',
            border: '1px solid #e74c3c',
            color: '#c0392b',
            borderRadius: 4,
            padding: '8px 12px',
            marginBottom: 12,
            fontSize: 12,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {error}
        </pre>
      )}

      {mode === 'form' ? (
        <>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0 14px' }}>
            <div className="form-group">
              <label>Slug</label>
              <input
                value={form.slug}
                disabled={!isNew}
                onChange={(e) => set({ slug: e.target.value })}
                placeholder="my-agent"
                style={{ fontSize: 13, background: isNew ? undefined : '#f4f6f6' }}
              />
            </div>
            <div className="form-group">
              <label>Name</label>
              <input
                value={form.name}
                onChange={(e) => set({ name: e.target.value })}
                style={{ fontSize: 13 }}
              />
            </div>
            <div className="form-group" style={{ gridColumn: '1 / span 2' }}>
              <label>Description</label>
              <input
                value={form.description}
                onChange={(e) => set({ description: e.target.value })}
                style={{ fontSize: 13 }}
              />
            </div>
            <div className="form-group">
              <label>Provider</label>
              <select
                value={form.provider}
                onChange={(e) => set({ provider: e.target.value })}
                style={{ fontSize: 13 }}
              >
                {providerNames.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label>Model</label>
              <ModelSelect
                value={form.model}
                models={activeProvider?.available_models || []}
                onChange={(model) => set({ model })}
                emptyLabel={
                  activeProvider?.default_model
                    ? `provider default (${activeProvider.default_model})`
                    : 'provider default'
                }
                style={{ fontSize: 13 }}
              />
            </div>
            <div className="form-group">
              <label>Write mode</label>
              <select
                value={form.write_mode}
                onChange={(e) => set({ write_mode: e.target.value as 'proposal' | 'direct' })}
                style={{ fontSize: 13 }}
              >
                <option value="proposal">proposal (changes need approval)</option>
                <option value="direct">direct (writes immediately)</option>
              </select>
            </div>
            <div className="form-group" style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 24 }}>
              <input
                id="repo-access"
                type="checkbox"
                checked={form.repo_access}
                onChange={(e) => set({ repo_access: e.target.checked })}
                style={{ width: 'auto' }}
              />
              <label htmlFor="repo-access" style={{ margin: 0, fontWeight: 400 }}>
                Repository access
              </label>
            </div>
            <div className="form-group">
              <label>Max turns</label>
              <input
                type="number"
                value={form.max_turns}
                onChange={(e) => set({ max_turns: Number(e.target.value) })}
                style={{ fontSize: 13 }}
              />
            </div>
            <div className="form-group">
              <label>Timeout (seconds)</label>
              <input
                type="number"
                value={form.timeout_seconds}
                onChange={(e) => set({ timeout_seconds: Number(e.target.value) })}
                style={{ fontSize: 13 }}
              />
            </div>
            <div className="form-group" style={{ gridColumn: '1 / span 2' }}>
              <label>Allowed tools (comma-separated)</label>
              <input
                value={form.allowed_tools}
                onChange={(e) => set({ allowed_tools: e.target.value })}
                placeholder="e.g. openv_search, openv_read, openv_propose"
                style={{ fontSize: 13 }}
              />
            </div>
          </div>
          <div className="form-group">
            <label>System prompt</label>
            <textarea
              value={form.system_prompt}
              onChange={(e) => set({ system_prompt: e.target.value })}
              style={{ minHeight: 220, fontFamily: 'monospace', fontSize: 12.5 }}
            />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              className="button"
              onClick={saveForm}
              disabled={saving || !form.slug.trim() || !form.name.trim()}
            >
              {saving ? 'Saving…' : isNew ? 'Create agent' : 'Save changes'}
            </button>
            <button className="button-secondary" onClick={onCancel}>
              Cancel
            </button>
          </div>
        </>
      ) : (
        <>
          <textarea
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            spellCheck={false}
            style={{
              minHeight: 420,
              fontFamily: 'monospace',
              fontSize: 12.5,
              width: '100%',
              marginBottom: 12,
            }}
          />
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="button" onClick={saveRawContent} disabled={saving || !rawLoaded}>
              {saving ? 'Saving…' : 'Save raw file'}
            </button>
            <button className="button-secondary" onClick={loadRaw}>
              Reload from server
            </button>
          </div>
        </>
      )}
    </div>
  );
};
