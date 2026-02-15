import React from 'react';
import { Artifact, Link, Attachment } from '../api/client';
import { ImageGallery } from './ImageGallery';

interface ArtifactDetailsProps {
  artifact: Artifact;
  links?: Link[];
  artifacts?: Artifact[];
  attachments?: Attachment[];
  onDeleteAttachment?: (attachmentId: string) => void;
  onSelectArtifact?: (artifactId: string) => void;
  previewVersion?: Artifact | null;
  onClosePreview?: () => void;
}

export const ArtifactDetails: React.FC<ArtifactDetailsProps> = ({ 
  artifact, 
  links = [], 
  artifacts = [],
  attachments = [],
  onDeleteAttachment,
  onSelectArtifact,
  previewVersion,
  onClosePreview,
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
      {/* Preview Notification and Side-by-Side Comparison */}
      {previewVersion && (
        <div>
          <div
            style={{
              backgroundColor: '#fff3cd',
              border: '1px solid #ffc107',
              color: '#856404',
              padding: '10px 12px',
              borderRadius: '3px',
              marginBottom: '16px',
              fontSize: '12px',
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
            }}
          >
            <span>
              <strong>Preview Mode:</strong> Comparing Version {previewVersion.version} with Current Version {artifact.version}
            </span>
            <button
              onClick={() => onClosePreview?.()}
              style={{
                backgroundColor: 'transparent',
                color: '#856404',
                border: '1px solid #856404',
                padding: '4px 8px',
                borderRadius: '3px',
                cursor: 'pointer',
                fontSize: '11px',
              }}
            >
              Close Preview
            </button>
          </div>

          {/* Side-by-Side Columns */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: '16px',
              marginBottom: '16px',
            }}
          >
            {/* Current Version Column */}
            <div
              style={{
                backgroundColor: '#f8f9fa',
                padding: '12px',
                borderRadius: '3px',
                border: '1px solid #e9ecef',
              }}
            >
              <h4 style={{ margin: '0 0 12px 0', fontSize: '13px', color: '#27ae60' }}>
                Current Version (v{artifact.version})
              </h4>
              <div style={{ fontSize: '12px' }}>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Title:</strong>
                  <p style={{ margin: '4px 0', color: '#2c3e50' }}>{artifact.title}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Type:</strong>
                  <p style={{ margin: '4px 0', color: '#2c3e50' }}>{artifact.type}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Description:</strong>
                  <p style={{ margin: '4px 0', color: '#2c3e50', whiteSpace: 'pre-wrap', wordBreak: 'break-word', minHeight: '60px' }}>
                    {artifact.body || '(empty)'}
                  </p>
                </div>
                {artifact.attributes && Object.keys(artifact.attributes).length > 0 && (
                  <div style={{ marginBottom: '12px' }}>
                    <strong>Attributes:</strong>
                    <pre style={{ fontSize: '11px', backgroundColor: '#ecf0f1', padding: '8px', borderRadius: '3px', margin: '4px 0', overflow: 'auto' }}>
                      {JSON.stringify(artifact.attributes, null, 2)}
                    </pre>
                  </div>
                )}
                <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid #dcdde1', fontSize: '11px', color: '#7f8c8d' }}>
                  <p style={{ margin: '0 0 4px 0' }}>
                    <strong>Created:</strong> {new Date(artifact.created_at).toLocaleString()}
                  </p>
                  <p style={{ margin: '0 0 4px 0' }}>
                    <strong>Updated:</strong> {new Date(artifact.updated_at).toLocaleString()}
                  </p>
                </div>
              </div>
            </div>

            {/* Preview Version Column */}
            <div
              style={{
                backgroundColor: '#f0f8f4',
                padding: '12px',
                borderRadius: '3px',
                border: '1px solid #c8e6c9',
              }}
            >
              <h4 style={{ margin: '0 0 12px 0', fontSize: '13px', color: '#2980b9' }}>
                Preview Version (v{previewVersion.version})
              </h4>
              <div style={{ fontSize: '12px' }}>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Title:</strong>
                  <p style={{ margin: '4px 0', color: '#2c3e50' }}>{previewVersion.title}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Type:</strong>
                  <p style={{ margin: '4px 0', color: '#2c3e50' }}>{previewVersion.type}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Description:</strong>
                  <p style={{ margin: '4px 0', color: '#2c3e50', whiteSpace: 'pre-wrap', wordBreak: 'break-word', minHeight: '60px' }}>
                    {previewVersion.body || '(empty)'}
                  </p>
                </div>
                {previewVersion.attributes && Object.keys(previewVersion.attributes).length > 0 && (
                  <div style={{ marginBottom: '12px' }}>
                    <strong>Attributes:</strong>
                    <pre style={{ fontSize: '11px', backgroundColor: '#ecf0f1', padding: '8px', borderRadius: '3px', margin: '4px 0', overflow: 'auto' }}>
                      {JSON.stringify(previewVersion.attributes, null, 2)}
                    </pre>
                  </div>
                )}
                <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid #dcdde1', fontSize: '11px', color: '#7f8c8d' }}>
                  <p style={{ margin: '0 0 4px 0' }}>
                    <strong>Created:</strong> {new Date(previewVersion.created_at).toLocaleString()}
                  </p>
                  <p style={{ margin: '0 0 4px 0' }}>
                    <strong>Updated:</strong> {new Date(previewVersion.updated_at).toLocaleString()}
                  </p>
                  {previewVersion.valid_to && (
                    <p style={{ margin: '4px 0 0 0' }}>
                      <strong>Archived:</strong> {new Date(previewVersion.valid_to).toLocaleString()}
                    </p>
                  )}
                </div>
              </div>
            </div>
          </div>

          <hr style={{ margin: '16px 0', borderColor: '#ecf0f1' }} />
        </div>
      )}

      {/* Only show standard artifact info if not in preview mode */}
      {!previewVersion && (
        <>
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
      {artifact.attributes && Object.keys(artifact.attributes).length > 0 && (
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
        </>
      )}
    </div>
  );
};
