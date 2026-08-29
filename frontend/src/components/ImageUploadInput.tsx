import React, { useRef } from 'react';
import { useAlert } from './ui';
import './ImageUploadInput.css';

interface ImageUploadInputProps {
  onUpload: (file: File) => void;
  isLoading?: boolean;
}

export const ImageUploadInput: React.FC<ImageUploadInputProps> = ({
  onUpload,
  isLoading = false,
}) => {
  const alertDialog = useAlert();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      // Validate file is an image
      if (!file.type.startsWith('image/')) {
        void alertDialog({ title: 'Upload image', message: 'Please select an image file.' });
        return;
      }

      // Validate file size (max 10MB)
      const maxSize = 10 * 1024 * 1024; // 10MB
      if (file.size > maxSize) {
        void alertDialog({ title: 'Upload image', message: 'File size must be less than 10MB.' });
        return;
      }

      onUpload(file);
      // Reset the input so the same file can be uploaded again
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

      onUpload(file);
    }
  };

  return (
    <div
      className="image-upload-container"
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      onClick={() => !isLoading && fileInputRef.current?.click()}
      role="button"
      tabIndex={0}
    >
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        onChange={handleFileChange}
        disabled={isLoading}
        className="image-upload-input"
        aria-label="Upload image"
      />

      <div className="upload-content">
        <div className="upload-icon">📷</div>
        <p className="upload-text">
          {isLoading ? 'Uploading...' : 'Click to upload or drag and drop'}
        </p>
        <p className="upload-hint">PNG, JPG, GIF, WebP up to 10MB</p>
      </div>
    </div>
  );
};

export default ImageUploadInput;
