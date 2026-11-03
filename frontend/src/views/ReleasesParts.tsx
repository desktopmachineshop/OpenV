// The parts of the Releases page (Releases.tsx): the labels and colours of
// a release's status and a requirement's rollup, the rollup bar, the
// burn-up chart, the release form and one release's detail.
import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import type {
  ReleaseBurnUpPoint,
  ReleaseProgress,
  ReleaseRange,
  ReleaseSummaryCounts,
  TargetReleaseStatus,
} from '../api/client';
import { rollupColor } from './VVDashboard';

export const STATUS_LABEL: Record<TargetReleaseStatus, string> = {
  planned: 'Planned',
  in_progress: 'In progress',
  released: 'Released',
};

export const STATUS_COLOR: Record<TargetReleaseStatus, string> = {
  planned: 'var(--neutral)',
  in_progress: 'var(--accent)',
  released: 'var(--success)',
};

const ROLLUP_ORDER = ['pass', 'verified-manually', 'fail', 'blocked', 'unrun', 'uncovered', 'method-missing'];

const ROLLUP_LABEL: Record<string, string> = {
  pass: 'Pass',
  'verified-manually': 'Verified manually',
  fail: 'Fail',
  blocked: 'Blocked',
  unrun: 'Not run',
  uncovered: 'Uncovered',
  'method-missing': 'No method',
};

const REVIEW_LABEL: Record<string, string> = {
  draft: 'Draft',
  in_review: 'In review',
  approved: 'Approved',
  superseded: 'Superseded',
};

const releaseRollupColor = (rollup: string) =>
  rollup === 'verified-manually' ? 'var(--success)' : rollupColor(rollup);

export const chip = (color: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 12,
  background: color,
  color: '#fff',
  fontSize: 12,
  fontWeight: 600,
  whiteSpace: 'nowrap',
});

const percent = (part: number, whole: number) => (whole > 0 ? Math.round((part / whole) * 100) : 0);

/** How a release range reads: "MVP onwards", "V1.0 only", "MVP to V1.0". */
export const rangeLabel = (range: ReleaseRange, names: Record<string, string>): string => {
  const from = names[range.from] || 'an unknown release';
  if (!range.until) return `${from} onwards`;
  if (range.until === range.from) return `${from} only`;
  return `${from} to ${names[range.until] || 'an unknown release'}`;
};

export const RollupBar: React.FC<{
  counts: Record<string, number>;
  total: number;
}> = ({ counts, total }) => {
  if (total === 0) return null;
  const keys = Object.keys(counts).sort((a, b) => ROLLUP_ORDER.indexOf(a) - ROLLUP_ORDER.indexOf(b));
  return (
    <div
      role="img"
      aria-label={keys.map((k) => `${ROLLUP_LABEL[k] || k}: ${counts[k]}`).join(', ')}
      style={{
        display: 'flex',
        height: 10,
        borderRadius: 5,
        overflow: 'hidden',
        border: '1px solid var(--border)',
      }}
    >
      {keys.map((k) =>
        counts[k] > 0 ? (
          <div
            key={k}
            title={`${ROLLUP_LABEL[k] || k}: ${counts[k]}`}
            style={{
              width: `${(counts[k] / total) * 100}%`,
              background: releaseRollupColor(k),
            }}
          />
        ) : null,
      )}
    </div>
  );
};

const Tile: React.FC<{
  label: string;
  value: React.ReactNode;
  hint?: string;
  color?: string;
}> = ({ label, value, hint, color }) => (
  <div className="card" style={{ padding: '12px 18px', marginBottom: 0, minWidth: 130 }}>
    <div style={{ fontSize: 24, fontWeight: 700, color: color || 'var(--text)' }}>{value}</div>
    <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{label}</div>
    {hint && <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{hint}</div>}
  </div>
);

/**
 * The burn-up: the requirements in the release that existed each day (scope)
 * and those verified by then. A target date inside the span is marked.
 */
