// The Tests page: the newest test runs as cards with a stacked pass / fail /
// blocked bar, then the project's test cases as a table (method, the
// requirements each verifies, its last result and the run that recorded it,
// its number of steps), with the runs as a second tab. A phone leads with the
// run in progress and what is failing or blocked. Gated by redesign-pages.
import React, { useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { apiErrorMessage } from '../api/errors';
import { Chip, ErrorBanner, FeatureGate, PanelHeader, ResultBar, buttonStyle, ellipsis, mutedStyle, pageType, panelStyle, refStyle } from '../components/ui';
import { REDESIGN_PAGES_FEATURE } from '../features';
import { useViewport } from '../hooks/useViewport';
import { CaseTable, RunCard, RunTable, runLine } from './TestsPageParts';
import { loadTests, RUN_STATE, type TestsData } from './TestsPageData';
import { EXECUTION_METHODS, type ExecutionMethod } from '../api/client';

type Tab = 'cases' | 'runs';

const tabStyle = (on: boolean): React.CSSProperties => ({
  minHeight: 38,
  padding: '0 10px',
  border: 'none',
  borderBottom: `2px solid ${on ? 'var(--accent)' : 'transparent'}`,
  background: 'transparent',
  color: on ? 'var(--text)' : 'var(--text-muted)',
  fontSize: 13,
  fontWeight: on ? 600 : 500,
  cursor: 'pointer',
});

const Tabs: React.FC<{ tab: Tab; onTab: (t: Tab) => void; cases: number; runs: number; phone?: boolean }> = ({ tab, onTab, cases, runs, phone }) => {
  const list: [Tab, string, number][] = phone
    ? [
        ['runs', 'Runs', runs],
        ['cases', 'Test cases', cases],
      ]
    : [
        ['cases', 'Test cases', cases],
        ['runs', 'Runs', runs],
      ];
  return (
    <div role="tablist" aria-label="Test view" style={{ display: 'flex', gap: 2 }}>
      {list.map(([t, label, n]) => (
        <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => onTab(t)} style={tabStyle(tab === t)}>
          {label} <span style={{ color: 'var(--text-muted)', fontWeight: 500 }}>{n}</span>
        </button>
      ))}
    </div>
  );
};

