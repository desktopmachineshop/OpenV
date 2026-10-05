// Ratchet, no snapshot (the file snapshots beside it regenerate with:
//   npx vitest run src/arch -u).
//
// K14's size budgets for the frontend's production TypeScript (refactor plan
// §4.3, step S12b; internal/archtest holds Go's): a file has at most
// FILE_BUDGET lines and a component at most COMPONENT_BUDGET. What was over
// a budget when this test landed is grandfathered at its size then plus R6's
// headroom (10%, at most 150 lines), and nothing is added: a new file or
// component over its budget is split instead. A ceiling only falls. When a
// change shrinks a grandfathered file or component, lower its ceiling to the
// new size plus headroom, or remove its entry once it is within budget, in
// the same change; never raise or add one. The Refactor guard fails a raised
// or added entry, or a raised constant here, in a commit of any class
// (GUARD_CODE_CEILINGS in scripts/refactor/refactor_guard.py), and lets a
// commit of any class lower one. It reads a map entry only as a quoted key
// and a decimal integer ('Login': 733), and fails anything else in the maps
// but comments (an unquoted key, arithmetic, a hex number) in any class.
// OVER_1000 is the plan's §10 metric, the number of files over 1,000 lines,
// and only falls too.
//
// What is measured: the production sources repo.ts lists (no tests, no
// declaration files, nothing under src/arch), less F2's mock helper
// (src/test), K11's generated code (src/generated) and test data. A file's
// lines are counted as `wc -l` counts them. A component is what a .tsx module
// declares at its top level, with a name that starts with a capital letter:
// a function, a class, or a const bound to a function or to a call that wraps
// one (memo, forwardRef). It runs from its first token to its last, and is
// keyed by its name, so it keeps its ceiling when it moves to another module.
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import ts from 'typescript';
import { productionSources, srcRel } from './repo';

const FILE_BUDGET = 600;
const COMPONENT_BUDGET = 300;
const OVER_1000 = 4;

// Each file over FILE_BUDGET when this test landed: its lines then plus headroom.
const FILE_CEILINGS = {
  'components/ArtifactDetails.tsx': 766,
  'components/ArtifactEditor.tsx': 710,
  'components/ArtifactList.tsx': 738,
  'components/ImageGallery.tsx': 680,
  'components/ProjectList.tsx': 1202,
  'components/UserSettingsPanel.tsx': 761,
  'components/agents/RunDetailPanel.tsx': 733,
  'components/wizard/GuidedChatPanel.tsx': 1009,
  'views/CrewBuilder.tsx': 933,
  'views/GuidedWizard.tsx': 1781,
  'views/KanbanBoard.tsx': 709,
  'views/Login.tsx': 778,
  'views/ModuleView.tsx': 1788,
  'views/OrgSettings.tsx': 665,
  'views/ProjectSettings.tsx': 877,
  'views/ReviewQueue.tsx': 1001,
};

// Each component over COMPONENT_BUDGET when this test landed: its lines then plus headroom.
const COMPONENT_CEILINGS = {
  'AgentEditor': 405,
  'ArtifactDetails': 664,
  'ArtifactEditor': 551,
  'ArtifactHeader': 445,
  'ArtifactList': 673,
  'AutomationsPage': 534,
  'ChatterPanel': 454,
  'CrewBuilder': 880,
  'CrewCanvas': 399,
  'DownloadWizard': 459,
  'GlobalSearch': 341,
  'GuidedChatPanel': 694,
  'GuidedWizard': 1682,
  'HostedRunnerCard': 396,
  'ImageGallery': 563,
  'InterviewChat': 463,
  'InterviewsPage': 462,
  'KanbanBoard': 663,
  'Landing': 405,
  'LinkPanel': 512,
  'Login': 733,
  'ModuleView': 1740,
  'NotificationBell': 589,
  'OrgBillingTab': 336,
  'OrgMembersTab': 376,
  'OrgSettings': 629,
  'OrgUsageTab': 366,
  'PlatformAdmin': 333,
  'ProductOverview': 475,
  'ProjectLayout': 523,
  'ProjectList': 1145,
  'ProjectSettings': 826,
  'ProviderConnectCard': 387,
  'ReviewQueue': 723,
  'RunDetailPanel': 509,
  'RunnerConnectPrompt': 421,
  'TestRunView': 574,
  'TraceabilityMatrix': 374,
  'UserSettingsPanel': 701,
  'VVDashboard': 501,
};

const fileCeilings: Record<string, number> = FILE_CEILINGS;
const componentCeilings: Record<string, number> = COMPONENT_CEILINGS;

/** Lines as `wc -l` counts them. */
function lineCount(text: string): number {
  return text === '' ? 0 : text.split('\n').length - (text.endsWith('\n') ? 1 : 0);
}

interface Measured {
  lines: number;
  where: string;
}

