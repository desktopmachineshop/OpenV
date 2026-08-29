import React, { useState } from 'react';
import { Link, Artifact } from '../api/client';
import { getAvailableLinkTypes, getAllowedTargetTypes, getLinkTypeLabel } from '../config/linkTypeRules';
import { useAlert } from './ui';

interface LinkPanelProps {
  artifacts: Artifact[];
  selectedArtifactId?: string;
  onCreateLink: (link: Partial<Link>) => void;
  links?: Link[];
  title?: string;
  readOnly?: boolean;
  onSelectArtifact?: (artifactId: string) => void;
  onDeleteLink?: (linkId: string) => void;
}

export const LinkPanel: React.FC<LinkPanelProps> = ({
  artifacts,
  selectedArtifactId,
  onCreateLink,
  links = [],
  title = 'Links',
  readOnly = false,
  onSelectArtifact,
  onDeleteLink,
}) => {
  const alertDialog = useAlert();
  const [isCreating, setIsCreating] = useState(false);
  const [linkType, setLinkType] = useState('');
  const [toArtifactId, setToArtifactId] = useState('');
  const [artifactSearch, setArtifactSearch] = useState('');
  const [showDropdown, setShowDropdown] = useState(false);

  // Get the source artifact to determine available link types
  const sourceArtifact = artifacts.find(a => a.id === selectedArtifactId);
  const availableLinkTypes = sourceArtifact ? getAvailableLinkTypes(sourceArtifact.type) : [];
  
  // Get allowed target types for selected link type
  const allowedTargetTypes = linkType ? getAllowedTargetTypes(linkType) : [];

  const getArtifactTitle = (id: string): string => {
    const art = artifacts.find((a) => a.id === id);
    return art ? art.title : id.substring(0, 8);
  };

  const handleCreateLink = () => {
    if (!selectedArtifactId || !toArtifactId) {
      void alertDialog({ title: 'Create link', message: 'Please select an artifact.' });
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

  // Filter links for the selected artifact and deduplicate by ID
  const allFilteredLinks = links.filter((l) => l.from_id === selectedArtifactId || l.to_id === selectedArtifactId);
  const uniqueLinks = Array.from(new Map(allFilteredLinks.map(link => [link.id, link])).values());
  
  const outgoingLinks = uniqueLinks.filter((l) => l.from_id === selectedArtifactId);
  const incomingLinks = uniqueLinks.filter((l) => l.to_id === selectedArtifactId);

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
      ? { header: 'var(--success)', bg: 'var(--tint-green)', border: 'var(--success)' }
      : { header: 'var(--accent-strong)', bg: 'var(--surface-alt)', border: 'var(--accent-strong)' };

    return Object.entries(linksByType).map(([linkType, typeLinks]) => {
      // Get the appropriate label (with inverse for incoming links)
      const displayLabel = getLinkTypeLabel(linkType, direction === 'incoming');
      
      return (
        <div key={linkType} style={{ marginBottom: '12px' }}>
          <strong style={{ color: colorScheme.header, fontSize: '13px' }}>
            {displayLabel} ({typeLinks.length})
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
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  overflow: 'hidden',
                  width: '100%',
                  boxSizing: 'border-box',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLElement).style.opacity = '0.8';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLElement).style.opacity = '1';
                }}
              >
                <div
                  style={{
                    flex: 1,
                    cursor: 'pointer',
                    overflow: 'hidden',
                    minWidth: 0,
                  }}
                  onClick={() => onSelectArtifact?.(linkedArtifactId)}
                >
                  <strong>
                    <span style={{ color: colorScheme.header, textDecoration: 'underline', cursor: 'pointer', display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {direction === 'outgoing' ? getArtifactTitle(link.to_id) : getArtifactTitle(link.from_id)}
                    </span>
                  </strong>
                  <div style={{ marginTop: '3px', color: 'var(--text-body)', fontSize: '11px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    ID: {linkedArtifactId.substring(0, 8)}...
                  </div>
                </div>
                {!readOnly && onDeleteLink && (
                  <button
                    onClick={() => onDeleteLink(link.id)}
                    style={{
                      backgroundColor: 'var(--danger)',
                      color: 'white',
                      border: 'none',
                      padding: '4px 8px',
                      borderRadius: '3px',
                      cursor: 'pointer',
                      fontSize: '11px',
                      marginLeft: '8px',
                    }}
                  >
                    Delete
                  </button>
                )}
              </div>
            );
          })}
        </div>
      </div>
    )});
  };

  return (
    <div className="card">
      <h3>{title}</h3>

      {/* Existing Links */}
      {(outgoingLinks.length > 0 || incomingLinks.length > 0) ? (
        <div style={{ marginBottom: '20px' }}>
          {outgoingLinks.length > 0 && (
            <div style={{ marginBottom: '15px' }}>
              <strong style={{ color: 'var(--success)', fontSize: '13px' }}>
                ↓ Links From This Artifact ({outgoingLinks.length})
              </strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(outgoingByType, 'outgoing')}
              </div>
            </div>
          )}

          {incomingLinks.length > 0 && (
            <div>
              <strong style={{ color: 'var(--accent-strong)', fontSize: '13px' }}>
                ↑ Links To This Artifact ({incomingLinks.length})
              </strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(incomingByType, 'incoming')}
              </div>
            </div>
          )}
        </div>
      ) : (
        <p style={{ color: 'var(--text-muted)', fontSize: '12px', marginBottom: '15px' }}>
          No links yet.
        </p>
      )}

      {!readOnly && (
        <div style={{ 
          borderTop: '1px solid var(--neutral-soft)', 
          paddingTop: '15px',
          display: 'flex',
          gap: '10px',
          alignItems: 'flex-end',
          flexWrap: 'wrap'
        }}>
          {!isCreating && (
            <button
              onClick={() => {
                setIsCreating(true);
                setArtifactSearch('');
                setToArtifactId('');
                setShowDropdown(false);
                // Set default link type to first available
                if (availableLinkTypes.length > 0) {
                  setLinkType(availableLinkTypes[0].type);
                }
              }}
              className="button"
              style={{ marginTop: '10px' }}
              disabled={availableLinkTypes.length === 0}
              title={availableLinkTypes.length === 0 ? 'No valid link types available for this artifact type' : ''}
            >
              + Add Link
            </button>
          )}

          {isCreating && (
            <>
              <div style={{ flex: 1, minWidth: '150px' }}>
                <label style={{ display: 'block', fontSize: '11px', marginBottom: '4px', color: 'var(--text-body)' }}>
                  Link Type
                </label>
                <select
                  value={linkType}
                  onChange={(e) => {
                    setLinkType(e.target.value);
                    setToArtifactId(''); // Reset target when link type changes
                  }}
                  style={{
                    width: '100%',
                    padding: '6px',
                    borderRadius: '4px',
                    border: '1px solid var(--neutral-mid)',
                    fontSize: '12px',
                  }}
                >
                  {availableLinkTypes.map(rule => (
                    <option key={rule.type} value={rule.type} title={rule.description}>
                      {rule.label}
                    </option>
                  ))}
                </select>
              </div>

              <div style={{ flex: 2, minWidth: '200px', position: 'relative' }}>
                <label style={{ display: 'block', fontSize: '11px', marginBottom: '4px', color: 'var(--text-body)' }}>
                  Link To {linkType && allowedTargetTypes.length > 0 && !allowedTargetTypes.includes('*') 
                    ? `(${allowedTargetTypes.join(', ')})` 
                    : ''}
                </label>
                <input
                  type="text"
                  value={artifactSearch}
                  onChange={(e) => {
                    setArtifactSearch(e.target.value);
                    setShowDropdown(true);
                    setToArtifactId(''); // Clear selection when typing
                  }}
                  onFocus={() => setShowDropdown(true)}
                  onBlur={() => setTimeout(() => setShowDropdown(false), 200)}
                  placeholder="Type to search by title or UID..."
                  style={{
                    width: '100%',
                    padding: '6px',
                    borderRadius: '4px',
                    border: '1px solid var(--neutral-mid)',
                    fontSize: '12px',
                  }}
                />
                {showDropdown && (() => {
                  const filteredArtifacts = artifacts.filter((a) => {
                    if (a.id === selectedArtifactId) return false;
                    // Filter based on allowed target types for selected link type
                    if (!allowedTargetTypes.includes('*') && !allowedTargetTypes.includes(a.type)) {
                      return false;
                    }
                    // Filter based on already existing links of this type
                    if (linkType && outgoingLinks.some(link => link.type === linkType && link.to_id === a.id)) {
                      return false;
                    }
                    // Filter based on search text (title or UID)
                    if (artifactSearch.trim()) {
                      const searchLower = artifactSearch.toLowerCase();
                      const matchesTitle = a.title.toLowerCase().includes(searchLower);
                      const matchesId = a.id.toLowerCase().includes(searchLower);
                      return matchesTitle || matchesId;
                    }
                    return true;
                  });

                  return filteredArtifacts.length > 0 ? (
                    <div
                      style={{
                        position: 'absolute',
                        top: '100%',
                        left: 0,
                        right: 0,
                        maxHeight: '200px',
                        overflowY: 'auto',
                        backgroundColor: 'var(--surface)',
                        border: '1px solid var(--neutral-mid)',
                        borderRadius: '4px',
                        boxShadow: '0 2px 4px rgba(0,0,0,0.1)',
                        zIndex: 1000,
                        marginTop: '2px',
                      }}
                    >
                      {filteredArtifacts.map((artifact) => (
                        <div
                          key={artifact.id}
                          onClick={() => {
                            setToArtifactId(artifact.id);
                            setArtifactSearch(artifact.title);
                            setShowDropdown(false);
                          }}
                          style={{
                            padding: '8px',
                            cursor: 'pointer',
                            fontSize: '12px',
                            borderBottom: '1px solid var(--neutral-soft)',
                            backgroundColor: toArtifactId === artifact.id ? 'var(--tint-blue)' : 'var(--surface)',
                          }}
                          onMouseEnter={(e) => {
                            e.currentTarget.style.backgroundColor = 'var(--tint-blue)';
                          }}
                          onMouseLeave={(e) => {
                            e.currentTarget.style.backgroundColor = toArtifactId === artifact.id ? 'var(--tint-blue)' : 'var(--surface)';
                          }}
                        >
                          <div style={{ fontWeight: 'bold' }}>{artifact.title}</div>
                          <div style={{ fontSize: '10px', color: 'var(--text-muted)', marginTop: '2px' }}>
                            {artifact.type} • {artifact.id.substring(0, 8)}...
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : artifactSearch.trim() ? (
                    <div
                      style={{
                        position: 'absolute',
                        top: '100%',
                        left: 0,
                        right: 0,
                        backgroundColor: 'var(--surface)',
                        border: '1px solid var(--neutral-mid)',
                        borderRadius: '4px',
                        padding: '8px',
                        fontSize: '12px',
                        color: 'var(--text-muted)',
                        zIndex: 1000,
                        marginTop: '2px',
                      }}
                    >
                      No matching artifacts found
                    </div>
                  ) : null;
                })()}
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
                  setArtifactSearch('');
                  setShowDropdown(false);
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