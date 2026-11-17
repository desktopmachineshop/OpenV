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
import { Chip, PanelHeader, ResultBar, buttonStyle, mutedStyle, panelStyle, refStyle, type ChipTone } from '../components/ui';

export const STATUS_LABEL: Record<TargetReleaseStatus, string> = {
  planned: 'Planned',
  in_progress: 'In progress',
  released: 'Released',
};

export const STATUS_TONE: Record<TargetReleaseStatus, ChipTone> = {
  planned: 'neutral',
  in_progress: 'info',
  released: 'success',
};


export const ROLLUP_LABEL: Record<string, string> = {
  pass: 'Pass',
  'verified-manually': 'Verified manually',
  fail: 'Fail',
  blocked: 'Blocked',
  unrun: 'Not run',
  uncovered: 'Uncovered',
  'method-missing': 'No method',
};

export const REVIEW_LABEL: Record<string, string> = {
  draft: 'Draft',
  in_review: 'In review',
  approved: 'Approved',
  superseded: 'Superseded',
};

export const percent = (part: number, whole: number) => (whole > 0 ? Math.round((part / whole) * 100) : 0);

/** How a release range reads: "MVP onwards", "V1.0 only", "MVP to V1.0". */
export const rangeLabel = (range: ReleaseRange, names: Record<string, string>): string => {
  const from = names[range.from] || 'an unknown release';
  if (!range.until) return `${from} onwards`;
  if (range.until === range.from) return `${from} only`;
  return `${from} to ${names[range.until] || 'an unknown release'}`;
};

/**
 * A release's V&V as the kit's result bar: passed (a passing rollup, or
 * verified by hand) in green, failing in red, blocked in amber, and the rest
 * (not run, uncovered, no method) the bar's grey.
 */
export const RollupBar: React.FC<{ counts: Record<string, number>; total: number }> = ({ counts, total }) =>
  total === 0 ? null : (
    <ResultBar
      pass={(counts.pass || 0) + (counts['verified-manually'] || 0)}
      fail={counts.fail || 0}
      blocked={counts.blocked || 0}
      total={total}
    />
  );

