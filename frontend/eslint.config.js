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
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';

export default tseslint.config(
  {
    ignores: ['build/**', 'dist/**', 'node_modules/**', 'coverage/**'],
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