const PhoneTests: React.FC<{ data: TestsData; projectId: string; tab: Tab; onTab: (t: Tab) => void }> = ({ data, projectId, tab, onTab }) => {
  const active = data.runs.find((s) => s.run.status === 'in-progress');
  const attention = data.cases.filter((c) => c.result === 'fail' || c.result === 'blocked');
  const rowLink: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    minHeight: 48,
    padding: '6px 12px',
    borderBottom: '1px solid var(--border-soft)',
    color: 'var(--text)',
    textDecoration: 'none',
  };
  return (
    <div data-help-pane="tests" style={{ ...pageType(true), padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Tabs tab={tab} onTab={onTab} cases={data.cases.length} runs={data.allRuns} phone />
      {tab === 'runs' ? (
        <>
          {active && (
            <section aria-label="Run in progress" style={{ ...panelStyle, borderColor: 'var(--tint-blue-border)', padding: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
              <span style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <b style={{ ...ellipsis, flex: 1 }}>{active.run.name}</b>
                <Chip>{RUN_STATE[active.run.status]}</Chip>
              </span>
              <ResultBar pass={active.pass} fail={active.fail} blocked={active.blocked} total={active.total} height={8} />
              <span style={mutedStyle}>{runLine(active)}</span>
              <Link to={`/projects/${projectId}/vv/runs/${active.run.id}`} style={buttonStyle('primary')}>
                Continue the run
              </Link>
            </section>
          )}
          <section aria-label="Recent runs" style={panelStyle} data-help-key="tests.runs">
            <PanelHeader title="Recent runs" />
            {data.runs.filter((s) => s !== active).map((s) => (
              <Link key={s.run.id} to={`/projects/${projectId}/vv/runs/${s.run.id}`} style={{ ...rowLink, flexDirection: 'column', alignItems: 'stretch' }}>
                <span style={{ display: 'flex', gap: 8 }}>
                  <b style={{ ...ellipsis, flex: 1 }}>{s.run.name}</b>
                  <Chip>{RUN_STATE[s.run.status]}</Chip>
                </span>
                <ResultBar pass={s.pass} fail={s.fail} blocked={s.blocked} total={s.total} />
                <span style={mutedStyle}>{runLine(s)}</span>
              </Link>
            ))}
            {data.runs.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>No test runs yet.</p>}
          </section>
          <section aria-label="Failing or blocked" style={panelStyle}>
            <PanelHeader title="Failing or blocked" count={attention.length} />
            {attention.map((c) => (
              <Link key={c.testCase.id} to={`/projects/${projectId}/requirements?artifact=${c.testCase.id}`} style={rowLink}>
                <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                  <span style={mutedStyle}>
                    <span style={refStyle}>{c.testCase.ref}</span> verifies {c.verifies.map((r) => r.ref || r.title).join(', ') || 'nothing yet'}
                  </span>
                  <span style={ellipsis}>{c.testCase.title}</span>
                </span>
                <Chip>{c.result}</Chip>
              </Link>
            ))}
            {attention.length === 0 && <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>Nothing is failing or blocked.</p>}
          </section>
        </>
      ) : (
        <section aria-label="Test cases" style={panelStyle} data-help-key="tests.cases">
          {data.cases.map((c) => (
            <Link key={c.testCase.id} to={`/projects/${projectId}/requirements?artifact=${c.testCase.id}`} style={rowLink}>
              <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                <span style={mutedStyle}>
                  <span style={refStyle}>{c.testCase.ref}</span> · {c.method}
                </span>
                <span style={ellipsis}>{c.testCase.title}</span>
              </span>
              <Chip>{c.result}</Chip>
            </Link>
          ))}
        </section>
      )}
    </div>
  );
};

const filterChip = (on: boolean): React.CSSProperties => ({
  minHeight: 26,
  padding: '0 8px',
  borderRadius: 6,
  fontSize: 12,
  fontWeight: on ? 600 : 500,
  border: on ? '1px solid var(--tint-blue-border)' : '1px dashed var(--border)',
  background: on ? 'var(--tint-blue)' : 'var(--surface)',
  color: on ? 'var(--accent-text)' : 'var(--text-secondary)',
  cursor: 'pointer',
});

const TestsContent: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { isPhone } = useViewport();
  const [data, setData] = useState<TestsData | null>(null);
  const [error, setError] = useState('');
  const [tab, setTab] = useState<Tab | null>(null);
  const [attentionOnly, setAttentionOnly] = useState(false);
  const [method, setMethod] = useState<ExecutionMethod | ''>('');

  useEffect(() => {
    let live = true;
    loadTests(projectId)
      .then((d) => live && setData(d))
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load the tests')));
    return () => {
      live = false;
    };
  }, [projectId]);

  const rows = useMemo(
    () =>
      (data?.cases || []).filter(
        (c) => (!attentionOnly || c.result === 'fail' || c.result === 'blocked') && (!method || c.method === method)
      ),
    [data, attentionOnly, method]
  );

  if (error) return <ErrorBanner message={error} />;
  if (!data) return <div style={{ padding: 16, color: 'var(--text-muted)' }}>Loading…</div>;
  if (isPhone) return <PhoneTests data={data} projectId={projectId} tab={tab || 'runs'} onTab={setTab} />;
  const current = tab || 'cases';
  return (
    <div data-help-pane="tests" style={{ ...pageType(false), padding: '12px 16px 16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
      <section
        aria-label="Recent test runs"
        data-help-key="tests.runs"
        style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: 10 }}
      >
        {data.runs.slice(0, 4).map((s) => (
          <RunCard key={s.run.id} s={s} projectId={projectId} />
        ))}
        {data.runs.length === 0 && (
          <p style={{ ...mutedStyle, margin: 0 }}>
            No test runs yet. Start one on the <Link to={`/projects/${projectId}/vv`}>V&amp;V</Link> page.
          </p>
        )}
      </section>
      <div style={panelStyle} data-help-key="tests.cases">
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 16px', padding: '0 12px', borderBottom: '1px solid var(--border)' }}>
          <Tabs tab={current} onTab={setTab} cases={data.cases.length} runs={data.allRuns} />
          <span style={{ flexGrow: 1 }} />
          {current === 'cases' && (
            <div style={{ display: 'flex', gap: 6, padding: '6px 0' }}>
              <button type="button" aria-pressed={attentionOnly} onClick={() => setAttentionOnly((v) => !v)} style={filterChip(attentionOnly)}>
                Last result: fail, blocked
              </button>
              <select
                aria-label="Method"
                value={method}
                onChange={(e) => setMethod(e.target.value as ExecutionMethod | '')}
                style={{ ...filterChip(method !== ''), margin: 0, width: 'auto' }}
              >
                <option value="">Method: any</option>
                {EXECUTION_METHODS.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
        {current === 'cases' ? <CaseTable rows={rows} projectId={projectId} /> : <RunTable runs={data.runs} projectId={projectId} />}
        {current === 'runs' && data.allRuns > data.runs.length && (
          <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>
            The newest {data.runs.length} of {data.allRuns} runs. Every run is on the <Link to={`/projects/${projectId}/vv`}>V&amp;V</Link> page.
          </p>
        )}
      </div>
    </div>
  );
};

export const TestsPage: React.FC = () => (
  <FeatureGate feature={REDESIGN_PAGES_FEATURE} name="Tests">
    <TestsContent />
  </FeatureGate>
);
