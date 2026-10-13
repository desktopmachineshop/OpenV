import React, { useEffect, useState } from 'react';
import { VisitorBreakdownRow, VisitorDashboard as Dashboard, adminAPI } from '../../api/client';
import { DailyBars, Stat, countryName, percent, td, th } from './dashboardParts';
import { apiErrorMessage } from '../../api/errors';

// VisitorDashboard is the Platform admin page's Visitors section: how many
// browsers reach the public site, how many are new, and how many of the new
// ones go on to make an account, with where they landed and came from. A
// visitor is one browser, named by a first-party cookie; bots and browsers
// that ask not to be tracked are not counted, and no IP address is kept.

// Conversion rates are small, so they keep one decimal.
const rate = (v: number) => `${(Math.round(v * 1000) / 10).toFixed(1)}%`;

const rateTrend = (now: number, before: number, beforeVisitors: number): string => {
  if (beforeVisitors === 0) return 'no visitors in the 30 days before';
  const points = Math.round((now - before) * 1000) / 10;
  return `${points >= 0 ? '+' : ''}${points.toFixed(1)} points on the 30 days before (${rate(before)})`;
};

const Breakdown: React.FC<{
  title: string;
  label: string;
  rows: VisitorBreakdownRow[];
  name: (raw: string) => string;
}> = ({ title, label, rows, name }) => (
  <div>
    <h3 style={{ fontSize: 14, margin: '0 0 6px' }}>{title}</h3>
    {rows.length === 0 ? (
      <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No new visitors in the last 30 days.</div>
    ) : (
      <div className="table-scroll">
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>{label}</th>
              <th style={{ ...th, textAlign: 'right' }}>Visitors</th>
              <th style={{ ...th, textAlign: 'right' }}>Accounts</th>
              <th style={{ ...th, textAlign: 'right' }}>Rate</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.name}>
                <td style={{ ...td, color: r.name ? undefined : 'var(--text-muted)', wordBreak: 'break-all' }}>
                  {name(r.name)}
                </td>
                <td style={{ ...td, textAlign: 'right' }}>{r.visitors}</td>
                <td style={{ ...td, textAlign: 'right' }}>{r.conversions}</td>
                <td style={{ ...td, textAlign: 'right', color: 'var(--text-muted)' }}>
                  {r.visitors ? percent(r.conversions / r.visitors) : ''}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )}
  </div>
);

export const VisitorDashboard: React.FC<{ compact: boolean }> = ({ compact }) => {
  const [data, setData] = useState<Dashboard | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    adminAPI
      .visitorMetrics()
      .then((r) => setData(r.data))
      .catch((err) => setError(apiErrorMessage(err, 'Could not load the visitor dashboard')));
  }, []);

  const grid = (min: number): React.CSSProperties => ({
    display: 'grid',
    gridTemplateColumns: `repeat(auto-fit, minmax(${min}px, 1fr))`,
    gap: 10,
    marginBottom: 16,
  });

  return (
    <section className="card" id="visitor-dashboard" style={{ padding: compact ? 14 : 20, marginBottom: 16 }}>
      <h2 style={{ margin: '0 0 6px', fontSize: 18 }}>Visitors</h2>
      {error ? (
        <div style={{ color: 'var(--danger-text)', fontSize: 13 }}>{error}</div>
      ) : data === null ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
      ) : (
        <>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0 }}>
            A visitor is one browser on the public site or the sign-in page while not signed in. It converts when it
            goes on to make an account, from that browser, after its first visit; signing in to an existing account
            never counts. Bots and browsers that send Global Privacy Control or Do Not Track are not counted.
            Visitors are counted from the day this dashboard shipped.
          </p>
          <div style={grid(compact ? 130 : 150)}>
            <Stat
              label="New visitors, 30 days"
              value={data.new_visitors_30d}
              hint={`${data.new_visitors_7d} in the last 7 days; ${data.new_visitors_prev_30d} in the 30 before`}
            />
            <Stat
              label="Became users, 30 days"
              value={data.conversions_30d}
              hint="new visitors of the last 30 days who have made an account"
            />
            <Stat
              label="Visitor → user rate"
              value={rate(data.conversion_rate_30d)}
              hint={rateTrend(data.conversion_rate_30d, data.conversion_rate_prev_30d, data.new_visitors_prev_30d)}
            />
            <Stat
              label="Sign-ups from tracked visits"
              value={`${data.tracked_signups_30d} of ${data.signups_30d}`}
              hint="the rest came from a browser that was not counted"
            />
            <Stat label="Visitors today" value={data.visitors_today} />
            <Stat label="Visitors, 7 days" value={data.visitors_7d} />
            <Stat label="Visitors, 30 days" value={data.visitors_30d} />
          </div>

          <div style={grid(compact ? 260 : 380)}>
            <DailyBars title="Visitors" points={data.visitors} />
            <DailyBars title="New visitors" points={data.new_visitors} />
            <DailyBars title="Visitors who became users" points={data.conversions} />
          </div>

          <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '0 0 8px' }}>
            The new visitors of the last 30 days, by their first visit, and how many of them have made an account.
          </p>
          <div style={grid(260)}>
            <Breakdown title="Landing pages" label="Page" rows={data.landing_pages} name={(n) => n || '(none)'} />
            <Breakdown title="Where they came from" label="Source" rows={data.sources} name={(n) => n || 'Direct'} />
            <Breakdown
              title="Countries"
              label="Country"
              rows={data.countries}
              name={(n) => (n ? countryName(n) : 'Unknown')}
            />
          </div>
        </>
      )}
    </section>
  );
};
