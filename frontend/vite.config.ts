/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Vite replaced Create React App (react-scripts), which had its final release
// in 2022 and was retired by the React team. See docs/DEVELOPMENT.md.
//
// The migration deliberately keeps the surface that the rest of the repository
// depends on unchanged: the same REACT_APP_ environment prefix, the same
// build/ output directory, and the same port 3000 for the dev server, so the
// Dockerfiles, compose files and E2E suite did not have to move with it.
export default defineConfig({
  plugins: [react()],

  // `REACT_APP_API_URL` is baked into the bundle by both Dockerfiles, named in
  // docker-compose, the E2E harness and the deployment docs. Keeping CRA's
  // prefix means none of those had to change; Vite's own VITE_ prefix stays
  // available for anything new.
  envPrefix: ['REACT_APP_', 'VITE_'],

  build: {
    // CRA wrote to build/; Dockerfile.prod copies from there and nginx serves
    // it. Vite's default is dist/, so it is pointed back at build/ rather than
    // changing the deployment.
    outDir: 'build',
    // The production Content-Security-Policy is `script-src 'self'` with no
    // 'unsafe-inline' (frontend/security-headers.conf), so nothing may be
    // inlined into index.html. Vite emits external <script type="module">
    // tags by default; this pins that rather than leaving it to the default,
    // which is what CRA's INLINE_RUNTIME_CHUNK=false was for.
    assetsInlineLimit: 0,
    sourcemap: true,
  },

  server: {
    // The dev image (frontend/Dockerfile) publishes 3000 and the E2E suite
    // defaults to http://localhost:3000. host:true binds 0.0.0.0 so the port
    // is reachable from outside the container; strictPort makes a clash fail
    // loudly instead of silently moving to 3001 and leaving E2E to time out.
    port: 3000,
    strictPort: true,
    host: true,
  },

  test: {
    globals: true,
    environment: 'jsdom',
    // Matches the files CRA's jest picked up.
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    css: false,
  },
});
