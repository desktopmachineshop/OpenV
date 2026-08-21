import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import './index.css';
import App from './App';

const root = ReactDOM.createRoot(
  document.getElementById('root') as HTMLElement
);
root.render(
  <React.StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </React.StrictMode>
);

// Register the minimal service worker so browsers offer "Install app"
// (manifest + icons + service worker + HTTPS/localhost are the criteria).
// Registered in development too: docker-compose serves the frontend with
// `npm start`, so gating on NODE_ENV would leave the default localhost:3000
// deployment non-installable. Safe because sw.js caches nothing — a caching
// worker is what breaks dev hot-reload, not the registration itself.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register(`${process.env.PUBLIC_URL || ''}/sw.js`)
      .catch(() => {
        // Non-fatal: the app works identically without it.
      });
  });
}
