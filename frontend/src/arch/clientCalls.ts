// Test-only: every METHOD PATH the frontend's API layer can send, read from
// the TypeScript AST of src/api (tests excluded) with the type checker, so
// the result does not depend on which file a wrapper lives in or what the
// axios instance is called. Three shapes are found:
//   axios   a method call on an AxiosInstance: client.get('/api/v1/...')
//   helper  a call to a function that hands its argument to the instance
//           (downloadBlob('/api/v1/...'))
//   url     a plain URL built on API_BASE_URL for EventSource, <a>, <img>
//           or a sign-in redirect; always a GET, and outside the interceptors
import path from 'node:path';
import ts from 'typescript';
import { SRC, lineOf, literalText, parseFile, productionSources, readRepoFile, srcRel, tsProgram, walk } from './repo';

export interface CallSite {
  /** One source call site; a path whose segment is a union of literals yields one CallSite per member. */
  site: number;
  method: string;
  /** The path as the client writes it, dynamic segments as {}. */
  template: string;
  /** Where the call sits: artifactAPI.list, downloadBlob, ... */
  owner: string;
  via: 'axios' | 'helper' | 'url';
  /** Why the template needed more than "${x} between slashes is {}". */
  normalised: string[];
  /** For failure messages only. */
  at: string;
}

const HTTP_METHODS = new Set(['get', 'post', 'put', 'delete', 'patch', 'head', 'options']);

/** The module-level name a node sits under, e.g. artifactAPI.list. */
function ownerOf(node: ts.Node): string {
  const names: string[] = [];
  const topLevel = (n: ts.Node) =>
    ts.isSourceFile(n.parent) || (!!n.parent?.parent?.parent && ts.isSourceFile(n.parent.parent.parent));
  for (let n: ts.Node | undefined = node.parent; n; n = n.parent) {
    if ((ts.isPropertyAssignment(n) || ts.isMethodDeclaration(n)) && ts.isObjectLiteralExpression(n.parent)) {
      names.unshift(n.name.getText());
    } else if (ts.isVariableDeclaration(n) && ts.isIdentifier(n.name) && topLevel(n)) {
      names.unshift(n.name.text);
      return names.join('.');
    } else if (ts.isFunctionDeclaration(n) && n.name && topLevel(n)) {
      names.unshift(n.name.text);
      return names.join('.');
    }
  }
  return names.join('.') || '(module)';
}

/** The string literals a union type allows, or null if it allows any string. */
function literalUnion(type: ts.Type): string[] | null {
  const members = type.isUnion() ? type.types : [type];
  if (!members.every((t) => t.isStringLiteral())) return null;
  return members.map((t) => (t as ts.StringLiteralType).value).sort();
}

/**
 * Turn a path expression into templates, or null if it is not a literal.
 * `${x}` between slashes is a {} segment, unless x's type is a union of
 * string literals, which expands to one template per member. `${x}` glued
 * to the end of the path is a query string, dropped like a literal `?...`.
 */
