import React, { useState, useEffect } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Artifact, Link, Attachment } from '../api/client';
import { linkAPI } from '../api/client';
import { ImageGallery } from './ImageGallery';
import { useConfirm } from './ui';
import { getLinkTypeLabel } from '../config/linkTypeRules';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface ArtifactDetailsProps {
  artifact: Artifact;
  links?: Link[];
  artifacts?: Artifact[];
  attachments?: Attachment[];
  onDeleteAttachment?: (attachmentId: string) => void;
  onSelectArtifact?: (artifactId: string) => void;
  previewVersion?: Artifact | null;
  onClosePreview?: () => void;
  /**
   * When true, the main Traceability Links section offers a Delete button
   * (backend still enforces editor rights). Version-preview/diff renderings
   * are always read-only regardless of this flag.
   */
  allowLinkDelete?: boolean;
  /**
   * When true (default), the links for the displayed artifact are fetched
   * LIVE from the link table instead of from the version-scoped snapshot.
   * Link writes version-bump the counterpart artifact server-side, so the
   * client-held version number can be stale — a version-scoped fetch would
   * then miss fresh incoming links until a full reload (issue #169).
   * Baseline/historical renderings pass false to keep the snapshot view.
   * Preview-version links are always fetched version-scoped.
   */
  liveLinks?: boolean;
  /**
   * Called after this component mutates links (e.g. a delete succeeded) so
   * the parent can refresh artifact versions and its own link list — the
   * backend auto-versions both linked artifacts on every link change.
   */
  onLinksChanged?: () => void;
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
  allowLinkDelete = false,
  liveLinks = true,
  onLinksChanged,
}) => {
  const confirm = useConfirm();
  const [currentVersionLinks, setCurrentVersionLinks] = useState<Link[]>(links || []);
  const [previewVersionLinks, setPreviewVersionLinks] = useState<Link[]>([]);
  const [deletingLinkId, setDeletingLinkId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  
  // Attachment filtering for versions
  const [currentVersionAttachments, setCurrentVersionAttachments] = useState<Attachment[]>(attachments || []);
  const [previewVersionAttachments, setPreviewVersionAttachments] = useState<Attachment[]>([]);

  // Filter attachments based on version timestamp
  useEffect(() => {
    if (previewVersion) {
      // For preview version, show only attachments created before or at the version's time
      const previewTime = new Date(previewVersion.updated_at).getTime();
      const previewAtts = (attachments || []).filter(att => 
        new Date(att.created_at).getTime() <= previewTime
      );
      setPreviewVersionAttachments(previewAtts);
      
      // For current version, show all current attachments
      setCurrentVersionAttachments(attachments || []);
    } else {
      setCurrentVersionAttachments(attachments || []);
      setPreviewVersionAttachments([]);
    }
  }, [previewVersion, attachments]);

  // Load the displayed artifact's links whenever it (or its version) changes.
  useEffect(() => {
    // Live view: fetch straight from the link table so a stale client-held
    // version number can never hide fresh links (issue #169 — link writes
    // version-bump the counterpart artifact server-side). Historical views
    // (baselines) keep the version-scoped snapshot fetch.
    const fetchLinks = liveLinks
      ? linkAPI.listForArtifact(artifact.id)
      : linkAPI.listForArtifactVersion(artifact.id, artifact.version);
    fetchLinks
      .then(res => {
        // Deduplicate links by ID
        const links = res.data || [];
        const uniqueLinks = Array.from(new Map(links.map(link => [link.id, link])).values());
        setCurrentVersionLinks(uniqueLinks);
      })
      .catch(err => {
        console.error('Failed to fetch current version links:', err);
        setCurrentVersionLinks([]);
      });

    // If in preview mode, also fetch preview version links
    if (previewVersion) {
      linkAPI.listForArtifactVersion(previewVersion.id, previewVersion.version)
        .then(res => {
          // Deduplicate links by ID
          const links = res.data || [];
          const uniqueLinks = Array.from(new Map(links.map(link => [link.id, link])).values());
          setPreviewVersionLinks(uniqueLinks);
        })
        .catch(() => setPreviewVersionLinks([]));
    } else {
      setPreviewVersionLinks([]);
    }
  }, [artifact.id, artifact.version, previewVersion, liveLinks]);
  // Handle link deletion (DELETE /api/v1/links/{id}; backend enforces
  // editor rights and refreshes link snapshots on both artifacts).
  const handleDeleteLink = async (linkId: string) => {
    const ok = await confirm({
      title: 'Delete link',
      message: 'Are you sure you want to delete this link?',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) {
      return;
    }

    setDeletingLinkId(linkId);
    setDeleteError(null);

    try {
      await linkAPI.delete(linkId);

      // Drop the link from the displayed lists immediately; the
      // authoritative lists are refetched whenever the artifact refreshes.
      setCurrentVersionLinks((prev) => prev.filter((l) => l.id !== linkId));
      setPreviewVersionLinks((prev) => prev.filter((l) => l.id !== linkId));

      // The backend auto-versions both artifacts touched by the deleted
      // link — let the parent refetch so displayed versions stay current.
      onLinksChanged?.();
    } catch (error) {
      console.error('Failed to delete link:', error);
      setDeleteError(`Failed to delete link: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setDeletingLinkId(null);
    }
  };

  // Get the title of an artifact by ID
  const getArtifactTitle = (id: string): string => {
    const art = artifacts.find((a) => a.id === id);
    return art ? art.title : id.substring(0, 8);
  };

  // Filter links related to current version
  const currentOutgoingLinks = (currentVersionLinks || []).filter((l) => l.from_id === artifact.id);
  const currentIncomingLinks = (currentVersionLinks || []).filter((l) => l.to_id === artifact.id);

  // Filter links related to preview version
  const previewOutgoingLinks = (previewVersionLinks || []).filter((l) => l.from_id === previewVersion?.id);
  const previewIncomingLinks = (previewVersionLinks || []).filter((l) => l.to_id === previewVersion?.id);

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

  const currentOutgoingByType = groupLinksByType(currentOutgoingLinks);
  const currentIncomingByType = groupLinksByType(currentIncomingLinks);
  const previewOutgoingByType = groupLinksByType(previewOutgoingLinks);
  const previewIncomingByType = groupLinksByType(previewIncomingLinks);

  // Render a group of links by type
  const renderLinkGroup = (linksByType: Record<string, Link[]>, direction: 'outgoing' | 'incoming', isPreview: boolean = false) => {
    const colorScheme = direction === 'outgoing' 
      ? { header: 'var(--success)', bg: 'var(--tint-green)', border: 'var(--success)' }
      : { header: 'var(--accent-strong)', bg: 'var(--surface-alt)', border: 'var(--accent-strong)' };

    return Object.entries(linksByType).map(([linkType, typeLinks]) => {
      // Deduplicate within each type group by link ID
      const uniqueTypeLinks = Array.from(new Map(typeLinks.map(link => [link.id, link])).values());
      
      // Get the appropriate label (with inverse for incoming links)
      const displayLabel = getLinkTypeLabel(linkType, direction === 'incoming');
      
      return (
        <div key={linkType} style={{ marginBottom: '12px' }}>
          <strong style={{ color: colorScheme.header, fontSize: '13px' }}>
            {displayLabel} ({uniqueTypeLinks.length})
          </strong>
          <div style={{ marginTop: '6px' }}>
            {uniqueTypeLinks.map((link) => {
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
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    transition: 'background-color 0.2s ease',
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
                  {!isPreview && (
                    <button
                      onClick={() => handleDeleteLink(link.id)}
                      disabled={deletingLinkId === link.id}
                      style={{
                        marginLeft: '8px',
                        padding: '4px 8px',
                        backgroundColor: 'var(--danger)',
                        color: 'white',
                        border: 'none',
                        borderRadius: '3px',
                        cursor: deletingLinkId === link.id ? 'not-allowed' : 'pointer',
                        fontSize: '11px',
                        opacity: deletingLinkId === link.id ? 0.6 : 1,
                        transition: 'opacity 0.2s ease',
                      }}
                    >
                      {deletingLinkId === link.id ? 'Deleting...' : 'Delete'}
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      );
    });
  };

  return (
    <div className="card">
      {/* Preview Notification and Side-by-Side Comparison */}
      {previewVersion && (
        <div>
          <div
            style={{
              backgroundColor: 'var(--tint-yellow)',
              border: '1px solid var(--warning-bright)',
              color: 'var(--warning-text)',
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
                color: 'var(--warning-text)',
                border: '1px solid var(--warning-text)',
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
                backgroundColor: 'var(--surface-alt)',
                padding: '12px',
                borderRadius: '3px',
                border: '1px solid var(--surface-alt)',
              }}
            >
              <h4 style={{ margin: '0 0 12px 0', fontSize: '13px', color: 'var(--success)' }}>
                Current Version (v{artifact.version})
              </h4>
              <div style={{ fontSize: '12px' }}>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Title:</strong>
                  <p style={{ margin: '4px 0', color: 'var(--text)' }}>{artifact.title}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Type:</strong>
                  <p style={{ margin: '4px 0', color: 'var(--text)' }}>{artifact.type}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Description:</strong>
                  <div style={{ 
                    margin: '4px 0', 
                    color: 'var(--text)', 
                    minHeight: '60px',
                    lineHeight: '1.6'
                  }} className="markdown-content">
                    {artifact.body ? (
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{artifact.body}</ReactMarkdown>
                    ) : (
                      <p style={{ fontStyle: 'italic', color: 'var(--text-muted)' }}>(empty)</p>
                    )}
                  </div>
                </div>
                {artifact.attributes && Object.keys(artifact.attributes).filter(k => !['links_snapshot', 'images_snapshot'].includes(k)).length > 0 && (
                  <div style={{ marginBottom: '12px' }}>
                    <strong>Attributes:</strong>
                    <pre style={{ fontSize: '11px', backgroundColor: 'var(--neutral-soft)', padding: '8px', borderRadius: '3px', margin: '4px 0', overflow: 'auto' }}>
                      {JSON.stringify(
                        Object.fromEntries(
                          Object.entries(artifact.attributes).filter(([k]) => !['links_snapshot', 'images_snapshot'].includes(k))
                        ),
                        null,
                        2
                      )}
                    </pre>
                  </div>
                )}                {currentVersionAttachments.length > 0 && (
                  <div style={{ marginBottom: '12px' }}>
                    <strong>Images:</strong>
                    <div style={{ marginTop: '6px' }}>
                      <ImageGallery 
                        artifactId={artifact.id} 
                        attachments={currentVersionAttachments}
                        onDelete={() => {}}
                        showUpload={false}
                        thumbnailSize={60}
                      />
                    </div>
                  </div>
                )}                {(currentOutgoingLinks.length > 0 || currentIncomingLinks.length > 0) && (
                  <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid var(--border)' }}>
                    <strong style={{ fontSize: '12px', color: 'var(--success)', marginBottom: '6px', display: 'block' }}>Links</strong>
                    {currentOutgoingLinks.length > 0 && (
                      <div style={{ marginBottom: '8px' }}>
                        <span style={{ fontSize: '11px', color: 'var(--success)', fontWeight: 'bold' }}>↓ From ({currentOutgoingLinks.length})</span>
                        <div style={{ marginTop: '4px' }}>
                          {renderLinkGroup(currentOutgoingByType, 'outgoing', true)}
                        </div>
                      </div>
                    )}
                    {currentIncomingLinks.length > 0 && (
                      <div>
                        <span style={{ fontSize: '11px', color: 'var(--accent-strong)', fontWeight: 'bold' }}>↑ To ({currentIncomingLinks.length})</span>
                        <div style={{ marginTop: '4px' }}>
                          {renderLinkGroup(currentIncomingByType, 'incoming', true)}
                        </div>
                      </div>
                    )}
                  </div>
                )}
                <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid var(--border)', fontSize: '11px', color: 'var(--text-muted)' }}>
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
                backgroundColor: 'var(--tint-green)',
                padding: '12px',
                borderRadius: '3px',
                border: '1px solid var(--tint-green-border)',
              }}
            >
              <h4 style={{ margin: '0 0 12px 0', fontSize: '13px', color: 'var(--accent-strong)' }}>
                Preview Version (v{previewVersion.version})
              </h4>
              <div style={{ fontSize: '12px' }}>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Title:</strong>
                  <p style={{ margin: '4px 0', color: 'var(--text)' }}>{previewVersion.title}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Type:</strong>
                  <p style={{ margin: '4px 0', color: 'var(--text)' }}>{previewVersion.type}</p>
                </div>
                <div style={{ marginBottom: '12px' }}>
                  <strong>Description:</strong>
                  <p style={{ margin: '4px 0', color: 'var(--text)', whiteSpace: 'pre-wrap', wordBreak: 'break-word', minHeight: '60px' }}>
                    {previewVersion.body || '(empty)'}
                  </p>
                </div>
                {previewVersion.attributes && Object.keys(previewVersion.attributes).filter(k => !['links_snapshot', 'images_snapshot'].includes(k)).length > 0 && (
                  <div style={{ marginBottom: '12px' }}>
                    <strong>Attributes:</strong>
                    <pre style={{ fontSize: '11px', backgroundColor: 'var(--neutral-soft)', padding: '8px', borderRadius: '3px', margin: '4px 0', overflow: 'auto' }}>
                      {JSON.stringify(
                        Object.fromEntries(
                          Object.entries(previewVersion.attributes).filter(([k]) => !['links_snapshot', 'images_snapshot'].includes(k))
                        ),
                        null,
                        2
                      )}
                    </pre>
                  </div>
                )}
                {previewVersionAttachments.length > 0 && (
                  <div style={{ marginBottom: '12px' }}>
                    <strong>Images:</strong>
                    <div style={{ marginTop: '6px' }}>
                      <ImageGallery 
                        artifactId={previewVersion.id} 
                        attachments={previewVersionAttachments}
                        onDelete={() => {}}
                        showUpload={false}
                        thumbnailSize={60}
                      />
                    </div>
                  </div>
                )}
                {(previewOutgoingLinks.length > 0 || previewIncomingLinks.length > 0) && (
                  <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid var(--border)' }}>
                    <strong style={{ fontSize: '12px', color: 'var(--accent-strong)', marginBottom: '6px', display: 'block' }}>Links</strong>
                    {previewOutgoingLinks.length > 0 && (
                      <div style={{ marginBottom: '8px' }}>
                        <span style={{ fontSize: '11px', color: 'var(--success)', fontWeight: 'bold' }}>↓ From ({previewOutgoingLinks.length})</span>
                        <div style={{ marginTop: '4px' }}>
                          {renderLinkGroup(previewOutgoingByType, 'outgoing', true)}
                        </div>
                      </div>
                    )}
                    {previewIncomingLinks.length > 0 && (
                      <div>
                        <span style={{ fontSize: '11px', color: 'var(--accent-strong)', fontWeight: 'bold' }}>↑ To ({previewIncomingLinks.length})</span>
                        <div style={{ marginTop: '4px' }}>
                          {renderLinkGroup(previewIncomingByType, 'incoming', true)}
                        </div>
                      </div>
                    )}
                  </div>
                )}
                <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid var(--border)', fontSize: '11px', color: 'var(--text-muted)' }}>
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

          <hr style={{ margin: '16px 0', borderColor: 'var(--neutral-soft)' }} />
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
            backgroundColor: 'var(--accent)',
            color: 'white',
            padding: '4px 8px',
            borderRadius: '3px',
            fontSize: '12px',
            marginRight: '10px',
          }}
        >
          {artifact.type}
        </span>
        <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
          Version {artifact.version}
        </span>
        {artifact.project_id && (
          <RouterLink
            to={`/projects/${artifact.project_id}/impact?artifact=${artifact.id}`}
            title="Trace what a change to this artifact would affect"
            style={{ marginLeft: '10px', fontSize: '12px', color: 'var(--accent-strong)', textDecoration: 'none' }}
          >
            Show impact →
          </RouterLink>
        )}
      </div>
      <div style={{ marginBottom: '15px' }}>
        <strong>ID:</strong>
        <code style={{ marginLeft: '10px', fontSize: '12px', backgroundColor: 'var(--surface-inset)', padding: '2px 6px' }}>
          {artifact.id}
        </code>
      </div>
      <div style={{ marginBottom: '15px' }}>
        <strong>Description:</strong>
        <div style={{ marginTop: '8px' }}>
          {artifact.body ? (
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{artifact.body}</ReactMarkdown>
          ) : (
            <p style={{ fontStyle: 'italic', color: 'var(--text-muted)' }}>(empty)</p>
          )}
        </div>
      </div>
      {artifact.attributes && Object.keys(artifact.attributes).filter(k => !['links_snapshot', 'images_snapshot'].includes(k)).length > 0 && (
        <div>
          <strong>Attributes:</strong>
          <pre style={{ 
            marginTop: '8px', 
            backgroundColor: 'var(--surface-inset)', 
            padding: '10px', 
            borderRadius: '4px',
            overflow: 'auto',
            fontSize: '12px'
          }}>
            {JSON.stringify(
              Object.fromEntries(
                Object.entries(artifact.attributes).filter(([k]) => !['links_snapshot', 'images_snapshot'].includes(k))
              ),
              null,
              2
            )}
          </pre>
        </div>
      )}
      
      {/* Images Gallery */}
      {attachments && attachments.length > 0 && (
        <div style={{ marginBottom: '15px' }}>
          <ImageGallery 
            artifactId={artifact.id} 
            attachments={attachments}
            onDelete={onDeleteAttachment || (() => {})}
            showUpload={false}
          />
        </div>
      )}

      {/* Links Section */}
      {(currentOutgoingLinks.length > 0 || currentIncomingLinks.length > 0) && (
        <div style={{ marginTop: '30px', paddingTop: '15px', borderTop: '1px solid var(--border)' }}>
          <h4 style={{ marginTop: 0, marginBottom: '12px' }}>Traceability Links</h4>

          {deleteError && (
            <div
              style={{
                backgroundColor: 'var(--tint-red)',
                border: '1px solid var(--danger)',
                color: 'var(--danger-strong)',
                padding: '8px 10px',
                borderRadius: '3px',
                marginBottom: '12px',
                fontSize: '12px',
              }}
            >
              {deleteError}
            </div>
          )}

          {currentOutgoingLinks.length > 0 && (
            <div style={{ marginBottom: '15px' }}>
              <strong style={{ color: 'var(--success)', fontSize: '13px' }}>↓ Links From This Artifact ({currentOutgoingLinks.length})</strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(currentOutgoingByType, 'outgoing', !allowLinkDelete)}
              </div>
            </div>
          )}

          {currentIncomingLinks.length > 0 && (
            <div>
              <strong style={{ color: 'var(--accent-strong)', fontSize: '13px' }}>↑ Links To This Artifact ({currentIncomingLinks.length})</strong>
              <div style={{ marginTop: '8px' }}>
                {renderLinkGroup(currentIncomingByType, 'incoming', !allowLinkDelete)}
              </div>
            </div>
          )}
        </div>
      )}
      
      <div style={{ marginTop: '15px', fontSize: '12px', color: 'var(--text-muted)' }}>
        <p>Created: {new Date(artifact.created_at).toLocaleString()}</p>
        <p>Updated: {new Date(artifact.updated_at).toLocaleString()}</p>
      </div>
        </>
      )}
    </div>
  );
};
