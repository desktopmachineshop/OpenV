import React from 'react';

/**
 * The V&V Assistant's cover card (project-covers) when it draws the cover
 * itself: the card carries the drawing as SVG, shows it before it is
 * applied, and applying it renders the drawing in this browser and uploads
 * the picture. The assistant needs no image model and the workspace no key.
 *
 * The SVG is only ever shown as an image (an <img> of a data: or blob: URL),
 * which runs no script and loads nothing from elsewhere; the server never
 * stores or serves it.
 */

/** A drawing's size limit, the server's (projects.MaxCoverSVGBytes). */
export const COVER_SVG_MAX = 256 * 1024;

/** The frame a cover is drawn into: the server's 1536x1024. */
export const COVER_FRAME = { width: 1536, height: 1024 };

type Suggestion = { kind: string; [key: string]: any };

/** The drawing a cover card carries, '' for a card that does not draw. */
export const coverDrawing = (s: Suggestion): string =>
  s.kind === 'cover' && typeof s.svg === 'string' ? s.svg.trim() : '';

/** Whether a drawing is one a card can show and apply. */
export const drawable = (svg: string): boolean => /^<svg[\s>]/i.test(svg) && svg.length <= COVER_SVG_MAX;

export const svgDataUrl = (svg: string): string => `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;

/** The picture on the face of a cover card that draws; null for any other card. */
export const coverPreview = (s: Suggestion): React.ReactNode => {
  const svg = coverDrawing(s);
  if (!svg || !drawable(svg)) return null;
  return (
    <img
      src={svgDataUrl(svg)}
      alt="The cover the assistant drew"
      style={{
        display: 'block',
        width: '100%',
        aspectRatio: '3 / 2',
        objectFit: 'cover',
        borderRadius: 4,
        margin: '2px 0 6px',
        background: 'var(--surface-hover)',
      }}
    />
  );
};

/**
 * The drawing sized to the cover's frame, so the browser draws it at that
 * size whatever width and height it gave itself: scaled to cover the frame
 * and cropped, as the server does.
 */
const framed = (svg: string): string => {
  const doc = new DOMParser().parseFromString(svg, 'image/svg+xml');
  const root = doc.documentElement;
  if (root.nodeName !== 'svg' || doc.getElementsByTagName('parsererror').length) throw new Error('not an SVG drawing');
  if (!root.getAttribute('viewBox')) {
    const w = parseFloat(root.getAttribute('width') || '') || COVER_FRAME.width;
    const h = parseFloat(root.getAttribute('height') || '') || COVER_FRAME.height;
    root.setAttribute('viewBox', `0 0 ${w} ${h}`);
  }
  root.setAttribute('width', String(COVER_FRAME.width));
  root.setAttribute('height', String(COVER_FRAME.height));
  root.setAttribute('preserveAspectRatio', 'xMidYMid slice');
  if (!root.getAttribute('xmlns')) root.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
  return new XMLSerializer().serializeToString(doc);
};

/**
 * renderCoverDrawing draws an SVG into a 1536x1024 PNG in this browser,
 * which draws everything SVG can (text and filters included). It rejects
 * when the browser cannot, and the caller then has the server render it.
 */
export const renderCoverDrawing = (svg: string): Promise<Blob> =>
  new Promise((resolve, reject) => {
    let url = '';
    try {
      url = URL.createObjectURL(new Blob([framed(svg)], { type: 'image/svg+xml' }));
    } catch (err) {
      reject(err);
      return;
    }
    const img = new Image();
    const done = () => URL.revokeObjectURL(url);
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        canvas.width = COVER_FRAME.width;
        canvas.height = COVER_FRAME.height;
        const ctx = canvas.getContext('2d');
        if (!ctx) throw new Error('no canvas');
        ctx.drawImage(img, 0, 0, COVER_FRAME.width, COVER_FRAME.height);
        canvas.toBlob((blob) => {
          done();
          if (blob && blob.size > 0) resolve(blob);
          else reject(new Error('the drawing rendered nothing'));
        }, 'image/png');
      } catch (err) {
        done();
        reject(err);
      }
    };
    img.onerror = () => {
      done();
      reject(new Error('the browser could not draw it'));
    };
    img.src = url;
  });