function templatesOf(
  expr: ts.Expression,
  checker: ts.TypeChecker
): { templates: string[]; normalised: string[]; base: boolean } | null {
  const text = literalText(expr);
  if (text !== null) return { templates: [text.split(/[?#]/)[0]], normalised: [], base: false };
  if (!ts.isTemplateExpression(expr)) return null;
  const normalised: string[] = [];
  let outs = [expr.head.text];
  let base = false;
  let inQuery = expr.head.text.includes('?');
  expr.templateSpans.forEach((span, i) => {
    const code = span.expression.getText().replace(/\s+/g, ' ');
    const next = span.literal.text;
    const prev = outs[0];
    if (i === 0 && prev === '' && code === 'API_BASE_URL') {
      base = true;
    } else if (inQuery) {
      // a query value: dropped with the rest of the query string
    } else if (prev.endsWith('/')) {
      const options = literalUnion(checker.getTypeAtLocation(span.expression));
      if (options) {
        normalised.push(`\${${code}} is one of ${options.join('|')}`);
        outs = outs.flatMap((o) => options.map((opt) => o + opt));
      } else {
        outs = outs.map((o) => o + '{}');
      }
    } else if (next === '' || next.startsWith('?')) {
      normalised.push(`\${${code}} is a query-string suffix`);
    } else {
      outs = outs.map((o) => o + `{?${code}}`);
    }
    outs = outs.map((o) => o + next);
    if (outs[0].includes('?')) inQuery = true;
  });
  return { templates: outs.map((o) => o.split(/[?#]/)[0]), normalised, base };
}

let cached: CallSite[] | null = null;

export function clientCallSites(): CallSite[] {
  if (cached) return cached;
  const files = productionSources(path.join(SRC, 'api'));
  const program = tsProgram(files);
  const checker = program.getTypeChecker();
  const isAxios = (e: ts.Expression) => checker.typeToString(checker.getTypeAtLocation(e)) === 'AxiosInstance';
  const sites: CallSite[] = [];
  const consumed = new Set<ts.Node>();
  const helpers = new Map<ts.Symbol, { index: number; method: string }>();

  let site = 0;
  const add = (arg: ts.Expression, method: string, via: CallSite['via'], owner: string) => {
    const t = templatesOf(arg, checker);
    if (!t) return false;
    consumed.add(arg);
    site++;
    for (const template of t.templates) {
      sites.push({
        site,
        method,
        template,
        owner,
        via,
        normalised: t.normalised,
        at: `${srcRel(arg.getSourceFile().fileName)}:${lineOf(arg)}`,
      });
    }
    return true;
  };

  const sources = files.map((f) => program.getSourceFile(f)!).filter(Boolean);
  // Pass 1: axios calls, and the helpers that pass a parameter to one.
  for (const sf of sources) {
    walk(sf, (node) => {
      if (!ts.isCallExpression(node) || !ts.isPropertyAccessExpression(node.expression)) return;
      const method = node.expression.name.text;
      if (!HTTP_METHODS.has(method) || !isAxios(node.expression.expression)) return;
      const arg = node.arguments[0];
      if (!arg) return;
      if (add(arg, method.toUpperCase(), 'axios', ownerOf(node))) return;
      // Not a literal: a helper forwarding one of its parameters?
      const sym = ts.isIdentifier(arg) ? checker.getSymbolAtLocation(arg) : undefined;
      const param = sym?.valueDeclaration;
      if (param && ts.isParameter(param)) {
        const fn = param.parent;
        const index = fn.parameters.indexOf(param);
        const nameNode = ts.isFunctionDeclaration(fn)
          ? fn.name
          : ts.isVariableDeclaration(fn.parent) && ts.isIdentifier(fn.parent.name)
            ? fn.parent.name
            : undefined;
        const fnSym = nameNode && checker.getSymbolAtLocation(nameNode);
        if (fnSym) {
          helpers.set(fnSym, { index, method: method.toUpperCase() });
          return;
        }
      }
      // Neither: an unreadable path, which the routes test reports.
      sites.push({
        site: ++site,
        method: method.toUpperCase(),
        template: `{?${arg.getText()}}`,
        owner: ownerOf(node),
        via: 'axios',
        normalised: [],
        at: `${srcRel(sf.fileName)}:${lineOf(node)}`,
      });
    });
  }
  // Pass 2: helper calls and URL builders.
  for (const sf of sources) {
    walk(sf, (node) => {
      if (ts.isCallExpression(node)) {
        const sym = checker.getSymbolAtLocation(node.expression);
        const target = sym && sym.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(sym) : sym;
        const helper = target && helpers.get(target);
        const arg = helper && node.arguments[helper.index];
        if (helper && arg) add(arg, helper.method, 'helper', ownerOf(node));
      }
      if (ts.isTemplateExpression(node) && !consumed.has(node)) {
        const t = templatesOf(node, checker);
        if (t?.base) add(node, 'GET', 'url', ownerOf(node));
      }
    });
  }
  cached = sites;
  return sites;
}

/** Literal '/api/...' strings in src/api that are not one of the call sites. */
export function strayApiLiterals(): string[] {
  const sites = new Set(clientCallSites().map((s) => s.at));
  const stray: string[] = [];
  for (const file of productionSources(path.join(SRC, 'api'))) {
    walk(parseFile(file), (node) => {
      const text = literalText(node) ?? (ts.isTemplateExpression(node) ? node.head.text : null);
      if (text === null || !text.startsWith('/api/')) return;
      const at = `${srcRel(file)}:${lineOf(node)}`;
      if (sites.has(at)) return;
      const call = node.parent;
      const prefixTest =
        ts.isCallExpression(call) &&
        ts.isPropertyAccessExpression(call.expression) &&
        ['includes', 'startsWith'].includes(call.expression.name.text);
      if (!prefixTest) stray.push(`${at} ${node.getText()}`);
    });
  }
  return stray;
}

export interface Route {
  method: string;
  path: string;
}

/** internal/api/testdata/routes.txt: the server's registered METHOD PATH pairs. */
export function serverRoutes(): Route[] {
  return readRepoFile('internal/api/testdata/routes.txt')
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const [method, p] = l.split(/\s+/);
      return { method, path: p };
    });
}

const isParam = (seg: string) => /^\{[^}]*\}$/.test(seg);

/**
 * The server routes a call can reach. Exact first: literal equals literal
 * and a client {} equals a route {param}. Only when nothing matches exactly
 * are a client {} against a route literal, or a client literal against a
 * route {param}, accepted, and each is reported as a normalisation.
 */
export function matchSite(site: CallSite, routes: Route[]): { paths: string[]; normalised: string[] } {
  const client = site.template.split('/');
  const candidates = routes.filter((r) => r.method === site.method && r.path.split('/').length === client.length);
  const exact = candidates.filter((r) =>
    r.path.split('/').every((seg, i) => (isParam(seg) ? client[i] === '{}' : seg === client[i]))
  );
  if (exact.length) return { paths: exact.map((r) => r.path), normalised: site.normalised };
  const loose = candidates.filter((r) =>
    r.path.split('/').every((seg, i) => isParam(seg) || client[i] === '{}' || seg === client[i])
  );
  const extra: string[] = [];
  if (loose.length) {
    client.forEach((seg, i) => {
      const options = [...new Set(loose.map((r) => r.path.split('/')[i]))];
      if (seg === '{}' && options.some((o) => !isParam(o))) extra.push(`{} at segment ${i} stands for ${options.join('|')}`);
      if (seg !== '{}' && options.some(isParam)) extra.push(`literal "${seg}" fills ${options.join('|')}`);
    });
  }
  return { paths: loose.map((r) => r.path), normalised: [...site.normalised, ...extra] };
}
