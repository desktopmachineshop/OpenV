import React, { useState, useEffect } from 'react';
import { useAppStore } from '../state/store';
import { artifactAPI, linkAPI, attachmentAPI, baselineAPI, projectAPI, Artifact, Link, Attachment, Baseline, ProjectExport } from '../api/client';
import { ArtifactEditor } from '../components/ArtifactEditor';
import { ArtifactList } from '../components/ArtifactList';
import { ArtifactDetails } from '../components/ArtifactDetails';
import { LinkPanel } from '../components/LinkPanel';
import { HelpSidebar } from '../components/HelpSidebar';
import { Navbar } from '../components/Navbar';

interface ModuleViewProps {
  onSwitchProject?: () => void;
}

export const ModuleView: React.FC<ModuleViewProps> = ({ onSwitchProject }) => {
  const [isCreating, setIsCreating] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editingArtifact, setEditingArtifact] = useState<Artifact | undefined>();
  const [error, setError] = useState<string>('');
  const [allLinks, setAllLinks] = useState<Link[]>([]);
  const [filterType, setFilterType] = useState<string>(''); // '' means show all
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [uploadingAttachmentId, setUploadingAttachmentId] = useState<string | null>(null);
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [activeBaselineId, setActiveBaselineId] = useState<string>('live');
  const [baselineData, setBaselineData] = useState<ProjectExport | null>(null);

  const {
    projectId,
    artifacts,
    setArtifacts,
    addArtifact,
    updateArtifact,
    removeArtifact,
    addLink,
    selectedArtifactId,
    setSelectedArtifactId,
    projects,
  } = useAppStore();

  const isBaselineView = activeBaselineId !== 'live';

  // Load artifacts on component mount
  useEffect(() => {
    if (projectId) {
      loadArtifacts();
      loadLinks();
      loadBaselines();
      setActiveBaselineId('live');
      setBaselineData(null);
    }
  }, [projectId]);

  // Load attachments when artifact is selected or when editing
  useEffect(() => {
    if (isBaselineView) {
      setAttachments([]);
      return;
    }
    const artifactIdToLoad = editingArtifact?.id || selectedArtifactId;
    if (artifactIdToLoad) {
      loadAttachments(artifactIdToLoad);
    } else {
      setAttachments([]);
    }
  }, [selectedArtifactId, editingArtifact?.id, isBaselineView]);

  const loadArtifacts = async () => {
    try {
      const response = await artifactAPI.list(projectId);
      setArtifacts(response.data || []);
      setError('');
    } catch (error: any) {
      console.error('Failed to load artifacts:', error);
      setArtifacts([]);
      setError(`Failed to load artifacts: ${error.response?.data || error.message}`);
    }
  };

  const loadLinks = async () => {
    try {
      const response = await linkAPI.list(projectId);
      setAllLinks(response.data || []);
    } catch (error: any) {
      console.error('Failed to load links:', error);
      setAllLinks([]);
    }
  };

  const loadBaselines = async () => {
    if (!projectId) return;
    try {
      const response = await baselineAPI.list(projectId);
      setBaselines(response.data || []);
    } catch (error: any) {
      console.error('Failed to load baselines:', error);
      setBaselines([]);
    }
  };

  const loadAttachments = async (artifactId: string) => {
    try {
      const response = await attachmentAPI.listByArtifact(artifactId);
      setAttachments(response.data || []);
    } catch (error: any) {
      console.error('Failed to load attachments:', error);
      setAttachments([]);
    }
  };

  const handleUploadAttachment = async (file: File) => {
    if (!selectedArtifactId) {
      setError('No artifact selected');
      return;
    }

    setUploadingAttachmentId(selectedArtifactId);
    try {
      const response = await attachmentAPI.upload(selectedArtifactId, file);
      setAttachments([...attachments, response.data]);
      setError('');
    } catch (error: any) {
      console.error('Failed to upload attachment:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to upload image: ${errorMsg}`);
    } finally {
      setUploadingAttachmentId(null);
    }
  };

  const handleDeleteAttachment = async (attachmentId: string) => {
    try {
      await attachmentAPI.delete(attachmentId);
      setAttachments(attachments.filter((a) => a.id !== attachmentId));
      setError('');
    } catch (error: any) {
      console.error('Failed to delete attachment:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to delete image: ${errorMsg}`);
    }
  };

  const handleCreateArtifact = async (data: Partial<Artifact>) => {
    try {
      const response = await artifactAPI.create({
        project_id: projectId,
        ...data,
      });
      addArtifact(response.data);
      setIsCreating(false);
      setError('');
    } catch (error: any) {
      console.error('Failed to create artifact:', error);
      let errorMsg = 'Unknown error';
      if (error.response?.data) {
        errorMsg = typeof error.response.data === 'string' ? error.response.data : JSON.stringify(error.response.data);
      } else if (error.message) {
        errorMsg = error.message;
      }
      setError(`Failed to create artifact: ${errorMsg}`);
    }
  };

  const handleUpdateArtifact = async (data: Partial<Artifact>) => {
    try {
      if (!editingArtifact) return;
      const response = await artifactAPI.update(editingArtifact.id, data);
      updateArtifact(response.data);
      setIsEditing(false);
      setEditingArtifact(undefined);
      setError('');
    } catch (error: any) {
      console.error('Failed to update artifact:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to update artifact: ${errorMsg}`);
    }
  };

  const handleDeleteArtifact = async (id: string) => {
    if (!window.confirm('Are you sure you want to delete this artifact?')) {
      return;
    }
    try {
      await artifactAPI.delete(id);
      removeArtifact(id);
      if (selectedArtifactId === id) {
        setSelectedArtifactId(null);
      }
      setError('');
    } catch (error: any) {
      console.error('Failed to delete artifact:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to delete artifact: ${errorMsg}`);
    }
  };

  const handleEditArtifact = (artifact: Artifact) => {
    setSelectedArtifactId(artifact.id);
    setEditingArtifact(artifact);
    setIsEditing(true);
    setError('');
  };

  const handleCreateLink = async (linkData: Partial<Link>) => {
    try {
      const response = await linkAPI.create(linkData);
      addLink(response.data);
      setAllLinks([...allLinks, response.data]);
      alert('Link created successfully');
      setError('');
    } catch (error: any) {
      console.error('Failed to create link:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to create link: ${errorMsg}`);
    }
  };

  const handleBaselineChange = async (baselineId: string) => {
    setActiveBaselineId(baselineId);
    setSelectedArtifactId(null);
    setIsEditing(false);
    setIsCreating(false);
    setEditingArtifact(undefined);

    if (baselineId === 'live') {
      setBaselineData(null);
      return;
    }

    try {
      const response = await baselineAPI.get(baselineId);
      setBaselineData(response.data);
      setError('');
    } catch (error: any) {
      console.error('Failed to load baseline:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to load baseline: ${errorMsg}`);
    }
  };

  const handleCaptureBaseline = async () => {
    if (!projectId) return;
    const name = window.prompt('Baseline name:', '') || '';
    if (!name.trim()) {
      setError('Baseline name is required');
      return;
    }

    try {
      await baselineAPI.create(projectId, name.trim());
      await loadBaselines();
      setError('');
    } catch (error: any) {
      console.error('Failed to capture baseline:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to capture baseline: ${errorMsg}`);
    }
  };

  const handleDeleteBaseline = async (baselineId: string) => {
    if (baselineId === 'live') return;
    if (!window.confirm('Delete this baseline? This cannot be undone.')) {
      return;
    }

    try {
      await baselineAPI.delete(baselineId);
      await loadBaselines();
      setActiveBaselineId('live');
      setBaselineData(null);
      setError('');
    } catch (error: any) {
      console.error('Failed to delete baseline:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to delete baseline: ${errorMsg}`);
    }
  };

  const handleGenerateReport = async () => {
    if (!projectId) return;
    try {
      await projectAPI.report(projectId, activeBaselineId);
      setError('');
    } catch (error: any) {
      console.error('Failed to generate report:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to generate report: ${errorMsg}`);
    }
  };

  const activeArtifacts = isBaselineView ? (baselineData?.artifacts || []) : artifacts;
  const activeLinks = isBaselineView ? (baselineData?.links || []) : allLinks;

  // Get unique artifact types for filter dropdown
  const getArtifactTypes = () => {
    const types = new Set(activeArtifacts.map((a) => a.type));
    return Array.from(types).sort();
  };

  // Filter artifacts based on selected type
  const filteredArtifacts = filterType
    ? activeArtifacts.filter((a) => a.type === filterType)
    : activeArtifacts;

  if (!projectId) {
    return (
      <div className="card">
        <h3>No Project Selected</h3>
        <p>Please select a project to get started.</p>
      </div>
    );
  }

  const selectedArtifact = activeArtifacts.find((a) => a.id === selectedArtifactId);
  const detailAttachments = isBaselineView ? [] : attachments;

  return (
    <>
      <HelpSidebar />
      <Navbar
        title={(() => {
          const project = projects.find((p) => p.id === projectId);
          const name = project?.name || 'Unknown';
          const description = project?.description || '';
          return (
            <div style={{ display: 'inline-flex', flexDirection: 'column', alignItems: 'center', gap: '2px' }}>
              <div style={{ fontSize: '18px', fontWeight: 600 }}>{name}</div>
              {description && (
                <div style={{ fontSize: '11px', color: '#7f8c8d' }}>{description}</div>
              )}
            </div>
          );
        })()}
        onSwitchProject={onSwitchProject}
        showSwitchButton={true}
        baselineOptions={baselines.map((b) => ({ id: b.id, name: b.name }))}
        selectedBaselineId={activeBaselineId}
        onBaselineChange={handleBaselineChange}
        onCaptureBaseline={handleCaptureBaseline}
        onDeleteBaseline={handleDeleteBaseline}
        onGenerateReport={handleGenerateReport}
      />
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 2fr', gap: '20px' }}>
      <div>
        {error && (
          <div
            style={{
              padding: '12px',
              marginBottom: '15px',
              backgroundColor: 'rgba(231, 76, 60, 0.2)',
              border: '1px solid #e74c3c',
              borderRadius: '4px',
              color: '#e74c3c',
              fontSize: '12px',
            }}
          >
            {error}
          </div>
        )}
        {!isBaselineView && (
          <button
            onClick={() => {
              setIsCreating(!isCreating);
              setError('');
            }}
            className="button"
            style={{ width: '100%', marginBottom: '20px' }}
          >
            {isCreating ? 'Cancel' : '+ New Artifact'}
          </button>
        )}

        <div style={{ marginBottom: '20px' }}>
          <label style={{ display: 'block', fontSize: '12px', fontWeight: 'bold', marginBottom: '8px', color: '#2c3e50' }}>
            Filter by Type:
          </label>
          <select
            value={filterType}
            onChange={(e) => setFilterType(e.target.value)}
            style={{
              width: '100%',
              padding: '8px',
              borderRadius: '4px',
              border: '1px solid #bdc3c7',
              fontSize: '14px',
              backgroundColor: 'white',
              cursor: 'pointer',
            }}
          >
            <option value="">All Types ({activeArtifacts.length})</option>
            {getArtifactTypes().map((type) => (
              <option key={type} value={type}>
                {type} ({activeArtifacts.filter((a) => a.type === type).length})
              </option>
            ))}
          </select>
        </div>

        {isCreating && !isBaselineView && (
          <ArtifactEditor
            artifacts={artifacts}
            onSave={handleCreateArtifact}
            onCancel={() => {
              setIsCreating(false);
              setError('');
            }}
          />
        )}

        <ArtifactList
          artifacts={filteredArtifacts}
          selectedId={selectedArtifactId || undefined}
          onSelect={setSelectedArtifactId}
          onEdit={handleEditArtifact}
          onDelete={handleDeleteArtifact}
          readOnly={isBaselineView}
        />
      </div>

      <div>
        {!isBaselineView && isEditing && editingArtifact && (
          <>
            <ArtifactEditor
              artifact={editingArtifact}
              artifacts={artifacts}
              onSave={handleUpdateArtifact}
              onCancel={() => {
                setIsEditing(false);
                setEditingArtifact(undefined);
              }}
              attachments={attachments}
              onUploadAttachment={handleUploadAttachment}
              onDeleteAttachment={handleDeleteAttachment}
              isUploadLoading={uploadingAttachmentId === editingArtifact.id}
            />
            <LinkPanel
              artifacts={artifacts}
              selectedArtifactId={editingArtifact.id}
              onCreateLink={handleCreateLink}
              links={allLinks}
              title="Edit Artifact Links"
            />
          </>
        )}

        {selectedArtifact && !isEditing && (
          <ArtifactDetails 
            artifact={selectedArtifact} 
            links={activeLinks} 
            artifacts={activeArtifacts}
            attachments={detailAttachments}
            onDeleteAttachment={isBaselineView ? undefined : handleDeleteAttachment}
          />
        )}

        {!selectedArtifact && !isEditing && (
          <div className="card">
            <h3>No Artifact Selected</h3>
            <p>Select an artifact from the list to view details.</p>
          </div>
        )}
      </div>
      </div>
    </>
  );
};

export default ModuleView;
