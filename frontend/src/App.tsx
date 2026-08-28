import React, { useEffect, useState } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { authAPI, metaAPI, orgsAPI } from './api/client';
import { useAppStore } from './state/store';
import { ProjectList } from './components/ProjectList';
import { ProjectLayout } from './components/ProjectLayout';
import { ModuleView } from './views/ModuleView';
import { Login } from './views/Login';
import { ProductOverview } from './views/ProductOverview';
import { GuidedWizard } from './views/GuidedWizard';
import { VVDashboard } from './views/VVDashboard';
import { TestRunView } from './views/TestRunView';
import { TraceabilityMatrix } from './views/TraceabilityMatrix';
import { KanbanBoard } from './views/KanbanBoard';
import { CrewBuilder } from './views/CrewBuilder';
import { AutomationsPage } from './views/AutomationsPage';
import { AgentRunsPage } from './views/AgentRunsPage';
import { AgentsPage } from './views/AgentsPage';
import { ProjectSettings } from './views/ProjectSettings';
import { OrgSettings } from './views/OrgSettings';
import { InterviewChat } from './views/InterviewChat';
import './index.css';

function App() {
  const { currentUser, setCurrentUser, setMeta, orgsLoaded, setOrgs, setActiveOrgId, setOrgsLoaded } =
    useAppStore();
  const [authChecked, setAuthChecked] = useState(false);

  useEffect(() => {
    // Public interview pages never need a session.
    if (window.location.pathname.startsWith('/interview/')) {
      setAuthChecked(true);
      return;
    }
    authAPI
      .me()
      .then((res) => setCurrentUser(res.data))
      .catch(() => setCurrentUser(null))
      .finally(() => setAuthChecked(true));
  }, [setCurrentUser]);

  // Load the user's workspaces once authenticated and resolve the active one.
  useEffect(() => {
    if (!currentUser) return;
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
  }, [currentUser, setOrgs, setActiveOrgId, setOrgsLoaded]);

  useEffect(() => {
    if (!currentUser || !orgsLoaded) return;
    Promise.all([metaAPI.artifactTypes(), metaAPI.linkTypes()])
      .then(([types, rules]) =>
        setMeta({ artifactTypes: types.data, linkTypeRules: rules.data, loaded: true })
      )
      .catch(() => setMeta({ loaded: false }));
  }, [currentUser, orgsLoaded, setMeta]);

  if (!authChecked) {
    return <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;
  }

  return (
    <div className="app-container">
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/interview/:token" element={<InterviewChat />} />
        <Route path="/" element={<Navigate to="/projects" replace />} />
        <Route path="/projects" element={<ProjectList />} />
        <Route path="/org/settings" element={<OrgSettings />} />
        <Route path="/projects/:projectId" element={<ProjectLayout />}>
          <Route index element={<ProductOverview />} />
          <Route path="requirements" element={<ModuleView />} />
          <Route path="guided" element={<GuidedWizard />} />
          <Route path="vv" element={<VVDashboard />} />
          <Route path="vv/runs/:runId" element={<TestRunView />} />
          <Route path="matrix" element={<TraceabilityMatrix />} />
          <Route path="board" element={<KanbanBoard />} />
          <Route path="crew" element={<CrewBuilder />} />
          <Route path="crew/network" element={<CrewBuilder />} />
          {/* Legacy "team" URLs redirect to the renamed "crew" routes. */}
          <Route path="team" element={<Navigate to="../crew" replace />} />
          <Route path="team/network" element={<Navigate to="../crew/network" replace />} />
          <Route path="automations" element={<AutomationsPage />} />
          <Route path="agent-runs" element={<AgentRunsPage />} />
          <Route path="agents" element={<AgentsPage />} />
          <Route path="settings" element={<ProjectSettings />} />
        </Route>
        <Route path="*" element={<Navigate to="/projects" replace />} />
      </Routes>
    </div>
  );
}

export default App;
