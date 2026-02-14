import React, { useState, useEffect } from 'react';
import { useAppStore } from '../state/store';
import { artifactAPI, linkAPI, Artifact, Link } from '../api/client';
import { ArtifactEditor } from '../components/ArtifactEditor';
import { ArtifactList } from '../components/ArtifactList';
import { ArtifactDetails } from '../components/ArtifactDetails';
import { LinkPanel } from '../components/LinkPanel';

interface ModuleViewProps {
  onSwitchProject?: () => void;
}

export const ModuleView: React.FC<ModuleViewProps> = ({ onSwitchProject }) => {
  const [isCreating, setIsCreating] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editingArtifact, setEditingArtifact] = useState<Artifact | undefined>();
  const [error, setError] = useState<string>('');
  const [allLinks, setAllLinks] = useState<Link[]>([]);

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
  } = useAppStore();

  // Load artifacts on component mount
  useEffect(() => {
    if (projectId) {
      loadArtifacts();
      loadLinks();
    }
  }, [projectId]);

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
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2 style={{ margin: 0 }}>Artifacts</h2>
        <button
          onClick={onSwitchProject}
          style={{
            padding: '8px 16px',
            backgroundColor: '#95a5a6',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
            fontSize: '14px',
          }}
        >
          ← Switch Project
        </button>
      </div>
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
          artifacts={artifacts}
          selectedId={selectedArtifactId || undefined}
          onSelect={setSelectedArtifactId}
          onEdit={handleEditArtifact}
          onDelete={handleDeleteArtifact}
        />
      </div>

      <div>
        {isEditing && editingArtifact && (
          <ArtifactEditor
            artifact={editingArtifact}
            onSave={handleUpdateArtifact}
            onCancel={() => {
              setIsEditing(false);
              setEditingArtifact(undefined);
            }}
          />
        )}

        {selectedArtifact && !isEditing && (
          <>
            <ArtifactDetails artifact={selectedArtifact} links={allLinks} artifacts={artifacts} />
            <LinkPanel
              artifacts={artifacts}
              selectedArtifactId={selectedArtifactId || undefined}
              onCreateLink={handleCreateLink}
            />
          </>
        )}

        {!selectedArtifact && !isEditing && (
          <div className="card">
            <h3>No Artifact Selected</h3>
            <p>Select an artifact from the list to view details.</p>
          </div>
        )}
      </div>
    </div>
    </div>
  );
};

export default ModuleView;
