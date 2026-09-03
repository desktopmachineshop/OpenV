import React, { useState, useRef } from 'react';
import { Attachment, AttachmentVersion, attachmentAPI } from '../api/client';
import { ImageLightbox } from './ImageLightbox';
import { useAlert, useConfirm } from './ui';
import './ImageGallery.css';

interface ImageGalleryProps {
  artifactId: string;
  attachments: Attachment[];
  onDelete: (attachmentId: string) => void;
  onUpload?: (file: File) => void;
  /**
   * Replace a figure's image with a new version. Without it the gallery is
   * read-only for versions — the figure history is still browsable.
   */
  onUploadVersion?: (attachmentId: string, file: File) => void;
  isUploadLoading?: boolean;
  showUpload?: boolean; // Controls whether upload box is displayed
  thumbnailSize?: number; // Custom thumbnail size in pixels (default 120)
}

/** What a figure is called: its reference where it has one, else its filename. */
const figureLabel = (a: Attachment): string => a.figure_ref || a.filename;

export const ImageGallery: React.FC<ImageGalleryProps> = ({
  artifactId,
  attachments,
  onDelete,
  onUpload,
  onUploadVersion,
  isUploadLoading = false,
  showUpload = false,
  thumbnailSize = 120,
}) => {
  const confirm = useConfirm();
  const alertDialog = useAlert();
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

  const validImage = (file: File): boolean => {
    if (!file.type.startsWith('image/')) {
      void alertDialog({ title: 'Upload image', message: 'Please select an image file.' });
      return false;
    }
    if (file.size > 10 * 1024 * 1024) {
      void alertDialog({ title: 'Upload image', message: 'File size must be less than 10MB.' });
      return false;
    }
    return true;
  };

  const openHistory = async (e: React.MouseEvent, attachment: Attachment) => {
    e.stopPropagation();
    setHistoryFor(attachment);
    setHistory([]);
    setHistoryError('');
    try {
      const res = await attachmentAPI.listVersions(attachment.id);
      setHistory(res.data || []);
    } catch {
      setHistoryError('Failed to load the figure history.');
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
    if (file && target && validImage(file)) {
      onUploadVersion?.(target, file);
    }
  };

  const handleDelete = async (e: React.MouseEvent, attachmentId: string) => {
    e.stopPropagation();
    const ok = await confirm({
      title: 'Delete image',
      message: 'Are you sure you want to delete this image?',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (ok) {
      onDelete(attachmentId);
    }
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      if (!file.type.startsWith('image/')) {
        void alertDialog({ title: 'Upload image', message: 'Please select an image file.' });
        return;
      }

      const maxSize = 10 * 1024 * 1024;
      if (file.size > maxSize) {
        void alertDialog({ title: 'Upload image', message: 'File size must be less than 10MB.' });
        return;
      }

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
    if (file) {
      if (!file.type.startsWith('image/')) {
        void alertDialog({ title: 'Upload image', message: 'Please drop an image file.' });
        return;
      }

      const maxSize = 10 * 1024 * 1024;
      if (file.size > maxSize) {
        void alertDialog({ title: 'Upload image', message: 'File size must be less than 10MB.' });
        return;
      }

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
        <h4 className="gallery-title">Images</h4>
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
              accept="image/*"
              onChange={handleFileChange}
              disabled={isUploadLoading}
              className="gallery-upload-input"
              aria-label="Upload image"
            />
            <div className="gallery-upload-content">
              <div className="upload-icon">📷</div>
              <p className="upload-text">{isUploadLoading ? 'Uploading...' : 'Drag images here or click'}</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="image-gallery">
        <h4 className="gallery-title">Images ({attachments.length})</h4>
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
              title={`${figureLabel(attachment)} (v${attachment.version}) — ${attachment.original_filename || attachment.filename}`}
            >
              <img
                src={attachmentAPI.getDownloadUrl(attachment.id, attachment.version)}
                alt={figureLabel(attachment)}
                className="gallery-thumbnail"
              />
              <div className="gallery-overlay">
                {onUploadVersion && (
                  <button
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
                    className="gallery-delete-btn"
                    onClick={(e) => openHistory(e, attachment)}
                    title="Figure history"
                    aria-label="Figure history"
                  >
                    🕘
                  </button>
                )}
                <button
                  className="gallery-delete-btn"
                  onClick={(e) => handleDelete(e, attachment.id)}
                  title="Delete image"
                  aria-label="Delete image"
                >
                  🗑
                </button>
              </div>
              <p className="gallery-filename">
                {figureLabel(attachment)}
                {attachment.version > 1 && (
                  <span style={{ color: 'var(--text-muted)' }}> · v{attachment.version}</span>
                )}
              </p>
            </div>
          ))}

          {showUpload && (
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
                accept="image/*"
                onChange={handleFileChange}
                disabled={isUploadLoading}
                className="gallery-upload-input"
                aria-label="Upload image"
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
        accept="image/*"
        onChange={handleVersionFileChange}
        style={{ display: 'none' }}
        aria-hidden="true"
        tabIndex={-1}
      />

      {selectedImage && (
        <ImageLightbox
          imageUrl={attachmentAPI.getDownloadUrl(selectedImage.id, selectedImage.version)}
          filename={`${figureLabel(selectedImage)} (v${selectedImage.version})`}
          onClose={() => setSelectedImage(null)}
        />
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
            {history.map((v) => (
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
                <img
                  src={attachmentAPI.getDownloadUrl(historyFor.id, v.version)}
                  alt={`${figureLabel(historyFor)} version ${v.version}`}
                  style={{ width: 56, height: 56, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)' }}
                />
                <div style={{ flex: 1, fontSize: 12 }}>
                  <div style={{ fontWeight: 700 }}>
                    Version {v.version}
                    {v.version === historyFor.version && (
                      <span style={{ color: 'var(--success)', fontWeight: 400 }}> · current</span>
                    )}
                  </div>
                  <div style={{ color: 'var(--text-muted)' }}>
                    {new Date(v.created_at).toLocaleString()} · {v.original_filename || v.filename}
                  </div>
                </div>
                <a
                  href={attachmentAPI.getDownloadUrl(historyFor.id, v.version)}
                  target="_blank"
                  rel="noreferrer"
                  style={{ fontSize: 12 }}
                >
                  Open
                </a>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
};

export default ImageGallery;
