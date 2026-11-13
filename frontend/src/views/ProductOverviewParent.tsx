// Where a project sits among the workspace's projects, on its Overview: the
// project it is a part of, which can be changed here, and the projects that
// are parts of it. A subsystem or supplier project sits under the system it
// belongs to; its requirements can then refine the parent's, and the
// parent's V&V rolls those refinements up.
import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { projectAPI, type Project } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { FLOW_DOWN_FEATURE } from '../features';
import { useFeature } from '../hooks/useFeature';
import { useAppStore } from '../state/store';
import { mutedStyle, panelStyle } from '../components/ui';

/**
 * The projects that can be a project's parent: those of its workspace,
 * less itself and everything filed under it, since placing it under one of
 * those would close a loop.
 */
export const parentCandidates = (projects: Project[], project: Project): Project[] => {
  const under = new Set<string>([project.id]);
  for (let grew = true; grew; ) {
    grew = false;
    for (const p of projects) {
      if (p.parent_project_id && under.has(p.parent_project_id) && !under.has(p.id)) {
        under.add(p.id);
        grew = true;
      }
    }
  }
  return projects.filter((p) => !under.has(p.id) && p.org_id === project.org_id);
};

export const ProductOverviewParent: React.FC<{ projectId: string }> = ({ projectId }) => {
  const on = useFeature(FLOW_DOWN_FEATURE);
  const updateProject = useAppStore((s) => s.updateProject);
  const [projects, setProjects] = useState<Project[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!on) return undefined;
    let live = true;
    projectAPI
      .list()
      .then((res) => live && setProjects(res.data || []))
      .catch(() => live && setProjects([]));
    return () => {
      live = false;
    };
  }, [on, projectId]);
  const project = projects.find((p) => p.id === projectId);
  const candidates = useMemo(() => (project ? parentCandidates(projects, project) : []), [projects, project]);
  const children = projects.filter((p) => p.parent_project_id === projectId);
  if (!on || !project) return null;

  const setParent = async (parentId: string) => {
    setSaving(true);
    setError('');
    try {
      const res = await projectAPI.update(projectId, { parent_project_id: parentId });
      setProjects((list) => list.map((p) => (p.id === projectId ? res.data : p)));
      updateProject(res.data);
    } catch (err) {
      setError(`Failed to set the parent project: ${apiErrorMessage(err)}`);
    } finally {
      setSaving(false);
    }
  };

  return (
    <section aria-label="Project hierarchy" data-help-key="settings.general.parent" style={{ ...panelStyle, padding: 12, marginBottom: 16, display: 'flex', gap: '8px 20px', flexWrap: 'wrap', alignItems: 'center', fontSize: 13 }}>
      <label htmlFor="parent-project" style={{ display: 'flex', alignItems: 'center', gap: 8, margin: 0, fontWeight: 600 }} title="A subsystem or supplier project sits under the system it belongs to: its requirements can refine the parent's, and the parent's V&V rolls them up">
        Part of
        <select id="parent-project" value={project.parent_project_id || ''} disabled={saving} onChange={(e) => setParent(e.target.value)} style={{ width: 'auto', maxWidth: 260, minHeight: 32, padding: '4px 8px', fontWeight: 400 }}>
          <option value="">No parent (top-level project)</option>
          {candidates.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      {children.length > 0 && (
        <span style={{ display: 'inline-flex', gap: '4px 10px', flexWrap: 'wrap', alignItems: 'center' }}>
          <span style={mutedStyle}>Child projects</span>
          {children.map((p) => (
            <Link key={p.id} to={`/projects/${p.id}`}>
              {p.name}
            </Link>
          ))}
        </span>
      )}
      {error && (
        <span role="alert" style={{ color: 'var(--danger-strong)', flexBasis: '100%' }}>
          {error}
        </span>
      )}
    </section>
  );
};
