import React, { useState } from 'react';
import { Artifact, Attachment } from '../api/client';
import { ImageGallery } from './ImageGallery';

interface ArtifactEditorProps {
  artifact?: Artifact;
  artifacts?: Artifact[];
  onSave: (artifact: Partial<Artifact>) => void;
  onCancel: () => void;
  attachments?: Attachment[];
  onUploadAttachment?: (file: File) => void;
  onDeleteAttachment?: (attachmentId: string) => void;
  isUploadLoading?: boolean;
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
}) => {
  const [formData, setFormData] = useState<Partial<Artifact>>(
    artifact || {
      type: 'requirement',
      title: '',
      body: '',
      attributes: {},
    }
  );

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>
  ) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    
    // Convert empty parent_id to null for proper UUID handling
    const dataToSave = {
      ...formData,
      parent_id: formData.parent_id === '' ? null : formData.parent_id,
    };
    
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
