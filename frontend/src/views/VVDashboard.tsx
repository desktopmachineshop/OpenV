import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Artifact,
  artifactAPI,
  Baseline,
  baselineAPI,
  CoverageReport,
  linkAPI,
  proposalsAPI,
  TestResult,
  TestRun,
  vvAPI,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, buttonStyle, useConfirm } from '../components/ui';
import { useViewport } from '../hooks/useViewport';
import { baselineLabel } from '../utils/baselines';
import { FILL, StatCard } from './VVDashboardParts';
import { countRollups, gapGroups, groupBySection, percent, tracedCount } from './VVDashboardData';
import { SectionCoverage } from './VVDashboardSections';
import { ActiveRunCard, GapsCard } from './VVDashboardSide';
import { NewRunForm, RunsSection } from './VVDashboardRuns';

// The runs whose results the page reads for their bars: the most recent
// ones, so a project with a long history does not fan out a request per run.
const RUNS_WITH_RESULTS = 12;

const EMPTY_FORM: NewRunForm = { name: '', description: '', baselineId: '' };

/** Runs in progress first, then the most recently started. */
const byRecency = (runs: TestRun[]): TestRun[] =>
  [...runs].sort((a, b) => {
    const live = Number(b.status === 'in-progress') - Number(a.status === 'in-progress');
    if (live !== 0) return live;
    return (b.started_at || '').localeCompare(a.started_at || '');
  });

