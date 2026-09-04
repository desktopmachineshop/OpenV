import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAppStore } from '../state/store';
import {
  agentRunsAPI,
  agentsAPI,
  guidedAPI,
  projectAPI,
  sharedProductsAPI,
  templateAPI,
  workerStatusAPI,
  Project,
  Template,
} from '../api/client';
import {
  describeProductFlaws,
  fromSharedProduct,
  generateRandomProduct,
  inventProductPrompt,
  isInventedProduct,
  isSharedProduct,
  loadInventedProducts,
  parseInventedProduct,
  saveInventedProduct,
  toSharePayload,
  RandomProduct,
} from '../utils/randomProduct';
import { apiErrorMessage } from '../api/errors';
import { Navbar } from './Navbar';
import { HelpSidebar } from './HelpSidebar';
import { DownloadWizard } from './DownloadWizard';
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
  // The project whose download wizard is open, if any.
  const [downloadProjectId, setDownloadProjectId] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [newProjectName, setNewProjectName] = useState<string>('');
  const [newProjectDesc, setNewProjectDesc] = useState<string>('');
  const [isCreating, setIsCreating] = useState<boolean>(false);
  const [isEditingProject, setIsEditingProject] = useState<boolean>(false);
  const [editingProjectId, setEditingProjectId] = useState<string | null>(null);
  const [editProjectName, setEditProjectName] = useState<string>('');
  const [editProjectDesc, setEditProjectDesc] = useState<string>('');
  const [templates, setTemplates] = useState<Template[]>([]);
  const [createMode, setCreateMode] = useState<'blank' | 'templates' | 'examples' | 'random'>('blank');
  const [selectedTemplateId, setSelectedTemplateId] = useState<string>('');
  const [randomProduct, setRandomProduct] = useState<RandomProduct | null>(null);

  // Agent-invented products: enabled only while a runner is online, since the
  // invention runs on the member's own machine with their own AI subscription.
  const [runnerOnline, setRunnerOnline] = useState<boolean | null>(null);
  const [inventing, setInventing] = useState<boolean>(false);
  const [inventError, setInventError] = useState<string>('');
  const [inventedByAgent, setInventedByAgent] = useState<boolean>(false);
  // Concepts already shown this session, so each invention asks for something
  // new rather than re-treading the same joke.
  const shownProductsRef = React.useRef<string[]>([]);

  // Inventions are also kept in this browser, as the fallback for one that
  // could not reach the shared pool (rate limit, no network). Read at roll
  // time rather than held in state — the pool is what the UI talks about.

  // The community pool: products other people chose to share. Everyone reads
  // the same list, so the roll gets richer over time without anyone spending
  // an agent run. Sharing is deliberate (the button below) — nothing an agent
  // invents leaves this workspace until someone reads it and presses Share.
  const [shared, setShared] = useState<RandomProduct[]>([]);
  // Set when an invention could not reach the shared pool (rate limit, a
  // name already taken, no network). The product still works locally, so
  // this is a note rather than a failure.
  const [shareError, setShareError] = useState<string>('');

  const loadSharedProducts = React.useCallback(async () => {
    try {
      const res = await sharedProductsAPI.list();
      setShared((res.data || []).map(fromSharedProduct));
    } catch {
      // The community pool is a bonus, not a dependency: the built-in
      // concepts and this browser's inventions still roll without it.
      setShared([]);
    }
  }, []);

  useEffect(() => {
    if (createMode === 'random') {
      loadSharedProducts();
    }
  }, [createMode, loadSharedProducts]);

  const applyProduct = (product: RandomProduct, fromAgent: boolean) => {
    setRandomProduct(product);
    setShareError('');
    setNewProjectName(product.name);
    setNewProjectDesc(product.description);
    setInventedByAgent(fromAgent);
    shownProductsRef.current = [
      ...shownProductsRef.current,
      `${product.name} (${product.category})`,
    ].slice(-12);
  };

  const rollRandomProduct = () => {
    setInventError('');
    // One collection: the built-in concepts plus every stored product. An
    // invention normally reaches the shared pool, so this browser's copies
    // only add the ones that did not — deduped by name so a product cannot
    // be twice as likely to roll as its neighbours.
    const kept = loadInventedProducts().filter(
      (k) => !shared.some((p) => p.name.toLowerCase() === k.name.toLowerCase())
    );
    const rolled = generateRandomProduct([...shared, ...kept]);
    applyProduct(rolled, isInventedProduct(rolled, kept));
  };

  // Publish an invention to the pool everyone rolls from. Inventions are
  // shared automatically: the point of the pool is that it grows on its own,
  // and a product nobody else can roll is a product invented twice. The
  // request carries the member's own session, so the server still records who
  // published it, sanitizes the text, and applies the per-workspace daily
  // cap — a failure there is a note, never a lost invention.
  const publishInvention = async (product: RandomProduct) => {
    try {
      const res = await sharedProductsAPI.publish(toSharePayload(product));
      setShared((prev) => [fromSharedProduct(res.data), ...prev]);
    } catch (err: any) {
      setShareError(`Kept in this browser, but not added to the shared pool: ${apiErrorMessage(err)}`);
    }
  };

  // Flag a shared product for review and move on. Enough distinct reporters
  // hide it for everyone; a platform admin can remove it outright.
  const reportProduct = async () => {
    if (!randomProduct?.sharedId) return;
    const id = randomProduct.sharedId;
    setShareError('');
    try {
      await sharedProductsAPI.report(id);
      setShared((prev) => prev.filter((p) => p.sharedId !== id));
      setShareError('Reported for review, and taken out of your roll list.');
    } catch (err: any) {
      setShareError(apiErrorMessage(err));
    }
  };

  // Runner presence drives whether the invent button is live. Checked when the
  // random mode is opened and after an invention finishes.
  const refreshRunnerPresence = React.useCallback(async () => {
    if (!activeOrgId) return;
    try {
      const res = await workerStatusAPI.get(activeOrgId);
      setRunnerOnline((res.data.workers || []).some((w) => w.online && !w.revoked));
    } catch {
      setRunnerOnline(false);
    }
  }, [activeOrgId]);

  useEffect(() => {
    if (createMode === 'random') refreshRunnerPresence();
  }, [createMode, refreshRunnerPresence]);

  // Launch the invention on the connected runner and wait for its result. The
  // agent replies with one JSON product; anything else is reported rather than
  // silently replaced with a canned concept.
  // Run the invention brief on the connected runner and wait for the result.
  // One run, one JSON product — and the product has to hold together as well
  // as a built-in one before it goes anywhere, so a weak reply buys one retry
  // that is told exactly what to fix rather than being quietly accepted.
  const runInvention = async (agentSlug: string, rejected: string[]): Promise<RandomProduct | null> => {
    const launched = await agentsAPI.launchRun(agentSlug, {
      prompt: inventProductPrompt(shownProductsRef.current, rejected),
    });

    // Runs are queued, then claimed by the runner, so this is
    // seconds-to-a-minute rather than instant.
    const deadline = Date.now() + 120000;
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      const run = (await agentRunsAPI.get(launched.data.id)).data;
      if (run.status === 'succeeded') {
        const product = parseInventedProduct(run.final_text);
        if (!product) {
          setInventError('Your agent replied, but not with a usable product. Try again.');
        }
        return product;
      }
      if (['failed', 'cancelled', 'timed_out'].includes(run.status)) {
        setInventError(`The invention run ${run.status}${run.error ? `: ${run.error}` : ''}.`);
        return null;
      }
      if (Date.now() > deadline) {
        setInventError('The invention run is taking unusually long — check Runs for progress.');
        return null;
      }
    }
  };

  const inventProduct = async () => {
    setInventing(true);
    setInventError('');
    try {
      const agents = (await agentsAPI.list()).data || [];
      const agent =
        agents.find((a) => a.slug === 'requirements-copilot') ||
        agents.find((a) => a.slug === 'chief-of-staff') ||
        agents[0];
      if (!agent) {
        setInventError('No agents are defined in this workspace yet.');
        return;
      }

      let product = await runInvention(agent.slug, []);
      if (!product) return;

      // The gate is what the built-in concepts get by construction: a name
      // that riffs on the gimmick, a vision carrying it, an audience that is
      // a moment. A miss costs one more run rather than diluting the pool.
      let flaws = describeProductFlaws(product);
      if (flaws.length) {
        const retry = await runInvention(agent.slug, flaws);
        if (retry) {
          const retryFlaws = describeProductFlaws(retry);
          if (retryFlaws.length < flaws.length) {
            product = retry;
            flaws = retryFlaws;
          }
        }
      }

      applyProduct(product, true);
      if (flaws.length) {
        // Usable, but below the bar: show it, keep it here, and leave the
        // shared collection alone.
        saveInventedProduct(product);
        setInventError(`Your agent's product is a bit off (${flaws[0]}) — reroll or invent again. Not added to the shared collection.`);
        return;
      }
      saveInventedProduct(product);
      await publishInvention(product);
    } catch (err: any) {
      setInventError(`Could not invent a product: ${apiErrorMessage(err)}`);
    } finally {
      setInventing(false);
      refreshRunnerPresence();
    }
  };

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

    if (createMode !== 'blank' && createMode !== 'random' && !selectedTemplateId) {
      setError('Please select a template or example');
      return;
    }

    try {
      if (createMode === 'random' && randomProduct) {
        const response = await projectAPI.create({
          name: newProjectName,
          description: newProjectDesc,
        });
        addProject(response.data);
        setProjectId(response.data.id);
        // Seed the Guided Wizard's framing step with the rolled concept and
        // land in the wizard, where the connected V&V Assistant picks it up
        // and helps expand it into personas, needs, and requirements.
        try {
          const session = await guidedAPI.start(response.data.id);
          await guidedAPI.saveStep(session.data.id, 1, {
            step_1: {
              vision: randomProduct.vision,
              problem_statement: randomProduct.problem,
              target_users: randomProduct.targetUsers,
            },
          });
          navigate(`/projects/${response.data.id}/guided`);
        } catch {
          // Wizard seeding is best-effort; the project itself exists.
          navigate(`/projects/${response.data.id}`);
        }
      } else if (createMode === 'blank') {
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
      setRandomProduct(null);
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
      {/* The same floating, context-aware help bubble every project page
          carries, rather than a one-off link to the manual index: on this
          page it explains creating, importing, templating and exporting
          projects. */}
      <HelpSidebar />
      {downloadProjectId && (
        <DownloadWizard
          projectId={downloadProjectId}
          onClose={() => setDownloadProjectId(null)}
        />
      )}
      <div className="project-list-container">


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
                        className="icon-btn export-btn"
                        onClick={(e) => {
                          e.stopPropagation();
                          setDownloadProjectId(project.id);
                        }}
                        title="Download this project — choose a format, sections and attachments"
                      >
                        ↓
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
                      const mode = e.target.value as 'blank' | 'templates' | 'examples' | 'random';
                      setCreateMode(mode);
                      setSelectedTemplateId('');
                      if (mode === 'blank') {
                        setNewProjectName('');
                        setNewProjectDesc('');
                        setRandomProduct(null);
                      } else if (mode === 'random') {
                        rollRandomProduct();
                      }
                    }}
                  >
                    <option value="blank">Blank Project</option>
                    <option value="templates" disabled={templatesOnly.length === 0}>Templates</option>
                    <option value="examples" disabled={exampleTemplates.length === 0}>Examples</option>
                    <option value="random">🎲 Random product (for testing)</option>
                  </select>
                </div>

                {createMode === 'random' && randomProduct && (
                  <div
                    style={{
                      background: 'var(--surface-alt)',
                      border: '1px solid var(--border-soft)',
                      borderRadius: 4,
                      padding: '10px 14px',
                      fontSize: 13,
                      marginBottom: 12,
                    }}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 10 }}>
                      <strong>
                        {randomProduct.name}{' '}
                        <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>({randomProduct.category})</span>
                        {inventedByAgent && (
                          <span
                            style={{ fontWeight: 400, color: 'var(--accent)', marginLeft: 6, fontSize: 12 }}
                            title="Invented by your connected agent"
                          >
                            ✨ invented by your agent
                          </span>
                        )}
                        {isSharedProduct(randomProduct) && (
                          <span
                            style={{ fontWeight: 400, color: 'var(--text-muted)', marginLeft: 6, fontSize: 12 }}
                            title="Shared by another OpenV user — report it if it does not belong here"
                          >
                            🌍 shared by the community
                          </span>
                        )}
                      </strong>
                      <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
                        <button
                          type="button"
                          className="button-secondary button"
                          style={{ width: 'auto', padding: '2px 10px', fontSize: 12 }}
                          onClick={rollRandomProduct}
                          disabled={inventing}
                        >
                          🎲 Reroll
                        </button>
                        <button
                          type="button"
                          className="button-secondary button"
                          style={{
                            width: 'auto',
                            padding: '2px 10px',
                            fontSize: 12,
                            opacity: runnerOnline && !inventing ? 1 : 0.5,
                            cursor: runnerOnline && !inventing ? 'pointer' : 'not-allowed',
                          }}
                          onClick={inventProduct}
                          disabled={!runnerOnline || inventing}
                          title={
                            runnerOnline
                              ? 'Your connected agent invents a brand-new product concept'
                              : 'Connect an agent (Workspace settings → Runners) to invent brand-new products'
                          }
                        >
                          {inventing ? '✨ Inventing…' : '✨ Invent with agent'}
                        </button>
                        {isSharedProduct(randomProduct) && (
                          <button
                            type="button"
                            className="button-secondary button"
                            style={{ width: 'auto', padding: '2px 10px', fontSize: 12 }}
                            onClick={reportProduct}
                            title="Flag this shared product for review"
                          >
                            ⚑ Report
                          </button>
                        )}
                      </div>
                    </div>
                    <p style={{ margin: '6px 0 4px' }}>{randomProduct.description}</p>
                    <p style={{ margin: '4px 0', color: 'var(--text-muted)' }}>
                      <em>Vision:</em> {randomProduct.vision}
                    </p>
                    <p style={{ margin: '4px 0 0', color: 'var(--text-muted)' }}>
                      <em>For:</em> {randomProduct.targetUsers}
                    </p>
                  </div>
                )}

                {createMode === 'random' && (inventError || runnerOnline === false) && (
                  <div
                    style={{
                      fontSize: 12,
                      color: inventError ? 'var(--danger-strong)' : 'var(--text-muted)',
                      marginBottom: 10,
                    }}
                  >
                    {inventError ||
                      'No agent is connected, so “Invent with agent” is unavailable — the built-in concepts still work. Connect one from Workspace settings → Runners.'}
                  </div>
                )}

                {createMode === 'random' && shareError && (
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
                    {shareError}
                  </div>
                )}

                {/* One line, outside the concept card — inside it, notices
                    read as part of the fake product. Inventions publish
                    themselves, so this has to be readable before anyone
                    presses "Invent with agent", not after. */}
                {createMode === 'random' && (
                  <div
                    style={{
                      display: 'flex',
                      gap: 8,
                      alignItems: 'flex-start',
                      fontSize: 12,
                      color: 'var(--text-muted)',
                      marginBottom: 12,
                    }}
                  >
                    <span aria-hidden>🌍</span>
                    <span>
                      Reroll picks from the shared collection of fake products — the built-ins plus{' '}
                      {shared.length === 1
                        ? '1 invented by another OpenV user'
                        : `${shared.length} invented by other OpenV users`}; ⚑ Report anything that
                      does not belong. <strong>What your agent invents joins the collection
                      automatically</strong>, so keep real people, customers, and unreleased work out
                      of it. Creating drops you into the Guided Wizard with this framing pre-filled.
                    </span>
                  </div>
                )}

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
