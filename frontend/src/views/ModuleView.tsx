import React, { useState, useEffect } from 'react';
import { useAppStore } from '../state/store';
import { artifactAPI, linkAPI, attachmentAPI, Artifact, Link, Attachment } from '../api/client';
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

  // Load artifacts on component mount
  useEffect(() => {
    if (projectId) {
      loadArtifacts();
      loadLinks();
    }
  }, [projectId]);

  // Load attachments when artifact is selected or when editing
  useEffect(() => {
    const artifactIdToLoad = editingArtifact?.id || selectedArtifactId;
    if (artifactIdToLoad) {
      loadAttachments(artifactIdToLoad);
    } else {
      setAttachments([]);
    }
  }, [selectedArtifactId, editingArtifact?.id]);

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

  // Get unique artifact types for filter dropdown
  const getArtifactTypes = () => {
    const types = new Set(artifacts.map((a) => a.type));
    return Array.from(types).sort();
  };

  // Filter artifacts based on selected type
  const filteredArtifacts = filterType
    ? artifacts.filter((a) => a.type === filterType)
    : artifacts;

  if (!projectId) {
    return (
      <div className="card">
        <h3>No Project Selected</h3>
        <p>Please select a project to get started.</p>
      </div>
    );
  }

  const selectedArtifact = artifacts.find((a) => a.id === selectedArtifactId);

  return (
    <>
      <HelpSidebar />
      <Navbar
        title={`Project: ${projects.find((p) => p.id === projectId)?.name || 'Unknown'}`}
        onSwitchProject={onSwitchProject}
        showSwitchButton={true}
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
            <option value="">All Types ({artifacts.length})</option>
            {getArtifactTypes().map((type) => (
              <option key={type} value={type}>
                {type} ({artifacts.filter((a) => a.type === type).length})
              </option>
            ))}
          </select>
        </div>

        {isCreating && (
          <ArtifactEditor
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
        />
      </div>

      <div>
        {isEditing && editingArtifact && (
          <>
            <ArtifactEditor
              artifact={editingArtifact}
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
            links={allLinks} 
            artifacts={artifacts}
            attachments={attachments}
            onDeleteAttachment={handleDeleteAttachment}
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
