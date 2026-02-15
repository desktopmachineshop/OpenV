import React from 'react';
import './ImageLightbox.css';

interface ImageLightboxProps {
  imageUrl: string;
  filename: string;
  onClose: () => void;
}

export const ImageLightbox: React.FC<ImageLightboxProps> = ({
  imageUrl,
  filename,
  onClose,
}) => {
  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  return (
    <div className="lightbox-backdrop" onClick={handleBackdropClick}>
      <div className="lightbox-container">
        <div className="lightbox-header">
          <h3 className="lightbox-title">{filename}</h3>
          <button
            className="lightbox-close"
            onClick={onClose}
            title="Close"
            aria-label="Close"
          >
            ✕
          </button>
        </div>
        <div className="lightbox-content">
          <img src={imageUrl} alt={filename} className="lightbox-image" />
        </div>
      </div>
    </div>
  );
};

export default ImageLightbox;