export const BurnUpChart: React.FC<{
  points: ReleaseBurnUpPoint[];
  targetDate?: string | null;
}> = ({ points, targetDate }) => {
  if (points.length === 0) return null;
  const width = 640;
  const height = 220;
  const pad = { left: 36, right: 12, top: 12, bottom: 28 };
  const max = Math.max(1, ...points.map((p) => p.scope));
  const plotW = width - pad.left - pad.right;
  const plotH = height - pad.top - pad.bottom;
  const x = (i: number) => pad.left + (points.length === 1 ? plotW / 2 : (i / (points.length - 1)) * plotW);
  const y = (v: number) => pad.top + plotH - (v / max) * plotH;
  const line = (pick: (p: ReleaseBurnUpPoint) => number) =>
    points.map((p, i) => `${x(i).toFixed(1)},${y(pick(p)).toFixed(1)}`).join(' ');
  const last = points[points.length - 1];
  const targetIndex = targetDate ? points.findIndex((p) => p.date >= targetDate) : -1;
  return (
    <figure style={{ margin: 0 }}>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        width="100%"
        role="img"
        aria-label={`Burn-up from ${points[0].date} to ${last.date}: ${last.verified} of ${last.scope} requirements verified`}
        style={{ maxWidth: width, display: 'block' }}
      >
        <line x1={pad.left} y1={y(0)} x2={width - pad.right} y2={y(0)} stroke="var(--border)" />
        <line x1={pad.left} y1={y(max)} x2={width - pad.right} y2={y(max)} stroke="var(--neutral-soft)" />
        <text x={pad.left - 6} y={y(max) + 4} textAnchor="end" fontSize="12" fill="var(--text-muted)">
          {max}
        </text>
        <text x={pad.left - 6} y={y(0) + 4} textAnchor="end" fontSize="12" fill="var(--text-muted)">
          0
        </text>
        <text x={pad.left} y={height - 8} fontSize="12" fill="var(--text-muted)">
          {points[0].date}
        </text>
        <text x={width - pad.right} y={height - 8} textAnchor="end" fontSize="12" fill="var(--text-muted)">
          {last.date}
        </text>
        {targetIndex >= 0 && targetDate && targetDate <= last.date && (
          <g>
            <line
              x1={x(targetIndex)}
              y1={pad.top}
              x2={x(targetIndex)}
              y2={y(0)}
              stroke="var(--warning)"
              strokeDasharray="4 3"
            />
            <text x={x(targetIndex) + 4} y={pad.top + 10} fontSize="12" fill="var(--text-muted)">
              Target {targetDate}
            </text>
          </g>
        )}
        <polyline
          fill="none"
          stroke="var(--neutral)"
          strokeWidth="2"
          strokeDasharray="6 4"
          points={line((p) => p.scope)}
        />
        <polyline fill="none" stroke="var(--success)" strokeWidth="2.5" points={line((p) => p.verified)} />
        {points.length === 1 && (
          <>
            <circle cx={x(0)} cy={y(last.scope)} r="3" fill="var(--neutral)" />
            <circle cx={x(0)} cy={y(last.verified)} r="3" fill="var(--success)" />
          </>
        )}
      </svg>
      <figcaption
        style={{
          display: 'flex',
          gap: 16,
          fontSize: 12,
          color: 'var(--text-muted)',
          marginTop: 4,
        }}
      >
        <span>
          <span aria-hidden="true" style={{ color: 'var(--neutral)' }}>
            ┅
          </span>{' '}
          Requirements in the release
        </span>
        <span>
          <span aria-hidden="true" style={{ color: 'var(--success)' }}>
            ━
          </span>{' '}
          Verified
        </span>
      </figcaption>
    </figure>
  );
};

export interface ReleaseFormValue {
  name: string;
  description: string;
  target_date: string;
  status: TargetReleaseStatus;
}

export const emptyForm: ReleaseFormValue = {
  name: '',
  description: '',
  target_date: '',
  status: 'planned',
};

export const ReleaseForm: React.FC<{
  initial: ReleaseFormValue;
  submitLabel: string;
  busy: boolean;
  onSubmit: (v: ReleaseFormValue) => void;
  onCancel: () => void;
}> = ({ initial, submitLabel, busy, onSubmit, onCancel }) => {
  const [value, setValue] = useState(initial);
  const set = (k: keyof ReleaseFormValue) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setValue((v) => ({ ...v, [k]: e.target.value }));
  return (
    <form
      className="card"
      data-help-key="releases.form"
      onSubmit={(e) => {
        e.preventDefault();
        if (value.name.trim()) onSubmit(value);
      }}
      style={{
        display: 'flex',
        gap: 12,
        flexWrap: 'wrap',
        alignItems: 'flex-end',
      }}
    >
      <label
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          fontSize: 13,
        }}
      >
        Name
        <input value={value.name} onChange={set('name')} placeholder="MVP" required style={{ width: 160 }} />
      </label>
      <label
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          fontSize: 13,
        }}
      >
        Target date
        <input type="date" value={value.target_date} onChange={set('target_date')} />
      </label>
      <label
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          fontSize: 13,
        }}
      >
        Status
        <select value={value.status} onChange={set('status')}>
          {(Object.keys(STATUS_LABEL) as TargetReleaseStatus[]).map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
      </label>
      <label
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          fontSize: 13,
          flex: 1,
          minWidth: 200,
        }}
      >
        Description
        <input value={value.description} onChange={set('description')} placeholder="What the release is for" />
      </label>
      <div style={{ display: 'flex', gap: 8 }}>
        <button type="submit" className="button" disabled={busy || !value.name.trim()}>
          {submitLabel}
        </button>
        <button type="button" className="button-secondary" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
};

