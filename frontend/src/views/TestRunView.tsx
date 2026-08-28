import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, ICellRendererParams } from 'ag-grid-community';
import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';
import { Artifact, artifactAPI, TestResult, TestRun, vvAPI } from '../api/client';

const STATUS_OPTIONS = ['pass', 'fail', 'blocked', 'not-run'];

const statusColor = (status: string): string => {
  switch (status) {
    case 'pass':
      return 'var(--success)';
    case 'fail':
      return 'var(--danger)';
    case 'blocked':
      return 'var(--warning)';
    default:
      return 'var(--neutral)';
  }
};

const runStatusColor = (status: string): string => {
  switch (status) {
    case 'in-progress':
      return 'var(--accent)';
    case 'completed':
      return 'var(--success)';
    case 'aborted':
      return 'var(--danger)';
    default:
      return 'var(--neutral)';
  }
};

interface ResultRow {
  testCaseId: string;
  testCaseTitle: string;
  versionTested: number | null;
  status: string;
  notes: string;
  executedAt: string | null;
}

export const TestRunView: React.FC = () => {
  const { projectId, runId } = useParams<{ projectId: string; runId: string }>();
  const [run, setRun] = useState<TestRun | null>(null);
  const [testCases, setTestCases] = useState<Artifact[]>([]);
  const [results, setResults] = useState<TestResult[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    if (!projectId || !runId) return;
    setLoading(true);
    Promise.all([
      vvAPI.getRun(runId),
      artifactAPI.list(projectId, 'test-case'),
      vvAPI.listResults(runId),
    ])
      .then(([r, tc, res]) => {
        setRun(r.data);
        setTestCases(tc.data || []);
        setResults(res.data || []);
        setError('');
      })
      .catch((err: any) => {
        setError(err.response?.data?.error || err.message || 'Failed to load test run');
      })
      .finally(() => setLoading(false));
  }, [projectId, runId]);

  useEffect(() => {
    load();
  }, [load]);

  const upsert = useCallback(
    async (testCaseId: string, status: string, notes: string) => {
      if (!runId) return;
      try {
        const res = await vvAPI.upsertResult(runId, {
          test_case_id: testCaseId,
          status,
          notes,
        });
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
      } catch (err: any) {
        setError(err.response?.data?.error || err.message || 'Failed to save result');
      }
    },
    [runId]
  );

  const rows: ResultRow[] = useMemo(() => {
    const byCase: Record<string, TestResult> = {};
    results.forEach((r) => {
      byCase[r.test_case_id] = r;
    });
    return testCases.map((tc) => {
      const r = byCase[tc.id];
      return {
        testCaseId: tc.id,
        testCaseTitle: tc.title,
        versionTested: r ? r.test_case_version : null,
        status: r ? r.status : 'not-run',
        notes: r ? r.notes : '',
        executedAt: r?.executed_at || null,
      };
    });
  }, [testCases, results]);

  const readOnly = run ? run.status !== 'in-progress' : false;

  const StatusRenderer = useCallback(
    (params: ICellRendererParams<ResultRow>) => {
      const row = params.data;
      if (!row) return null;
      return (
        <select
          value={row.status}
          disabled={readOnly}
          onChange={(e) => upsert(row.testCaseId, e.target.value, row.notes)}
          style={{
            background: statusColor(row.status),
            color: '#fff',
            border: 'none',
            borderRadius: 12,
            padding: '3px 10px',
            fontSize: 12,
            fontWeight: 600,
            cursor: readOnly ? 'default' : 'pointer',
            width: 'auto',
          }}
        >
          {STATUS_OPTIONS.map((s) => (
            <option key={s} value={s} style={{ background: 'var(--surface)', color: 'var(--text)' }}>
              {s}
            </option>
          ))}
        </select>
      );
    },
    [upsert, readOnly]
  );

  const columnDefs: ColDef<ResultRow>[] = useMemo(
    () => [
      {
        headerName: 'Test case',
        field: 'testCaseTitle',
        pinned: 'left',
        minWidth: 260,
        flex: 2,
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
        cellRenderer: StatusRenderer,
      },
      {
        headerName: 'Notes',
        field: 'notes',
        editable: !readOnly,
        flex: 2,
        minWidth: 220,
      },
      {
        headerName: 'Executed at',
        field: 'executedAt',
        width: 190,
        valueFormatter: (p) => (p.value ? new Date(p.value).toLocaleString() : '—'),
      },
    ],
    [StatusRenderer, readOnly]
  );

  const handleRunStatus = async (status: string) => {
    if (!runId) return;
    try {
      const res = await vvAPI.updateRun(runId, status);
      setRun(res.data);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to update run');
    }
  };

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 16, flexWrap: 'wrap' }}>
        <Link
          to={`/projects/${projectId}/vv`}
          style={{ color: 'var(--accent)', textDecoration: 'none', fontSize: 13 }}
        >
          ← Back to V&amp;V
        </Link>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>{run?.name || 'Test run'}</h2>
        {run && (
          <span
            style={{
              padding: '2px 10px',
              borderRadius: 12,
              background: runStatusColor(run.status),
              color: '#fff',
              fontSize: 12,
              fontWeight: 600,
            }}
          >
            {run.status}
          </span>
        )}
        <div style={{ flex: 1 }} />
        {run?.status === 'in-progress' && (
          <>
            <button className="button" onClick={() => handleRunStatus('completed')}>
              Complete run
            </button>
            <button
              style={{
                background: 'var(--danger)',
                color: '#fff',
                border: 'none',
                padding: '10px 20px',
                borderRadius: 4,
                cursor: 'pointer',
                fontSize: 14,
              }}
              onClick={() => handleRunStatus('aborted')}
            >
              Abort run
            </button>
          </>
        )}
      </div>

      {run?.description && (
        <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>{run.description}</div>
      )}
      {readOnly && run && (
        <div
          style={{
            background: 'var(--tint-yellow)',
            border: '1px solid var(--warning)',
            color: 'var(--warning-text)',
            borderRadius: 4,
            padding: '8px 12px',
            marginBottom: 12,
            fontSize: 13,
          }}
        >
          This run is {run.status}; results are read-only.
        </div>
      )}
      {error && <div style={{ color: 'var(--danger)', marginBottom: 10, fontSize: 13 }}>{error}</div>}
      {loading && <div style={{ color: 'var(--text-muted)', marginBottom: 10 }}>Loading…</div>}

      <div className="ag-theme-quartz" style={{ flex: 1, minHeight: 420 }}>
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
    </div>
  );
};
