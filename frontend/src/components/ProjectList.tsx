import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { projectAPI, templateAPI, Project, Template } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Navbar } from './Navbar';
import { CreateOrgModal } from './CreateOrgModal';
import { useConfirm, usePrompt } from './ui';
import './ProjectList.css';

export const ProjectList: React.FC = () => {
  const navigate = useNavigate();
  const { projectId, setProjectId, projects, setProjects, addProject, updateProject, removeProject, orgs, activeOrgId } = useAppStore();
  const activeOrg = orgs.find((o) => o.id === activeOrgId) || null;
  const confirm = useConfirm();
  const prompt = usePrompt();
  const [showCreateOrg, setShowCreateOrg] = useState<boolean>(false);

  const openProject = (id: string) => {
    setProjectId(id);
    navigate(`/projects/${id}`);
  };
  const [error, setError] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(true);
  const [newProjectName, setNewProjectName] = useState<string>('');
  const [newProjectDesc, setNewProjectDesc] = useState<string>('');
  const [isCreating, setIsCreating] = useState<boolean>(false);
  const [isEditingProject, setIsEditingProject] = useState<boolean>(false);
  const [editingProjectId, setEditingProjectId] = useState<string | null>(null);
  const [editProjectName, setEditProjectName] = useState<string>('');
  const [editProjectDesc, setEditProjectDesc] = useState<string>('');
  const [templates, setTemplates] = useState<Template[]>([]);
  const [createMode, setCreateMode] = useState<'blank' | 'templates' | 'examples'>('blank');
  const [selectedTemplateId, setSelectedTemplateId] = useState<string>('');
  const fileInputRef = React.useRef<HTMLInputElement>(null);

  // Load projects on mount and whenever the active workspace changes.
  useEffect(() => {
    loadProjects();
    loadTemplates();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId]);

  const loadProjects = async () => {
    try {
      setLoading(true);
      const response = await projectAPI.list();
      setProjects(response.data || []);
      setError('');
    } catch (err: any) {
      console.error('Failed to load projects:', err);
      setError(`Failed to load projects: ${apiErrorMessage(err)}`);
      setProjects([]);
    } finally {
      setLoading(false);
    }
  };

  const loadTemplates = async () => {
    try {
      const response = await templateAPI.list();
      setTemplates(response.data || []);
    } catch (err: any) {
      console.error('Failed to load templates:', err);
      setTemplates([]);
    }
  };

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    
    if (!newProjectName.trim()) {
      setError('Project name is required');
      return;
    }

    if (createMode !== 'blank' && !selectedTemplateId) {
      setError('Please select a template or example');
      return;
    }

    try {
      if (createMode === 'blank') {
        const response = await projectAPI.create({
          name: newProjectName,
          description: newProjectDesc,
        });
        addProject(response.data);
        openProject(response.data.id);
      } else {
        const response = await templateAPI.createProject(
          selectedTemplateId,
          newProjectName,
          newProjectDesc
        );
        addProject(response.data);
        openProject(response.data.id);
      }
      setNewProjectName('');
      setNewProjectDesc('');
      setSelectedTemplateId('');
      setCreateMode('blank');
      setIsCreating(false);
      setError('');
    } catch (err: any) {
      console.error('Failed to create project:', err);
      setError(`Failed to create project: ${apiErrorMessage(err)}`);
    }
  };

  const handleDeleteProject = async (id: string) => {
    const ok = await confirm({
      title: 'Delete project',
      message: 'Are you sure you want to delete this project and all its artifacts?',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) {
      return;
    }

    try {
      await projectAPI.delete(id);
      removeProject(id);
      if (projectId === id) {
        setProjectId('');
      }
      setError('');
    } catch (err: any) {
      console.error('Failed to delete project:', err);
      setError(`Failed to delete project: ${apiErrorMessage(err)}`);
    }
  };

  const handleSelectProject = (id: string) => {
    openProject(id);
  };

  const handleEditProject = (project: Project, e: React.MouseEvent) => {
    e.stopPropagation();
    setIsCreating(false);
    setIsEditingProject(true);
    setEditingProjectId(project.id);
    setEditProjectName(project.name);
    setEditProjectDesc(project.description || '');
    setError('');
  };

  const handleUpdateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingProjectId) return;
    if (!editProjectName.trim()) {
      setError('Project name is required');
      return;
    }

    try {
      const response = await projectAPI.update(editingProjectId, {
        name: editProjectName,
        description: editProjectDesc,
      });
      updateProject(response.data);
      setIsEditingProject(false);
      setEditingProjectId(null);
      setEditProjectName('');
      setEditProjectDesc('');
      setError('');
    } catch (err: any) {
      console.error('Failed to update project:', err);
      setError(`Failed to update project: ${apiErrorMessage(err)}`);
    }
  };

  const cancelEditProject = () => {
    setIsEditingProject(false);
    setEditingProjectId(null);
    setEditProjectName('');
    setEditProjectDesc('');
  };

  const handleExportProject = async (id: string, format: 'json' | 'csv', e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await projectAPI.export(id, format);
      setError('');
    } catch (err: any) {
      console.error('Failed to export project:', err);
      setError(`Failed to export project: ${apiErrorMessage(err)}`);
    }
  };

  const handleImportClick = () => {
    fileInputRef.current?.click();
  };

  const handleImportProject = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    try {
      const response = await projectAPI.import(file);
      const newProjectId = response.data.project_id;
      
      // Reload projects to include the new one
      await loadProjects();
      
      // Select the newly imported project
      openProject(newProjectId);
      setError('');
      
      // Reset file input
      e.target.value = '';
    } catch (err: any) {
      console.error('Failed to import project:', err);
      setError(`Failed to import project: ${apiErrorMessage(err)}`);
      e.target.value = '';
    }
  };

  const handleSaveTemplate = async (project: Project, e: React.MouseEvent) => {
    e.stopPropagation();
    const nameInput = await prompt({
      title: 'Save as template',
      label: 'Template name',
      defaultValue: project.name,
    });
    if (nameInput === null) return;
    const name = nameInput;
    if (!name.trim()) {
      setError('Template name is required');
      return;
    }
    const descriptionInput = await prompt({
      title: 'Save as template',
      label: 'Template description (optional)',
      defaultValue: project.description || '',
      allowEmpty: true,
    });
    if (descriptionInput === null) return;
    const description = descriptionInput;

    try {
      await templateAPI.create(project.id, name.trim(), description.trim());
      await loadTemplates();
      setError('');
    } catch (err: any) {
      console.error('Failed to save template:', err);
      setError(`Failed to save template: ${apiErrorMessage(err)}`);
    }
  };

  const templatesOnly = templates.filter((t) => t.source === 'database');
  const exampleTemplates = templates.filter((t) => t.source === 'file');

  return (
    <>
      <Navbar title="Projects" showWorkspaceControls />
      <div className="project-list-container">

      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: 10, marginBottom: 16 }}>
        {/* Plain anchor styled like the button: the manual opens in a new
            tab so it doesn't navigate away from the projects list. */}
        <a
          className="button-secondary"
          href="/manual"
          target="_blank"
          rel="noopener"
          style={{ padding: '8px 14px', fontSize: 13, textDecoration: 'none', display: 'inline-block' }}
          title="Open the user manual in a new tab"
        >
          ? Help
        </a>
      </div>


      {error && (
        <div className="error-message">
          {error}
          <button 
            onClick={() => setError('')}
            style={{ marginLeft: '10px', background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer' }}
          >
            ✕
          </button>
        </div>
      )}

      {loading ? (
        <div className="loading">Loading projects...</div>
      ) : (
        <>
          <div className="projects-grid">
            {projects.length === 0 ? (
              <div className="create-import-placeholder">
                <h3>Get Started</h3>
                <p>Create a new project or import an existing one</p>
                <div className="placeholder-actions">
                  <button
                    className="button button-primary"
                    onClick={() => {
                      setCreateMode('blank');
                      setIsCreating(true);
                    }}
                  >
                    + New Project
                  </button>
                  <button
                    className="button button-secondary"
                    onClick={handleImportClick}
                  >
                    ↑ Import Project
                  </button>
                  {activeOrg?.type === 'personal' && (
                    <button
                      className="button button-secondary"
                      onClick={() => setShowCreateOrg(true)}
                      title="Set up a shared workspace for your company"
                    >
                      Create a company workspace
                    </button>
                  )}
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept=".json"
                    style={{ display: 'none' }}
                    onChange={handleImportProject}
                  />
                </div>
              </div>
            ) : (
              projects.map((project) => (
                <div
                  key={project.id}
                  className={`project-card ${projectId === project.id ? 'selected' : ''}`}
                  onClick={() => handleSelectProject(project.id)}
                >
                  <div className="project-card-header">
                    <h3>{project.name}</h3>
                    <div className="project-actions">
                      <button
                        className="icon-btn template-btn"
                        onClick={(e) => handleSaveTemplate(project, e)}
                        title="Save as template"
                      >
                        ⧉
                      </button>
                      <button
                        className="icon-btn edit-btn"
                        onClick={(e) => handleEditProject(project, e)}
                        title="Edit project"
                      >
                        ✎
                      </button>
                      <button
                        className="icon-btn export-btn export-format-btn"
                        onClick={(e) => handleExportProject(project.id, 'json', e)}
                        title="Export project as JSON (full project, re-importable)"
                      >
                        ↓ JSON
                      </button>
                      <button
                        className="icon-btn export-btn export-format-btn"
                        onClick={(e) => handleExportProject(project.id, 'csv', e)}
                        title="Export artifacts as CSV (flat spreadsheet rows)"
                      >
                        ↓ CSV
                      </button>
                      <button
                        className="icon-btn delete-btn"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDeleteProject(project.id);
                        }}
                        title="Delete project"
                      >
                        ✕
                      </button>
                    </div>
                  </div>
                  {project.description && (
                    <p className="project-description">{project.description}</p>
                  )}
                  <div className="project-footer">
                    <code className="project-id">{project.id.substring(0, 8)}...</code>
                    <span className="project-date">
                      {new Date(project.created_at).toLocaleDateString()}
                    </span>
                  </div>
                </div>
              ))
            )}
          </div>

          {isEditingProject ? (
            <div className="create-project-form">
              <h3>Edit Project</h3>
              <form onSubmit={handleUpdateProject}>
                <div className="form-group">
                  <label htmlFor="edit-name">Project Name *</label>
                  <input
                    id="edit-name"
                    type="text"
                    value={editProjectName}
                    onChange={(e) => setEditProjectName(e.target.value)}
                    placeholder="Enter project name"
                    autoFocus
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="edit-desc">Description</label>
                  <textarea
                    id="edit-desc"
                    value={editProjectDesc}
                    onChange={(e) => setEditProjectDesc(e.target.value)}
                    placeholder="Enter project description (optional)"
                    rows={3}
                  />
                </div>
                <div className="form-actions">
                  <button type="submit" className="button button-primary">
                    Update Project
                  </button>
                  <button
                    type="button"
                    className="button button-secondary"
                    onClick={cancelEditProject}
                  >
                    Cancel
                  </button>
                </div>
              </form>
            </div>
          ) : isCreating ? (
            <div className="create-project-form">
              <h3>Create New Project</h3>
              <form onSubmit={handleCreateProject}>
                <div className="form-group">
                  <label htmlFor="mode">Project Type</label>
                  <select
                    id="mode"
                    value={createMode}
                    onChange={(e) => {
                      const mode = e.target.value as 'blank' | 'templates' | 'examples';
                      setCreateMode(mode);
                      setSelectedTemplateId('');
                      if (mode === 'blank') {
                        setNewProjectName('');
                        setNewProjectDesc('');
                      }
                    }}
                  >
                    <option value="blank">Blank Project</option>
                    <option value="templates" disabled={templatesOnly.length === 0}>Templates</option>
                    <option value="examples" disabled={exampleTemplates.length === 0}>Examples</option>
                  </select>
                </div>

                {createMode === 'templates' && (
                  <div className="form-group">
                    <label htmlFor="template">Template</label>
                    <select
                      id="template"
                      value={selectedTemplateId}
                      onChange={(e) => {
                        const value = e.target.value;
                        setSelectedTemplateId(value);
                        const selected = templatesOnly.find((t) => t.id === value);
                        if (selected) {
                          setNewProjectName(selected.name);
                          setNewProjectDesc(selected.description || '');
                        }
                      }}
                    >
                      <option value="">-- Select template --</option>
                      {templatesOnly.map((template) => (
                        <option key={template.id} value={template.id}>
                          {template.is_default ? '[Default] ' : ''}{template.name}
                        </option>
                      ))}
                    </select>
                  </div>
                )}

                {createMode === 'examples' && (
                  <div className="form-group">
                    <label htmlFor="example">Example</label>
                    <select
                      id="example"
                      value={selectedTemplateId}
                      onChange={(e) => {
                        const value = e.target.value;
                        setSelectedTemplateId(value);
                        const selected = exampleTemplates.find((t) => t.id === value);
                        if (selected) {
                          setNewProjectName(selected.name);
                          setNewProjectDesc(selected.description || '');
                        }
                      }}
                    >
                      <option value="">-- Select example --</option>
                      {exampleTemplates.map((template) => (
                        <option key={template.id} value={template.id}>
                          {template.name}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
                <div className="form-group">
                  <label htmlFor="name">Project Name *</label>
                  <input
                    id="name"
                    type="text"
                    value={newProjectName}
                    onChange={(e) => setNewProjectName(e.target.value)}
                    placeholder="Enter project name"
                    autoFocus
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="desc">Description</label>
                  <textarea
                    id="desc"
                    value={newProjectDesc}
                    onChange={(e) => setNewProjectDesc(e.target.value)}
                    placeholder="Enter project description (optional)"
                    rows={3}
                  />
                </div>
                <div className="form-actions">
                  <button type="submit" className="button button-primary">
                    Create Project
                  </button>
                  <button
                    type="button"
                    className="button button-secondary"
                    onClick={() => {
                      setIsCreating(false);
                      setCreateMode('blank');
                      setSelectedTemplateId('');
                      setNewProjectName('');
                      setNewProjectDesc('');
                    }}
                  >
                    Cancel
                  </button>
                </div>
              </form>
            </div>
          ) : (
            <div className="create-button-container">
              <button
                className="button button-primary"
                onClick={() => {
                  setCreateMode('blank');
                  setIsCreating(true);
                }}
              >
                + New Project
              </button>
              <button
                className="button button-secondary"
                onClick={handleImportClick}
              >
                ↑ Import Project
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept=".json"
                style={{ display: 'none' }}
                onChange={handleImportProject}
              />
            </div>
          )}
        </>
      )}
    </div>
    {showCreateOrg && (
      <CreateOrgModal
        onClose={() => setShowCreateOrg(false)}
        onCreated={() => navigate('/projects')}
      />
    )}
    </>
  );
};

export default ProjectList;
