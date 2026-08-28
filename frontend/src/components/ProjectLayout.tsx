import React, { useEffect, useState } from 'react';
import { NavLink, Outlet, useNavigate, useParams } from 'react-router-dom';
import { projectAPI, Project } from '../api/client';
import { useAppStore } from '../state/store';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';

const navItems: { to: string; label: string; end?: boolean }[] = [
  { to: '', label: 'Overview', end: true },
  { to: 'requirements', label: 'Requirements' },
  { to: 'guided', label: 'Guided Definition' },
  { to: 'vv', label: 'V&V' },
  { to: 'matrix', label: 'Matrix' },
  { to: 'board', label: 'Board' },
  { to: 'crew', label: 'Crew' },
  { to: 'agents', label: 'Agents' },
  { to: 'automations', label: 'Automations' },
  { to: 'agent-runs', label: 'Runs' },
  { to: 'settings', label: 'Settings' },
];

// ProjectLayout syncs the URL param into the store and renders the app
// shell (left nav + top bar) around the active module.
export const ProjectLayout: React.FC = () => {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { setProjectId, orgs, activeOrgId, setActiveOrgId } = useAppStore();
  const [project, setProject] = useState<Project | null>(null);

  useEffect(() => {
    if (!projectId) return;
    setProjectId(projectId);
    projectAPI
      .get(projectId)
      .then((res) => setProject(res.data))
      .catch(() => navigate('/projects'));
  }, [projectId, setProjectId, navigate]);

  // Deep link into a project from another workspace: silently switch the
  // active workspace when the project belongs to an org the user is in.
  // (If they aren't a member, the project fetch 403s and redirects above.)
  useEffect(() => {
    if (!project?.org_id || orgs.length === 0) return;
    if (project.org_id !== activeOrgId && orgs.some((o) => o.id === project.org_id)) {
      setActiveOrgId(project.org_id, { clearProjects: false });
    }
  }, [project, orgs, activeOrgId, setActiveOrgId]);

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      <aside
        style={{
          width: 200,
          minWidth: 200,
          background: '#2c3e50',
          color: '#ecf0f1',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{ padding: '16px 14px', borderBottom: '1px solid #34495e' }}>
          <OrgSwitcher variant="dark" />
          <div
            style={{
              fontSize: 12,
              color: '#95a5a6',
              marginTop: 6,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              cursor: 'pointer',
            }}
            title="All projects"
            onClick={() => navigate('/projects')}
          >
            {project?.name || '…'}
          </div>
        </div>
        <nav style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              style={({ isActive }) => ({
                display: 'block',
                padding: '9px 16px',
                color: isActive ? '#fff' : '#bdc3c7',
                background: isActive ? '#3498db' : 'transparent',
                textDecoration: 'none',
                fontSize: 14,
              })}
            >
              {item.label}
            </NavLink>
          ))}
          <div style={{ borderTop: '1px solid #34495e', margin: '8px 0' }} />
          <NavLink
            to="/manual"
            title="Open the user manual"
            style={{
              display: 'block',
              padding: '9px 16px',
              color: '#bdc3c7',
              textDecoration: 'none',
              fontSize: 14,
            }}
          >
            Help
          </NavLink>
        </nav>
        <div style={{ borderTop: '1px solid #34495e', padding: 12 }}>
          <UserMenu variant="dark" />
        </div>
      </aside>
      <main style={{ flex: 1, overflow: 'auto', background: '#f5f6fa' }}>
        <Outlet />
      </main>
    </div>
  );
};
