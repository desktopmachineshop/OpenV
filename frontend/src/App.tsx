import React, { Suspense, lazy, useEffect, useState } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { authAPI, metaAPI, orgsAPI } from './api/client';
import { useAppStore } from './state/store';
import { ProjectList } from './components/ProjectList';
import { ProjectLayout } from './components/ProjectLayout';
import { Login } from './views/Login';
import { VerifyEmail } from './views/VerifyEmail';
import { ProductOverview } from './views/ProductOverview';
import { InterviewsPage } from './views/InterviewsPage';
import { GuidedWizard } from './views/GuidedWizard';
import { EvidenceView } from './views/EvidenceView';
import { VVDashboard } from './views/VVDashboard';
import { KanbanBoard } from './views/KanbanBoard';
import { AutomationsPage } from './views/AutomationsPage';
import { AgentRunsPage } from './views/AgentRunsPage';
import { AgentsPage } from './views/AgentsPage';
import { ProjectSettings } from './views/ProjectSettings';
import { OrgSettings } from './views/OrgSettings';
import { InterviewChat } from './views/InterviewChat';
import { DialogProvider } from './components/ui';
import { ReleaseUpdateBanner } from './components/ReleaseUpdateBanner';
import './index.css';

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
// ReviewQueue is a focused reviewer view, split out of the main bundle.
const ReviewQueue = lazy(() =>
  import('./views/ReviewQueue').then((m) => ({ default: m.ReviewQueue }))
);

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
    emailVerificationRequired,
    setEmailVerificationRequired,
  } = useAppStore();
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
        }
      })
      .finally(() => setAuthChecked(true));
  }, [setCurrentUser, setEmailVerificationRequired]);

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
        let stored = '';
        try {
          stored =
            sessionStorage.getItem('openv_active_org') ||
            localStorage.getItem('openv_active_org') ||
            '';
        } catch {
          stored = '';
        }
        const personal = orgs.find((o) => o.type === 'personal');
        const active =
          (stored && orgs.some((o) => o.id === stored) && stored) ||
          (res.data.active_org && orgs.some((o) => o.id === res.data.active_org) && res.data.active_org) ||
          personal?.id ||
          orgs[0]?.id ||
          '';
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
  // switch never shows one workspace's gates against another's screens.
  useEffect(() => {
    if (!currentUser || walled || !activeOrgId) return;
    let cancelled = false;
    setFeatures(null);
    orgsAPI
      .features(activeOrgId)
      .then((res) => {
        if (!cancelled) setFeatures(res.data);
      })
      .catch(() => {
        // Gates stay closed until the next load; nothing else is affected.
      });
    return () => {
      cancelled = true;
    };
  }, [currentUser, walled, activeOrgId, setFeatures]);

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
        <Route path="/interview/:token" element={<InterviewChat />} />
        {/* The front page for visitors; a signed-in user goes straight to work.
            authChecked gates rendering above, so currentUser is settled here. */}
        <Route path="/" element={currentUser ? <Navigate to="/projects" replace /> : <Landing />} />
        <Route path="/pricing" element={<Landing section="pricing" />} />
        {walled ? (
          <Route path="*" element={<Navigate to="/verify-email" replace />} />
        ) : (
          <>
        <Route path="/projects" element={<ProjectList />} />
        <Route path="/org/settings" element={<OrgSettings />} />
        <Route path="/manual" element={<ManualView />} />
        <Route path="/manual/:chapterSlug" element={<ManualView />} />
        <Route path="/whats-new" element={<WhatsNew />} />
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