export const VVDashboard: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const { isPhone } = useViewport();
  const confirm = useConfirm();

  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [baselineId, setBaselineId] = useState<string>('live');
  const [coverage, setCoverage] = useState<CoverageReport | null>(null);
  const [gaps, setGaps] = useState<Record<string, string[]>>({});
  const [runs, setRuns] = useState<TestRun[]>([]);
  const [runResults, setRunResults] = useState<Record<string, TestResult[]>>({});
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
  // Live links flagged suspect; null until read, or when it could not be.
  const [suspect, setSuspect] = useState<number | null>(null);
  const [pendingProposals, setPendingProposals] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const [showNewRun, setShowNewRun] = useState(false);
  const [form, setForm] = useState<NewRunForm>(EMPTY_FORM);
  const [creating, setCreating] = useState(false);
  const formRef = useRef<HTMLDivElement>(null);

  const effectiveBaseline = baselineId === 'live' ? undefined : baselineId;

  // Baselines, the artifact map, suspect links and pending proposals: once.
  useEffect(() => {
    if (!projectId) return;
    baselineAPI
      .list(projectId)
      .then((res) => setBaselines(res.data || []))
      .catch(() => setBaselines([]));
    artifactAPI
      .list(projectId)
      .then((res) => {
        const map: Record<string, Artifact> = {};
        (res.data || []).forEach((a) => {
          map[a.id] = a;
        });
        setArtifactMap(map);
      })
      .catch(() => setArtifactMap({}));
    linkAPI
      .list(projectId)
      .then((res) => setSuspect((res.data || []).filter((l) => l.suspect).length))
      .catch(() => setSuspect(null));
    proposalsAPI
      .list({ project_id: projectId, status: 'pending' })
      .then((res) => setPendingProposals((res.data || []).length))
      .catch(() => setPendingProposals(0));
  }, [projectId]);

  const loadVV = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    setError('');
    Promise.all([
      vvAPI.coverage(projectId, effectiveBaseline),
      vvAPI.gaps(projectId, effectiveBaseline),
      vvAPI.listRuns(projectId),
    ])
      .then(([cov, gp, rn]) => {
        setCoverage(cov.data);
        setGaps(gp.data || {});
        setRuns(byRecency(rn.data || []));
      })
      .catch((err: unknown) => setError(apiErrorMessage(err, 'Failed to load V&V data')))
      .finally(() => setLoading(false));
  }, [projectId, effectiveBaseline]);

  useEffect(() => {
    loadVV();
  }, [loadVV]);

  // Each recent run's results, for its bar. A run whose results cannot be
  // read simply shows no bar.
  useEffect(() => {
    let live = true;
    runs.slice(0, RUNS_WITH_RESULTS).forEach((run) => {
      vvAPI
        .listResults(run.id)
        .then((res) => {
          if (live) setRunResults((prev) => ({ ...prev, [run.id]: res.data || [] }));
        })
        .catch(() => undefined);
    });
    return () => {
      live = false;
    };
  }, [runs]);

  const entries = useMemo(() => coverage?.entries || [], [coverage]);
  const totals = useMemo(() => countRollups(entries), [entries]);
  const sections = useMemo(() => groupBySection(entries, artifactMap), [entries, artifactMap]);
  const groups = useMemo(() => gapGroups(gaps), [gaps]);
  const caseCount = useMemo(
    () => Object.values(artifactMap).filter((a) => a.type === 'test-case').length,
    [artifactMap]
  );
  const activeRun = runs.find((r) => r.status === 'in-progress');
  const traced = tracedCount(totals);
  const noTest = (gaps.requirements_without_test_case || []).length;
  const showSuspect = baselineId === 'live' && suspect !== null;

  const handleCreateRun = async () => {
    if (!form.name.trim() || !projectId) return;
    setCreating(true);
    try {
      await vvAPI.createRun(projectId, {
        name: form.name.trim(),
        description: form.description,
        baseline_id: form.baselineId || undefined,
      });
      setForm(EMPTY_FORM);
      setShowNewRun(false);
      loadVV();
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Failed to create run'));
    } finally {
      setCreating(false);
    }
  };

  const handleRunStatus = async (runId: string, status: 'completed' | 'aborted') => {
    if (status === 'aborted') {
      const ok = await confirm({
        title: 'Abort test run',
        message: 'Abort this test run? Its recorded results are kept, but the run ends.',
        confirmLabel: 'Abort run',
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await vvAPI.updateRun(runId, status);
      loadVV();
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Failed to update run'));
    }
  };

  const openNewRun = () => {
    setShowNewRun(true);
    window.requestAnimationFrame(() => formRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'center' }));
  };

  return (
    <div style={{ padding: isPhone ? 12 : 16, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0, fontSize: 20 }}>V&amp;V coverage</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }} data-help-key="vv.baselines">
          <label htmlFor="vv-baseline" style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
            Baseline
          </label>
          <select
            id="vv-baseline"
            value={baselineId}
            onChange={(e) => setBaselineId(e.target.value)}
            style={{ width: isPhone ? 180 : 220, maxWidth: '100%', height: 32, padding: '0 8px', fontSize: 13 }}
          >
            <option value="live">Live</option>
            {baselines.map((b) => (
              <option key={b.id} value={b.id}>
                {baselineLabel(b)}
              </option>
            ))}
          </select>
        </div>
        <div style={{ flex: 1 }} />
        <button
          type="button"
          style={buttonStyle('secondary')}
          onClick={() => vvAPI.report(projectId, effectiveBaseline)}
          data-help-key="vv.report"
          title="Download the V&V report as a PDF for the live project or the baseline shown"
        >
          Export report (PDF)
        </button>
        <button type="button" style={buttonStyle('primary')} onClick={openNewRun} data-help-key="vv.runs">
          New test run
        </button>
      </div>

      {baselineId !== 'live' && (
        <div
          style={{
            background: 'var(--tint-yellow)',
            border: '1px solid var(--tint-yellow-border)',
            color: 'var(--warning-text)',
            borderRadius: 6,
            padding: '8px 12px',
            fontSize: 13,
          }}
        >
          Viewing baseline "{baselines.find((b) => b.id === baselineId)?.name || baselineId}" — this
          is a read-only historical snapshot.
        </div>
      )}

      <ErrorBanner message={error} onDismiss={() => setError('')} />
      {loading && <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>}

      <div
        data-help-key="vv.coverage"
        style={{
          display: 'grid',
          gridTemplateColumns: `repeat(auto-fit, minmax(${isPhone ? 140 : 170}px, 1fr))`,
          gap: isPhone ? 8 : 10,
        }}
      >
        <StatCard
          label={isPhone ? 'Traced' : 'Requirements traced'}
          value={isPhone ? `${percent(traced, totals.total)}%` : traced}
          sub={isPhone ? undefined : `of ${totals.total} · ${percent(traced, totals.total)}%`}
          fill={totals.total ? traced / totals.total : 0}
          barColor={FILL.accent}
        />
        <StatCard
          label={isPhone ? 'Verified' : 'Verified (pass)'}
          value={isPhone ? `${percent(totals.pass, totals.total)}%` : totals.pass}
          sub={isPhone ? undefined : `${percent(totals.pass, totals.total)}%`}
          fill={totals.total ? totals.pass / totals.total : 0}
          barColor={FILL.pass}
        />
        <StatCard
          label="Failing"
          value={totals.fail}
          valueColor={totals.fail ? 'var(--danger-strong)' : undefined}
          sub={isPhone ? undefined : totals.blocked ? `requirements · ${totals.blocked} blocked` : 'requirements'}
          fill={isPhone ? undefined : totals.total ? totals.fail / totals.total : 0}
          barColor={FILL.fail}
        />
        {showSuspect && (
          <StatCard
            label="Suspect links"
            value={suspect}
            valueColor={suspect ? 'var(--warning-text)' : undefined}
            sub={isPhone ? undefined : 'need confirming'}
          />
        )}
        {(!isPhone || !showSuspect) && (
          <StatCard
            label="No test case"
            value={noTest}
            sub={isPhone ? undefined : totals.unrun ? `gaps · ${totals.unrun} not run` : 'gaps'}
            fill={isPhone ? undefined : totals.total ? noTest / totals.total : 0}
            barColor={FILL.notRun}
          />
        )}
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'flex-start' }}>
        <SectionCoverage
          projectId={projectId}
          sections={sections}
          totals={totals}
          artifacts={artifactMap}
          isPhone={isPhone}
          loading={loading}
        />
        <div style={{ flex: '1 1 320px', maxWidth: isPhone ? undefined : 420, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12 }}>
          {activeRun && (
            <ActiveRunCard
              projectId={projectId}
              run={activeRun}
              results={runResults[activeRun.id]}
              caseCount={caseCount}
              baselines={baselines}
            />
          )}
          <GapsCard
            projectId={projectId}
            groups={groups}
            artifacts={artifactMap}
            pendingProposals={pendingProposals}
            isPhone={isPhone}
            loading={loading}
          />
        </div>
      </div>

      <RunsSection
        projectId={projectId}
        runs={runs}
        results={runResults}
        caseCount={caseCount}
        baselines={baselines}
        loading={loading}
        showNewRun={showNewRun}
        onToggleNewRun={() => (showNewRun ? setShowNewRun(false) : openNewRun())}
        form={form}
        onFormChange={setForm}
        creating={creating}
        onCreate={handleCreateRun}
        onRunStatus={handleRunStatus}
        formRef={formRef}
      />
    </div>
  );
};
