import React, { Suspense, lazy, useEffect, useState } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { authAPI, metaAPI, orgsAPI } from './api/client';
import { useAppStore } from './state/store';
import { pickActiveOrg } from './utils/activeOrg';
import { readActiveOrg } from './state/activeOrgStorage';
import { ProjectList } from './components/ProjectList';
import { ProjectLayout } from './components/ProjectLayout';
import { Login } from './views/Login';
import { VerifyEmail } from './views/VerifyEmail';
import { ResetPassword } from './views/ResetPassword';
import { ProductOverview } from './views/ProductOverview';
import { InterviewsPage } from './views/InterviewsPage';
import { GuidedWizard } from './views/GuidedWizard';
import { EvidenceView } from './views/EvidenceView';
import { VVDashboard } from './views/VVDashboard';
import { KanbanBoard } from './views/KanbanBoard';
import { TodoList } from './views/TodoList';
import { AutomationsPage } from './views/AutomationsPage';
import { AgentRunsPage } from './views/AgentRunsPage';
import { AgentsPage } from './views/AgentsPage';
import { ProjectSettings } from './views/ProjectSettings';
import { OrgSettings } from './views/OrgSettings';
import { InterviewChatRoute } from './views/InterviewChat';
import { DialogProvider } from './components/ui';
import { ReleaseUpdateBanner } from './components/ReleaseUpdateBanner';
import { releasePrerender } from './site/prerenderHandoff';
import { sendVisitBeacon } from './site/visitBeacon';
import { CAPABILITY_HOSTED, hasCapability } from './capabilities';
import './index.css';
import { pendingReturnTo } from './utils/returnTo';

// Route-level code splitting for the heavyweight views so their large
// dependencies stay out of the main bundle:
//   - ModuleView pulls in react-markdown/remark-gfm (via ArtifactDetails)
//   - ManualView pulls in react-markdown/remark-gfm
//   - CrewBuilder pulls in cytoscape (via CrewCanvas)
//   - TestRunView and TraceabilityMatrix pull in ag-grid
const ModuleView = lazy(() =>
  import('./views/ModuleView').then((m) => ({ default: m.ModuleView }))
);
const ManualView = lazy(() =>
  import('./views/ManualView').then((m) => ({ default: m.ManualView }))
);
const WhatsNew = lazy(() => import('./views/WhatsNew').then((m) => ({ default: m.WhatsNew })));
const OAuthConsent = lazy(() => import('./views/OAuthConsent').then((m) => ({ default: m.OAuthConsent })));
const PlatformAdmin = lazy(() =>
  import('./views/PlatformAdmin').then((m) => ({ default: m.PlatformAdmin }))
);
// Landing is the public front page; signed-in users never render it, so it
// stays out of the main bundle they download.
const Landing = lazy(() => import('./views/Landing').then((m) => ({ default: m.Landing })));
const CrewBuilder = lazy(() =>
  import('./views/CrewBuilder').then((m) => ({ default: m.CrewBuilder }))
);
const TestRunView = lazy(() =>
  import('./views/TestRunView').then((m) => ({ default: m.TestRunView }))
);
const TraceabilityMatrix = lazy(() =>
  import('./views/TraceabilityMatrix').then((m) => ({ default: m.TraceabilityMatrix }))
);
// ImpactView traces the affected set from one artifact; a focused analysis
// view kept out of the main bundle.
const ImpactView = lazy(() =>
  import('./views/ImpactView').then((m) => ({ default: m.ImpactView }))
);
// ActivityLog is a low-traffic audit view, so keep it out of the main bundle.
const ActivityLog = lazy(() =>
  import('./views/ActivityLog').then((m) => ({ default: m.ActivityLog }))
);
// BaselineCompare is a low-traffic review view, so keep it out of the main bundle.
const BaselineCompare = lazy(() =>
  import('./views/BaselineCompare').then((m) => ({ default: m.BaselineCompare }))
);
// The workspace's runs with no project, a low-traffic view.
const WorkspaceRunsPage = lazy(() =>
  import('./views/WorkspaceRunsPage').then((m) => ({ default: m.WorkspaceRunsPage }))
);
// ReviewQueue is a focused reviewer view, split out of the main bundle.
const HowItWorks = lazy(() => import('./site/HowItWorks').then((m) => ({ default: m.HowItWorks })));
const Demos = lazy(() => import('./site/Demos').then((m) => ({ default: m.Demos })));
const Faq = lazy(() => import('./site/Faq').then((m) => ({ default: m.Faq })));
const OpenSourceProjects = lazy(() =>
  import('./site/OpenSourceProjects').then((m) => ({ default: m.OpenSourceProjects }))
);
const Customers = lazy(() => import('./site/Customers').then((m) => ({ default: m.Customers })));
const WhitePapers = lazy(() => import('./site/WhitePapers').then((m) => ({ default: m.WhitePapers })));
// One lazy layout for every topic page; its copy is data in site/topics.
const TopicPage = lazy(() => import('./site/TopicPage').then((m) => ({ default: m.TopicPage })));
const SharedProjectView = lazy(() =>
  import('./views/SharedProjectView').then((m) => ({ default: m.SharedProjectView }))
);
const ReviewQueue = lazy(() =>
  import('./views/ReviewQueue').then((m) => ({ default: m.ReviewQueue }))
);

