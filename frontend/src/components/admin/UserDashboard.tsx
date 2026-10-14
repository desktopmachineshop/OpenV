import React, { useEffect, useState } from 'react';
import { UserDashboard as Dashboard, adminAPI } from '../../api/client';
import { DailyBars, Stat, countryName, percent, td, th } from './dashboardParts';
import { apiErrorMessage } from '../../api/errors';

// UserDashboard is the Platform admin page's Users section: how many people
// signed up, who is active, who has gone quiet, how they sign in, where
// they sign in from (Cloudflare's country, no IP is kept) and how each
// month's sign-ups keep coming back.

const trend = (now: number, before: number): string => {
  if (before === 0) return 'none in the 30 days before';
  const change = (now - before) / before;
  return `${change >= 0 ? '+' : ''}${Math.round(change * 100)}% on the 30 days before`;
};

export const UserDashboard: React.FC<{ compact: boolean }> = ({ compact }) => {
  const [data, setData] = useState<Dashboard | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    adminAPI
      .userMetrics()
      .then((r) => setData(r.data))
      .catch((err) => setError(apiErrorMessage(err, 'Could not load the user dashboard')));
  }, []);

  const grid = (min: number): React.CSSProperties => ({
    display: 'grid',
    gridTemplateColumns: `repeat(auto-fit, minmax(${min}px, 1fr))`,
    gap: 10,
    marginBottom: 16,
  });

  return (
    <section className="card" id="user-dashboard" style={{ padding: compact ? 14 : 20, marginBottom: 16 }}>
      <h2 style={{ margin: '0 0 6px', fontSize: 18 }}>Users</h2>
      {error ? (
        <div style={{ color: 'var(--danger-text)', fontSize: 13 }}>{error}</div>
      ) : data === null ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
      ) : (
        <>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0 }}>
            A user is active on a day they use the app while signed in, and lost after {data.lost_after_days} days
            with no activity. Activity is counted from the day this dashboard shipped, plus each account's sign-up day.
          </p>
          <div style={grid(compact ? 130 : 150)}>
            <Stat label="Total users" value={data.total_users} />
            <Stat label="New, last 7 days" value={data.new_users_7d} />
            <Stat label="New, last 30 days" value={data.new_users_30d} hint={trend(data.new_users_30d, data.new_users_prev_30d)} />
            <Stat label="Active today" value={data.dau} />
            <Stat label="Active, 7 days" value={data.wau} />
            <Stat label="Active, 30 days" value={data.mau} hint={`stickiness ${percent(data.stickiness)} (today ÷ 30 days)`} />
            <Stat label="Lost users" value={data.lost_users} hint={`${data.newly_lost_30d} went quiet in the last 30 days`} />
          </div>

          <div style={grid(compact ? 260 : 380)}>
            <DailyBars title="Sign-ups" points={data.signups} />
            <DailyBars title="Active users" points={data.active} />
          </div>

          <div style={grid(260)}>
            <div>
              <h3 style={{ fontSize: 14, margin: '0 0 6px' }}>Where users sign in from</h3>
              {data.countries.length === 0 ? (
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                  No country recorded yet: it comes from Cloudflare's CF-IPCountry header.
                </div>
              ) : (
                <div className="table-scroll">
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                      <tr>
                        <th style={th}>Country</th>
                        <th style={{ ...th, textAlign: 'right' }}>Users</th>
                        <th style={{ ...th, textAlign: 'right' }}>Active, 30 days</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.countries.map((c) => (
                        <tr key={c.country}>
                          <td style={td}>{countryName(c.country)}</td>
                          <td style={{ ...td, textAlign: 'right' }}>{c.users}</td>
                          <td style={{ ...td, textAlign: 'right' }}>{c.active_30d}</td>
                        </tr>
                      ))}
                      {data.unknown_country > 0 && (
                        <tr>
                          <td style={{ ...td, color: 'var(--text-muted)' }}>Unknown</td>
                          <td style={{ ...td, textAlign: 'right' }}>{data.unknown_country}</td>
                          <td style={td} />
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
            <div>
              <h3 style={{ fontSize: 14, margin: '0 0 6px' }}>How users sign in</h3>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <tbody>
                  {data.auth_providers.map((p) => (
                    <tr key={p.name}>
                      <td style={td}>{p.name}</td>
                      <td style={{ ...td, textAlign: 'right' }}>{p.count}</td>
                      <td style={{ ...td, textAlign: 'right', color: 'var(--text-muted)' }}>
                        {data.total_users ? percent(p.count / data.total_users) : ''}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <h3 style={{ fontSize: 14, margin: '0 0 6px' }}>Monthly retention</h3>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '0 0 6px' }}>
            Of each month's sign-ups, the share active in that month and in each month after.
          </p>
          <div className="table-scroll">
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={th}>Signed up</th>
                  <th style={{ ...th, textAlign: 'right' }}>Users</th>
                  {data.cohorts.map((_, i) => (
                    <th key={i} style={{ ...th, textAlign: 'center' }}>
                      {i === 0 ? 'Month 0' : `+${i}`}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.cohorts.map((c) => (
                  <tr key={c.month}>
                    <td style={td}>{c.month}</td>
                    <td style={{ ...td, textAlign: 'right' }}>{c.size}</td>
                    {data.cohorts.map((_, i) => {
                      const v = c.retained[i];
                      return (
                        <td key={i} style={{ ...td, textAlign: 'center', position: 'relative' }}
                          title={v === undefined ? '' : `${c.month}, month +${i}: ${percent(v)} of ${c.size}`}>
                          {v !== undefined && c.size > 0 && (
                            <>
                              <span aria-hidden style={{ position: 'absolute', inset: 2, borderRadius: 4,
                                background: 'var(--accent)', opacity: 0.08 + v * 0.6 }} />
                              <span style={{ position: 'relative' }}>{percent(v)}</span>
                            </>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </section>
  );
};
