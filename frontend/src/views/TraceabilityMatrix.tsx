import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { AgGridReact } from 'ag-grid-react';
import { ColDef, ColGroupDef, ICellRendererParams } from 'ag-grid-community';
import 'ag-grid-community/styles/ag-grid.css';
import 'ag-grid-community/styles/ag-theme-quartz.css';
import {
  Artifact,
  artifactAPI,
  Baseline,
  baselineAPI,
  MatrixRow,
  vvAPI,
} from '../api/client';
import { useAppStore } from '../state/store';

const resultColor = (status: string): string => {
  switch ((status || '').toLowerCase()) {
    case 'pass':
      return '#27ae60';
    case 'fail':
      return '#e74c3c';
    case 'blocked':
      return '#f39c12';
    default:
      return '#95a5a6';
  }
};

const chip = (text: string, background: string, color = '#fff'): React.CSSProperties => ({
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

export const TraceabilityMatrix: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const gridRef = useRef<AgGridReact<MatrixRow>>(null);
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [baselineId, setBaselineId] = useState('live');
  const [rows, setRows] = useState<MatrixRow[]>([]);
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
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

  const ChipListRenderer = useCallback(
    (params: ICellRendererParams<MatrixRow>) => {
      const ids: string[] = params.value || [];
      if (ids.length === 0) return <span style={{ color: '#bdc3c7' }}>—</span>;
      return (
        <div style={{ lineHeight: '20px', whiteSpace: 'normal', padding: '4px 0' }}>
          {ids.map((id) => (
            <span key={id} style={chip(titleOf(id), '#ecf0f1', '#2c3e50')}>
              {titleOf(id)}
            </span>
          ))}
        </div>
      );
    },
    [titleOf]
  );

  const TestCaseRenderer = useCallback(
    (params: ICellRendererParams<MatrixRow>) => {
      const row = params.data;
      const ids: string[] = params.value || [];
      if (ids.length === 0) return <span style={{ color: '#bdc3c7' }}>—</span>;
      return (
        <div style={{ lineHeight: '20px', whiteSpace: 'normal', padding: '4px 0' }}>
          {ids.map((id) => {
            const result = row?.latest_results?.[id] || 'not-run';
            return (
              <span
                key={id}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 5,
                  border: '1px solid #ecf0f1',
                  borderRadius: 10,
                  padding: '2px 8px',
                  fontSize: 11,
                  margin: '2px 4px 2px 0',
                  background: '#fff',
                  color: '#2c3e50',
                  whiteSpace: 'nowrap',
                }}
              >
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
    [titleOf]
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
    [ChipListRenderer, TestCaseRenderer, joinTitles]
  );

  const exportCsv = () => {
    gridRef.current?.api?.exportDataAsCsv({
      fileName: `traceability_matrix_${new Date().toISOString().slice(0, 10)}.csv`,
    });
  };

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 16, flexWrap: 'wrap' }}>
        <h2 style={{ color: '#2c3e50', margin: 0 }}>Traceability Matrix</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ margin: 0, fontSize: 13, color: '#7f8c8d' }}>Baseline</label>
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
            background: '#fef5e7',
            border: '1px solid #f39c12',
            color: '#9c6408',
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
      {error && <div style={{ color: '#e74c3c', marginBottom: 10, fontSize: 13 }}>{error}</div>}
      {loading && <div style={{ color: '#7f8c8d', marginBottom: 10 }}>Loading…</div>}

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
