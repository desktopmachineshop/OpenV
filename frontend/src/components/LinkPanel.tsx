import React, { useState } from 'react';
import { Link, Artifact } from '../api/client';

interface LinkPanelProps {
  artifacts: Artifact[];
  selectedArtifactId?: string;
  onCreateLink: (link: Partial<Link>) => void;
  links?: Link[];
  title?: string;
  readOnly?: boolean;
  onSelectArtifact?: (artifactId: string) => void;
}

export const LinkPanel: React.FC<LinkPanelProps> = ({
  artifacts,
  selectedArtifactId,
  onCreateLink,
  links = [],
  title = 'Links',
  readOnly = false,
  onSelectArtifact,
}) => {
  const [isCreating, setIsCreating] = useState(false);
  const [linkType, setLinkType] = useState('verifies');
  const [toArtifactId, setToArtifactId] = useState('');

  const getArtifactTitle = (id: string): string => {
    const art = artifacts.find((a) => a.id === id);
    return art ? art.title : id.substring(0, 8);
  };

  const handleCreateLink = () => {
    if (!selectedArtifactId || !toArtifactId) {
      alert('Please select an artifact');
      return;
    }

    onCreateLink({
      from_id: selectedArtifactId,
      to_id: toArtifactId,
      type: linkType,
      attributes: {},
    });

    setToArtifactId('');
    setIsCreating(false);
  };

  if (!selectedArtifactId) {
    return null;
  }

  // Filter links for the selected artifact
  const outgoingLinks = links.filter((l) => l.from_id === selectedArtifactId);
  const incomingLinks = links.filter((l) => l.to_id === selectedArtifactId);

  // Group links by type
  const groupLinksByType = (linkList: Link[]): Record<string, Link[]> => {
    return linkList.reduce((acc, link) => {
      if (!acc[link.type]) {
        acc[link.type] = [];
      }
      acc[link.type].push(link);
      return acc;
    }, {} as Record<string, Link[]>);
  };

  const outgoingByType = groupLinksByType(outgoingLinks);
  const incomingByType = groupLinksByType(incomingLinks);

  // Render a group of links by type
  const renderLinkGroup = (linksByType: Record<string, Link[]>, direction: 'outgoing' | 'incoming') => {
    const colorScheme = direction === 'outgoing' 
      ? { header: '#27ae60', bg: '#f0f8f4', border: '#27ae60' }
      : { header: '#2980b9', bg: '#f0f4f8', border: '#2980b9' };

    return Object.entries(linksByType).map(([linkType, typeLinks]) => (
      <div key={linkType} style={{ marginBottom: '12px' }}>
        <strong style={{ color: colorScheme.header, fontSize: '13px' }}>
          {linkType} ({typeLinks.length})
        </strong>
        <div style={{ marginTop: '6px' }}>
          {typeLinks.map((link) => {
            const linkedArtifactId = direction === 'outgoing' ? link.to_id : link.from_id;
            return (
              <div
                key={link.id}
                style={{
                  padding: '8px',
                  marginBottom: '5px',
                  backgroundColor: colorScheme.bg,
                  borderLeft: `3px solid ${colorScheme.border}`,
                  borderRadius: '2px',
                  fontSize: '12px',
                  cursor: 'pointer',
                  transition: 'background-color 0.2s ease',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLElement).style.opacity = '0.8';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLElement).style.opacity = '1';
                }}
                onClick={() => onSelectArtifact?.(linkedArtifactId)}
              >
                <strong>
                  <span style={{ color: colorScheme.header, textDecoration: 'underline', cursor: 'pointer' }}>
                    {direction === 'outgoing' ? getArtifactTitle(link.to_id) : getArtifactTitle(link.from_id)}
                  </span>
                </strong>
                <div style={{ marginTop: '3px', color: '#555', fontSize: '11px' }}>
                  ID: {linkedArtifactId.substring(0, 8)}...
                </div>
              </div>
            );
          })}
        </div>
      </div>
    ));
  };

  return (
    <div className="card">
      <h3>{title}</h3>

      {/* Existing Links */}
      {(outgoingLinks.length > 0 || incomingLinks.length > 0) ? (
        <div style={{ marginBottom: '20px' }}>
          {outgoingLinks.length > 0 && (
            <div style={{ marginBottom: '15px' }}>
              <strong style={{ color: '#27ae60', fontSize: '13px' }}>
                ↓ Links From This Artifact ({outgoingLinks.length})
              </strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(outgoingByType, 'outgoing')}
              </div>
            </div>
          )}

          {incomingLinks.length > 0 && (
            <div>
              <strong style={{ color: '#2980b9', fontSize: '13px' }}>
                ↑ Links To This Artifact ({incomingLinks.length})
              </strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(incomingByType, 'incoming')}
              </div>
            </div>
          )}
        </div>
      ) : (
        <p style={{ color: '#7f8c8d', fontSize: '12px', marginBottom: '15px' }}>
          No links yet.
        </p>
      )}

      {!readOnly && (
        <div style={{ 
          borderTop: '1px solid #ecf0f1', 
          paddingTop: '15px',
          display: 'flex',
          gap: '10px',
          alignItems: 'flex-end',
          flexWrap: 'wrap'
        }}>
          {!isCreating && (
            <button
              onClick={() => setIsCreating(true)}
              className="button"
              style={{ marginTop: '10px' }}
            >
              + Add Link
            </button>
          )}

          {isCreating && (
            <>
              <div style={{ flex: 1, minWidth: '150px' }}>
                <label style={{ display: 'block', fontSize: '11px', marginBottom: '4px', color: '#555' }}>
                  Type
                </label>
                <select
                  value={linkType}
                  onChange={(e) => setLinkType(e.target.value)}
                  style={{
                    width: '100%',
                    padding: '6px',
                    borderRadius: '4px',
                    border: '1px solid #bdc3c7',
                    fontSize: '12px',
                  }}
                >
                  <option value="verifies">Verifies</option>
                  <option value="satisfies">Satisfies</option>
                  <option value="mitigates">Mitigates</option>
                  <option value="relates-to">Relates To</option>
                  <option value="decomposes-to">Decomposes To</option>
                  <option value="impacts">Impacts</option>
                </select>
              </div>

              <div style={{ flex: 2, minWidth: '200px' }}>
                <label style={{ display: 'block', fontSize: '11px', marginBottom: '4px', color: '#555' }}>
                  Link To
                </label>
                <select
                  value={toArtifactId}
                  onChange={(e) => setToArtifactId(e.target.value)}
                  style={{
                    width: '100%',
                    padding: '6px',
                    borderRadius: '4px',
                    border: '1px solid #bdc3c7',
                    fontSize: '12px',
                  }}
                >
                  <option value="">-- Select artifact --</option>
                  {artifacts
                    .filter((a) => a.id !== selectedArtifactId)
                    .map((artifact) => (
                      <option key={artifact.id} value={artifact.id}>
                        {artifact.title} ({artifact.type})
                      </option>
                    ))}
                </select>
              </div>

              <button
                onClick={handleCreateLink}
                className="button"
                style={{ marginBottom: 0 }}
              >
                Create
              </button>

              <button
                onClick={() => {
                  setIsCreating(false);
                  setToArtifactId('');
                }}
                className="button-secondary"
                style={{ marginBottom: 0 }}
              >
                Cancel
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
};

export default LinkPanel;