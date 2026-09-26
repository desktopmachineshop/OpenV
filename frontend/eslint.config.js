// Replaces the lint gate that Create React App used to run inside the build.
//
// CRA compiled with eslint-config-react-app and, with CI=true, turned its
// warnings into build failures; the CI comment called out unused symbols and
// react-hooks/exhaustive-deps specifically. That config still pins eslint 8
// and is unmaintained, so the same rules are taken from their current upstream
// homes and run as their own `npm run lint` step instead of riding inside the
// bundler.
//
// Deliberately scoped to what CRA actually enforced. eslint-plugin-react-hooks
// v7 ships a much larger rule set (set-state-in-effect, refs, purity,
// immutability — the React Compiler rules), which flags ~77 places in this
// codebase. Those may well be worth acting on, but turning them on here would
// hide a toolchain migration behind a large, unrelated refactor. They are left
// for a change that can be reviewed as the code change it would be.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';

// ---------------------------------------------------------------------------
// Architecture boundaries (refactor plan S12). Frozen as they stood at
// d11dee8: each allowlist below names today's exceptions by file, may only
// shrink, and an entry that no longer needs its exception is reported so the
// list shrinks with the code. The rules are local rather than
// no-restricted-imports/no-restricted-syntax because flat config lets a later
// block replace an earlier block's options for the same rule; one rule that
// resolves real paths keeps the boundaries independent of each other and of
// any restricted-imports rule added later.
// ---------------------------------------------------------------------------
const FRONTEND = path.dirname(fileURLToPath(import.meta.url));
const SRC = path.join(FRONTEND, 'src');
const relToFrontend = (abs) => path.relative(FRONTEND, abs).split(path.sep).join('/');
const under = (rel, dir) => rel === dir || rel.startsWith(`${dir}/`);

// Components that import from src/views today: both read TODO_LIST_FEATURE
// from views/TodoList (pain point fe-requirements-13).
const COMPONENTS_IMPORTING_VIEWS = ['src/components/ChatterPanel.tsx', 'src/components/ProjectLayout.tsx'];

// The four hand-written SSE consumers (quirk Q21). A new stream belongs in a
// shared hook, not a fifth copy of the reconnect loop.
const EVENT_SOURCE_SITES = [
  'src/components/NotificationBell.tsx',
  'src/components/agents/RunDetailPanel.tsx',
  'src/components/wizard/GuidedChatPanel.tsx',
  'src/views/InterviewChat.tsx',
];

const importBoundaries = {
  meta: {
    type: 'problem',
    schema: [],
    messages: {
      outside:
        "'{{spec}}' resolves outside frontend/. The production image builds from frontend/ alone, so nothing may import from beyond it.",
      apiImportsUi: "src/api may not import '{{spec}}': the API layer sits below components and views and imports no UI.",
      apiImportsCss: "src/api may not import the stylesheet '{{spec}}': the API layer imports no UI.",
      componentImportsView:
        "Components may not import views ('{{spec}}'). Move what both need into a module under src/ that neither owns.",
      staleAllowance:
        'This file no longer imports from src/views: remove it from COMPONENTS_IMPORTING_VIEWS in eslint.config.js.',
    },
  },
  create(context) {
    const file = relToFrontend(context.filename);
    const inApi = under(file, 'src/api');
    const inComponents = under(file, 'src/components');
    const allowed = COMPONENTS_IMPORTING_VIEWS.includes(file);
    let importsViews = false;

    const check = (source) => {
      if (!source || source.type !== 'Literal' || typeof source.value !== 'string') return;
      const spec = source.value;
      let target = null;
      if (spec.startsWith('.') || path.isAbsolute(spec)) {
        target = path.resolve(path.dirname(context.filename), spec);
      } else if (fs.existsSync(path.join(SRC, spec.split('/')[0]))) {
        // tsconfig's baseUrl is ./src, so 'views/X' would resolve there.
        target = path.join(SRC, spec);
      }
      const rel = target && relToFrontend(target);
      if (rel && (rel.startsWith('../') || rel === '..' || path.isAbsolute(rel))) {
        context.report({ node: source, messageId: 'outside', data: { spec } });
        return;
      }
      if (inApi && /\.css($|\?)/.test(spec)) {
        context.report({ node: source, messageId: 'apiImportsCss', data: { spec } });
        return;
      }
      if (!rel) return;
      if (inApi && (under(rel, 'src/components') || under(rel, 'src/views'))) {
        context.report({ node: source, messageId: 'apiImportsUi', data: { spec } });
      }
      if (inComponents && under(rel, 'src/views')) {
        importsViews = true;
        if (!allowed) context.report({ node: source, messageId: 'componentImportsView', data: { spec } });
      }
    };

    return {
      ImportDeclaration: (node) => check(node.source),
      ExportNamedDeclaration: (node) => check(node.source),
      ExportAllDeclaration: (node) => check(node.source),
      ImportExpression: (node) => check(node.source),
      'Program:exit': (node) => {
        if (allowed && !importsViews) context.report({ node, messageId: 'staleAllowance' });
      },
    };
  },
};

const eventSourceSites = {
  meta: {
    type: 'problem',
    schema: [],
    messages: {
      newSite:
        'No new EventSource sites: today there are exactly four (EVENT_SOURCE_SITES in eslint.config.js), each with its own reconnect policy.',
      staleAllowance: 'This file no longer opens an EventSource: remove it from EVENT_SOURCE_SITES in eslint.config.js.',
    },
  },
  create(context) {
    const allowed = EVENT_SOURCE_SITES.includes(relToFrontend(context.filename));
    let opens = false;
    const isEventSource = (callee) =>
      (callee.type === 'Identifier' && callee.name === 'EventSource') ||
      (callee.type === 'MemberExpression' && !callee.computed && callee.property.name === 'EventSource');
    return {
      NewExpression: (node) => {
        if (!isEventSource(node.callee)) return;
        opens = true;
        if (!allowed) context.report({ node, messageId: 'newSite' });
      },
      'Program:exit': (node) => {
        if (allowed && !opens) context.report({ node, messageId: 'staleAllowance' });
      },
    };
  },
};

export default tseslint.config(
  {
    ignores: ['build/**', 'dist/**', 'node_modules/**', 'coverage/**'],
  },
  {
    files: ['src/**/*.{ts,tsx,js,jsx}'],
    plugins: {
      openv: { rules: { 'import-boundaries': importBoundaries, 'event-source-sites': eventSourceSites } },
    },
    rules: {
      'openv/import-boundaries': 'error',
      'openv/event-source-sites': 'error',
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    // configs.base is a single config object (parser + plugin wiring), not an
    // array, so it is listed rather than spread.
    extends: [tseslint.configs.base],
    plugins: { 'react-hooks': reactHooks },
    rules: {
      // The two rules CRA's gate was really about.
      'react-hooks/rules-of-hooks': 'error',
      // A stale dependency array shows up as a panel that never refreshes,
      // which is why this was worth failing a build over.
      'react-hooks/exhaustive-deps': 'error',

      // CRA reported unused symbols as warnings and CI=true made them fatal.
      // The TypeScript-aware rule replaces the base one because it understands
      // type-only usage, and the options mirror what eslint-config-react-app
      // set: unused function arguments are not reported (an interface often
      // names parameters an implementation does not need) and neither is a
      // name destructured only to keep it out of a rest spread.
      '@typescript-eslint/no-unused-vars': [
        'error',
        { args: 'none', ignoreRestSiblings: true, caughtErrors: 'none' },
      ],
    },
  }
);
