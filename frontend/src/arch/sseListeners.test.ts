// Regenerate the snapshot (only in a PR that means to change what the app
// listens for):
//   npx vitest run src/arch -u
//
// The SSE events the frontend listens for (refactor plan S6, invariant I9):
// every addEventListener call, and every onmessage, onerror and onopen
// handler, on a value the type checker types as EventSource, across the
// production sources. Each name is resolved by the checker (a literal, a
// const, a string-literal type, or a parameter bound at each call of its
// function), and each server event must be one the server sends:
// contracts/sse-events.json, which TestSSEContract writes from the Go
// sources. A rename on either side fails here or there. The snapshot holds
// names only, never a file, so moving a listener into another module (X15's
// hooks/useEventStream) leaves it unchanged while each name still resolves;
// the files appear in failure messages instead. onerror also receives the
// server's own "error" event, so it counts as a listener for it.
import ts from 'typescript';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { REGENERATE, lineOf, literalText, productionSources, readRepoFile, srcRel, tsProgram, walk } from './repo';

/** The file the Go test writes; its events[].name are what the server sends. */
const CONTRACT = 'contracts/sse-events.json';

/** EventSource's own connection events, which the server never names. */
const CONNECTION_EVENTS = new Set(['open']);

/** The EventSource handler properties and the event each one receives. */
const HANDLER_PROPERTIES: Record<string, string> = { onmessage: 'message', onerror: 'error', onopen: 'open' };

interface Listener {
  /** The event name, or <unresolved: expression> when the scan cannot tell. */
  name: string;
  at: string;
}

let program: ts.Program;
let checker: ts.TypeChecker;
let sources: ts.SourceFile[];

beforeAll(() => {
  const files = productionSources();
  program = tsProgram(files);
  checker = program.getTypeChecker();
  sources = files.map((f) => program.getSourceFile(f)!);
}, 120_000);

const at = (node: ts.Node) => `${srcRel(node.getSourceFile().fileName)}:${lineOf(node)}`;

const symbolOf = (node: ts.Node) => {
  const sym = checker.getSymbolAtLocation(node);
  return sym && sym.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(sym) : sym;
};

/** Whether an expression is an EventSource (null and undefined aside). */
function isEventSource(expr: ts.Expression): boolean {
  const type = checker.getNonNullableType(checker.getTypeAtLocation(expr));
  return type.getSymbol()?.getName() === 'EventSource';
}

/**
 * The event names an expression can hold: a literal, a value of a string
 * literal type or a union of them (a const, say), or a parameter, read at
 * every call of its function with that call's argument. null when the scan
 * cannot tell.
 */
function resolveNames(expr: ts.Expression, depth = 0): string[] | null {
  const lit = literalText(expr);
  if (lit !== null) return [lit];
  const type = checker.getTypeAtLocation(expr);
  if (type.isStringLiteral()) return [type.value];
  if (type.isUnion() && type.types.every((t) => t.isStringLiteral())) {
    return type.types.map((t) => (t as ts.StringLiteralType).value);
  }
  if (depth >= 4 || !ts.isIdentifier(expr)) return null;
  const decl = symbolOf(expr)?.valueDeclaration;
  if (!decl || !ts.isParameter(decl) || !ts.isFunctionLike(decl.parent)) return null;
  const fn = decl.parent;
  const index = fn.parameters.indexOf(decl);
  const nameNode = ts.isVariableDeclaration(fn.parent) ? fn.parent.name : (fn as ts.FunctionDeclaration).name;
  const target = nameNode && symbolOf(nameNode);
  if (!target) return null;
  const names: string[] = [];
  let calls = 0;
  for (const sf of sources) {
    let failed = false;
    walk(sf, (node) => {
      if (failed || !ts.isCallExpression(node) || symbolOf(node.expression) !== target) return;
      calls += 1;
      const arg = node.arguments[index];
      const got = arg ? resolveNames(arg, depth + 1) : null;
      if (got === null) failed = true;
      else names.push(...got);
    });
    if (failed) return null;
  }
  return calls > 0 ? names : null;
}

function collectListeners(): Listener[] {
  const out: Listener[] = [];
  const add = (names: string[] | null, node: ts.Node, expr: ts.Node) => {
    for (const name of names ?? [`<unresolved: ${expr.getText().replace(/\s+/g, ' ')}>`]) out.push({ name, at: at(node) });
  };
  for (const sf of sources) {
    walk(sf, (node) => {
      // es.addEventListener(name, handler)
      if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
        const access = node.expression;
        if (access.name.text === 'addEventListener' && isEventSource(access.expression)) {
          const arg = node.arguments[0];
          add(arg ? resolveNames(arg) : null, node, arg ?? node);
        }
        return;
      }
      // es.onerror = handler
      if (
        ts.isBinaryExpression(node) &&
        node.operatorToken.kind === ts.SyntaxKind.EqualsToken &&
        ts.isPropertyAccessExpression(node.left) &&
        Object.prototype.hasOwnProperty.call(HANDLER_PROPERTIES, node.left.name.text) &&
        isEventSource(node.left.expression)
      ) {
        add([HANDLER_PROPERTIES[node.left.name.text]], node, node.left);
      }
    });
  }
  return out;
}

interface Contract {
  events: { name: string; streams: string[] }[];
}

function serverEvents(): string[] {
  const contract = JSON.parse(readRepoFile(CONTRACT)) as Contract;
  return contract.events.map((e) => e.name);
}

let cached: Listener[] | undefined;
const listeners = () => (cached ??= collectListeners());
const uniq = (items: string[]) => [...new Set(items)].sort();

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('SSE listeners', () => {
  it('are found at all (the scan has not gone blind)', () => {
    expect(listeners().length).toBeGreaterThan(0);
    expect(serverEvents().length).toBeGreaterThan(0);
  });

  it('resolve every event name', () => {
    const unresolved = listeners().filter((l) => l.name.startsWith('<unresolved'));
    expect(
      unresolved.map((l) => `${l.at}  ${l.name}`),
      'name each event with a literal, a const or a string-literal type the checker can read'
    ).toEqual([]);
  });

  it(`listen only for events the server sends (${CONTRACT})`, () => {
    const sent = new Set(serverEvents());
    const stray = listeners().filter((l) => !CONNECTION_EVENTS.has(l.name) && !sent.has(l.name));
    expect(
      stray.map((l) => `${l.at}  ${l.name}`),
      `the server sends only: ${[...sent].sort().join(', ')} (${CONTRACT}, written by TestSSEContract in ` +
        'internal/api/sse_contract_test.go); an event is renamed on both sides together, in a behavior-change PR'
    ).toEqual([]);
  });

  it('match the pinned list', async () => {
    const all = listeners();
    const names = uniq(all.map((l) => l.name));
    const text = [
      '# SSE events the frontend listens for (invariant I9): each addEventListener, onmessage, onerror and onopen',
      `# on an EventSource, by event name. Every server event must be in ${CONTRACT}.`,
      `# Regenerate: ${REGENERATE}. Deployed servers send these names; renaming one silently drops its events.`,
      '',
      `events: ${names.length}`,
      '',
      ...names.map((n) => (CONNECTION_EVENTS.has(n) ? `${n}  (EventSource connection event)` : n)),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/sseListeners.txt');
  });
});
