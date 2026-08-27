import React, { useState, useEffect, useMemo } from 'react';
import { Artifact, Attachment, Link } from '../api/client';
import { ImageGallery } from './ImageGallery';
import { LinkPanel } from './LinkPanel';
import {
  applyLinkDeletion,
  createPendingLinkId,
  serializePendingAdds,
} from '../utils/pendingLinks';

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
  const [pendingLinkAdds, setPendingLinkAdds] = useState<Partial<Link>[]>([]);
  const [pendingLinkRemoves, setPendingLinkRemoves] = useState<string[]>([]);

  // Displayed links = persisted links minus pending removals, plus pending
  // adds. Derived so a refresh of the links prop can't clobber pending edits.
  const currentLinks = useMemo<Link[]>(() => {
    const base = (links || []).filter((l) => !pendingLinkRemoves.includes(l.id));
    return [...base, ...(pendingLinkAdds as Link[])];
  }, [links, pendingLinkAdds, pendingLinkRemoves]);

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
    // Give the unsaved link a stable client-side temp id so it can be
    // individually identified (and deleted) before it is persisted.
    const pendingLink: Partial<Link> = { ...link, id: createPendingLinkId() };
    // Add to pending additions (will be saved when Update is clicked);
    // it appears in the displayed links via the derived currentLinks.
    setPendingLinkAdds((prev) => [...prev, pendingLink]);
    // DO NOT call onCreateLink here - links are only persisted on Update
  };

  const handleDeleteLinkFromEditor = (linkId: string) => {
    // Classify by id: a pending (unsaved) add carries a temp id and is
    // simply dropped from the pending list; a persisted link id is recorded
    // in pendingLinkRemoves so the Update request deletes it server-side.
    const next = applyLinkDeletion({ pendingLinkAdds, pendingLinkRemoves }, linkId);
    setPendingLinkAdds(next.pendingLinkAdds);
    setPendingLinkRemoves(next.pendingLinkRemoves);
    // The displayed links update via the derived currentLinks.

    // DO NOT call onDeleteLink here - deletions only happen on Update
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    
    // Convert empty parent_id to null for proper UUID handling
    const dataToSave = {
      ...formData,
      parent_id: formData.parent_id === '' ? null : formData.parent_id,
      // Include link change information for backend to process.
      // Temp ids are client-side only — strip them before sending.
      pendingLinkAdds: serializePendingAdds(pendingLinkAdds),
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
              <p style={{ margin: 0, fontSize: '12px', color: '#7f8c8d' }}>
                UID: <code style={{ backgroundColor: '#ecf0f1', padding: '2px 6px', borderRadius: '3px' }}>{artifact.id}</code>
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
