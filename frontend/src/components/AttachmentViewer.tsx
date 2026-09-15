import React, { useEffect, useMemo, useState } from 'react';
import { Attachment, attachmentAPI } from '../api/client';
import { StlPreview, StlModel, parseStl } from './StlPreview';
import { attachmentLabel, formatFileSize, formatLabel, isStl } from './attachmentKinds';
import './ImageLightbox.css';

// One window for looking at an attached file, whatever it turned out to be.
//
// The API serves everything but a plain picture as an opaque download under a
// policy that permits nothing, and refuses to be framed at all. That is what
// makes storing a PDF or a CAD model safe, and it is not worth relaxing for a
// preview — so the bytes are fetched with the member's session and rendered
// here, on the app's own origin, where the browser's PDF viewer and the STL
// renderer can have them without the API conceding anything.
//
// Every kind ends in the same place: a Download button. A preview is a
// convenience; the file is the deliverable.

interface AttachmentViewerProps {
  attachment: Attachment;
  /** The version to show; defaults to the attachment's current one. */
  version?: number;
  onClose: () => void;
}

type Loaded =
  | { state: 'loading' }
  | { state: 'error'; message: string }
  | { state: 'ready'; url: string; model?: StlModel; unsupportedModel?: boolean };

export const AttachmentViewer: React.FC<AttachmentViewerProps> = ({
  attachment,
  version,
  onClose,
}) => {
  const shown = version ?? attachment.version;
  const kind = attachment.kind;
  const directUrl = useMemo(
    () => attachmentAPI.getDownloadUrl(attachment.id, shown),
    [attachment.id, shown]
  );

  // An image is already renderable from its own URL — the API serves a picture
  // inline and an <img> carries the session cookie — so only the kinds that
  // need the bytes in hand fetch them.
  const needsBytes = kind === 'document' || isStl(attachment);
  const [loaded, setLoaded] = useState<Loaded>(
    needsBytes ? { state: 'loading' } : { state: 'ready', url: directUrl }
  );

  useEffect(() => {
    if (!needsBytes) {
      setLoaded({ state: 'ready', url: directUrl });
      return;
    }
    let objectUrl = '';
    let cancelled = false;
    setLoaded({ state: 'loading' });

    attachmentAPI
      .fetchFile(attachment.id, shown)
      .then(async (res) => {
        if (cancelled) return;
        const blob = res.data as Blob;
        if (isStl(attachment)) {
          const model = parseStl(await blob.arrayBuffer());
          if (cancelled) return;
          setLoaded(
            model
              ? { state: 'ready', url: '', model }
              : { state: 'ready', url: '', unsupportedModel: true }
          );
          return;
        }
        // The blob is retyped rather than trusted: the browser renders it
        // from this origin, so what it is told the bytes are matters.
        objectUrl = URL.createObjectURL(blob.slice(0, blob.size, 'application/pdf'));
        setLoaded({ state: 'ready', url: objectUrl });
      })
      .catch(() => {
        if (!cancelled) {
          setLoaded({ state: 'error', message: 'That file could not be loaded. It may have been deleted.' });
        }
      });

    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [attachment, shown, needsBytes, directUrl]);

  // Escape closes it, as it closes everything else this app floats over the
  // document.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const title = `${attachmentLabel(attachment)} (v${shown})`;
  const original = attachment.original_filename || attachment.filename;

  return (
    <div
      className="lightbox-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div className="lightbox-container">
        <div className="lightbox-header">
          <h3 className="lightbox-title">{title}</h3>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <a
              href={directUrl}
              download={attachment.filename}
              style={{ fontSize: 13, color: 'var(--accent)', whiteSpace: 'nowrap' }}
            >
              Download
            </a>
            <button className="lightbox-close" onClick={onClose} title="Close" aria-label="Close">
              ✕
            </button>
          </div>
        </div>
        <div className="lightbox-content">
          <Body attachment={attachment} loaded={loaded} title={title} />
        </div>
        <p
          style={{
            margin: 0,
            padding: '8px 16px 12px',
            fontSize: 12,
            color: 'var(--text-muted)',
            textAlign: 'center',
          }}
        >
          {original} · {formatLabel(attachment)} · {formatFileSize(attachment.file_size)}
        </p>
      </div>
    </div>
  );
};

/** What fills the window, by kind. */
const Body: React.FC<{ attachment: Attachment; loaded: Loaded; title: string }> = ({
  attachment,
  loaded,
  title,
}) => {
  if (loaded.state === 'loading') {
    return <p style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</p>;
  }
  if (loaded.state === 'error') {
    return <p style={{ color: 'var(--danger)', padding: 40 }}>{loaded.message}</p>;
  }

  if (attachment.kind === 'image') {
    return <img src={loaded.url} alt={title} className="lightbox-image" />;
  }

  if (attachment.kind === 'document') {
    return (
      <iframe
        title={title}
        src={loaded.url}
        style={{
          width: 'min(900px, 88vw)',
          height: 'min(700px, 70vh)',
          border: '1px solid var(--border)',
          borderRadius: 6,
          background: 'var(--surface)',
        }}
      />
    );
  }

  if (loaded.model) {
    return <StlPreview model={loaded.model} />;
  }

  return <NoPreview attachment={attachment} unreadable={!!loaded.unsupportedModel} />;
};

/**
 * What a format with no browser preview says for itself.
 *
 * It says which format it is and what opens it, rather than a shrug: a
 * reviewer who cannot see the part still needs to know whether they have the
 * tool for it before they download 40 MB.
 */
const NoPreview: React.FC<{ attachment: Attachment; unreadable: boolean }> = ({
  attachment,
  unreadable,
}) => (
  <div style={{ padding: '32px 24px', textAlign: 'center', maxWidth: 460 }}>
    <div style={{ fontSize: 40, marginBottom: 12 }} aria-hidden="true">
      {attachment.kind === 'model' ? '📐' : '📎'}
    </div>
    <p style={{ margin: '0 0 8px', fontWeight: 600 }}>
      {unreadable
        ? 'This STL could not be read.'
        : `${formatLabel(attachment)} files are not previewed in the browser.`}
    </p>
    <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)' }}>
      {unreadable
        ? 'The file is stored and can still be downloaded; it may be truncated or in a format that only names itself STL.'
        : 'Download it to open in the tool that owns the format. Solid geometry needs a CAD kernel to interpret, and an approximation of a part is worse than none.'}
    </p>
  </div>
);

export default AttachmentViewer;