/** A headline figure, as Home's head row draws them: the value, then its name. */
export const Figure: React.FC<{ label: string; value: React.ReactNode; hint?: string; color?: string }> = ({
  label,
  value,
  hint,
  color,
}) => (
  <span title={hint} style={{ whiteSpace: 'nowrap' }}>
    <b style={{ fontSize: 16, fontWeight: 700, color: color || 'var(--text)' }}>{value}</b>{' '}
    <span style={mutedStyle}>{label}</span>
  </span>
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

const field: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' };

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
      data-help-key="releases.form"
      onSubmit={(e) => {
        e.preventDefault();
        if (value.name.trim()) onSubmit(value);
      }}
      style={{ ...panelStyle, padding: 12, display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}
    >
      <label style={field}>
        Name
        <input value={value.name} onChange={set('name')} placeholder="MVP" required style={{ width: 160 }} />
      </label>
      <label style={field}>
        Target date
        <input type="date" value={value.target_date} onChange={set('target_date')} />
      </label>
      <label style={field}>
        Status
        <select value={value.status} onChange={set('status')}>
          {(Object.keys(STATUS_LABEL) as TargetReleaseStatus[]).map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
      </label>
      <label style={{ ...field, flex: 1, minWidth: 200 }}>
        Description
        <input value={value.description} onChange={set('description')} placeholder="What the release is for" />
      </label>
      <div style={{ display: 'flex', gap: 8 }}>
        <button type="submit" disabled={busy || !value.name.trim()} style={buttonStyle('primary', { disabled: busy || !value.name.trim() })}>
          {submitLabel}
        </button>
        <button type="button" onClick={onCancel} style={buttonStyle('secondary')}>
          Cancel
        </button>
      </div>
    </form>
  );
};

export const SummaryLine: React.FC<{ s: ReleaseSummaryCounts }> = ({ s }) => (
  <div style={{ fontSize: 12, color: 'var(--text-body)' }}>
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

/** The release's headline figures: its size, and how much is approved, verified and ready. */
export const ReleaseFigures: React.FC<{ s: ReleaseSummaryCounts }> = ({ s }) => (
  <>
    <Figure label="requirements" value={s.total} hint={`${s.introduced} new in this release`} />
    <Figure label="approved" value={`${percent(s.approved, s.total)}%`} hint={`${s.approved} of ${s.total}`} />
    <Figure label="verified" value={`${percent(s.verified, s.total)}%`} hint={`${s.verified} of ${s.total}`} />
    <Figure label="ready" value={`${percent(s.ready, s.total)}%`} hint="approved and verified" color="var(--success-text)" />
    {(s.by_rollup.fail || 0) > 0 && <Figure label="failing" value={s.by_rollup.fail} color="var(--danger-strong)" />}
  </>
);

/** The release's requirements as a dense table: ref, title, review state, V&V and range. */
export const ReleaseRequirements: React.FC<{ progress: ReleaseProgress; projectId: string; names: Record<string, string> }> = ({
  progress,
  projectId,
  names,
}) => (
  <section aria-label={`Requirements in ${progress.release.name}`} style={panelStyle}>
    <PanelHeader title={`Requirements in ${progress.release.name}`} count={progress.entries.length} />
    {progress.entries.length === 0 ? (
      <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No requirement targets this release yet.</p>
    ) : (
      <div className="table-scroll">
        <table className="ov-dense-table">
          <thead>
            <tr>
              {['Ref', 'Requirement', 'Review', 'V&V', 'Releases'].map((h) => (
                <th key={h}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {progress.entries.map((e) => (
              <tr key={e.requirement_id}>
                <td style={{ whiteSpace: 'nowrap' }}>
                  <Link to={`/projects/${projectId}/requirements?artifact=${e.requirement_id}`} style={refStyle}>
                    {e.ref}
                  </Link>
                </td>
                <td>{e.title}</td>
                <td>
                  <Chip>{REVIEW_LABEL[e.status] || e.status}</Chip>
                </td>
                <td>
                  <Chip tone={e.rollup === 'verified-manually' ? 'success' : undefined}>{ROLLUP_LABEL[e.rollup] || e.rollup}</Chip>
                </td>
                <td style={{ color: 'var(--text-body)' }}>
                  {rangeLabel(e.range, names)}
                  {e.range.inherited_from && <span style={{ color: 'var(--text-muted)' }}> (inherited)</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )}
  </section>
);

/** One release's detail: its head with the figures, its burn-up and its requirements. */
export const ReleaseDetail: React.FC<{
  progress: ReleaseProgress;
  projectId: string;
  names: Record<string, string>;
}> = ({ progress, projectId, names }) => (
  <section aria-labelledby="release-detail" data-help-key="releases.progress" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
    <div style={{ display: 'flex', alignItems: 'baseline', gap: '6px 16px', flexWrap: 'wrap' }}>
      <h2 id="release-detail" style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>
        {progress.release.name}: progress
      </h2>
      <Chip tone={STATUS_TONE[progress.release.status]}>{STATUS_LABEL[progress.release.status]}</Chip>
      {progress.release.target_date && <span style={mutedStyle}>Target {progress.release.target_date}</span>}
      <span style={{ flex: 1 }} />
      <ReleaseFigures s={progress.summary} />
    </div>
    {progress.burn_up && progress.burn_up.length > 0 && (
      <section aria-label="Burn-up" style={panelStyle} data-help-key="releases.burnup">
        <PanelHeader title="Burn-up" />
        <div style={{ padding: 12 }}>
          <BurnUpChart points={progress.burn_up} targetDate={progress.release.target_date} />
        </div>
      </section>
    )}
    <ReleaseRequirements progress={progress} projectId={projectId} names={names} />
  </section>
);
