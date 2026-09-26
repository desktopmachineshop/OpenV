// Test-only: reads the route table out of src/App.tsx with the TypeScript
// compiler API, so the guards see exactly what the JSX declares: every
// <Route>, its nesting, which branch of a condition it sits in, what it
// renders and whether that component is imported eagerly or through lazy().
import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';
import type { RouteObject } from 'react-router-dom';
import { SRC, literalText, parseFile, resolveModule, walk } from './repo';

export const APP_FILE = path.join(SRC, 'App.tsx');

/** One in-app URL the Go backend builds (testdata/backendDeepLinks.json). */
export interface DeepLink {
  url: string;
  what: string;
  source: string;
  goLiteral: string;
  /** false: nginx serves it from the API; it is not an app route. */
  spa?: boolean;
}

export function loadDeepLinks(): DeepLink[] {
  const raw = fs.readFileSync(path.join(SRC, 'arch/testdata/backendDeepLinks.json'), 'utf8');
  return (JSON.parse(raw) as { links: DeepLink[] }).links;
}

export interface ImportInfo {
  module: string;
  /** "default", a named export, or "*" for a namespace import. */
  bindings: { local: string; imported: string; typeOnly: boolean }[];
  sideEffect: boolean;
}

export interface ComponentSource {
  load: 'eager' | 'lazy';
  module: string;
  exportName: string;
  /** Absolute path of the module, when it is one of ours. */
  file: string | null;
}

export type RouteElement =
  | { kind: 'component'; name: string; props: string[]; source: ComponentSource | null }
  | { kind: 'redirect'; to: string; replace: boolean }
  | { kind: 'conditional'; condition: string; whenTrue: RouteElement; whenFalse: RouteElement }
  | { kind: 'other'; text: string };

export interface RouteNode {
  path: string | null;
  index: boolean;
  element: RouteElement | null;
  /** The chain of JSX conditions this route sits under, e.g. ["!walled"]. */
  branch: string[];
  children: RouteNode[];
}

export interface AppRoutes {
  imports: ImportInfo[];
  lazy: { local: string; module: string; exportName: string }[];
  routes: RouteNode[];
}

const tagName = (node: ts.JsxElement | ts.JsxSelfClosingElement) =>
  (ts.isJsxElement(node) ? node.openingElement.tagName : node.tagName).getText();

const attributesOf = (node: ts.JsxElement | ts.JsxSelfClosingElement) =>
  (ts.isJsxElement(node) ? node.openingElement.attributes : node.attributes).properties;

function attr(node: ts.JsxElement | ts.JsxSelfClosingElement, name: string): ts.JsxAttribute | undefined {
  return attributesOf(node).find(
    (p): p is ts.JsxAttribute => ts.isJsxAttribute(p) && p.name.getText() === name
  );
}

/** `lazy(() => import('./x').then((m) => ({ default: m.Y })))` → module and export. */
function readLazy(init: ts.Expression): { module: string; exportName: string } | null {
  if (!ts.isCallExpression(init) || init.expression.getText() !== 'lazy') return null;
  const fn = init.arguments[0];
  if (!fn || !ts.isArrowFunction(fn) || !ts.isCallExpression(fn.body)) return null;
  const then = fn.body;
  if (!ts.isPropertyAccessExpression(then.expression) || then.expression.name.text !== 'then') return null;
  const dyn = then.expression.expression;
  if (!ts.isCallExpression(dyn) || dyn.expression.kind !== ts.SyntaxKind.ImportKeyword) return null;
  const module = literalText(dyn.arguments[0]);
  let exportName = 'default';
  const pick = then.arguments[0];
  if (pick && ts.isArrowFunction(pick)) {
    let body: ts.Node = pick.body;
    while (ts.isParenthesizedExpression(body)) body = body.expression;
    if (ts.isObjectLiteralExpression(body)) {
      const prop = body.properties.find((p) => p.name?.getText() === 'default');
      if (prop && ts.isPropertyAssignment(prop) && ts.isPropertyAccessExpression(prop.initializer)) {
        exportName = prop.initializer.name.text;
      }
    }
  }
  return module ? { module, exportName } : null;
}

