import React, { useState, useEffect } from 'react';
import { useAppStore } from '../state/store';
import { projectAPI, Project } from '../api/client';
import './ProjectList.css';

export const ProjectList: React.FC = () => {
  const { projectId, setProjectId, projects, setProjects, addProject, removeProject } = useAppStore();
  const [error, setError] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(true);
  const [newProjectName, setNewProjectName] = useState<string>('');
  const [newProjectDesc, setNewProjectDesc] = useState<string>('');
  const [isCreating, setIsCreating] = useState<boolean>(false);

  // Load projects on mount
  useEffect(() => {
    loadProjects();
  }, []);

  const loadProjects = async () => {
    try {
      setLoading(true);
      const response = await projectAPI.list();
      setProjects(response.data || []);
      setError('');
    } catch (err: any) {
      console.error('Failed to load projects:', err);
      setError(`Failed to load projects: ${err.response?.data || err.message}`);
      setProjects([]);
    } finally {
      setLoading(false);
    }
  };

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    
    if (!newProjectName.trim()) {
      setError('Project name is required');
      return;
    }

    try {
      const response = await projectAPI.create({
        name: newProjectName,
        description: newProjectDesc,
      });
      addProject(response.data);
      setNewProjectName('');
      setNewProjectDesc('');
      setIsCreating(false);
      setError('');
    } catch (err: any) {
      console.error('Failed to create project:', err);
      setError(`Failed to create project: ${err.response?.data || err.message}`);
    }
  };

  const handleDeleteProject = async (id: string) => {
    if (!window.confirm('Are you sure you want to delete this project and all its artifacts?')) {
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
      setError(`Failed to delete project: ${err.response?.data || err.message}`);
    }
  };

  const handleSelectProject = (id: string) => {
    setProjectId(id);
  };

  return (
    <div className="project-list-container">
      <div className="project-list-header">
        <h1>Projects</h1>
        <p>Select or create a project to get started</p>
      </div>

      {error && (
        <div className="error-message">
          {error}
          <button 
            onClick={() => setError('')}
            style={{ marginLeft: '10px', background: 'none', border: 'none', color: '#e74c3c', cursor: 'pointer' }}
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
              <div className="no-projects">
                <p>No projects yet. Create one to get started!</p>
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
                    <button
                      className="delete-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleDeleteProject(project.id);
                      }}
                      title="Delete project"
                    >
                      ✕
                    </button>
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

          {isCreating ? (
            <div className="create-project-form">
              <h3>Create New Project</h3>
              <form onSubmit={handleCreateProject}>
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
                onClick={() => setIsCreating(true)}
              >
                + New Project
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default ProjectList;
