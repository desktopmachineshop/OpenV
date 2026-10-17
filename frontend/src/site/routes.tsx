import React, { lazy } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { COMPARISONS } from './compare';
import { TOPICS } from './topics';

// The public site's own routes (ext/registry.ts, Site): App.tsx renders
// SitePages in its catch-all, so these are the paths the app itself does not
// route. One table, in the order the sitemap lists them (crawl.ts), with
// each view loaded only when its page is opened.

const Landing = lazy(() => import('../views/Landing').then((m) => ({ default: m.Landing })));
const HowItWorks = lazy(() => import('./HowItWorks').then((m) => ({ default: m.HowItWorks })));
const Demos = lazy(() => import('./Demos').then((m) => ({ default: m.Demos })));
const Faq = lazy(() => import('./Faq').then((m) => ({ default: m.Faq })));
const Privacy = lazy(() => import('./Privacy').then((m) => ({ default: m.Privacy })));
const OpenSourceProjects = lazy(() => import('./OpenSourceProjects').then((m) => ({ default: m.OpenSourceProjects })));
const Customers = lazy(() => import('./Customers').then((m) => ({ default: m.Customers })));
const WhitePapers = lazy(() => import('./WhitePapers').then((m) => ({ default: m.WhitePapers })));
const Changelog = lazy(() => import('./Changelog').then((m) => ({ default: m.Changelog })));
// The comparison hub, and one lazy layout for every comparison; copy is data in compare/.
const CompareHub = lazy(() => import('./CompareHub').then((m) => ({ default: m.CompareHub })));
const ComparePage = lazy(() => import('./ComparePage').then((m) => ({ default: m.ComparePage })));
// One lazy layout for every topic page; its copy is data in topics/.
const TopicPage = lazy(() => import('./TopicPage').then((m) => ({ default: m.TopicPage })));

export interface SiteRoute {
  path: string;
  element: React.ReactElement;
  /** A path that only sends the visitor elsewhere: no page of its own to list. */
  redirect?: boolean;
}

export const SITE_ROUTES: SiteRoute[] = [
  { path: '/pricing', element: <Landing section="pricing" /> },
  { path: '/how-it-works', element: <HowItWorks /> },
  { path: '/demos', element: <Demos /> },
  { path: '/faq', element: <Faq /> },
  { path: '/security', element: <Navigate to="/faq#security" replace />, redirect: true },
  { path: '/privacy', element: <Privacy /> },
  { path: '/open-source', element: <OpenSourceProjects /> },
  { path: '/customers', element: <Customers /> },
  { path: '/white-papers', element: <WhitePapers /> },
  { path: '/changelog', element: <Changelog /> },
  { path: '/compare', element: <CompareHub /> },
  ...COMPARISONS.map((c) => ({ path: c.path, element: <ComparePage path={c.path} /> })),
  ...TOPICS.map((t) => ({ path: t.path, element: <TopicPage path={t.path} /> })),
];

/** The site's front page, for a visitor who is signed out. */
export const SiteHome: React.FC = () => <Landing />;

/** The site's pages; any other path gets what the app does with it. */
export const SitePages: React.FC<{ fallback: React.ReactElement }> = ({ fallback }) => (
  <Routes>
    {SITE_ROUTES.map((r) => (
      <Route key={r.path} path={r.path} element={r.element} />
    ))}
    <Route path="*" element={fallback} />
  </Routes>
);