export function parseAppRoutes(file: string = APP_FILE): AppRoutes {
  const sf = parseFile(file);
  const imports: ImportInfo[] = [];
  const lazy: AppRoutes['lazy'] = [];

  for (const stmt of sf.statements) {
    if (ts.isImportDeclaration(stmt)) {
      const module = literalText(stmt.moduleSpecifier) || '';
      const clause = stmt.importClause;
      const bindings: ImportInfo['bindings'] = [];
      const typeOnly = !!clause?.isTypeOnly;
      if (clause?.name) bindings.push({ local: clause.name.text, imported: 'default', typeOnly });
      const nb = clause?.namedBindings;
      if (nb && ts.isNamespaceImport(nb)) bindings.push({ local: nb.name.text, imported: '*', typeOnly });
      if (nb && ts.isNamedImports(nb)) {
        for (const el of nb.elements) {
          bindings.push({
            local: el.name.text,
            imported: (el.propertyName || el.name).text,
            typeOnly: typeOnly || el.isTypeOnly,
          });
        }
      }
      imports.push({ module, bindings, sideEffect: !clause });
    }
    if (ts.isVariableStatement(stmt)) {
      for (const decl of stmt.declarationList.declarations) {
        const found = decl.initializer && readLazy(decl.initializer);
        if (found && ts.isIdentifier(decl.name)) lazy.push({ local: decl.name.text, ...found });
      }
    }
  }

  const sourceOf = (name: string): ComponentSource | null => {
    const l = lazy.find((x) => x.local === name);
    if (l) return { load: 'lazy', module: l.module, exportName: l.exportName, file: resolveModule(file, l.module) };
    for (const imp of imports) {
      const b = imp.bindings.find((x) => x.local === name);
      if (b) return { load: 'eager', module: imp.module, exportName: b.imported, file: resolveModule(file, imp.module) };
    }
    return null;
  };

  const readElement = (expr: ts.Expression): RouteElement => {
    while (ts.isParenthesizedExpression(expr)) expr = expr.expression;
    if (ts.isConditionalExpression(expr)) {
      return {
        kind: 'conditional',
        condition: expr.condition.getText(),
        whenTrue: readElement(expr.whenTrue),
        whenFalse: readElement(expr.whenFalse),
      };
    }
    if (ts.isJsxSelfClosingElement(expr) || ts.isJsxElement(expr)) {
      const name = tagName(expr);
      if (name === 'Navigate') {
        return {
          kind: 'redirect',
          to: literalText(attr(expr, 'to')?.initializer) ?? `{${attr(expr, 'to')?.initializer?.getText()}}`,
          replace: !!attr(expr, 'replace'),
        };
      }
      const props = attributesOf(expr).map((p) => p.getText().replace(/\s+/g, ' '));
      return { kind: 'component', name, props, source: sourceOf(name) };
    }
    return { kind: 'other', text: expr.getText() };
  };

  const readChildren = (children: ts.NodeArray<ts.JsxChild>, branch: string[]): RouteNode[] => {
    const out: RouteNode[] = [];
    const visitExpr = (expr: ts.Expression, b: string[]) => {
      while (ts.isParenthesizedExpression(expr)) expr = expr.expression;
      if (ts.isConditionalExpression(expr)) {
        const cond = expr.condition.getText();
        visitExpr(expr.whenTrue, [...b, cond]);
        visitExpr(expr.whenFalse, [...b, `!${cond}`]);
      } else if (ts.isBinaryExpression(expr) && expr.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) {
        visitExpr(expr.right, [...b, expr.left.getText()]);
      } else if (ts.isJsxFragment(expr)) {
        out.push(...readChildren(expr.children, b));
      } else if (ts.isJsxElement(expr) || ts.isJsxSelfClosingElement(expr)) {
        visitChild(expr, b);
      } else {
        throw new Error(`App.tsx: unexpected expression among routes: ${expr.getText()}`);
      }
    };
    const visitChild = (child: ts.JsxChild, b: string[]) => {
      if (ts.isJsxText(child)) return;
      if (ts.isJsxExpression(child)) {
        if (child.expression) visitExpr(child.expression, b);
        return;
      }
      if (ts.isJsxFragment(child)) {
        out.push(...readChildren(child.children, b));
        return;
      }
      if (ts.isJsxElement(child) || ts.isJsxSelfClosingElement(child)) {
        if (tagName(child) !== 'Route') throw new Error(`App.tsx: <${tagName(child)}> inside <Routes>`);
        const elementAttr = attr(child, 'element')?.initializer;
        const elementExpr = elementAttr && ts.isJsxExpression(elementAttr) ? elementAttr.expression : undefined;
        out.push({
          path: literalText(attr(child, 'path')?.initializer) ?? null,
          index: !!attr(child, 'index'),
          element: elementExpr ? readElement(elementExpr) : null,
          branch: b,
          children: ts.isJsxElement(child) ? readChildren(child.children, []) : [],
        });
      }
    };
    for (const child of children) visitChild(child, branch);
    return out;
  };

  let routesEl: ts.JsxElement | undefined;
  walk(sf, (node) => {
    if (!routesEl && ts.isJsxElement(node) && tagName(node) === 'Routes') routesEl = node;
  });
  if (!routesEl) throw new Error('App.tsx: no <Routes> element found');
  return { imports, lazy, routes: readChildren(routesEl.children, []) };
}

