import type { Artifact, CoverageEntry, Link } from '../api/client';
import { ago, buildFacts, moduleFigures, traceDirection } from './artifactFacts';

const art = (id: string, type: string, parent: string | null = null): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type,
    title: id,
    body: '',
    attributes: {},
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '',
    updated_at: '',
  }) as Artifact;

const link = (id: string, from: string, to: string, type: string, suspect = false): Link =>
  ({ id, from_id: from, to_id: to, type, suspect, attributes: {}, version: 1, created_at: '', updated_at: '' }) as Link;

const entry = (id: string, rollup: string, results: Record<string, string> = {}): CoverageEntry => ({
  requirement_id: id,
  title: id,
  verification_method: 'test',
  verification_status: '',
  test_case_ids: Object.keys(results),
  latest_results: results,
  rollup,
});

//  hdg
//    need
//    req-1  derives from need; verified by tc (pass)
//    req-2  derives from need (suspect); fails
//  tc
const ARTS = [art('hdg', 'heading'), art('need', 'user-need', 'hdg'), art('req-1', 'requirement', 'hdg'), art('req-2', 'requirement', 'hdg'), art('tc', 'test-case')];
const LINKS = [
  link('l1', 'req-1', 'need', 'derives-from'),
  link('l2', 'req-2', 'need', 'derives-from', true),
  link('l3', 'tc', 'req-1', 'verifies'),
  link('l4', 'hdg', 'tc', 'relates-to'),
];
const COVERAGE = [entry('req-1', 'pass', { tc: 'pass' }), entry('req-2', 'fail')];

describe('artifact facts', () => {
  it('reads a link upwards from the artifact that answers to another, and downwards from the other end', () => {
    expect(traceDirection(LINKS[0], 'req-1')).toBe('up');
    expect(traceDirection(LINKS[0], 'need')).toBe('down');
    expect(traceDirection(link('d', 'sys', 'sub', 'decomposes-to'), 'sys')).toBe('down');
    expect(traceDirection(link('d', 'sys', 'sub', 'decomposes-to'), 'sub')).toBe('up');
    expect(traceDirection(LINKS[3], 'hdg')).toBe('peer');
  });

  it('counts trace, suspect links and verification per row', () => {
    const facts = buildFacts(ARTS, LINKS, { 'req-1': { score: 80, band: 'good', findingCount: 1 } }, COVERAGE);
    expect(facts.get('need')).toMatchObject({ up: 0, down: 2, suspect: 1, vv: { status: 'none' } });
    expect(facts.get('req-1')).toMatchObject({ up: 1, down: 1, suspect: 0, vv: { status: 'pass' }, quality: { score: 80 } });
    expect(facts.get('req-2').vv.status).toBe('fail');
    expect(facts.get('tc').vv).toEqual({ status: 'pass', label: 'pass' });
    // A heading rolls up what is below it: one of two requirements passes, one fails.
    expect(facts.get('hdg').vv).toEqual({ status: 'fail', label: 'fail', count: '1/2' });
    expect(facts.verifiedBelow('hdg')).toEqual({ passing: 1, total: 2, failing: 1, blocked: 0 });
    expect(facts.testResult('tc')).toBe('pass');
    expect(facts.testResult('other')).toBe('not-run');
  });

  it('figures the list: traced and verified shares, suspect links and quality findings', () => {
    expect(moduleFigures(ARTS, LINKS, { 'req-1': { score: 80, band: 'good', findingCount: 2 } }, COVERAGE)).toEqual({
      artifacts: 5,
      tracedPct: 100,
      verifiedPct: 50,
      suspectLinks: 1,
      qualityFindings: 2,
    });
    expect(moduleFigures([], [], undefined, [])).toMatchObject({ tracedPct: null, verifiedPct: null });
  });

  it('says how long ago in the dense rows’ words', () => {
    const now = Date.parse('2026-06-10T12:00:00Z');
    expect(ago('2026-06-10T11:30:00Z', now)).toBe('30m');
    expect(ago('2026-06-10T07:00:00Z', now)).toBe('5h');
    expect(ago('2026-06-07T12:00:00Z', now)).toBe('3d');
    expect(ago('2026-05-20T12:00:00Z', now)).toBe('3w');
    expect(ago(undefined, now)).toBe('');
  });
});
