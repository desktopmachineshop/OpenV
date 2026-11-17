// Every stylesheet closes every block it opens. A rule left open does not
// fail the build: the browser reads what follows as rules nested inside it
// (CSS nesting), so every later rule in the bundle stops matching. 0.27.0
// shipped that way after a merge dropped two closing braces in index.css, and
// the projects page lost its cards. Comments and strings are skipped, so a
// brace inside either does not count.
import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { SRC, srcRel } from './repo';

function stylesheets(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const abs = path.join(dir, e.name);
    if (e.isDirectory()) return stylesheets(abs);
    return e.name.endsWith('.css') ? [abs] : [];
  });
}

/** The line of each block left open at the end, or of a stray close. */
export function unbalanced(css: string): number[] {
  const open: number[] = [];
  const stray: number[] = [];
  let line = 1;
  for (let i = 0; i < css.length; i++) {
    const c = css[i];
    if (c === '\n') line++;
    else if (c === '/' && css[i + 1] === '*') {
      const end = css.indexOf('*/', i + 2);
      const stop = end === -1 ? css.length : end + 2;
      line += (css.slice(i, stop).match(/\n/g) || []).length;
      i = stop - 1;
    } else if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < css.length && css[j] !== c && css[j] !== '\n') j += css[j] === '\\' ? 2 : 1;
      i = j;
    } else if (c === '{') open.push(line);
    else if (c === '}') {
      if (open.length) open.pop();
      else stray.push(line);
    }
  }
  return [...open, ...stray];
}

describe('stylesheets', () => {
  it('finds an unclosed rule and a stray brace', () => {
    expect(unbalanced('a {\n  color: red;\n\nb { color: blue; }\n')).toEqual([1]);
    expect(unbalanced('a { color: red; }\n}\n')).toEqual([2]);
    expect(unbalanced("/* { */ a { content: '{'; }")).toEqual([]);
  });

  it.each(stylesheets(SRC).map((f) => [srcRel(f), f]))('%s closes every block it opens', (_rel, abs) => {
    expect(unbalanced(fs.readFileSync(abs, 'utf8'))).toEqual([]);
  });
});
