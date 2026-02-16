import React, { useState, useRef } from 'react';
import { Attachment, attachmentAPI } from '../api/client';
import { ImageLightbox } from './ImageLightbox';
import './ImageGallery.css';

interface ImageGalleryProps {
  artifactId: string;
  attachments: Attachment[];
  onDelete: (attachmentId: string) => void;
  onUpload?: (file: File) => void;
  isUploadLoading?: boolean;
  showUpload?: boolean; // Controls whether upload box is displayed
  thumbnailSize?: number; // Custom thumbnail size in pixels (default 120)
}

export const ImageGallery: React.FC<ImageGalleryProps> = ({
  artifactId,
  attachments,
  onDelete,
  onUpload,
  isUploadLoading = false,
  showUpload = false,
  thumbnailSize = 120,
}) => {
  const [selectedImage, setSelectedImage] = useState<Attachment | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleDelete = (e: React.MouseEvent, attachmentId: string) => {
    e.stopPropagation();
    if (window.confirm('Are you sure you want to delete this image?')) {
      onDelete(attachmentId);
    }
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      if (!file.type.startsWith('image/')) {
        alert('Please select an image file');
        return;
      }

      const maxSize = 10 * 1024 * 1024;
      if (file.size > maxSize) {
        alert('File size must be less than 10MB');
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
        alert('Please drop an image file');
        return;
      }

      const maxSize = 10 * 1024 * 1024;
      if (file.size > maxSize) {
        alert('File size must be less than 10MB');
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
              title={attachment.filename}
            >
              <img
                src={attachmentAPI.getDownloadUrl(attachment.id)}
                alt={attachment.filename}
                className="gallery-thumbnail"
              />
              <div className="gallery-overlay">
                <button
                  className="gallery-delete-btn"
                  onClick={(e) => handleDelete(e, attachment.id)}
                  title="Delete image"
                  aria-label="Delete image"
                >
                  🗑
                </button>
              </div>
              <p className="gallery-filename">{attachment.filename}</p>
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

      {selectedImage && (
        <ImageLightbox
          imageUrl={attachmentAPI.getDownloadUrl(selectedImage.id)}
          filename={selectedImage.filename}
          onClose={() => setSelectedImage(null)}
        />
      )}
    </>
  );
};

export default ImageGallery;
