import React from 'react';
import { Artifact, Link, Attachment } from '../api/client';
import { ImageGallery } from './ImageGallery';

interface ArtifactDetailsProps {
  artifact: Artifact;
  links?: Link[];
  artifacts?: Artifact[];
  attachments?: Attachment[];
  onDeleteAttachment?: (attachmentId: string) => void;
}

export const ArtifactDetails: React.FC<ArtifactDetailsProps> = ({ 
  artifact, 
  links = [], 
  artifacts = [],
  attachments = [],
  onDeleteAttachment,
}) => {
  // Get the title of an artifact by ID
  const getArtifactTitle = (id: string): string => {
    const art = artifacts.find((a) => a.id === id);
    return art ? art.title : id.substring(0, 8);
  };

  // Filter links related to this artifact
  const outgoingLinks = links.filter((l) => l.from_id === artifact.id);
  const incomingLinks = links.filter((l) => l.to_id === artifact.id);

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
          {typeLinks.map((link) => (
            <div
              key={link.id}
              style={{
                padding: '8px',
                marginBottom: '5px',
                backgroundColor: colorScheme.bg,
                borderLeft: `3px solid ${colorScheme.border}`,
                borderRadius: '2px',
                fontSize: '12px',
              }}
            >
              <strong>
                {direction === 'outgoing' ? getArtifactTitle(link.to_id) : getArtifactTitle(link.from_id)}
              </strong>
              <div style={{ marginTop: '3px', color: '#555', fontSize: '11px' }}>
                ID: {(direction === 'outgoing' ? link.to_id : link.from_id).substring(0, 8)}...
              </div>
            </div>
          ))}
        </div>
      </div>
    ));
  };

  return (
    <div className="card">
      <h3>{artifact.title}</h3>
      <div style={{ marginBottom: '15px' }}>
        <span
          style={{
            display: 'inline-block',
            backgroundColor: '#3498db',
            color: 'white',
            padding: '4px 8px',
            borderRadius: '3px',
            fontSize: '12px',
            marginRight: '10px',
          }}
        >
          {artifact.type}
        </span>
        <span style={{ fontSize: '12px', color: '#7f8c8d' }}>
          Version {artifact.version}
        </span>
      </div>
      <div style={{ marginBottom: '15px' }}>
        <strong>ID:</strong>
        <code style={{ marginLeft: '10px', fontSize: '12px', backgroundColor: '#f5f5f5', padding: '2px 6px' }}>
          {artifact.id}
        </code>
      </div>
      <div style={{ marginBottom: '15px' }}>
        <strong>Description:</strong>
        <p style={{ marginTop: '8px', whiteSpace: 'pre-wrap' }}>{artifact.body}</p>
      </div>
      {Object.keys(artifact.attributes).length > 0 && (
        <div>
          <strong>Attributes:</strong>
          <pre style={{ 
            marginTop: '8px', 
            backgroundColor: '#f5f5f5', 
            padding: '10px', 
            borderRadius: '4px',
            overflow: 'auto',
            fontSize: '12px'
          }}>
            {JSON.stringify(artifact.attributes, null, 2)}
          </pre>
        </div>
      )}
      
      {/* Images Gallery */}
      {attachments && attachments.length > 0 && (
        <ImageGallery 
          artifactId={artifact.id} 
          attachments={attachments}
          onDelete={onDeleteAttachment || (() => {})}
          showUpload={false}
        />
      )}

      {/* Links Section */}
      {(outgoingLinks.length > 0 || incomingLinks.length > 0) && (
        <div style={{ marginTop: '20px', paddingTop: '15px', borderTop: '1px solid #ddd' }}>
          <h4 style={{ marginTop: 0, marginBottom: '12px' }}>Traceability Links</h4>
          
          {outgoingLinks.length > 0 && (
            <div style={{ marginBottom: '15px' }}>
              <strong style={{ color: '#27ae60', fontSize: '13px' }}>↓ Links From This Artifact ({outgoingLinks.length})</strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(outgoingByType, 'outgoing')}
              </div>
            </div>
          )}
          
          {incomingLinks.length > 0 && (
            <div>
              <strong style={{ color: '#2980b9', fontSize: '13px' }}>↑ Links To This Artifact ({incomingLinks.length})</strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(incomingByType, 'incoming')}
              </div>
            </div>
          )}
        </div>
      )}
      
      <div style={{ marginTop: '15px', fontSize: '12px', color: '#7f8c8d' }}>
        <p>Created: {new Date(artifact.created_at).toLocaleString()}</p>
        <p>Updated: {new Date(artifact.updated_at).toLocaleString()}</p>
      </div>
    </div>
  );
};
