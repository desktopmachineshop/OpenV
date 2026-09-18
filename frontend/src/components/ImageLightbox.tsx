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
    // role and aria-modal like every other overlay in the app: it is what tells
    // a screen reader this has taken over, and what the reading keys and the
    // swipe check before claiming a keystroke or a gesture behind it.
    <div
      className="lightbox-backdrop"
      role="dialog"
      aria-modal="true"
      aria-label={filename}
      onClick={handleBackdropClick}
    >
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
