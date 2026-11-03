import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, ICellRendererParams } from 'ag-grid-community';
import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';
import {
  AgentDef,
  Artifact,
  EvidenceBundle,
  EvidenceCitation,
  TestResult,
  TestRun,
  agentsAPI,
  artifactAPI,
  evidenceAPI,
  executionMethodOf,
  vvAPI,
} from '../api/client';
import { EvidencePicker } from '../components/EvidencePicker';
import { moveCitations } from './testRunCitations';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';
import { refStyle } from './VVDashboardParts';
import { AgentMark, EvidenceCell, MethodChip, ResultRow, StatusSelect } from './TestRunViewCells';
import { RunHeader, RunNotices } from './TestRunViewHeader';
import { PhoneCaseList } from './TestRunViewPhone';
import { TestRunStepper } from './TestRunViewStepper';

export const TestRunView: React.FC = () => {
  const { isPhone } = useViewport();
  const { projectId, runId } = useParams<{ projectId: string; runId: string }>();
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const [run, setRun] = useState<TestRun | null>(null);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [results, setResults] = useState<TestResult[]>([]);
  // Test case id -> the requirements it verifies, from the live coverage.
  const [verifies, setVerifies] = useState<Record<string, string[]>>({});
  // The results as last rendered, for upsert to find the one a new result
  // supersedes without being re-created on every change.
  const resultsRef = useRef<TestResult[]>([]);
  resultsRef.current = results;
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  // Agent execution
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [agentSlug, setAgentSlug] = useState('');
  const [launching, setLaunching] = useState(false);
  const [launchNotice, setLaunchNotice] = useState<React.ReactNode>(null);

  // Evidence. Citations arrive keyed by result id in one request rather than
  // one per row, and the project's bundles are loaded once for the picker.
  const [citations, setCitations] = useState<Record<string, EvidenceCitation[]>>({});
  const [bundles, setBundles] = useState<EvidenceBundle[]>([]);
  // The case evidence is being cited for, by test case and result.
  const [citing, setCiting] = useState<{ testCaseId: string; resultId: string } | null>(null);
  // On a phone: the case the recorder is open at, or null.
  const [stepAt, setStepAt] = useState<number | null>(null);

  const load = useCallback(() => {
    if (!projectId || !runId) return;
    setLoading(true);
    Promise.all([
      vvAPI.getRun(runId),
      artifactAPI.list(projectId),
      vvAPI.listResults(runId),
      // Evidence and coverage must never be the reason the run fails to
      // open, so they degrade to empty rather than rejecting the batch.
      evidenceAPI.citationsForRun(runId).catch(() => ({ data: {} })),
      evidenceAPI.list(projectId).catch(() => ({ data: [] })),
      vvAPI.coverage(projectId).catch(() => ({ data: null })),
    ])
      .then(([r, arts, res, cites, pool, cov]) => {
        setRun(r.data);
        setArtifacts(arts.data || []);
        setResults(res.data || []);
        setCitations(cites.data || {});
        setBundles(pool.data || []);
        const map: Record<string, string[]> = {};
        (cov.data?.entries || []).forEach((e) =>
          (e.test_case_ids || []).forEach((tc) => {
            (map[tc] = map[tc] || []).push(e.requirement_id);
          })
        );
        setVerifies(map);
        setError('');
      })
      .catch((err: unknown) => setError(apiErrorMessage(err, 'Failed to load test run')))
      .finally(() => setLoading(false));
  }, [projectId, runId]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    agentsAPI
      .list()
      .then((res) => {
        const list = res.data || [];
        setAgents(list);
        setAgentSlug((prev) => (prev && list.some((a) => a.slug === prev) ? prev : list[0]?.slug || ''));
      })
      .catch(() => setAgents([]));
    // activeOrgId: the agent roster is scoped by the X-Org-ID header the API
    // client sends. On a cross-org deep link ProjectLayout switches the active
    // workspace after this first fetch fired, so refetch when it changes —
    // otherwise the "run with agent" picker offers the wrong workspace's
    // agents (issue #112, same pattern as #111).
  }, [activeOrgId]);

  /** Records an outcome; resolves to the new result's id, or null on failure. */
  const upsert = useCallback(
    async (testCaseId: string, status: string, notes: string): Promise<string | null> => {
      if (!runId) return null;
      const superseded = resultsRef.current.find((r) => r.test_case_id === testCaseId);
      try {
        const res = await vvAPI.upsertResult(runId, {
          test_case_id: testCaseId,
          status,
          notes,
        });
        // A result recorded again is a new result, and the server moved the
        // superseded one's citations onto it; the evidence column follows.
        if (superseded) {
          setCitations((prev) => moveCitations(prev, superseded.id, res.data.id));
        }
        setResults((prev) => {
          const idx = prev.findIndex((r) => r.test_case_id === testCaseId);
          if (idx >= 0) {
            const next = [...prev];
            next[idx] = res.data;
            return next;
          }
          return [...prev, res.data];
        });
        setError('');
        return res.data.id;
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Failed to save result'));
        return null;
      }
    },
    [runId]
  );

  // Re-read the run's citations after a change. Only the citation map moves,
  // so the grid's results and test cases are left where they are.
  const refreshCitations = useCallback(async () => {
    if (!runId) return;
    try {
      const res = await evidenceAPI.citationsForRun(runId);
      setCitations(res.data || {});
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Failed to reload the evidence'));
    }
  }, [runId]);

  const rows: ResultRow[] = useMemo(() => {
    const byCase: Record<string, TestResult> = {};
    results.forEach((r) => {
      byCase[r.test_case_id] = r;
    });
    const byId: Record<string, Artifact> = {};
    artifacts.forEach((a) => {
      byId[a.id] = a;
    });
    return artifacts
      .filter((a) => a.type === 'test-case')
      .map((tc) => {
        const r = byCase[tc.id];
        return {
          testCaseId: tc.id,
          ref: tc.ref || '',
          testCaseTitle: tc.title,
          body: tc.body || '',
          executionMethod: executionMethodOf(tc),
          verifies: (verifies[tc.id] || []).map((id) => byId[id]?.ref || byId[id]?.title || id),
          versionTested: r ? r.test_case_version : null,
          status: r ? r.status : 'not-run',
          notes: r ? r.notes : '',
          executedAt: r?.executed_at || null,
          byAgent: Boolean(r?.executed_by_agent_run_id),
          resultId: r ? r.id : null,
          citations: r ? citations[r.id] || [] : [],
        };
      });
  }, [artifacts, results, citations, verifies]);

  const readOnly = run ? run.status !== 'in-progress' : false;
  const citingRow = citing ? rows.find((r) => r.testCaseId === citing.testCaseId) : undefined;

  const columnDefs: ColDef<ResultRow>[] = useMemo(
    () => [
      {
        headerName: 'Ref',
        field: 'ref',
        pinned: 'left',
        width: 96,
        cellRenderer: (p: ICellRendererParams<ResultRow>) => <span style={refStyle}>{p.value}</span>,
      },
      {
        headerName: 'Test case',
        field: 'testCaseTitle',
        pinned: 'left',
        minWidth: 260,
        flex: 2,
      },
      {
        headerName: 'Verified by',
        field: 'executionMethod',
        width: 150,
        cellRenderer: (p: ICellRendererParams<ResultRow>) => (p.data ? <MethodChip method={p.data.executionMethod} /> : null),
      },
      {
        headerName: 'Verifies',
        field: 'verifies',
        width: 140,
        valueFormatter: (p) => (p.value && p.value.length ? p.value.join(', ') : '—'),
        cellStyle: { fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace', fontSize: 12, color: 'var(--accent-text)' },
      },
      {
        headerName: 'Version tested',
        field: 'versionTested',
        width: 140,
        valueFormatter: (p) => (p.value == null ? '—' : `v${p.value}`),
      },
      {
        headerName: 'Status',
        field: 'status',
        width: 140,
        cellRenderer: (p: ICellRendererParams<ResultRow>) => {
          const row = p.data;
          if (!row) return null;
          return (
            <StatusSelect
              value={row.status}
              disabled={readOnly}
              label={`Result of ${row.testCaseTitle}`}
              onChange={(s) => upsert(row.testCaseId, s, row.notes)}
            />
          );
        },
      },
      {
        headerName: 'Notes',
        field: 'notes',
        editable: !readOnly,
        flex: 2,
        minWidth: 220,
      },
      {
        headerName: 'Evidence',
        field: 'citations',
        width: 170,
        sortable: false,
        cellRenderer: (p: ICellRendererParams<ResultRow>) =>
          p.data ? (
            <EvidenceCell
              row={p.data}
              readOnly={readOnly}
              onCite={(row) => row.resultId && setCiting({ testCaseId: row.testCaseId, resultId: row.resultId })}
            />
          ) : null,
      },
      {
        headerName: 'Executed at',
        field: 'executedAt',
        width: 220,
        valueFormatter: (p) => (p.value ? new Date(p.value).toLocaleString() : '—'),
        cellRenderer: (p: ICellRendererParams<ResultRow>) => (
          <span>
            {p.value ? new Date(p.value).toLocaleString() : '—'}
            {p.data?.byAgent && <AgentMark />}
          </span>
        ),
      },
    ],
    [readOnly, upsert]
  );

  const agentRunnable = rows.filter((r) => r.executionMethod === 'automated').length;
  const humanOnly = rows.length - agentRunnable;
  const firstOpen = rows.findIndex((r) => r.status === 'not-run');

  const handleLaunchAgent = async () => {
    if (!runId || !agentSlug) return;
    setLaunching(true);
    setError('');
    setLaunchNotice(null);
    try {
      const res = await vvAPI.launchAgentRun(runId, { agent_slug: agentSlug });
      const { run: agentRun, executing, skipped } = res.data;
      setLaunchNotice(
        <>
          Agent run started for {executing} test case{executing === 1 ? '' : 's'}
          {skipped.length > 0 && (
            <> — {skipped.length} left for a person ({skipped.map((sc) => sc.title).join(', ')})</>
          )}
          .{' '}
          <Link to={`/projects/${projectId}/agent-runs`} style={{ color: 'var(--success-text)', fontWeight: 600 }}>
            Watch run {agentRun.id.slice(0, 8)} →
          </Link>{' '}
          Results appear here as it records them; use Refresh.
        </>
      );
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Failed to start agent run'));
    } finally {
      setLaunching(false);
    }
  };

  const handleRunStatus = async (status: string) => {
    if (!runId) return;
    try {
      const res = await vvAPI.updateRun(runId, status);
      setRun(res.data);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Failed to update run'));
    }
  };

  return (
    <div
      style={{ padding: isPhone ? 12 : 16, display: 'flex', flexDirection: 'column', gap: 12, height: isPhone ? undefined : '100%' }}
      data-help-key="vv.runs"
    >
      <RunHeader
        projectId={projectId || ''}
        run={run}
        rows={rows}
        isPhone={isPhone}
        loading={loading}
        onRefresh={load}
        agents={agents}
        agentSlug={agentSlug}
        onAgentSlug={setAgentSlug}
        launching={launching}
        agentRunnable={agentRunnable}
        onLaunch={handleLaunchAgent}
        onRunStatus={handleRunStatus}
        onRecord={isPhone ? () => setStepAt(readOnly || firstOpen < 0 ? 0 : firstOpen) : undefined}
        recordLabel={
          readOnly
            ? 'Step through the results'
            : firstOpen < 0
              ? 'Review from the first test case'
              : `Record results · continue at ${rows[firstOpen]?.ref || `test case ${firstOpen + 1}`}`
        }
      />
      <RunNotices
        run={run}
        readOnly={readOnly}
        launchNotice={launchNotice}
        humanOnly={humanOnly}
        total={rows.length}
        error={error}
        loading={loading}
      />

      {isPhone ? (
        <PhoneCaseList rows={rows} readOnly={readOnly} onOpen={setStepAt} />
      ) : (
        <div className="ag-theme-quartz" style={{ flex: 1, minHeight: 420, width: '100%' }}>
          <AgGridReact<ResultRow>
            rowData={rows}
            columnDefs={columnDefs}
            getRowId={(p) => p.data.testCaseId}
            onCellValueChanged={(event) => {
              if (event.colDef.field === 'notes' && event.data) {
                upsert(event.data.testCaseId, event.data.status, event.data.notes || '');
              }
            }}
            defaultColDef={{ resizable: true, sortable: true }}
            suppressCellFocus={false}
            stopEditingWhenCellsLoseFocus={true}
          />
        </div>
      )}

      {isPhone && run && stepAt !== null && rows[stepAt] && (
        <TestRunStepper
          run={run}
          rows={rows}
          index={stepAt}
          onIndex={setStepAt}
          readOnly={readOnly}
          onClose={() => setStepAt(null)}
          onRecord={(row, status, notes) => upsert(row.testCaseId, status, notes)}
          onCite={(row, resultId) => setCiting({ testCaseId: row.testCaseId, resultId })}
        />
      )}

      {citing && (
        <EvidencePicker
          resultId={citingRow?.resultId || citing.resultId}
          testCaseTitle={citingRow?.testCaseTitle || ''}
          bundles={bundles}
          cited={citingRow?.citations || []}
          onClose={() => setCiting(null)}
          onChanged={refreshCitations}
        />
      )}
    </div>
  );
};
