import React, { useCallback, useRef, useState } from 'react';
import { Attachment, AttachmentVersion, attachmentAPI } from '../api/client';
import { AttachmentViewer } from './AttachmentViewer';
import {
  ACCEPTED_UPLOAD_TYPES,
  ATTACHMENT_FORMATS_FEATURE,
  UNSUPPORTED_UPLOAD_MESSAGE,
  formatFileSize,
  formatLabel,
  isAcceptedUpload,
  kindGlyph,
} from './attachmentKinds';
import { useAlert, useConfirm, usePrompt } from './ui';
import { useFeature } from '../hooks/useFeature';
import './ImageGallery.css';

/** The feature key that gates renaming a figure (REQ-157). */
export const FIGURE_TITLES_FEATURE = 'figure-titles';

/** Restoring an older version of a figure sits behind this gate. */
export const FIGURE_REVERT_FEATURE = 'figure-revert';

interface ImageGalleryProps {
  artifactId: string;
  attachments: Attachment[];
  /** Omitted when the gallery is read-only. */
  onDelete?: (attachmentId: string) => void;
  /**
   * Show figures without the controls that change them. Adding, replacing and
   * removing a figure belongs to editing the artifact, so the details view
   * passes this: what the document shows should change by a deliberate edit,
   * not a stray click while reading.
   */
  readOnly?: boolean;
  onUpload?: (file: File) => void;
  /**
   * Replace a figure's image with a new version. Without it the gallery is
   * read-only for versions — the figure history is still browsable.
   */
  onUploadVersion?: (attachmentId: string, file: File) => void;
  /**
   * Called after an older version has been restored, so the caller can
   * reload the artifact's figures. Without it the restore still happens and
   * the history dialog updates; only the thumbnails behind it go stale.
   */
  onRestored?: () => void;
  /**
   * Give a figure a title. Without it the gallery shows names but cannot
   * change them.
   */
  onRename?: (attachmentId: string, title: string) => void;
  isUploadLoading?: boolean;
  showUpload?: boolean; // Controls whether upload box is displayed
  thumbnailSize?: number; // Custom thumbnail size in pixels (default 120)
}

// Every button here is type="button". The gallery is rendered inside the
// artifact editor's <form>, where a button with no type is a submit
// button: clicking "new version" or "delete" saved the artifact and closed
// the editor before the file was even chosen, and the image never changed.

/** What a figure is called: its reference where it has one, else its filename. */
const figureLabel = (a: Attachment): string => a.figure_ref || a.filename;

/**
 * The most one figure may weigh, mirroring the server's OPENV_MAX_UPLOAD_MB
 * default. CAD is why it is no longer the 10 MB it was for images alone: an
 * assembly clears that on its own.
 */
const MAX_UPLOAD_BYTES = 25 * 1024 * 1024;

/** A figure's name to a reader: its title, else the name it was uploaded under. */
export const figureName = (a: { title?: string; original_filename?: string; filename: string }): string =>
  a.title?.trim() || a.original_filename || a.filename;

/**
 * What one history entry changed. The first version is the upload; a later
 * one over the same file is a rename, and one with a new file is a new image.
 *
 * A restore is checked first and on its own field. It reuses the stored file
 * of the version it brings back, so by path alone it looks like a new image
 * — or, if the figure had been renamed and then restored to a version with
 * the same picture, like a rename. Neither describes what happened.
 */
export const versionKind = (
  v: AttachmentVersion,
  older: AttachmentVersion | undefined
): string => {
  if (v.restored_from) return `restored from v${v.restored_from}`;
  if (!older) return 'uploaded';
  return older.file_path === v.file_path ? 'renamed' : 'new image';
};