/** A route's path from the root, given its parent's (an index route shares it). */
function fullPathOf(node: RouteNode, parent: string): string {
  const own = node.index ? '' : node.path || '';
  if (own.startsWith('/')) return own;
  return own ? `${parent.replace(/\/$/, '')}/${own}` : parent || '/';
}

export interface FlatRoute {
  node: RouteNode;
  fullPath: string;
  /** Every JSX condition on the way down, the parents' included. */
  branch: string[];
}

/** Every route in document order, with its full path from the root. */
export function flattenRoutes(routes: RouteNode[], parent = '', parentBranch: string[] = []): FlatRoute[] {
  const out: FlatRoute[] = [];
  for (const node of routes) {
    const fullPath = fullPathOf(node, parent);
    const branch = [...parentBranch, ...node.branch];
    out.push({ node, fullPath, branch });
    out.push(...flattenRoutes(node.children, fullPath, branch));
  }
  return out;
}

/**
 * The route objects react-router would build for one evaluation of the JSX
 * conditions. `when` says which conditions hold (anything unlisted is false).
 * Each object's id is the route's full path, or "<parent> (index)".
 */
export function toRouteObjects(routes: RouteNode[], when: Record<string, boolean>, parent = ''): RouteObject[] {
  const holds = (cond: string) => (cond.startsWith('!') ? !when[cond.slice(1)] : !!when[cond]);
  const out: RouteObject[] = [];
  for (const node of routes) {
    if (!node.branch.every(holds)) continue;
    const fullPath = fullPathOf(node, parent);
    const id = node.index ? `${parent} (index)` : fullPath;
    if (node.index) {
      out.push({ index: true, id });
    } else {
      out.push({ path: node.path || undefined, id, children: toRouteObjects(node.children, when, fullPath) });
    }
  }
  return out;
}

/** The components an element can render (both arms of a condition). */
export function componentsOf(el: RouteElement | null): Extract<RouteElement, { kind: 'component' }>[] {
  if (!el) return [];
  if (el.kind === 'component') return [el];
  if (el.kind === 'conditional') return [...componentsOf(el.whenTrue), ...componentsOf(el.whenFalse)];
  return [];
}

export function describeElement(el: RouteElement | null): string {
  if (!el) return '(no element)';
  switch (el.kind) {
    case 'component': {
      const props = el.props.length ? ` ${el.props.join(' ')}` : '';
      const src = el.source ? `  [${el.source.load} ${el.source.module}#${el.source.exportName}]` : '  [unresolved]';
      return `<${el.name}${props}>${src}`;
    }
    case 'redirect': {
      const relative = el.to.startsWith('/') ? '' : ' (relative)';
      return `redirect to="${el.to}"${el.replace ? ' replace' : ''}${relative}`;
    }
    case 'conditional':
      return `${el.condition} ? ${describeElement(el.whenTrue)} : ${describeElement(el.whenFalse)}`;
    default:
      return `{${el.text}}`;
  }
}
