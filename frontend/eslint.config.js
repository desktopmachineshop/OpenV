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
// d11dee8: each allowlist below names today's exceptions per file and per
// target or site count, may only shrink, and an entry that no longer needs its
// exception is reported (a file that is gone stops the lint run) so the list
// shrinks with the code. The rules are local rather than
// no-restricted-imports/no-restricted-syntax because flat config lets a later
// block replace an earlier block's options for the same rule; one rule that
// resolves real paths keeps the boundaries independent of each other and of
// any restricted-imports rule added later.
// ---------------------------------------------------------------------------
const FRONTEND = path.dirname(fileURLToPath(import.meta.url));
const SRC = path.join(FRONTEND, 'src');
const relToFrontend = (abs) => path.relative(FRONTEND, abs).split(path.sep).join('/');
const under = (rel, dir) => rel === dir || rel.startsWith(`${dir}/`);

// Components that import from src/views today, each with the one view it
// reads: both take TODO_LIST_FEATURE from views/TodoList (pain point
// fe-requirements-13).
const COMPONENTS_IMPORTING_VIEWS = {
  // None since X4b: both now take TODO_LIST_FEATURE from src/features.ts.
};

// The four hand-written SSE consumers (quirk Q21), each with how many
// EventSources it opens. A new stream belongs in the shared hook, not a fifth
// copy of the reconnect loop: EVENT_SOURCE_HOOK (refactor plan X15a) may open
// one without an entry here, before or after it exists, and X15b-X15e then
// lower this list to nothing.
const EVENT_SOURCE_HOOK = 'src/hooks/useEventStream.ts';
const EVENT_SOURCE_SITES = {
  'src/components/NotificationBell.tsx': 1,
  'src/components/agents/RunDetailPanel.tsx': 1,
  'src/components/wizard/GuidedChatPanel.tsx': 1,
  'src/views/InterviewChat.tsx': 1,
};

// An entry for a file that is gone would never be reported stale (only linted
// files are), and a new file at that path would inherit its exception.
for (const file of [...Object.keys(COMPONENTS_IMPORTING_VIEWS), ...Object.keys(EVENT_SOURCE_SITES)]) {
  if (!fs.existsSync(path.join(FRONTEND, file))) {
    throw new Error(`eslint.config.js allowlists ${file}, which no longer exists: remove the entry.`);
  }
}
const own = (map, key) => (Object.hasOwn(map, key) ? map[key] : undefined);

// UI for the api/** boundary: the React code outside src/api (components,
// views, the routed site pages, hooks, the app shell and its entry) and the
// React packages themselves. src/state, src/utils and src/theme are not UI.
const UI_DIRS = ['src/components', 'src/views', 'src/site', 'src/hooks'];
const UI_FILES = /^src\/(App|index)(\.tsx?)?$/;
const UI_PACKAGES = /^(react|react-dom|react-router-dom)(\/|$)/;
const isUi = (rel) => UI_DIRS.some((d) => under(rel, d)) || UI_FILES.test(rel);

// The entry points of src/api (refactor plan K12, F1): code outside src/api
// imports the api/client barrel and these three modules only, never an area
// module, api/types/* or api/http, so a whole-module vi.mock('../api/client')
// still stubs every call and an endpoint can move between areas freely.
const API_ENTRY_POINTS = ['src/api/client', 'src/api/errors', 'src/api/baseURL', 'src/api/contentDisposition'];
const isApiEntryPoint = (rel) => API_ENTRY_POINTS.includes(rel.replace(/\.(tsx?|jsx?)$/, ''));

const importBoundaries = {
  meta: {
    type: 'problem',
    schema: [],
    messages: {
      outside:
        "'{{spec}}' resolves outside frontend/. The production image builds from frontend/ alone, so nothing may import from beyond it.",
      apiImportsUi: "src/api may not import '{{spec}}': the API layer sits below the UI and imports none of it.",
      apiImportsCss: "src/api may not import the stylesheet '{{spec}}': the API layer imports no UI.",
      apiInternals:
        "Outside src/api, import the API layer from api/client (or api/errors, api/baseURL, api/contentDisposition), not '{{spec}}' (K12).",
      componentImportsView:
        "Components may not import views ('{{spec}}'). Move what both need into a module under src/ that neither owns.",
      importsArch:
        "Only src/arch may import '{{spec}}': src/arch holds the test-only architecture guards and never ships.",
      staleAllowance:
        "This file no longer imports '{{target}}': remove it from its COMPONENTS_IMPORTING_VIEWS entry in eslint.config.js.",
    },
  },
  create(context) {
    const file = relToFrontend(context.filename);
    const inApi = under(file, 'src/api');
    const inComponents = under(file, 'src/components');
    const inArch = under(file, 'src/arch');
    const allowed = own(COMPONENTS_IMPORTING_VIEWS, file) || [];
    const importedViews = new Set();

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
      if (inApi && UI_PACKAGES.test(spec)) {
        context.report({ node: source, messageId: 'apiImportsUi', data: { spec } });
        return;
      }
      if (inApi && /\.css($|\?)/.test(spec)) {
        context.report({ node: source, messageId: 'apiImportsCss', data: { spec } });
        return;
      }
      if (!rel) return;
      if (!inArch && under(rel, 'src/arch')) {
        context.report({ node: source, messageId: 'importsArch', data: { spec } });
      }
      if (inApi && isUi(rel)) {
        context.report({ node: source, messageId: 'apiImportsUi', data: { spec } });
      }
      if (!inApi && under(rel, 'src/api') && !isApiEntryPoint(rel)) {
        context.report({ node: source, messageId: 'apiInternals', data: { spec } });
      }
      if (inComponents && under(rel, 'src/views')) {
        const view = rel.replace(/\.(tsx?|jsx?)$/, '');
        importedViews.add(view);
        if (!allowed.includes(view)) context.report({ node: source, messageId: 'componentImportsView', data: { spec } });
      }
    };

    return {
      ImportDeclaration: (node) => check(node.source),
      ExportNamedDeclaration: (node) => check(node.source),
      ExportAllDeclaration: (node) => check(node.source),
      ImportExpression: (node) => check(node.source),
      'Program:exit': (node) => {
        for (const target of allowed) {
          if (!importedViews.has(target)) context.report({ node, messageId: 'staleAllowance', data: { target } });
        }
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
        `No new EventSource sites: open a stream through ${EVENT_SOURCE_HOOK}. This file may open {{allowed}} (EVENT_SOURCE_HOOK and EVENT_SOURCE_SITES in eslint.config.js).`,
      staleAllowance:
        'This file now opens {{count}} EventSource(s) where EVENT_SOURCE_SITES allows {{allowed}}: lower or remove its entry in eslint.config.js.',
    },
  },
  create(context) {
    const file = relToFrontend(context.filename);
    const allowed = file === EVENT_SOURCE_HOOK ? 1 : own(EVENT_SOURCE_SITES, file) || 0;
    let count = 0;
    const isEventSource = (callee) =>
      (callee.type === 'Identifier' && callee.name === 'EventSource') ||
      (callee.type === 'MemberExpression' && !callee.computed && callee.property.name === 'EventSource');
    return {
      NewExpression: (node) => {
        if (!isEventSource(node.callee)) return;
        count += 1;
        if (count > allowed) context.report({ node, messageId: 'newSite', data: { allowed } });
      },
      'Program:exit': (node) => {
        if (file !== EVENT_SOURCE_HOOK && count < allowed) context.report({ node, messageId: 'staleAllowance', data: { count, allowed } });
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
