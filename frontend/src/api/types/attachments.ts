// The types api/attachments.ts sends and receives.
// Files attached to artifacts, their versions, uploads and downloads.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

/**
 * How far a file upload has got, as a whole percentage, or null where the
 * browser cannot say how big the body is. Progress matters because an upload
 * is the one call whose duration is the member's bandwidth rather than the
 * server's speed: a minute of no feedback reads as a hang.
 */
export type UploadProgressHandler = (percent: number | null) => void;

/**
 * What a figure's file can be done with. Derived server-side from the MIME
 * type so that "can I show this?" has one answer rather than one per screen:
 * an image renders, a document opens in the viewer, a model is downloaded and
 * opened in the tool that owns the format.
 */
export type AttachmentKind = 'image' | 'document' | 'model' | 'other';

export interface Attachment {
  id: string;
  artifact_id: string;
  /** Stored name, derived from figure_ref (e.g. "REQ-17-FIG-1.png"). */
  filename: string;
  /** The name the uploaded file had. */
  original_filename: string;
  /**
   * The name a member gave the figure; empty when it has none, in which
   * case readers fall back to original_filename. Renaming is a figure
   * version.
   */
  title: string;
  mime_type: string;
  file_path: string;
  file_size: number;
  /**
   * The figure's citable reference and its number within the artifact. Absent
   * only on images whose artifact has no stable reference to build one from.
   */
  figure_ref?: string;
  figure_num?: number;
  /** The family the file belongs to, from its MIME type. */
  kind: AttachmentKind;
  /** The figure's current version, starting at 1. */
  version: number;
  created_at: string;
}

/** One revision of a figure: a new image, or a new title over the same one. */
export interface AttachmentVersion {
  id: string;
  attachment_id: string;
  version: number;
  filename: string;
  original_filename: string;
  /** The title the figure carried at this version. */
  title: string;
  mime_type: string;
  file_path: string;
  file_size: number;
  /** A figure can change format between versions, so each one says which. */
  kind: AttachmentKind;
  created_by?: string | null;
  created_at: string;
  /**
   * The older version this one brought back, when it was written by a
   * restore. A restore reuses the older version's stored file, so this is
   * the only thing that tells it apart from a re-upload of the same image.
   */
  restored_from?: number | null;
}
