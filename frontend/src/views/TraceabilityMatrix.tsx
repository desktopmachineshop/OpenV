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
import { useViewport } from '../hooks/useViewport';
import { baselineLabel } from '../utils/baselines';
import { Chip, buttonStyle, chipStyle } from '../components/ui';
import { TraceabilityMatrixPhone } from './TraceabilityMatrixPhone';

// A linked artifact as a chip: neutral, or a warning tint while its link
// is suspect. Weight 500 rather than the status chip's 700: these name
// artifacts rather than state a status.
const linkChip = (suspect: boolean): React.CSSProperties => ({
  ...chipStyle(suspect ? 'warning' : 'neutral'),
  fontWeight: 500,
  color: suspect ? 'var(--warning-text)' : 'var(--text)',
  margin: '2px 4px 2px 0',
  whiteSpace: 'normal',
  maxWidth: '100%',
  textDecoration: 'none',
  cursor: 'pointer',
});

const SUSPECT_TITLE =
  'Suspect link: content changed after this link was made — confirm it in the artifact editor or re-approve the artifact';

// Unordered pair key for a link's endpoints: a suspect link between the
// requirement and a chip's artifact flags that chip whichever way the link
// points.
const pairKey = (a: string, b: string): string => (a < b ? `${a}|${b}` : `${b}|${a}`);

export const TraceabilityMatrix: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  // A phone keeps requirement, needs and test cases; design outputs and
  // hazards are a swipe too far and can be exported instead.
  const { isPhone } = useViewport();

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
                title={suspect ? SUSPECT_TITLE : 'Open in Requirements'}
                // The link is a finger-sized target around the chip.
                style={{ display: 'inline-flex', alignItems: 'center', minHeight: isPhone ? 32 : 24, verticalAlign: 'middle', textDecoration: 'none' }}
              >
                <span style={linkChip(suspect)}>
                  {suspect ? '⚠ ' : ''}
                  {titleOf(id)}
                </span>
              </Link>
            );
          })}
        </div>
      );
    },
    [titleOf, projectId, isSuspectPair, isPhone]
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
              <span key={id} title={suspect ? SUSPECT_TITLE : undefined} style={{ ...linkChip(suspect), cursor: 'default' }}>
                {suspect ? '⚠ ' : ''}
                {titleOf(id)}
                <Chip title={`latest result: ${result}`}>{result}</Chip>
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
        minWidth: isPhone ? 150 : 240,
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
                style={{ fontSize: 12, color: 'var(--accent-strong)', textDecoration: 'none', display: 'inline-block', padding: '6px 0' }}
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
        hide: isPhone,
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
        hide: isPhone,
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
    [ChipListRenderer, TestCaseRenderer, joinTitles, projectId, isPhone]
  );

  const exportCsv = () => {
    gridRef.current?.api?.exportDataAsCsv({
      fileName: `traceability_matrix_${new Date().toISOString().slice(0, 10)}.csv`,
    });
  };

  const compactGrid = rows.length <= 25;
  // Nothing linked at all: every row is a requirement with empty columns.
  const nothingLinked =
    rows.length > 0 &&
    rows.every((r) => !(r.user_need_ids || []).length && !(r.design_ids || []).length && !(r.test_case_ids || []).length && !(r.hazard_ids || []).length);

  return (
    <div style={{ padding: isPhone ? 12 : 16, display: 'flex', flexDirection: 'column', gap: 12, height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }} data-help-key="matrix">
        <h2 style={{ color: 'var(--text)', margin: 0, fontSize: 20 }}>Traceability Matrix</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <label htmlFor="matrix-baseline" style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)' }}>
            Baseline
          </label>
          <select
            id="matrix-baseline"
            value={baselineId}
            onChange={(e) => setBaselineId(e.target.value)}
            style={{ width: isPhone ? 180 : 220, height: 32, padding: '0 8px', fontSize: 13 }}
          >
            <option value="live">Live</option>
            {baselines.map((b) => (
              <option key={b.id} value={b.id}>
                {baselineLabel(b)}
              </option>
            ))}
          </select>
        </div>
        <input
          type="search"
          aria-label="Quick filter"
          value={quickFilter}
          onChange={(e) => setQuickFilter(e.target.value)}
          placeholder="Quick filter…"
          style={{ width: 220, height: 32, padding: '0 10px', fontSize: 13 }}
        />
        <div style={{ flex: 1 }} />
        {!isPhone && <button type="button" style={buttonStyle('secondary', { disabled: rows.length === 0 })} onClick={exportCsv} disabled={rows.length === 0}>
          Export CSV
        </button>}
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
      {error && <div style={{ color: 'var(--danger-strong)', fontSize: 13 }}>{error}</div>}
      {loading && <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>}

      {nothingLinked && (
        <div
          style={{
            background: 'var(--tint-blue)',
            border: '1px solid var(--tint-blue-border)',
            color: 'var(--accent-text)',
            borderRadius: 6,
            padding: '8px 12px',
            fontSize: 13,
          }}
        >
          No requirement is linked to anything yet. Link a requirement to the user need it derives
          from and the test case that verifies it, and the matrix fills in.
        </div>
      )}

      {!loading && !error && rows.length === 0 ? (
        <section
          aria-label="Empty matrix"
          data-help-key="matrix"
          style={{
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 6,
            padding: '32px 16px',
            textAlign: 'center',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 10,
          }}
        >
          <h3 style={{ margin: 0, fontSize: 16 }}>Nothing to trace yet</h3>
          <p style={{ margin: 0, maxWidth: 520, color: 'var(--text-muted)', fontSize: 14 }}>
            {baselineId === 'live'
              ? 'The matrix has a row for each requirement, showing the user needs it derives from, the design that realises it, the test cases that verify it and the hazards it mitigates. This project has no requirements yet.'
              : 'This baseline holds no requirements.'}
          </p>
          {baselineId === 'live' && (
            <Link to={`/projects/${projectId}/requirements`} style={{ ...buttonStyle('primary'), textDecoration: 'none' }}>
              Write requirements
            </Link>
          )}
        </section>
      ) : isPhone ? (
        <TraceabilityMatrixPhone
          rows={rows}
          query={quickFilter}
          projectId={projectId || ''}
          artifactMap={artifactMap}
          isSuspectPair={isSuspectPair}
        />
      ) : (
        <div
          className="ag-theme-quartz"
          style={compactGrid ? { width: '100%' } : { flex: 1, minHeight: isPhone ? 320 : 480, width: '100%' }}
          data-help-key="matrix"
        >
          <AgGridReact<MatrixRow>
            ref={gridRef}
            // A short matrix is as tall as its rows, not a near-empty pane.
            domLayout={compactGrid ? 'autoHeight' : 'normal'}
            rowData={rows}
            columnDefs={columnDefs}
            quickFilterText={quickFilter}
            getRowId={(p) => p.data.requirement_id}
            defaultColDef={{ resizable: true, sortable: true }}
          />
        </div>
      )}
    </div>
  );
};
