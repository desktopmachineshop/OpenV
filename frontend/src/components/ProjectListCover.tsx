import React, { useState } from 'react';
import { projectAPI, Project } from '../api/client';

// A project's cover photo: the image on its card in All projects and at the
// top of its Overview (views/ProductOverviewCover.tsx), behind the
// project-covers feature key (PROJECT_COVERS_FEATURE).

/** The image types the server accepts for a cover photo. */
export const COVER_ACCEPT = 'image/png,image/jpeg,image/gif,image/webp';

/** A cover's longest edge after scaling: wide enough for the Overview's banner on a large screen. */
export const COVER_MAX_EDGE = 1600;

/** A cover this small, and no larger than COVER_MAX_EDGE, is uploaded as it is. */
const COVER_KEEP_BYTES = 1024 * 1024;

/**
 * prepareCover scales a large photo down before it is uploaded, so that a
 * camera original neither runs into the server's 5 MB cap nor makes All
 * projects load megabytes per card. A PNG stays a PNG (it may be
 * transparent); anything else becomes a JPEG. A GIF (perhaps animated), a
 * browser that cannot decode the file, or a result no smaller than the
 * original leaves the file as it is, for the server to accept or refuse.
 */
export async function prepareCover(file: File): Promise<{ blob: Blob; name: string }> {
  const original = { blob: file as Blob, name: file.name };
  if (file.type === 'image/gif' || typeof createImageBitmap !== 'function') return original;
  try {
    const bitmap = await createImageBitmap(file);
    const scale = Math.min(1, COVER_MAX_EDGE / Math.max(bitmap.width, bitmap.height));
    if (scale === 1 && file.size <= COVER_KEEP_BYTES) {
      bitmap.close();
      return original;
    }
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext('2d');
    if (!ctx) {
      bitmap.close();
      return original;
    }
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const type = file.type === 'image/png' ? 'image/png' : 'image/jpeg';
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, 0.85));
    if (!blob || blob.size >= file.size) return original;
    const stem = file.name.replace(/\.[^.]+$/, '') || 'cover';
    return { blob, name: `${stem}${type === 'image/png' ? '.png' : '.jpg'}` };
  } catch {
    return original;
  }
}

/**
 * ProjectCoverImage draws a project's cover photo, or nothing when it has
 * none. The image is decoration beside the project's name, so its alt text
 * is empty; one that fails to load is hidden rather than shown broken.
 */
export const ProjectCoverImage: React.FC<{
  project: Pick<Project, 'id' | 'cover_updated_at'>;
  className: string;
}> = ({ project, className }) => {
  const [failed, setFailed] = useState<string | null>(null);
  const version = project.cover_updated_at;
  if (!version || failed === version) return null;
  return (
    <img
      className={className}
      src={projectAPI.coverUrl(project.id, version)}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setFailed(version)}
    />
  );
};

/**
 * ProjectCardWithCover lays a project's row out in two: its cover photo on
 * the left, as tall as the row, and the row's content beside it. side is
 * set when any project in the list has a cover, so every row keeps the
 * same column: one with no photo shows its initial on a plain tile there.
 * Without side it draws the content alone, exactly as before.
 */
export const ProjectCardWithCover: React.FC<{
  project: Pick<Project, 'id' | 'name' | 'cover_updated_at'>;
  side: boolean;
  children: React.ReactNode;
}> = ({ project, side, children }) => {
  const [failed, setFailed] = useState<string | null>(null);
  if (!side) return <>{children}</>;
  const version = project.cover_updated_at;
  const photo = version && failed !== version;
  return (
    <>
      {photo ? (
        <img
          className="project-card-side"
          src={projectAPI.coverUrl(project.id, version)}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setFailed(version)}
        />
      ) : (
        <div className="project-card-side is-empty" aria-hidden>
          {project.name.trim().charAt(0).toUpperCase()}
        </div>
      )}
      <div className="project-card-body">{children}</div>
    </>
  );
};
