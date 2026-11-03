// Fixtures the redesign pages' tests share: a small project with a
// requirement, a design item under another, two test cases, links, two
// baselines and a test run in progress. Plain data, no mocks.
import type { Artifact, Link, OrgFeatures } from '../api/client';

export const ok = (data: unknown, headers: Record<string, string> = {}) => Promise.resolve({ data, headers });

export const features = (on: boolean): OrgFeatures =>
  ({ channel: 'nightly', stable_release: '', preview: false, features: { 'redesign-pages': on } }) as OrgFeatures;

export const art = (id: string, type: string, ref: string, title: string, extra: Partial<Artifact> = {}): Artifact => ({
  id,
  project_id: 'p1',
  type,
  ref,
  title,
  body: '',
  attributes: {},
  version: 1,
  status: 'draft',
  valid_from: '2026-10-01T00:00:00Z',
  valid_to: null,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-20T00:00:00Z',
  ...extra,
});

export const ARTIFACTS: Artifact[] = [
  art('req1', 'requirement', 'REQ-1', 'Detect wheel slip', { status: 'in_review' }),
  art('req2', 'requirement', 'REQ-2', 'Warn the driver'),
  art('des1', 'design-item', 'DES-1', 'Sensor front end', { body: 'Conditions the sensor signals.' }),
  art('des2', 'design-item', 'DES-2', 'Wheel speed interface', {
    parent_id: 'des1',
    status: 'approved',
    attributes: { supplier_part: 'WS-400', status: 'approved' },
  }),
  art('tc1', 'test-case', 'TC-1', 'Slip on wet asphalt', { body: '1. Wet the track\n2. Brake hard', attributes: { execution_method: 'physical' } }),
  art('tc2', 'test-case', 'TC-2', 'Detection latency'),
];

const link = (id: string, from: string, to: string, type: string, suspect = false): Link => ({
  id,
  from_id: from,
  to_id: to,
  type,
  suspect,
  attributes: {},
  version: 1,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
});

export const LINKS: Link[] = [
  link('l1', 'des2', 'req1', 'satisfies'),
  link('l2', 'tc1', 'req1', 'verifies'),
  link('l3', 'tc2', 'req1', 'verifies', true),
];

export const COVERAGE = {
  project_id: 'p1',
  entries: [
    {
      requirement_id: 'req1',
      title: 'Detect wheel slip',
      verification_method: 'test',
      verification_status: '',
      test_case_ids: ['tc1', 'tc2'],
      latest_results: { tc1: 'pass', tc2: 'fail' },
      rollup: 'fail',
    },
  ],
  summary: { total: 2, covered: 1, uncovered: 1, verified: 1, pass: 0, fail: 1 },
};

export const RUN = {
  id: 'run1',
  project_id: 'p1',
  name: 'HIL regression',
  description: '',
  status: 'in-progress',
  started_at: '2026-10-21T09:00:00Z',
};

export const RESULTS = [
  { id: 'r1', run_id: 'run1', test_case_id: 'tc1', test_case_version: 1, status: 'pass', notes: '', evidence: [] },
  { id: 'r2', run_id: 'run1', test_case_id: 'tc2', test_case_version: 1, status: 'fail', notes: '', evidence: [] },
];

export const BASELINES = [
  { id: 'bl1', project_id: 'p1', name: 'BL-1', created_at: '2026-09-01T00:00:00Z', created_by_name: 'Sam Example' },
  { id: 'bl2', project_id: 'p1', name: 'BL-2', created_at: '2026-10-12T00:00:00Z' },
];
