import type { Artifact, CoverageEntry } from '../api/client';
import { NO_SECTION, countRollups, flattenGaps, gapGroups, groupBySection, sectionOf, tracedCount } from './VVDashboardData';

const art = (id: string, type: string, title: string, parent?: string): Artifact =>
  ({ id, type, title, parent_id: parent || null, ref: id.toUpperCase(), body: '', attributes: {} }) as unknown as Artifact;

const entry = (id: string, rollup: string): CoverageEntry =>
  ({ requirement_id: id, title: id, verification_method: 'test', verification_status: '', test_case_ids: [], latest_results: {}, rollup }) as CoverageEntry;

const artifacts: Record<string, Artifact> = Object.fromEntries(
  [
    art('h1', 'heading', 'System'),
    art('h2', 'heading', 'Braking', 'h1'),
    art('h3', 'heading', 'Pedal', 'h2'),
    art('r1', 'requirement', 'A', 'h3'),
    art('r2', 'requirement', 'B', 'h2'),
    art('h4', 'heading', 'Software'),
    art('r3', 'requirement', 'C', 'h4'),
    art('r4', 'requirement', 'D'),
  ].map((a) => [a.id, a])
);

it('places a requirement in its two outermost headings', () => {
  expect(sectionOf('r1', artifacts)).toEqual({ key: 'h2', name: 'System · Braking' });
  expect(sectionOf('r3', artifacts)).toEqual({ key: 'h4', name: 'Software' });
  expect(sectionOf('r4', artifacts).name).toBe(NO_SECTION);
  expect(sectionOf('gone', artifacts).name).toBe(NO_SECTION);
});

it('groups the report by section, unsectioned last, and counts each', () => {
  const sections = groupBySection(
    [entry('r4', 'uncovered'), entry('r1', 'pass'), entry('r2', 'fail'), entry('r3', 'verified-manually')],
    artifacts
  );
  expect(sections.map((s) => s.name)).toEqual(['System · Braking', 'Software', NO_SECTION]);
  expect(sections[0].counts).toMatchObject({ total: 2, pass: 1, fail: 1 });
  expect(sections[1].counts.pass).toBe(1);
});

it('counts traced requirements as those with a way to be verified', () => {
  const c = countRollups([entry('a', 'pass'), entry('b', 'uncovered'), entry('c', 'method-missing'), entry('d', 'unrun')]);
  expect(tracedCount(c)).toBe(2);
});

it('orders gaps worst first and lists each artifact once', () => {
  const groups = gapGroups({
    requirements_without_test_case: ['r1', 'r2'],
    requirements_failing: ['r2'],
    orphan_test_cases: [],
    something_new: ['x'],
  });
  expect(groups.map((g) => g.key)).toEqual(['requirements_failing', 'requirements_without_test_case', 'something_new']);
  expect(flattenGaps(groups).map((g) => `${g.id}:${g.why}`)).toEqual(['r2:failing', 'r1:no test', 'x:something new']);
});
