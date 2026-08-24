import React, { useCallback, useEffect, useState } from 'react';
import { ProviderSetting, providerSettingsAPI } from '../../api/client';
import { ModelSelect } from '../agents/ModelSelect';
import { ProviderConnectCard } from '../agents/ProviderConnectCard';

const chipStyle = (bg: string, color = '#fff'): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 10,
  fontSize: 11,
  fontWeight: 600,
  color,
  background: bg,
});

interface OrgProvidersTabProps {
  isAdmin: boolean;
}

// AI provider settings for the active workspace. The X-Org-ID header scopes
// the provider-settings endpoints, so no org id is needed in the calls.
export const OrgProvidersTab: React.FC<OrgProvidersTabProps> = ({ isAdmin }) => {
  const [providers, setProviders] = useState<ProviderSetting[]>([]);
  const [providersLoading, setProvidersLoading] = useState(true);
  const [savingProvider, setSavingProvider] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const flash = (msg: string) => {
    setNotice(msg);
    window.setTimeout(() => setNotice(''), 2500);
  };

  const loadProviders = useCallback(async () => {
    setProvidersLoading(true);
    try {
      const res = await providerSettingsAPI.list();
      setProviders(res.data || []);
    } catch (err: any) {
      setError(`Failed to load provider settings: ${err.response?.data || err.message}`);
    } finally {
      setProvidersLoading(false);
    }
  }, []);

  useEffect(() => {
    loadProviders();
  }, [loadProviders]);

  const updateProviderLocal = (index: number, patch: Partial<ProviderSetting>) => {
    setProviders(providers.map((p, i) => (i === index ? { ...p, ...patch } : p)));
  };

  const handleSaveProvider = async (setting: ProviderSetting) => {
    setSavingProvider(setting.provider);
    setError('');
    try {
      const res = await providerSettingsAPI.upsert(setting);
      setProviders(providers.map((p) => (p.provider === setting.provider ? res.data : p)));
      flash(`${setting.provider} settings saved.`);
    } catch (err: any) {
      setError(`Failed to save provider settings: ${err.response?.data || err.message}`);
    } finally {
      setSavingProvider('');
    }
  };

  const detectionChip = (p: ProviderSetting) => {
    const ld = p.last_detected || {};
    if (!ld || Object.keys(ld).length === 0) {
      return <span style={chipStyle('#ecf0f1', '#7f8c8d')}>never detected</span>;
    }
    const parts: string[] = [];
    if (ld.installed) parts.push('installed');
    if (ld.logged_in) parts.push('logged in');
    if (ld.version) parts.push(`v${String(ld.version).replace(/^v/, '')}`);
    if (parts.length === 0) parts.push('detected');
    const ok = !!ld.installed || !!ld.logged_in;
    return <span style={chipStyle(ok ? '#27ae60' : '#f39c12')}>{parts.join(' · ')}</span>;
  };

  if (!isAdmin) {
    return (
      <div className="card">
        <h3>AI providers</h3>
        <p style={{ fontSize: 13, color: '#7f8c8d', marginBottom: 0 }}>
          Only workspace admins can view and change AI provider settings. Ask a workspace admin if a
          provider needs to be connected or reconfigured.
        </p>
      </div>
    );
  }

  return (
    <>
      {error && (
        <div
          style={{
            background: '#fdecea',
            border: '1px solid #e74c3c',
            color: '#c0392b',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {error}{' '}
          <button
            onClick={() => setError('')}
            style={{ background: 'none', border: 'none', color: '#c0392b', cursor: 'pointer', width: 'auto', padding: 0 }}
          >
            ✕
          </button>
        </div>
      )}
      {notice && (
        <div
          style={{
            background: '#eafaf1',
            border: '1px solid #27ae60',
            color: '#1e8449',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {notice}
        </div>
      )}

      {providersLoading ? (
        <div style={{ color: '#7f8c8d', fontSize: 13 }}>Loading provider settings…</div>
      ) : providers.length === 0 ? (
        <div className="card">
          <div style={{ color: '#95a5a6', fontSize: 13 }}>
            No AI providers configured on this server yet.
          </div>
        </div>
      ) : (
        providers.map((p, i) => (
          <div key={p.provider || p.id} className="card">
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
              <h3 style={{ marginBottom: 0, textTransform: 'capitalize' }}>{p.provider}</h3>
              {detectionChip(p)}
              <div style={{ flex: 1 }} />
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, fontWeight: 400, marginBottom: 0 }}>
                <input
                  type="checkbox"
                  style={{ width: 'auto' }}
                  checked={p.enabled}
                  onChange={(e) => updateProviderLocal(i, { enabled: e.target.checked })}
                />
                Enabled
              </label>
            </div>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
              <div style={{ width: 200 }}>
                <label style={{ fontSize: 12 }}>Auth mode</label>
                <select
                  value={p.auth_mode}
                  onChange={(e) => updateProviderLocal(i, { auth_mode: e.target.value as ProviderSetting['auth_mode'] })}
                  style={{ padding: '6px 8px', fontSize: 13 }}
                >
                  <option value="subscription-cli">subscription-cli</option>
                  <option value="api-key">api-key</option>
                </select>
              </div>
              <div style={{ flex: 1, minWidth: 180 }}>
                <label style={{ fontSize: 12 }}>Default model</label>
                <ModelSelect
                  value={p.default_model}
                  models={p.available_models || []}
                  onChange={(model) => updateProviderLocal(i, { default_model: model })}
                  emptyLabel="let the provider choose"
                  style={{ padding: '6px 8px', fontSize: 13 }}
                />
              </div>
              {p.auth_mode === 'api-key' && (
                <div style={{ flex: 1, minWidth: 180 }}>
                  <label style={{ fontSize: 12 }}>API key environment variable</label>
                  <input
                    value={p.api_key_env}
                    onChange={(e) => updateProviderLocal(i, { api_key_env: e.target.value })}
                    placeholder="e.g. ANTHROPIC_API_KEY"
                    style={{ padding: '6px 8px', fontSize: 13 }}
                  />
                </div>
              )}
              <button
                className="button"
                style={{ padding: '7px 16px' }}
                onClick={() => handleSaveProvider(providers[i])}
                disabled={savingProvider === p.provider}
              >
                {savingProvider === p.provider ? 'Saving…' : 'Save'}
              </button>
            </div>
            {p.auth_mode === 'api-key' && (
              <div style={{ fontSize: 12, color: '#7f8c8d', marginTop: 8 }}>
                The key itself is never stored here — set the environment variable on the server
                running OpenV.
              </div>
            )}
            {p.auth_mode === 'subscription-cli' && (
              <ProviderConnectCard
                provider={p.provider}
                loggedIn={Boolean((p.last_detected || {})['logged_in'])}
                onComplete={loadProviders}
              />
            )}
          </div>
        ))
      )}

      <div className="card" style={{ background: '#eaf4fc', border: '1px solid #3498db' }}>
        <h3 style={{ color: '#2c3e50' }}>Agent definitions</h3>
        <p style={{ fontSize: 13, color: '#2c3e50', marginBottom: 0 }}>
          Agents are defined as markdown files with frontmatter (model, tools, write mode) stored on
          the server. Edit them on a project's Agents page — changes to the files on disk are picked
          up with a sync.
        </p>
      </div>
    </>
  );
};