export const SummaryLine: React.FC<{ s: ReleaseSummaryCounts }> = ({ s }) => (
  <div style={{ fontSize: 13, color: 'var(--text-body)' }}>
    {s.total} requirement{s.total === 1 ? '' : 's'}
    {s.total > 0 && (
      <>
        {' '}
        ({s.introduced} new) · {s.approved} approved · {s.verified} verified ·{' '}
        <strong>{percent(s.ready, s.total)}% ready</strong>
      </>
    )}
  </div>
);

/** One release's detail: its counts, its burn-up and its requirements. */
export const ReleaseDetail: React.FC<{
  progress: ReleaseProgress;
  projectId: string;
  names: Record<string, string>;
}> = ({ progress, projectId, names }) => (
  <section aria-labelledby="release-detail" style={{ marginTop: 24 }} data-help-key="releases.progress">
    <h3 id="release-detail" style={{ color: 'var(--text)' }}>
      {progress.release.name}: progress
    </h3>
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
      <Tile
        label="Requirements"
        value={progress.summary.total}
        hint={`${progress.summary.introduced} new in this release`}
      />
      <Tile
        label="Approved"
        value={`${percent(progress.summary.approved, progress.summary.total)}%`}
        hint={`${progress.summary.approved} of ${progress.summary.total}`}
      />
      <Tile
        label="Verified"
        value={`${percent(progress.summary.verified, progress.summary.total)}%`}
        hint={`${progress.summary.verified} of ${progress.summary.total}`}
        color="var(--success)"
      />
      <Tile
        label="Ready"
        value={`${percent(progress.summary.ready, progress.summary.total)}%`}
        hint="approved and verified"
        color="var(--success)"
      />
      {(progress.summary.by_rollup.fail || 0) > 0 && (
        <Tile label="Failing" value={progress.summary.by_rollup.fail} color="var(--danger)" />
      )}
    </div>

    {progress.burn_up && progress.burn_up.length > 0 && (
      <div className="card" data-help-key="releases.burnup">
        <h4 style={{ marginTop: 0 }}>Burn-up</h4>
        <BurnUpChart points={progress.burn_up} targetDate={progress.release.target_date} />
      </div>
    )}

    <div className="card">
      <h4 style={{ marginTop: 0 }}>Requirements in {progress.release.name}</h4>
      {progress.entries.length === 0 ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No requirement targets this release yet.</div>
      ) : (
        <div className="table-scroll">
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>
                {['Ref', 'Requirement', 'Review', 'V&V', 'Releases'].map((h) => (
                  <th
                    key={h}
                    style={{
                      textAlign: 'left',
                      borderBottom: '2px solid var(--neutral-soft)',
                      padding: '8px 10px',
                      color: 'var(--text-muted)',
                      fontWeight: 600,
                    }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {progress.entries.map((e) => (
                <tr key={e.requirement_id}>
                  <td
                    style={{
                      padding: '8px 10px',
                      borderBottom: '1px solid var(--neutral-soft)',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    <Link to={`/projects/${projectId}/requirements?artifact=${e.requirement_id}`}>{e.ref}</Link>
                  </td>
                  <td
                    style={{
                      padding: '8px 10px',
                      borderBottom: '1px solid var(--neutral-soft)',
                    }}
                  >
                    {e.title}
                  </td>
                  <td
                    style={{
                      padding: '8px 10px',
                      borderBottom: '1px solid var(--neutral-soft)',
                    }}
                  >
                    {REVIEW_LABEL[e.status] || e.status}
                  </td>
                  <td
                    style={{
                      padding: '8px 10px',
                      borderBottom: '1px solid var(--neutral-soft)',
                    }}
                  >
                    <span style={chip(releaseRollupColor(e.rollup))}>{ROLLUP_LABEL[e.rollup] || e.rollup}</span>
                  </td>
                  <td
                    style={{
                      padding: '8px 10px',
                      borderBottom: '1px solid var(--neutral-soft)',
                      color: 'var(--text-body)',
                    }}
                  >
                    {rangeLabel(e.range, names)}
                    {e.range.inherited_from && <span style={{ color: 'var(--text-muted)' }}> (inherited)</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  </section>
);
