import React, { useState } from 'react';
import { Artifact } from '../api/client';

interface ArtifactEditorProps {
  artifact?: Artifact;
  onSave: (artifact: Partial<Artifact>) => void;
  onCancel: () => void;
}

export const ArtifactEditor: React.FC<ArtifactEditorProps> = ({
  artifact,
  onSave,
  onCancel,
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
    onSave(formData);
  };

  return (
    <div className="card">
      <h3>{artifact ? 'Edit Artifact' : 'New Artifact'}</h3>
      <form onSubmit={handleSubmit}>
        <div className="form-group">
          <label htmlFor="type">Type</label>
          <select
            id="type"
            name="type"
            value={formData.type || ''}
            onChange={handleChange}
          >
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

        <div style={{ display: 'flex', gap: '10px', marginTop: '20px' }}>
          <button type="submit" className="button">
            {artifact ? 'Update' : 'Create'}
          </button>
          <button
            type="button"
            className="button-secondary"
            onClick={onCancel}
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  );
};
