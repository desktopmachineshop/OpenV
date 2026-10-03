// Verification and validation: test runs and results, execution methods,
// coverage, the traceability matrix, impact analysis and evidence bundles.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client, downloadBlob, uploadConfig } from './http';
import type { UploadProgressHandler } from './types/attachments';
import type {
  CoverageReport,
  EvidenceBundle,
  EvidenceBundleInput,
  EvidenceCitation,
  EvidenceFile,
  ExecutionMethod,
  ImpactDirection,
  ImpactReport,
  MatrixRow,
  TestResult,
  TestRun,
} from './types/vv';
import type { AgentRun } from './types/agents';

export const EXECUTION_METHODS: { value: ExecutionMethod; label: string; hint: string }[] = [
  { value: 'automated', label: 'Automated', hint: 'An agent or CI job can run this end to end.' },
  { value: 'manual', label: 'Manual (human)', hint: 'Needs a person: inspection, judgement, usability.' },
  { value: 'physical', label: 'Physical test', hint: 'Needs hardware, a rig, or lab measurement.' },
];

// Reads a test case artifact's execution method, defaulting to automated.
export const executionMethodOf = (artifact?: { attributes?: Record<string, any> } | null): ExecutionMethod => {
  const raw = artifact?.attributes?.execution_method;
  if (typeof raw !== 'string') return 'automated';
  const v = raw.trim().toLowerCase();
  return v === 'manual' || v === 'physical' ? v : 'automated';
};

export const evidenceAPI = {
  list: (projectId: string) =>
    client.get<EvidenceBundle[]>(`/api/v1/projects/${projectId}/evidence-bundles`),
  create: (projectId: string, payload: EvidenceBundleInput) =>
    client.post<EvidenceBundle>(`/api/v1/projects/${projectId}/evidence-bundles`, payload),
  get: (id: string) => client.get<EvidenceBundle>(`/api/v1/evidence-bundles/${id}`),
  update: (id: string, payload: EvidenceBundleInput) =>
    client.put<EvidenceBundle>(`/api/v1/evidence-bundles/${id}`, payload),
  remove: (id: string) => client.delete(`/api/v1/evidence-bundles/${id}`),

  uploadFile: (bundleId: string, file: File, onProgress?: UploadProgressHandler) => {
    const form = new FormData();
    form.append('file', file);
    return client.post<EvidenceFile>(
      `/api/v1/evidence-bundles/${bundleId}/files`,
      form,
      uploadConfig(onProgress)
    );
  },
  deleteFile: (fileId: string) => client.delete(`/api/v1/evidence-files/${fileId}`),
  /** The server always answers as a download, so this is a plain link target. */
  downloadUrl: (fileId: string) => `${API_BASE_URL}/api/v1/evidence-files/${fileId}/download`,

  citationsForResult: (resultId: string) =>
    client.get<EvidenceCitation[]>(`/api/v1/test-results/${resultId}/citations`),
  /** Keyed by test result id, so the run grid renders its evidence column in
   *  one request rather than one per row. */
  citationsForRun: (runId: string) =>
    client.get<Record<string, EvidenceCitation[]>>(`/api/v1/test-runs/${runId}/citations`),
  cite: (resultId: string, bundleId: string, note?: string) =>
    client.post<EvidenceCitation>(`/api/v1/test-results/${resultId}/citations`, {
      bundle_id: bundleId,
      note: note || '',
    }),
  uncite: (resultId: string, bundleId: string) =>
    client.delete(`/api/v1/test-results/${resultId}/citations/${bundleId}`),
};

export const vvAPI = {
  createRun: (projectId: string, payload: { name: string; description?: string; baseline_id?: string }) =>
    client.post<TestRun>(`/api/v1/projects/${projectId}/test-runs`, payload),
  listRuns: (projectId: string) =>
    client.get<TestRun[]>(`/api/v1/projects/${projectId}/test-runs`),
  getRun: (id: string) => client.get<TestRun>(`/api/v1/test-runs/${id}`),
  updateRun: (id: string, status: string) =>
    client.put<TestRun>(`/api/v1/test-runs/${id}`, { status }),
  deleteRun: (id: string) => client.delete(`/api/v1/test-runs/${id}`),
  upsertResult: (runId: string, payload: { test_case_id: string; status: string; notes?: string; evidence?: string[] }) =>
    client.post<TestResult>(`/api/v1/test-runs/${runId}/results`, payload),
  listResults: (runId: string) =>
    client.get<TestResult[]>(`/api/v1/test-runs/${runId}/results`),
  // Launch an agent run that executes this test run's agent-executable cases.
  // Manual/physical cases are excluded server-side and returned as `skipped`.
  launchAgentRun: (runId: string, payload: { agent_slug: string; test_case_ids?: string[] }) =>
    client.post<{
      run: AgentRun;
      executing: number;
      skipped: { id: string; title: string; execution_method: ExecutionMethod }[];
    }>(`/api/v1/test-runs/${runId}/agent-run`, payload),
  coverage: (projectId: string, baselineId?: string) =>
    client.get<CoverageReport>(`/api/v1/projects/${projectId}/vv/coverage`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  matrix: (projectId: string, baselineId?: string) =>
    client.get<{ rows: MatrixRow[] }>(`/api/v1/projects/${projectId}/vv/matrix`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  gaps: (projectId: string, baselineId?: string) =>
    client.get<Record<string, string[]>>(`/api/v1/projects/${projectId}/vv/gaps`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  report: (projectId: string, baselineId?: string) =>
    downloadBlob(
      `/api/v1/projects/${projectId}/vv/report${baselineId ? `?baseline_id=${baselineId}` : ''}`,
      `vv_report_${new Date().toISOString().slice(0, 10)}.pdf`
    ),
  // Change-impact analysis: the artifacts reachable from `artifactId` through
  // the traceability link graph, grouped by type. `direction` selects
  // downstream (things that depend on it), upstream (what it depends on), or
  // both (default).
  impact: (
    projectId: string,
    artifactId: string,
    direction?: ImpactDirection,
    baselineId?: string
  ) =>
    client.get<ImpactReport>(`/api/v1/projects/${projectId}/impact`, {
      params: {
        artifact: artifactId,
        ...(direction ? { direction } : {}),
        ...(baselineId ? { baseline_id: baselineId } : {}),
      },
    }),
};