export const ImageGallery: React.FC<ImageGalleryProps> = ({
  artifactId,
  attachments,
  onDelete,
  readOnly = false,
  onUpload,
  onUploadVersion,
  onRestored,
  onRename,
  isUploadLoading = false,
  showUpload = false,
  thumbnailSize = 120,
}) => {
  const confirm = useConfirm();
  const alertDialog = useAlert();
  const prompt = usePrompt();
  const canRename = useFeature(FIGURE_TITLES_FEATURE) && !readOnly && Boolean(onRename);
  // Restoring changes what the figure shows, so it needs the same write
  // access as uploading a version — hence onUploadVersion, which is only
  // passed where the caller can write.
  const canRestore = useFeature(FIGURE_REVERT_FEATURE) && !readOnly && Boolean(onUploadVersion);
  // What may be ATTACHED is gated (REQ-137); what may be opened never is, so
  // a workspace still on the previous stable release reads every figure a
  // colleague attached and is simply offered images to add.
  const wideFormats = useFeature(ATTACHMENT_FORMATS_FEATURE);
  const acceptTypes = wideFormats ? ACCEPTED_UPLOAD_TYPES : 'image/*';
  const [selectedImage, setSelectedImage] = useState<Attachment | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // One input serves every figure's "new version" button; the figure it is
  // acting for is held here between the click and the file being chosen.
  const versionInputRef = useRef<HTMLInputElement>(null);
  const versionTargetRef = useRef<string>('');
  // Figure whose history is open, and the versions once fetched.
  const [historyFor, setHistoryFor] = useState<Attachment | null>(null);
  const [history, setHistory] = useState<AttachmentVersion[]>([]);
  const [historyError, setHistoryError] = useState('');
  // Which version is being restored, so its own button can say so without
  // every row in the dialog going busy.
  const [restoring, setRestoring] = useState<number | null>(null);

  const validUpload = (file: File): boolean => {
    if (!wideFormats && !file.type.startsWith('image/')) {
      void alertDialog({
        title: 'Attach a figure',
        message:
          'Attaching PDFs and CAD files reaches stable-channel workspaces at their next stable release. Switch the workspace to nightly, or preview the next release, in workspace settings.',
      });
      return false;
    }
    if (!isAcceptedUpload(file)) {
      void alertDialog({ title: 'Attach a figure', message: UNSUPPORTED_UPLOAD_MESSAGE });
      return false;
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      void alertDialog({
        title: 'Attach a figure',
        message: `That file is ${formatFileSize(file.size)}. The limit is ${formatFileSize(MAX_UPLOAD_BYTES)}.`,
      });
      return false;
    }
    return true;
  };

  const startRename = async (e: React.MouseEvent, attachment: Attachment) => {
    e.stopPropagation();
    const title = await prompt({
      title: `Rename ${figureLabel(attachment)}`,
      message: 'The name appears under the figure in documents. Clear it to go back to the uploaded filename.',
      label: 'Title',
      defaultValue: attachment.title || '',
      placeholder: attachment.original_filename || attachment.filename,
      confirmLabel: 'Rename',
      allowEmpty: true,
    });
    if (title === null) return;
    const next = title.trim();
    if (next === (attachment.title || '')) return;
    onRename?.(attachment.id, next);
  };

  const loadHistory = useCallback(async (attachmentId: string) => {
    setHistoryError('');
    try {
      const res = await attachmentAPI.listVersions(attachmentId);
      setHistory(res.data || []);
    } catch {
      setHistoryError('Failed to load the figure history.');
    }
  }, []);

  const openHistory = async (e: React.MouseEvent, attachment: Attachment) => {
    e.stopPropagation();
    setHistoryFor(attachment);
    setHistory([]);
    await loadHistory(attachment.id);
  };

  const restoreVersion = async (version: number) => {
    if (!historyFor) return;
    const ok = await confirm({
      title: `Restore version ${version}`,
      message:
        `${figureLabel(historyFor)} goes back to the image and name it had at version ${version}. ` +
        'Nothing is deleted: this is recorded as a new version, and every version stays in the history.',
      confirmLabel: 'Restore',
    });
    if (!ok) return;

    setRestoring(version);
    setHistoryError('');
    try {
      const res = await attachmentAPI.restoreVersion(historyFor.id, version);
      // The figure's own row must catch up too, or the gallery keeps showing
      // the superseded thumbnail behind the open dialog.
      setHistoryFor({
        ...historyFor,
        version: res.data.version,
        title: res.data.title,
        original_filename: res.data.original_filename,
        filename: res.data.filename,
      });
      await loadHistory(historyFor.id);
      onRestored?.();
    } catch (err: any) {
      setHistoryError(
        err?.response?.status === 409
          ? 'That version is already the current one.'
          : 'Failed to restore that version.'
      );
    } finally {
      setRestoring(null);
    }
  };

  const startVersionUpload = (e: React.MouseEvent, attachmentId: string) => {
    e.stopPropagation();
    versionTargetRef.current = attachmentId;
    versionInputRef.current?.click();
  };

  const handleVersionFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    const target = versionTargetRef.current;
    e.target.value = '';
    versionTargetRef.current = '';
    if (file && target && validUpload(file)) {
      onUploadVersion?.(target, file);
    }
  };

  const handleDelete = async (e: React.MouseEvent, attachmentId: string) => {
    e.stopPropagation();
    const ok = await confirm({
      title: 'Delete figure',
      message: 'Are you sure you want to delete this figure? Its number is never reissued.',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (ok) {
      onDelete?.(attachmentId);
    }
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file && validUpload(file)) {
      onUpload?.(file);
      e.target.value = '';
    }
  };

  const handleDragOver = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.currentTarget.classList.add('drag-over');
  };

  const handleDragLeave = (e: React.DragEvent<HTMLDivElement>) => {
    e.currentTarget.classList.remove('drag-over');
  };

  const handleDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.currentTarget.classList.remove('drag-over');

    const file = e.dataTransfer.files?.[0];
    if (file && validUpload(file)) {
      onUpload?.(file);
    }
  };

  // Show gallery if there are attachments, or if upload is enabled
  if (!attachments || attachments.length === 0) {
    if (!showUpload) {
      return null;
    }

    return (
      <div className="image-gallery">
        <h4 className="gallery-title">Figures</h4>
        <div 
          className="gallery-grid"
          style={{
            gridTemplateColumns: `repeat(auto-fill, minmax(${thumbnailSize}px, 1fr))`,
          }}
        >
          <div
            className="gallery-upload-item"
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
            onClick={() => !isUploadLoading && fileInputRef.current?.click()}
            role="button"
            tabIndex={0}
          >
            <input
              ref={fileInputRef}
              type="file"
              accept={acceptTypes}
              onChange={handleFileChange}
              disabled={isUploadLoading}
              className="gallery-upload-input"
              aria-label="Upload a figure"
            />
            <div className="gallery-upload-content">
              <div className="upload-icon">{wideFormats ? '📎' : '📷'}</div>
              <p className="upload-text">
                {isUploadLoading
                  ? 'Uploading...'
                  : wideFormats
                  ? 'Drag an image, PDF or CAD file here, or click'
                  : 'Drag figures here or click'}
              </p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="image-gallery">
        <h4 className="gallery-title">Figures ({attachments.length})</h4>
        <div 
          className="gallery-grid"
          style={{
            gridTemplateColumns: `repeat(auto-fill, minmax(${ thumbnailSize}px, 1fr))`,
          }}
        >
          {attachments.map((attachment) => (
            <div
              key={attachment.id}
              className="gallery-item"
              onClick={() => setSelectedImage(attachment)}
              title={`${figureLabel(attachment)} (v${attachment.version}) — ${figureName(attachment)}`}
            >
              {attachment.kind === 'image' ? (
                <img
                  src={attachmentAPI.getDownloadUrl(attachment.id, attachment.version)}
                  alt={figureLabel(attachment)}
                  className="gallery-thumbnail"
                />
              ) : (
                // Nothing can draw a thumbnail of a PDF or a solid model, so
                // the tile says what the file is and how big it is instead —
                // which is what a reviewer needs before deciding to open it.
                <div className="gallery-thumbnail gallery-thumbnail-file">
                  <span className="gallery-file-glyph" aria-hidden="true">
                    {kindGlyph(attachment.kind)}
                  </span>
                  <span className="gallery-file-format">{formatLabel(attachment)}</span>
                  <span className="gallery-file-size">{formatFileSize(attachment.file_size)}</span>
                </div>
              )}
              <div className="gallery-overlay">
                {canRename && (
                  <button
                    type="button"
                    className="gallery-delete-btn"
                    onClick={(e) => startRename(e, attachment)}
                    title="Rename this figure"
                    aria-label="Rename this figure"
                  >
                    ✎
                  </button>
                )}
                {!readOnly && onUploadVersion && (
                  <button
                    type="button"
                    className="gallery-delete-btn"
                    onClick={(e) => startVersionUpload(e, attachment.id)}
                    title="Upload a new version of this figure"
                    aria-label="Upload a new version of this figure"
                  >
                    ⬆
                  </button>
                )}
                {attachment.version > 1 && (
                  <button
                    type="button"
                    className="gallery-delete-btn"
                    onClick={(e) => openHistory(e, attachment)}
                    title="Figure history"
                    aria-label="Figure history"
                  >
                    🕘
                  </button>
                )}
                {!readOnly && (
                  <button
                    type="button"
                    className="gallery-delete-btn"
                    onClick={(e) => handleDelete(e, attachment.id)}
                    title="Delete figure"
                    aria-label="Delete figure"
                  >
                    🗑
                  </button>
                )}
              </div>
              <p className="gallery-filename">
                {figureLabel(attachment)}
                {attachment.version > 1 && (
                  <span style={{ color: 'var(--text-muted)' }}> · v{attachment.version}</span>
                )}
              </p>
              <p className="gallery-figure-name" title={figureName(attachment)}>
                {figureName(attachment)}
              </p>
            </div>
          ))}

          {showUpload && !readOnly && (
            <div
              className="gallery-upload-item"
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDrop={handleDrop}
              onClick={() => !isUploadLoading && fileInputRef.current?.click()}
              role="button"
              tabIndex={0}
            >
              <input
                ref={fileInputRef}
                type="file"
                accept={acceptTypes}
                onChange={handleFileChange}
                disabled={isUploadLoading}
                className="gallery-upload-input"
                aria-label="Upload a figure"
              />
              <div className="gallery-upload-content">
                <div className="upload-icon">➕</div>
                <p className="upload-text">{isUploadLoading ? 'Uploading...' : 'Click or drag'}</p>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* One input for every figure's "new version"; the target is held in a
          ref between the button click and the file being chosen. */}
      <input
        ref={versionInputRef}
        type="file"
        accept={acceptTypes}
        onChange={handleVersionFileChange}
        style={{ display: 'none' }}
        aria-hidden="true"
        tabIndex={-1}
      />

      {selectedImage && (
        <AttachmentViewer attachment={selectedImage} onClose={() => setSelectedImage(null)} />
      )}

      {historyFor && (
        <div
          onClick={() => setHistoryFor(null)}
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              background: 'var(--surface)',
              border: '1px solid var(--border)',
              borderRadius: 6,
              padding: 16,
              width: 'min(520px, 92vw)',
              maxHeight: '80vh',
              overflowY: 'auto',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
              <h4 style={{ margin: 0, flex: 1 }}>{figureLabel(historyFor)} — history</h4>
              <button
                type="button"
                onClick={() => setHistoryFor(null)}
                style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: 18, width: 'auto' }}
                aria-label="Close figure history"
              >
                ×
              </button>
            </div>
            {historyError && <p style={{ color: 'var(--danger)', fontSize: 12 }}>{historyError}</p>}
            {!historyError && history.length === 0 && (
              <p style={{ color: 'var(--text-muted)', fontSize: 12 }}>Loading…</p>
            )}
            {history.map((v, i) => (
              <div
                key={v.id}
                style={{
                  display: 'flex',
                  gap: 10,
                  alignItems: 'center',
                  padding: '8px 0',
                  borderBottom: '1px solid var(--border-soft)',
                }}
              >
                {v.kind === 'image' ? (
                  <img
                    src={attachmentAPI.getDownloadUrl(historyFor.id, v.version)}
                    alt={`${figureLabel(historyFor)} version ${v.version}`}
                    style={{ width: 56, height: 56, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)' }}
                  />
                ) : (
                  <div
                    aria-hidden="true"
                    style={{
                      width: 56,
                      height: 56,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontSize: 22,
                      borderRadius: 4,
                      border: '1px solid var(--border)',
                      background: 'var(--neutral-soft)',
                    }}
                  >
                    {kindGlyph(v.kind)}
                  </div>
                )}
                <div style={{ flex: 1, fontSize: 12 }}>
                  <div style={{ fontWeight: 700 }}>
                    Version {v.version}
                    <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}> · {versionKind(v, history[i + 1])}</span>
                    {v.version === historyFor.version && (
                      <span style={{ color: 'var(--success)', fontWeight: 400 }}> · current</span>
                    )}
                  </div>
                  <div>{figureName(v)}</div>
                  <div style={{ color: 'var(--text-muted)' }}>
                    {new Date(v.created_at).toLocaleString()}
                    {v.title && v.original_filename ? ` · ${v.original_filename}` : ''}
                    {v.file_size ? ` · ${formatFileSize(v.file_size)}` : ''}
                  </div>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <a
                    href={attachmentAPI.getDownloadUrl(historyFor.id, v.version)}
                    target="_blank"
                    rel="noreferrer"
                    style={{ fontSize: 12 }}
                  >
                    Open
                  </a>
                  {/* The current version has nothing to restore to. */}
                  {canRestore && v.version !== historyFor.version && (
                    <button
                      type="button"
                      onClick={() => restoreVersion(v.version)}
                      disabled={restoring !== null}
                      title={`Bring ${figureLabel(historyFor)} back to version ${v.version}`}
                      style={{
                        minHeight: 32,
                        padding: '0 10px',
                        width: 'auto',
                        background: 'none',
                        border: '1px solid var(--border)',
                        borderRadius: 4,
                        color: 'var(--text)',
                        fontSize: 12,
                        cursor: restoring !== null ? 'not-allowed' : 'pointer',
                      }}
                    >
                      {restoring === v.version ? 'Restoring…' : 'Restore'}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
};

export default ImageGallery;
