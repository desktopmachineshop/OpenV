import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, ColGroupDef, ICellRendererParams } from 'ag-grid-community';
import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';
import {
  Artifact,
  artifactAPI,
  Baseline,
  baselineAPI,
  linkAPI,
  MatrixRow,
  vvAPI,
} from '../api/client';
import { useAppStore } from '../state/store';

const resultColor = (status: string): string => {
  switch ((status || '').toLowerCase()) {
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

const chip = (text: string, background: string, color = 'var(--surface)'): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 8px',
  borderRadius: 10,
  background,
  color,
  fontSize: 11,
  fontWeight: 600,
  margin: '2px 4px 2px 0',
  whiteSpace: 'nowrap',
});

// Unordered pair key for a link's endpoints: a suspect link between the
// requirement and a chip's artifact flags that chip whichever way the link
// points.
const pairKey = (a: string, b: string): string => (a < b ? `${a}|${b}` : `${b}|${a}`);

export const TraceabilityMatrix: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const gridRef = useRef<AgGridReact<MatrixRow>>(null);
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [baselineId, setBaselineId] = useState('live');
  const [rows, setRows] = useState<MatrixRow[]>([]);
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
  // Endpoint pairs of live links currently flagged suspect (issue #131).
  const [suspectPairs, setSuspectPairs] = useState<Set<string>>(new Set());
  const [quickFilter, setQuickFilter] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const effectiveBaseline = baselineId === 'live' ? undefined : baselineId;

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
      .then((res) => {
        const pairs = new Set<string>();
        (res.data || []).forEach((l) => {
          if (l.suspect) {
            pairs.add(pairKey(l.from_id, l.to_id));
          }
        });
        setSuspectPairs(pairs);
      })
      .catch(() => setSuspectPairs(new Set()));
  }, [projectId]);

  useEffect(() => {
    if (!projectId) return;
    setLoading(true);
    vvAPI
      .matrix(projectId, effectiveBaseline)
      .then((res) => {
        setRows(res.data?.rows || []);
        setError('');
      })
      .catch((err: any) => {
        setError(err.response?.data?.error || err.message || 'Failed to load matrix');
      })
      .finally(() => setLoading(false));
  }, [projectId, effectiveBaseline]);

  const titleOf = useCallback(
    (id: string) => artifactMap[id]?.title || id,
    [artifactMap]
  );

  // A chip is suspect when the live link between the row's requirement and
  // the chip's artifact is flagged (either direction). Suspicion is a
  // property of live links, so baseline snapshots don't show it.
  const isSuspectPair = useCallback(
    (requirementId: string | undefined, otherId: string): boolean =>
      baselineId === 'live' && !!requirementId && suspectPairs.has(pairKey(requirementId, otherId)),
    [baselineId, suspectPairs]
  );

  const ChipListRenderer = useCallback(
    (params: ICellRendererParams<MatrixRow>) => {
      const ids: string[] = params.value || [];
      if (ids.length === 0) return <span style={{ color: 'var(--neutral-mid)' }}>—</span>;
      return (
        <div style={{ lineHeight: '20px', whiteSpace: 'normal', padding: '4px 0' }}>
          {ids.map((id) => {
            const suspect = isSuspectPair(params.data?.requirement_id, id);
            return (
              <Link
                key={id}
                to={`/projects/${projectId}/requirements?artifact=${id}`}
                title={
                  suspect
                    ? 'Suspect link: content changed after this link was made — confirm it in the artifact editor or re-approve the artifact'
                    : 'Open in Requirements'
                }
                style={{
                  ...chip(titleOf(id), suspect ? 'var(--tint-yellow)' : 'var(--neutral-soft)', suspect ? 'var(--warning-text)' : 'var(--text)'),
                  ...(suspect ? { border: '1px solid var(--warning)' } : {}),
                  textDecoration: 'none',
                  cursor: 'pointer',
                }}
              >
                {suspect ? '⚠ ' : ''}
                {titleOf(id)}
              </Link>
            );
          })}
        </div>
      );
    },
    [titleOf, projectId, isSuspectPair]
  );

  const TestCaseRenderer = useCallback(
    (params: ICellRendererParams<MatrixRow>) => {
      const row = params.data;
      const ids: string[] = params.value || [];
      if (ids.length === 0) return <span style={{ color: 'var(--neutral-mid)' }}>—</span>;
      return (
        <div style={{ lineHeight: '20px', whiteSpace: 'normal', padding: '4px 0' }}>
          {ids.map((id) => {
            const result = row?.latest_results?.[id] || 'not-run';
            const suspect = isSuspectPair(row?.requirement_id, id);
            return (
              <span
                key={id}
                title={
                  suspect
                    ? 'Suspect link: content changed after this link was made — confirm it in the artifact editor or re-approve the artifact'
                    : undefined
                }
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 5,
                  border: suspect ? '1px solid var(--warning)' : '1px solid var(--neutral-soft)',
                  borderRadius: 10,
                  padding: '2px 8px',
                  fontSize: 11,
                  margin: '2px 4px 2px 0',
                  background: suspect ? 'var(--tint-yellow)' : 'var(--surface)',
                  color: suspect ? 'var(--warning-text)' : 'var(--text)',
                  whiteSpace: 'nowrap',
                }}
              >
                {suspect ? '⚠ ' : ''}
                {titleOf(id)}
                <span
                  title={result}
                  style={{
                    padding: '1px 6px',
                    borderRadius: 8,
                    background: resultColor(result),
                    color: '#fff',
                    fontWeight: 600,
                  }}
                >
                  {result}
                </span>
              </span>
            );
          })}
        </div>
      );
    },
    [titleOf, isSuspectPair]
  );

  const joinTitles = useCallback(
    (ids: string[] | undefined) => (ids || []).map(titleOf).join('; '),
    [titleOf]
  );

  const columnDefs: (ColDef<MatrixRow> | ColGroupDef<MatrixRow>)[] = useMemo(
    () => [
      {
        headerName: 'Requirement',
        field: 'title',
        pinned: 'left',
        minWidth: 240,
        flex: 1,
        wrapText: true,
        autoHeight: true,
        getQuickFilterText: (p) => p.value || '',
        cellRenderer: (p: ICellRendererParams<MatrixRow>) => (
          <div style={{ padding: '4px 0', lineHeight: '20px', whiteSpace: 'normal' }}>
            <div>{p.value}</div>
            {p.data?.requirement_id && (
              <Link
                to={`/projects/${projectId}/impact?artifact=${p.data.requirement_id}`}
                title="Trace what a change to this requirement would affect"
                style={{ fontSize: 11, color: 'var(--accent-strong)', textDecoration: 'none' }}
              >
                Show impact →
              </Link>
            )}
          </div>
        ),
      },
      {
        headerName: 'User Needs',
        children: [
          {
            headerName: 'Linked needs',
            field: 'user_need_ids',
            cellRenderer: ChipListRenderer,
            valueFormatter: (p) => joinTitles(p.value),
            getQuickFilterText: (p) => joinTitles(p.value),
            minWidth: 220,
            flex: 1,
            autoHeight: true,
            wrapText: true,
          },
        ],
      },
      {
        headerName: 'Design',
        children: [
          {
            headerName: 'Design outputs',
            field: 'design_ids',
            cellRenderer: ChipListRenderer,
            valueFormatter: (p) => joinTitles(p.value),
            getQuickFilterText: (p) => joinTitles(p.value),
            minWidth: 220,
            flex: 1,
            autoHeight: true,
            wrapText: true,
          },
        ],
      },
      {
        headerName: 'Test Cases',
        children: [
          {
            headerName: 'Test cases + latest result',
            field: 'test_case_ids',
            cellRenderer: TestCaseRenderer,
            valueFormatter: (p) => joinTitles(p.value),
            getQuickFilterText: (p) => joinTitles(p.value),
            minWidth: 280,
            flex: 1.4,
            autoHeight: true,
            wrapText: true,
          },
        ],
      },
      {
        headerName: 'Hazards',
        children: [
          {
            headerName: 'Linked hazards',
            field: 'hazard_ids',
            cellRenderer: ChipListRenderer,
            valueFormatter: (p) => joinTitles(p.value),
            getQuickFilterText: (p) => joinTitles(p.value),
            minWidth: 200,
            flex: 1,
            autoHeight: true,
            wrapText: true,
          },
        ],
      },
    ],
    [ChipListRenderer, TestCaseRenderer, joinTitles, projectId]
  );

  const exportCsv = () => {
    gridRef.current?.api?.exportDataAsCsv({
      fileName: `traceability_matrix_${new Date().toISOString().slice(0, 10)}.csv`,
    });
  };

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 16, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Traceability Matrix</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>Baseline</label>
          <select
            value={baselineId}
            onChange={(e) => setBaselineId(e.target.value)}
            style={{ width: 220, padding: '6px 10px' }}
          >
            <option value="live">Live</option>
            {baselines.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </div>
        <input
          value={quickFilter}
          onChange={(e) => setQuickFilter(e.target.value)}
          placeholder="Quick filter…"
          style={{ width: 220, padding: '6px 10px' }}
        />
        <div style={{ flex: 1 }} />
        <button className="button-secondary" onClick={exportCsv}>
          Export CSV
        </button>
      </div>

      {baselineId !== 'live' && (
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
          Viewing baseline "{baselines.find((b) => b.id === baselineId)?.name || baselineId}" — this
          is a read-only historical snapshot.
        </div>
      )}
      {error && <div style={{ color: 'var(--danger)', marginBottom: 10, fontSize: 13 }}>{error}</div>}
      {loading && <div style={{ color: 'var(--text-muted)', marginBottom: 10 }}>Loading…</div>}

      <div className="ag-theme-quartz" style={{ flex: 1, minHeight: 480 }}>
        <AgGridReact<MatrixRow>
          ref={gridRef}
          rowData={rows}
          columnDefs={columnDefs}
          quickFilterText={quickFilter}
          getRowId={(p) => p.data.requirement_id}
          defaultColDef={{ resizable: true, sortable: true }}
        />
      </div>
    </div>
  );
};