// How long App waits before each new try of a gate load that failed; after
// the last, only the window's focus or the browser coming back online tries
// again.
const GATE_RETRY_DELAYS_MS = [2000, 5000, 15000];

function RouteFallback() {
  return (
    <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
  );
}

function App() {
  const {
    currentUser,
    setCurrentUser,
    setMeta,
    orgsLoaded,
    setOrgs,
    activeOrgId,
    setActiveOrgId,
    setOrgsLoaded,
    setFeatures,
    setFeaturesFailed,
    emailVerificationRequired,
    setEmailVerificationRequired,
    capabilities,
    setCapabilities,
  } = useAppStore();
  // The public site and its visit beacon are the hosted service's; a
  // self-hosted OpenV opens on its sign-in page.
  const hosted = hasCapability(capabilities, CAPABILITY_HOSTED);
  const [authChecked, setAuthChecked] = useState(false);

  useEffect(() => {
    // Public interview pages never need a session.
    if (window.location.pathname.startsWith('/interview/')) {
      setAuthChecked(true);
      return;
    }
    // The session and the server's verification policy are read together:
    // both decide what a signed-in user sees first.
    Promise.allSettled([authAPI.me(), authAPI.config()])
      .then(([me, config]) => {
        setCurrentUser(me.status === 'fulfilled' ? me.value.data : null);
        if (config.status === 'fulfilled') {
          setEmailVerificationRequired(!!config.value.data.email_verification_required);
          if (Array.isArray(config.value.data.capabilities)) setCapabilities(config.value.data.capabilities);
        }
      })
      .finally(() => setAuthChecked(true));
  }, [setCurrentUser, setEmailVerificationRequired, setCapabilities]);

  // The site visit beacon (the platform admin's Visitors dashboard): once
  // the session is known, and again when this browser signs in or up.
  useEffect(() => {
    if (authChecked && hosted) sendVisitBeacon(currentUser?.id ?? null);
  }, [authChecked, hosted, currentUser?.id]);

  // A signed-in visitor to a pre-rendered page goes into the app (the front
  // page redirects to /projects), so its snapshot is dropped as soon as the
  // session is known rather than left for a page that will not mount.
  useEffect(() => {
    if (authChecked && currentUser) releasePrerender();
  }, [authChecked, currentUser]);

  // The wall: a signed-in account that has not confirmed its email, on a
  // server that requires it, sees only the verification page. The API
  // refuses everything else anyway; skipping the loads below just keeps the
  // console quiet.
  const walled = !!currentUser && emailVerificationRequired && !currentUser.email_verified;

  // Load the user's workspaces once authenticated and resolve the active one.
  useEffect(() => {
    if (!currentUser || walled) return;
    orgsAPI
      .list()
      .then((res) => {
        const orgs = res.data.orgs || [];
        // With storage unavailable both are '' and the server's answer decides.
        const { tabOrg, lastUsed } = readActiveOrg();
        const active = pickActiveOrg(orgs, tabOrg, res.data.active_org || '', lastUsed);
        setOrgs(orgs);
        if (active) setActiveOrgId(active, { clearProjects: false });
        setOrgsLoaded(true);
      })
      .catch(() => {
        // Org endpoints unavailable — don't block the app.
        setOrgsLoaded(true);
      });
  }, [currentUser, walled, setOrgs, setActiveOrgId, setOrgsLoaded]);

  // Feature gates follow the active workspace (REQ-137). Cleared first so a
  // switch never shows one workspace's gates against another's screens. A
  // load that fails is tried again after each of GATE_RETRY_DELAYS_MS, and
  // once more whenever the window regains focus or the browser comes back
  // online after a failed try (#379 bug 176). A switch cancels what is
  // pending, so the old workspace's answer never lands.
  useEffect(() => {
    if (!currentUser || walled || !activeOrgId) return;
    let cancelled = false;
    let failed = false;
    let retries = 0;
    let timer: number | undefined;
    const load = () => {
      failed = false;
      orgsAPI
        .features(activeOrgId)
        .then((res) => {
          if (!cancelled) setFeatures(res.data);
        })
        .catch(() => {
          if (cancelled) return;
          // Gates stay closed until a try succeeds. A page that waits for
          // them waits through the timed retries, and stops waiting
          // (featuresFailed) once none is left (#379 bug 179).
          failed = true;
          if (retries < GATE_RETRY_DELAYS_MS.length) timer = window.setTimeout(load, GATE_RETRY_DELAYS_MS[retries++]);
          else setFeaturesFailed();
        });
    };
    const retryNow = () => {
      if (!failed) return;
      window.clearTimeout(timer);
      load();
    };
    setFeatures(null);
    load();
    window.addEventListener('focus', retryNow);
    window.addEventListener('online', retryNow);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      window.removeEventListener('focus', retryNow);
      window.removeEventListener('online', retryNow);
    };
  }, [currentUser, walled, activeOrgId, setFeatures, setFeaturesFailed]);

  useEffect(() => {
    if (!currentUser || walled || !orgsLoaded) return;
    Promise.all([metaAPI.artifactTypes(), metaAPI.linkTypes()])
      .then(([types, rules]) =>
        setMeta({ artifactTypes: types.data, linkTypeRules: rules.data, loaded: true })
      )
      .catch(() => setMeta({ loaded: false }));
  }, [currentUser, walled, orgsLoaded, setMeta]);

  if (!authChecked) {
    return <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;
  }

  return (
    <div className="app-container">
      <DialogProvider>
        {currentUser && !walled && <ReleaseUpdateBanner />}
        <Suspense fallback={<RouteFallback />}>
        <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/verify-email" element={<VerifyEmail />} />
        <Route path="/reset-password" element={<ResetPassword />} />
        <Route path="/interview/:token" element={<InterviewChatRoute />} />
        {/* A share link opens without a session (REQ-149): /share/:token is
            the link handed out (the deployed nginx sends unfurlers to the
            API's preview page for it) and /s/:token is where the app takes
            over. /open-source/:id is a published snapshot (REQ-151). */}
        <Route path="/share/:token" element={<SharedProjectView source="share" />} />
        <Route path="/s/:token" element={<SharedProjectView source="share" />} />
        <Route path="/open-source/:id" element={<SharedProjectView source="open-source" />} />
        {/* The front page for visitors; a signed-in user goes straight to work.
            authChecked gates rendering above, so currentUser is settled here. */}
        <Route
          path="/"
          element={
            currentUser ? (
              <Navigate to={pendingReturnTo() || '/projects'} replace />
            ) : hosted ? (
              <Landing />
            ) : (
              <Navigate to="/login" replace />
            )
          }
        />
        <Route path="/pricing" element={<Landing section="pricing" />} />
        {/* The storefront's other pages, public like the landing page. */}
        <Route path="/how-it-works" element={<HowItWorks />} />
        <Route path="/demos" element={<Demos />} />
        <Route path="/faq" element={<Faq />} />
        <Route path="/security" element={<Navigate to="/faq#security" replace />} />
        <Route path="/open-source" element={<OpenSourceProjects />} />
        <Route path="/customers" element={<Customers />} />
        <Route path="/white-papers" element={<WhitePapers />} />
        <Route path="/requirements-traceability" element={<TopicPage path="/requirements-traceability" />} />
        <Route path="/verification-evidence" element={<TopicPage path="/verification-evidence" />} />
        <Route path="/baselines-and-audit-trail" element={<TopicPage path="/baselines-and-audit-trail" />} />
        <Route path="/reqif" element={<TopicPage path="/reqif" />} />
        <Route path="/self-hosted-requirements-management" element={<TopicPage path="/self-hosted-requirements-management" />} />
        <Route path="/safety-critical" element={<TopicPage path="/safety-critical" />} />
        <Route path="/glossary" element={<TopicPage path="/glossary" />} />
        {walled ? (
          <Route path="*" element={<Navigate to="/verify-email" replace />} />
        ) : (
          <>
        <Route path="/projects" element={<ProjectList />} />
        <Route path="/org/settings" element={<OrgSettings />} />
        {/* Gated on workspace-runs by the page itself, which waits for the
            gates and sends a member to /projects without it. */}
        <Route path="/org/runs" element={<WorkspaceRunsPage />} />
        <Route path="/manual" element={<ManualView />} />
        <Route path="/manual/:chapterSlug" element={<ManualView />} />
        <Route path="/whats-new" element={<WhatsNew />} />
        {/* The consent an AI app asks for (REQ-44): the API's /oauth/authorize
            sends the member here. Signed out, the 401 sends them to sign in
            and back. */}
        <Route path="/connect" element={<OAuthConsent />} />
        <Route path="/admin" element={<PlatformAdmin />} />
        <Route path="/projects/:projectId" element={<ProjectLayout />}>
          <Route index element={<ProductOverview />} />
          <Route path="requirements" element={<ModuleView />} />
          <Route path="baselines/:baselineId/compare" element={<BaselineCompare />} />
          <Route path="guided" element={<GuidedWizard />} />
          <Route path="interviews" element={<InterviewsPage />} />
          <Route path="vv" element={<VVDashboard />} />
          <Route path="evidence" element={<EvidenceView />} />
          <Route path="vv/runs/:runId" element={<TestRunView />} />
          <Route path="matrix" element={<TraceabilityMatrix />} />
          <Route path="impact" element={<ImpactView />} />
          <Route path="review" element={<ReviewQueue />} />
          <Route path="board" element={<KanbanBoard />} />
          <Route path="todos" element={<TodoList />} />
          <Route path="crew" element={<CrewBuilder />} />
          <Route path="crew/network" element={<CrewBuilder />} />
          {/* Legacy "team" URLs redirect to the renamed "crew" routes. */}
          <Route path="team" element={<Navigate to="../crew" replace />} />
          <Route path="team/network" element={<Navigate to="../crew/network" replace />} />
          <Route path="automations" element={<AutomationsPage />} />
          <Route path="agent-runs" element={<AgentRunsPage />} />
          <Route path="agents" element={<AgentsPage />} />
          <Route path="activity" element={<ActivityLog />} />
          <Route path="settings" element={<ProjectSettings />} />
        </Route>
          <Route path="*" element={<Navigate to="/projects" replace />} />
          </>
        )}
        </Routes>
        </Suspense>
      </DialogProvider>
    </div>
  );
}

export default App;
