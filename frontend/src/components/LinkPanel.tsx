import React, { useState } from 'react';
import { Link, Artifact } from '../api/client';

interface LinkPanelProps {
  artifacts: Artifact[];
  selectedArtifactId?: string;
  onCreateLink: (link: Partial<Link>) => void;
}

export const LinkPanel: React.FC<LinkPanelProps> = ({
  artifacts,
  selectedArtifactId,
  onCreateLink,
}) => {
  const [linkType, setLinkType] = useState('verifies');
  const [toArtifactId, setToArtifactId] = useState('');

  const handleCreateLink = () => {
    if (!selectedArtifactId || !toArtifactId) {
      alert('Please select both artifacts');
      return;
    }

    onCreateLink({
      from_id: selectedArtifactId,
      to_id: toArtifactId,
      type: linkType,
      attributes: {},
    });

    setToArtifactId('');
  };

  if (!selectedArtifactId) {
    return (
      <div className="card">
        <h3>Links</h3>
        <p>Select an artifact to create links.</p>
      </div>
    );
  }

  return (
    <div className="card">
      <h3>Create Link</h3>
      <div className="form-group">
        <label htmlFor="linkType">Link Type</label>
        <select
          id="linkType"
          value={linkType}
          onChange={(e) => setLinkType(e.target.value)}
        >
          <option value="verifies">Verifies</option>
          <option value="satisfies">Satisfies</option>
          <option value="mitigates">Mitigates</option>
          <option value="implements">Implements</option>
          <option value="depends-on">Depends On</option>
        </select>
      </div>

      <div className="form-group">
        <label htmlFor="toArtifact">Link To</label>
        <select
          id="toArtifact"
          value={toArtifactId}
          onChange={(e) => setToArtifactId(e.target.value)}
        >
          <option value="">-- Select an artifact --</option>
          {artifacts
            .filter((a) => a.id !== selectedArtifactId)
            .map((artifact) => (
              <option key={artifact.id} value={artifact.id}>
                {artifact.title} ({artifact.type})
              </option>
            ))}
        </select>
      </div>

      <button onClick={handleCreateLink} className="button">
        Create Link
      </button>
    </div>
  );
};
