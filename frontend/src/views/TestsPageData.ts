// The Tests page's model: each recent run with its counts, and each test
// case with its method, what it verifies, its latest result and the run that
// recorded it. Read from the artifacts, links, test runs and their results,
// and V&V coverage for a result older than the runs read here.
import {
  artifactAPI,
  executionMethodOf,
  linkAPI,
  vvAPI,
  type Artifact,
  type CoverageReport,
  type ExecutionMethod,
  type Link,
  type TestResult,
  type TestRun,
} from '../api/client';

/** How many of the newest runs the page reads the results of. */
export const RECENT_RUNS = 8;

export interface RunSummary {
  run: TestRun;
  pass: number;
  fail: number;
  blocked: number;
  /** The project's test cases with no result in the run yet. */
  toGo: number;
  total: number;
}

export interface CaseRow {
  testCase: Artifact;
  method: ExecutionMethod;
  verifies: Artifact[];
  result: string;
  /** The run that recorded the result; null when it is older than the runs read, or none. */
  run: TestRun | null;
  /** Numbered steps in the test case's text; 0 when it lists none. */
  steps: number;
}

export interface TestsData {
  runs: RunSummary[];
  allRuns: number;
  cases: CaseRow[];
}

/** The numbered steps a test case's text lists ("1. Apply power"). */
export const countSteps = (body: string): number => (body || '').split('\n').filter((l) => /^\s*\d+[.)]\s+\S/.test(l)).length;

export const summarize = (run: TestRun, results: TestResult[], total: number): RunSummary => {
  const n = (s: string) => results.filter((r) => r.status === s).length;
  const pass = n('pass');
  const fail = n('fail');
  const blocked = n('blocked');
  return { run, pass, fail, blocked, toGo: Math.max(0, total - pass - fail - blocked), total };
};

export const buildCases = (
  artifacts: Artifact[],
  links: Link[],
  runs: { run: TestRun; results: TestResult[] }[],
  coverage: CoverageReport | null
): CaseRow[] => {
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const latest = new Map<string, { result: string; run: TestRun | null }>();
  // Newest run first, so the first recorded result per case is its latest.
  for (const { run, results } of runs) {
    for (const r of results) {
      if (r.status === 'not-run' || latest.has(r.test_case_id)) continue;
      latest.set(r.test_case_id, { result: r.status, run });
    }
  }
  for (const e of coverage?.entries || []) {
    for (const [tc, status] of Object.entries(e.latest_results || {})) {
      if (!latest.has(tc) && status) latest.set(tc, { result: status, run: null });
    }
  }
  return artifacts
    .filter((a) => a.type === 'test-case')
    .map((tc) => ({
      testCase: tc,
      method: executionMethodOf(tc),
      verifies: links
        .filter((l) => l.type === 'verifies' && l.from_id === tc.id && byId.has(l.to_id))
        .map((l) => byId.get(l.to_id)!),
      result: latest.get(tc.id)?.result || 'not-run',
      run: latest.get(tc.id)?.run || null,
      steps: countSteps(tc.body),
    }));
};

export const loadTests = async (projectId: string): Promise<TestsData> => {
  const [artifacts, links, runList, coverage] = await Promise.all([
    artifactAPI.list(projectId),
    linkAPI.list(projectId),
    vvAPI.listRuns(projectId),
    vvAPI.coverage(projectId).catch(() => null),
  ]);
  const sorted = [...(runList.data || [])].sort((a, b) => b.started_at.localeCompare(a.started_at));
  const recent = await Promise.all(
    sorted.slice(0, RECENT_RUNS).map(async (run) => ({
      run,
      results: (await vvAPI.listResults(run.id).catch(() => null))?.data || [],
    }))
  );
  const total = artifacts.data.filter((a) => a.type === 'test-case').length;
  return {
    runs: recent.map(({ run, results }) => summarize(run, results, total)),
    allRuns: sorted.length,
    cases: buildCases(artifacts.data, links.data || [], recent, coverage?.data || null),
  };
};

export const RUN_STATE: Record<TestRun['status'], string> = {
  'in-progress': 'in progress',
  completed: 'completed',
  aborted: 'aborted',
};
