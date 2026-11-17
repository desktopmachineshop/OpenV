// The coverage page's test runs: the form that starts one, and a card per
// run with its results as a stacked bar, completing or aborting a run in
// progress from its card.
import React from 'react';
import { Link } from 'react-router-dom';
import type { Baseline, TestResult, TestRun } from '../api/client';
import { Chip, buttonStyle } from '../components/ui';
import { baselineLabel, runBaselineLabel } from '../utils/baselines';
import {
  StackedBar,
  cardHeadStyle,
  cardStyle,
  cardTitleStyle,
  countResults,
  resultSegments,
  resultSummary,
} from './VVDashboardParts';

export interface NewRunForm {
  name: string;
  description: string;
  baselineId: string;
}

interface Props {
  projectId: string;
  runs: TestRun[];
  results: Record<string, TestResult[]>;
  caseCount: number;
  baselines: Baseline[];
  loading: boolean;
  showNewRun: boolean;
  onToggleNewRun: () => void;
  form: NewRunForm;
  onFormChange: (form: NewRunForm) => void;
  creating: boolean;
  onCreate: () => void;
  onRunStatus: (runId: string, status: 'completed' | 'aborted') => void;
  formRef: React.RefObject<HTMLDivElement>;
}

const RunCard: React.FC<{
  projectId: string;
  run: TestRun;
  results?: TestResult[];
  caseCount: number;
  baselines: Baseline[];
  onRunStatus: Props['onRunStatus'];
}> = ({ projectId, run, results, caseCount, baselines, onRunStatus }) => {
  const counts = countResults(results || []);
  const live = run.status === 'in-progress';
  return (
    <div
      style={{
        ...cardStyle,
        borderColor: live ? 'var(--tint-blue-border)' : 'var(--border)',
        padding: '10px 12px',
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        minWidth: 0,
      }}
    >
      <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <Link
          to={`/projects/${projectId}/vv/runs/${run.id}`}
          style={{
            flex: 1,
            minWidth: 0,
            minHeight: 32,
            lineHeight: '32px',
            fontWeight: 600,
            color: 'var(--text)',
            textDecoration: 'none',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
        >
          {run.name}
        </Link>
        <Chip>{run.status.replace('-', ' ')}</Chip>
      </span>
      {run.description && (
        <span style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {run.description}
        </span>
      )}
      {results && <StackedBar segments={resultSegments(counts)} total={caseCount} height={6} />}
      <span style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap', fontSize: 12, color: 'var(--text-muted)' }}>
        <span>{results ? resultSummary(counts, caseCount) : runBaselineLabel(run, baselines)}</span>
        <span>{run.started_at ? new Date(run.started_at).toLocaleString() : '—'}</span>
      </span>
      {results && (
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Baseline: {runBaselineLabel(run, baselines)}</span>
      )}
      {live && (
        <span style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 2 }}>
          <button type="button" style={{ ...buttonStyle('secondary', { compact: true }), minHeight: 32 }} onClick={() => onRunStatus(run.id, 'completed')}>
            Complete
          </button>
          <button type="button" style={{ ...buttonStyle('danger', { compact: true }), minHeight: 32 }} onClick={() => onRunStatus(run.id, 'aborted')}>
            Abort
          </button>
        </span>
      )}
    </div>
  );
};

export const RunsSection: React.FC<Props> = (p) => (
  <section aria-label="Test runs" data-help-key="vv.runs" style={cardStyle}>
    <div style={cardHeadStyle}>
      <h3 style={cardTitleStyle}>Test runs</h3>
      <button type="button" style={buttonStyle('secondary')} onClick={p.onToggleNewRun}>
        {p.showNewRun ? 'Cancel' : 'New test run'}
      </button>
    </div>
    {p.showNewRun && (
      <div ref={p.formRef} style={{ padding: 12, borderBottom: '1px solid var(--border-soft)', background: 'var(--surface-alt)' }}>
        <div className="form-group">
          <label htmlFor="vv-run-name">Name</label>
          <input
            id="vv-run-name"
            value={p.form.name}
            onChange={(e) => p.onFormChange({ ...p.form, name: e.target.value })}
            placeholder="e.g. Design verification — rev B"
          />
        </div>
        <div className="form-group">
          <label htmlFor="vv-run-description">Description</label>
          <textarea
            id="vv-run-description"
            value={p.form.description}
            onChange={(e) => p.onFormChange({ ...p.form, description: e.target.value })}
            style={{ minHeight: 60 }}
          />
        </div>
        <div className="form-group">
          <label htmlFor="vv-run-baseline">Baseline (optional)</label>
          <select
            id="vv-run-baseline"
            value={p.form.baselineId}
            onChange={(e) => p.onFormChange({ ...p.form, baselineId: e.target.value })}
          >
            <option value="">None (live artifacts)</option>
            {p.baselines.map((b) => (
              <option key={b.id} value={b.id}>
                {baselineLabel(b)}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          style={buttonStyle('primary', { disabled: p.creating || !p.form.name.trim() })}
          onClick={p.onCreate}
          disabled={p.creating || !p.form.name.trim()}
        >
          {p.creating ? 'Creating…' : 'Create run'}
        </button>
      </div>
    )}
    {p.runs.length === 0 && !p.loading ? (
      <div style={{ padding: 16, fontSize: 13, color: 'var(--text-muted)' }}>
        No test runs yet. Create one to start recording results.
      </div>
    ) : (
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
          gap: 10,
          padding: 12,
        }}
      >
        {p.runs.map((run) => (
          <RunCard
            key={run.id}
            projectId={p.projectId}
            run={run}
            results={p.results[run.id]}
            caseCount={p.caseCount}
            baselines={p.baselines}
            onRunStatus={p.onRunStatus}
          />
        ))}
      </div>
    )}
  </section>
);
