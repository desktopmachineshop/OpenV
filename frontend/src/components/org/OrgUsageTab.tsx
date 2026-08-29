import React, { useCallback, useEffect, useState } from 'react';
import { Org, OrgUsageSummary, orgsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner } from '../ui';

interface OrgUsageTabProps {
  org: Org;
}

const WINDOWS = [7, 30, 90];

const fmtTokens = (n: number): string => {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
};

const fmtCost = (n: number): string => `$${n.toFixed(2)}`;

const thStyle: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  fontWeight: 600,
  padding: '6px 10px',
  borderBottom: '1px solid var(--border)',
};

const tdStyle: React.CSSProperties = {
  fontSize: 13,
  color: 'var(--text)',
  padding: '6px 10px',
  borderBottom: '1px solid var(--neutral-soft)',
};

const numStyle: React.CSSProperties = { ...tdStyle, textAlign: 'right', fontVariantNumeric: 'tabular-nums' };

// TokenBar renders a proportional inline bar (no chart lib): value against
// the window's max, so rows compare at a glance.
const TokenBar: React.FC<{ value: number; max: number }> = ({ value, max }) => (
  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
    <div style={{ flex: 1, background: 'var(--neutral-soft)', borderRadius: 3, height: 8, overflow: 'hidden' }}>
      <div
        style={{
          width: max > 0 ? `${Math.max(2, (value / max) * 100)}%` : 0,
          background: 'var(--accent)',
          height: '100%',
          borderRadius: 3,
        }}
      />
    </div>
    <span style={{ fontSize: 12, color: 'var(--text-muted)', minWidth: 48, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
      {fmtTokens(value)}
    </span>
  </div>
);

// Workspace usage rollup: totals plus per-agent and per-day tables over a
// trailing window. Every workspace member can see it (org-wide read).
export const OrgUsageTab: React.FC<OrgUsageTabProps> = ({ org }) => {
  const [usage, setUsage] = useState<OrgUsageSummary | null>(null);
  const [days, setDays] = useState(30);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await orgsAPI.usage(org.id, days);
      setUsage(res.data);
    } catch (err: any) {
      setError(`Failed to load usage: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [org.id, days]);

  useEffect(() => {
    load();
  }, [load]);

  const maxAgentTokens = Math.max(0, ...(usage?.by_agent || []).map((a) => a.tokens_in + a.tokens_out));
  const maxDayTokens = Math.max(0, ...(usage?.by_day || []).map((d) => d.tokens_in + d.tokens_out));

  return (
    <>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />

      <div className="card">
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <h3 style={{ flex: 1, margin: 0 }}>Usage</h3>
          {WINDOWS.map((w) => (
            <button
              key={w}
              onClick={() => setDays(w)}
              style={{
                background: days === w ? 'var(--accent)' : 'var(--surface-alt)',
                color: days === w ? '#fff' : 'var(--text)',
                border: '1px solid var(--border)',
                borderRadius: 4,
                padding: '4px 10px',
                fontSize: 12,
                cursor: 'pointer',
                width: 'auto',
              }}
            >
              {w}d
            </button>
          ))}
        </div>
        <p style={{ fontSize: 12.5, color: 'var(--text-muted)', margin: '6px 0 12px' }}>
          Agent-run activity across the whole workspace over the last {usage?.days ?? days} days. Tokens and
          cost land when a run finishes.
        </p>

        {loading && <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>Loading…</div>}

        {!loading && usage && (
          <>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
              {[
                { label: 'Runs', value: String(usage.totals.runs) },
                { label: 'Tokens in', value: fmtTokens(usage.totals.tokens_in) },
                { label: 'Tokens out', value: fmtTokens(usage.totals.tokens_out) },
                { label: 'Cost', value: fmtCost(usage.totals.cost_usd) },
              ].map((stat) => (
                <div
                  key={stat.label}
                  style={{
                    flex: '1 1 120px',
                    background: 'var(--surface-alt)',
                    border: '1px solid var(--neutral-soft)',
                    borderRadius: 4,
                    padding: '10px 14px',
                  }}
                >
                  <div style={{ fontSize: 11.5, color: 'var(--text-muted)', fontWeight: 600 }}>{stat.label}</div>
                  <div style={{ fontSize: 20, color: 'var(--text)', fontVariantNumeric: 'tabular-nums' }}>
                    {stat.value}
                  </div>
                </div>
              ))}
            </div>

            <h4 style={{ fontSize: 13, color: 'var(--text)', margin: '0 0 6px' }}>By agent</h4>
            {usage.by_agent.length === 0 ? (
              <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>No runs in this window yet.</p>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 18 }}>
                <thead>
                  <tr>
                    <th style={thStyle}>Agent</th>
                    <th style={{ ...thStyle, width: '35%' }}>Tokens</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>Runs</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>In</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>Out</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {usage.by_agent.map((a) => (
                    <tr key={a.agent_slug}>
                      <td style={tdStyle}>{a.agent_name || a.agent_slug}</td>
                      <td style={tdStyle}>
                        <TokenBar value={a.tokens_in + a.tokens_out} max={maxAgentTokens} />
                      </td>
                      <td style={numStyle}>{a.runs}</td>
                      <td style={numStyle}>{fmtTokens(a.tokens_in)}</td>
                      <td style={numStyle}>{fmtTokens(a.tokens_out)}</td>
                      <td style={numStyle}>{fmtCost(a.cost_usd)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}

            <h4 style={{ fontSize: 13, color: 'var(--text)', margin: '0 0 6px' }}>By day</h4>
            {usage.by_day.length === 0 ? (
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                No runs in this window yet.
              </p>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={thStyle}>Day</th>
                    <th style={{ ...thStyle, width: '35%' }}>Tokens</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>Runs</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>In</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>Out</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {usage.by_day.map((d) => (
                    <tr key={d.day}>
                      <td style={tdStyle}>{d.day}</td>
                      <td style={tdStyle}>
                        <TokenBar value={d.tokens_in + d.tokens_out} max={maxDayTokens} />
                      </td>
                      <td style={numStyle}>{d.runs}</td>
                      <td style={numStyle}>{fmtTokens(d.tokens_in)}</td>
                      <td style={numStyle}>{fmtTokens(d.tokens_out)}</td>
                      <td style={numStyle}>{fmtCost(d.cost_usd)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </div>
    </>
  );
};
