import React, { useState, useEffect } from 'react';
import { Artifact, Attachment, Link } from '../api/client';
import { ImageGallery } from './ImageGallery';
import { LinkPanel } from './LinkPanel';

interface ArtifactEditorProps {
  artifact?: Artifact;
  artifacts?: Artifact[];
  onSave: (artifact: Partial<Artifact>) => void;
  onCancel: () => void;
  attachments?: Attachment[];
  onUploadAttachment?: (file: File) => void;
  onDeleteAttachment?: (attachmentId: string) => void;
  isUploadLoading?: boolean;
  links?: Link[];
  onCreateLink?: (link: Partial<Link>) => void;
  onDeleteLink?: (linkId: string) => void;
}

export const ArtifactEditor: React.FC<ArtifactEditorProps> = ({
  artifact,
  artifacts = [],
  onSave,
  onCancel,
  attachments = [],
  onUploadAttachment,
  onDeleteAttachment,
  isUploadLoading,
  links = [],
  onCreateLink,
  onDeleteLink,
}) => {
  const [formData, setFormData] = useState<Partial<Artifact>>(
    artifact || (window as any).__pendingArtifactData || {
      type: 'requirement',
      title: '',
      body: '',
      attributes: {},
    }
  );

  // Clear pending artifact data after using it
  useEffect(() => {
    if (!artifact && (window as any).__pendingArtifactData) {
      return () => {
        delete (window as any).__pendingArtifactData;
      };
    }
  }, [artifact]);

  // Track pending link changes during edit
  const [currentLinks, setCurrentLinks] = useState<Link[]>(links || []);
  const [pendingLinkAdds, setPendingLinkAdds] = useState<Partial<Link>[]>([]);
  const [pendingLinkRemoves, setPendingLinkRemoves] = useState<string[]>([]);

  // Update currentLinks when links prop changes
  useEffect(() => {
    setCurrentLinks(links || []);
  }, [links]);

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>
  ) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
  };

  const handleCreateLinkFromEditor = (link: Partial<Link>) => {
    // Add to pending additions (will be saved when Update is clicked)
    setPendingLinkAdds((prev) => [...prev, link]);
    // Add to current display
    setCurrentLinks((prev) => [...prev, link as Link]);
    // DO NOT call onCreateLink here - links are only persisted on Update
  };

  const handleDeleteLinkFromEditor = (linkId: string) => {
    // Check if it's a newly added link (not yet persisted)
    const isNewlyAdded = pendingLinkAdds.some((l) => l.id === linkId || (l.from_id && l.to_id && l.type));
    
    if (isNewlyAdded) {
      // Remove from pending additions
      setPendingLinkAdds((prev) => prev.filter((l) => l.id !== linkId));
    } else {
      // Mark existing link for removal (will be deleted when Update is clicked)
      setPendingLinkRemoves((prev) => [...prev, linkId]);
    }
    
    // Remove from current display
    setCurrentLinks((prev) => prev.filter((l) => l.id !== linkId));
    
    // DO NOT call onDeleteLink here - deletions only happen on Update
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    
    // Convert empty parent_id to null for proper UUID handling
    const dataToSave = {
      ...formData,
      parent_id: formData.parent_id === '' ? null : formData.parent_id,
      // Include link change information for backend to process
      pendingLinkAdds,
      pendingLinkRemoves,
    } as any;
    
    onSave(dataToSave);
  };

  return (
    <>
      {artifact && (
        <div className="card" style={{ marginBottom: '20px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
            <div>
              <h3 style={{ margin: '0 0 8px 0' }}>{formData.title || 'Untitled Artifact'}</h3>
              <p style={{ margin: 0, fontSize: '12px', color: 'var(--text-muted)' }}>
                UID: <code style={{ backgroundColor: 'var(--neutral-soft)', padding: '2px 6px', borderRadius: '3px' }}>{artifact.id}</code>
              </p>
            </div>
            <div style={{ display: 'flex', gap: '10px' }}>
              <button 
                type="submit" 
                className="button"
                onClick={(e) => {
                  e.preventDefault();
                  handleSubmit(e as any);
                }}
              >
                Update
              </button>
              <button
                type="button"
                className="button-secondary"
                onClick={onCancel}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="card">
        <h3>{artifact ? 'Edit Artifact Details' : 'New Artifact'}</h3>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label htmlFor="parent_id">Parent Artifact (Optional)</label>
            <select
              id="parent_id"
              name="parent_id"
              value={formData.parent_id || ''}
              onChange={handleChange}
            >
              <option value="">None (Top Level)</option>
              {artifacts
                .filter((a) => a.id !== artifact?.id)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.title || 'Untitled'} ({a.type})
                  </option>
                ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="type">Type</label>
            <select
              id="type"
              name="type"
              value={formData.type || ''}
              onChange={handleChange}
            >
              <option value="heading">Heading</option>
              <option value="description">Description</option>
              <option value="requirement">Requirement</option>
              <option value="test-case">Test Case</option>
              <option value="hazard">Hazard</option>
              <option value="design-item">Design Item</option>
              <option value="other">Other</option>
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="title">Title</label>
            <input
              id="title"
              type="text"
              name="title"
              value={formData.title || ''}
              onChange={handleChange}
              required
              placeholder="Enter artifact title"
            />
          </div>

          <div className="form-group">
            <label htmlFor="body">Description</label>
            <textarea
              id="body"
              name="body"
              value={formData.body || ''}
              onChange={handleChange}
              placeholder="Enter artifact description (markdown supported)"
            />
          </div>

          {artifact && (
            <ImageGallery
              artifactId={artifact.id}
              attachments={attachments}
              onUpload={onUploadAttachment || (() => {})}
              onDelete={onDeleteAttachment || (() => {})}
              isUploadLoading={isUploadLoading}
              showUpload={true}
            />
          )}

          {artifact && (
            <div style={{ marginTop: '30px' }}>
              <LinkPanel
                artifacts={artifacts}
                selectedArtifactId={artifact.id}
                onCreateLink={handleCreateLinkFromEditor}
                links={currentLinks}
                title="Manage Links (Edit Mode)"
                readOnly={false}
                onSelectArtifact={() => {}}
                onDeleteLink={handleDeleteLinkFromEditor}
              />
            </div>
          )}

          {!artifact && (
            <div style={{ display: 'flex', gap: '10px', marginTop: '20px' }}>
              <button type="submit" className="button">
                Create
              </button>
              <button
                type="button"
                className="button-secondary"
                onClick={onCancel}
              >
                Cancel
              </button>
            </div>
          )}
        </form>
      </div>
    </>
  );
};
