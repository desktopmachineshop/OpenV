// The workspace's Agents page: the agents of the workspace, outside any
// project. It is the project's Agents page under the workspace's menu,
// where Runs is the tab beside it.
import React from 'react';
import { Navigate } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { Navbar } from '../components/Navbar';
import { AgentsPage } from './AgentsPage';

export const AgentsWorkspace: React.FC = () => {
  const noWorkspace = useAppStore((s) => s.orgsLoaded && !s.activeOrgId);
  if (noWorkspace) return <Navigate to="/projects" replace />;
  return (
    <div className="app-shell" style={{ background: 'var(--bg-app)', display: 'flex', flexDirection: 'column' }}>
      <Navbar title="Agents" showWorkspaceControls />
      <AgentsPage />
    </div>
  );
};
