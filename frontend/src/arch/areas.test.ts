// The ownership test of refactor plan step N1 (convention K15), from the
// frontend's side: every tracked file of the repository that is not a test
// file belongs to exactly one area of docs/areas.json, no file belongs to
// two, and every glob matches a tracked file. Its Go twin is
// internal/tools/areas/areas_test.go; to see which area owns a path, run
//   go run ./internal/tools/areas which <path>
import { execFileSync } from 'node:child_process';
import { describe, expect, it } from 'vitest';
import { REPO, readRepoFile } from './repo';

interface Area {
  name: string;
  description: string;
  globs: string[];
}

interface AreaIndex {
  test_files: { globs: string[] };
  glossary: { term: string; aka: string[]; meaning: string }[];
  areas: Area[];
}

const INDEX_FILE = 'docs/areas.json';

/**
 * A path glob as an anchored RegExp, with the syntax of glob_regex in
 * scripts/refactor/refactor_guard.py: `**\/` spans any number of
 * directories (none included), a trailing `/**` everything below, and `*`
 * and `?` stay within one path segment.
 */
function globRegExp(glob: string): RegExp {
  let out = '';
  let i = 0;
  while (i < glob.length) {
    if (glob.startsWith('**/', i)) {
      out += '(?:.*/)?';
      i += 3;
    } else if (glob.startsWith('/**', i) && i + 3 === glob.length) {
      out += '(?:/.*)?';
      i += 3;
    } else if (glob.startsWith('**', i)) {
      out += '.*';
      i += 2;
    } else if (glob[i] === '*') {
      out += '[^/]*';
      i += 1;
    } else if (glob[i] === '?') {
      out += '[^/]';
      i += 1;
    } else {
      out += glob[i].replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
      i += 1;
    }
  }
  return new RegExp(`^${out}$`);
}

const index = JSON.parse(readRepoFile(INDEX_FILE)) as AreaIndex;
const testGlobs = index.test_files.globs.map(globRegExp);
const areaGlobs = index.areas.map((a) => ({ area: a, globs: a.globs.map((g) => ({ glob: g, rx: globRegExp(g) })) }));

const isTestFile = (path: string) => testGlobs.some((rx) => rx.test(path));

/** The areas that claim path, each through the first of its globs that matches. */
function claims(path: string): string[] {
  const out: string[] = [];
  for (const { area, globs } of areaGlobs) {
    const hit = globs.find(({ rx }) => rx.test(path));
    if (hit) out.push(`${area.name} (${hit.glob})`);
  }
  return out;
}

function trackedFiles(): string[] {
  const out = execFileSync('git', ['-c', 'core.quotePath=false', 'ls-files', '-z'], {
    cwd: REPO,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  });
  return out.split('\0').filter(Boolean);
}

describe('area index (K15)', () => {
  const files = trackedFiles();

  it('reads the whole repository and a well-formed index', () => {
    expect(files.length).toBeGreaterThan(100);
    const names = index.areas.map((a) => a.name);
    expect(new Set(names).size).toBe(names.length);
    for (const a of index.areas) {
      expect(a.description.trim(), a.name).not.toBe('');
      expect(a.globs.length, a.name).toBeGreaterThan(0);
    }
    for (const t of index.glossary) expect(t.meaning.trim(), t.term).not.toBe('');
  });

  it('gives every non-test file exactly one area, and no file two', () => {
    const problems: string[] = [];
    for (const f of files) {
      const c = claims(f);
      if (c.length > 1) problems.push(`${f} is claimed by ${c.length} areas: ${c.join(', ')}`);
      else if (c.length === 0 && !isTestFile(f)) problems.push(`no area claims ${f}`);
    }
    expect(problems, `fix the globs in ${INDEX_FILE}`).toEqual([]);
  });

  it('matches a tracked file with every glob', () => {
    const unused = areaGlobs.flatMap(({ area, globs }) =>
      globs.filter(({ rx }) => !files.some((f) => rx.test(f))).map(({ glob }) => `${area.name}: ${glob}`)
    );
    expect(unused, `globs in ${INDEX_FILE} that match no tracked file`).toEqual([]);
  });

  it('reads globs the way refactor_guard.py and the areas tool do', () => {
    const cases: [string, string, boolean][] = [
      ['internal/api/*.go', 'internal/api/routes.go', true],
      ['internal/api/*.go', 'internal/api/testdata/x.go', false],
      ['internal/runner/**', 'internal/runner', true],
      ['internal/runner/**', 'internal/runner/testdata/wire/claim.json', true],
      ['internal/runner/**', 'internal/runners/x.go', false],
      ['**/*_test.go', 'main_test.go', true],
      ['frontend/src/api/**/vv*', 'frontend/src/api/vv.ts', true],
      ['frontend/src/api/**/vv*', 'frontend/src/api/types/vv.ts', true],
      ['e2e/*', 'e2e/.gitignore', true],
      ['e2e/*', 'e2e/tests/smoke.spec.ts', false],
      ['Dockerfile*', 'frontend/Dockerfile', false],
      ['cmd/?gentd/**', 'cmd/agentd/main.go', true],
      ['a.b', 'axb', false],
    ];
    for (const [glob, path, want] of cases) expect(globRegExp(glob).test(path), `${glob} on ${path}`).toBe(want);
  });
});