/** The components a .tsx module declares at its top level, with their first line and length. */
function componentsIn(fileName: string, text: string): { name: string; line: number; lines: number }[] {
  if (!fileName.endsWith('.tsx')) return [];
  const sf = ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const lineOf = (pos: number) => sf.getLineAndCharacterOfPosition(pos).line + 1;
  const out: { name: string; line: number; lines: number }[] = [];
  const add = (name: string | undefined, node: ts.Node) => {
    if (!name || !/^[A-Z]/.test(name)) return;
    const line = lineOf(node.getStart(sf));
    out.push({ name, line, lines: lineOf(node.getEnd()) - line + 1 });
  };
  const isComponent = (e: ts.Expression): boolean =>
    ts.isArrowFunction(e) ||
    ts.isFunctionExpression(e) ||
    (ts.isCallExpression(e) && e.arguments.length > 0 && isComponent(e.arguments[0]));
  for (const stmt of sf.statements) {
    if (ts.isFunctionDeclaration(stmt) || ts.isClassDeclaration(stmt)) add(stmt.name?.text, stmt);
    else if (ts.isVariableStatement(stmt)) {
      for (const d of stmt.declarationList.declarations) {
        if (ts.isIdentifier(d.name) && d.initializer && isComponent(d.initializer)) {
          add(d.name.text, stmt.declarationList.declarations.length === 1 ? stmt : d);
        }
      }
    }
  }
  return out;
}

/** frontend/src-relative path of every measured source, sorted. */
function measuredSources(): string[] {
  return productionSources().filter((f) => !/^(test|generated)\/|(^|\/)(testdata|__snapshots__)\//.test(srcRel(f)));
}

function measure(): { files: Map<string, Measured>; components: Map<string, Measured> } {
  const files = new Map<string, Measured>();
  const components = new Map<string, Measured>();
  for (const abs of measuredSources()) {
    const rel = srcRel(abs);
    const text = fs.readFileSync(abs, 'utf8');
    files.set(rel, { lines: lineCount(text), where: rel });
    for (const c of componentsIn(abs, text)) {
      // Two components of one name share a key, and the larger size.
      if (c.lines > (components.get(c.name)?.lines ?? -1)) components.set(c.name, { lines: c.lines, where: `${rel}:${c.line}` });
    }
  }
  return { files, components };
}

/** What is over its budget and not within a grandfathered ceiling. */
function overruns(sizes: Map<string, Measured>, ceilings: Record<string, number>, budget: number, noun: string): string[] {
  const out: string[] = [];
  for (const [key, { lines, where }] of [...sizes].sort(([a], [b]) => (a < b ? -1 : 1))) {
    if (lines <= budget) continue;
    const ceiling = Object.prototype.hasOwnProperty.call(ceilings, key) ? ceilings[key] : undefined;
    const label = where === key ? key : `${key} (${where})`;
    if (ceiling === undefined) {
      out.push(`${label} is ${lines} lines; a ${noun} may have at most ${budget}, so split it (K14). Only the ${noun}s over it when this test landed are grandfathered, and none is added`);
    } else if (lines > ceiling) {
      out.push(`${label} is ${lines} lines, above its grandfathered ceiling of ${ceiling}; shrink it back under the ceiling (R6)`);
    }
  }
  return out;
}

describe('K14 size budgets for production TypeScript', () => {
  const { files, components } = measure();

  it(`files stay within ${FILE_BUDGET} lines, or their grandfathered ceilings`, () => {
    expect(overruns(files, fileCeilings, FILE_BUDGET, 'file')).toEqual([]);
  });

  it(`components stay within ${COMPONENT_BUDGET} lines, or their grandfathered ceilings`, () => {
    expect(overruns(components, componentCeilings, COMPONENT_BUDGET, 'component')).toEqual([]);
  });

  it(`at most ${OVER_1000} files are over 1,000 lines (§10)`, () => {
    const giants = [...files.values()].filter((f) => f.lines > 1000).map((f) => `${f.where} (${f.lines})`);
    expect(giants.length, `files over 1,000 lines: ${giants.join(', ')}; split one rather than add one`).toBeLessThanOrEqual(OVER_1000);
  });

  it('measures every production module, and finds components the ways they are declared', () => {
    expect(files.size).toBeGreaterThan(100);
    expect(components.size).toBeGreaterThan(100);
    const text = [
      'export function Page() {',
      '  const Inner = () => null;',
      '  return <Inner />;',
      '}',
      'export default class Boundary extends Object {}',
      'const Card = memo(forwardRef(function Card() {',
      '  return null;',
      '}));',
      'export const Row = () => <tr />, Cell = function () { return <td />; };',
      'function helper() { return 1; }',
      'const THEME = createTheme({});',
      'const Lazy = lazy(() => import("./Lazy"));',
      '',
    ].join('\n');
    expect(componentsIn('x.tsx', text)).toEqual([
      { name: 'Page', line: 1, lines: 4 },
      { name: 'Boundary', line: 5, lines: 1 },
      { name: 'Card', line: 6, lines: 3 },
      { name: 'Row', line: 9, lines: 1 },
      { name: 'Cell', line: 9, lines: 1 },
      { name: 'Lazy', line: 12, lines: 1 },
    ]);
    expect(componentsIn('x.ts', text)).toEqual([]);
    expect([lineCount(''), lineCount('a'), lineCount('a\n'), lineCount('a\nb\n')]).toEqual([0, 1, 1, 2]);
  });
});
