import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { coverPreview, drawable, svgDataUrl } from './coverCard';

// The face of a cover card that draws: the drawing as an image, never as
// markup in the page.

describe('coverPreview', () => {
  const svg = `<svg xmlns='http://www.w3.org/2000/svg'><script>alert(1)</script><rect width='9' height='9'/></svg>`;

  it('shows the drawing as an image of a data: URL', () => {
    const html = renderToStaticMarkup(<>{coverPreview({ kind: 'cover', svg })}</>);
    // The script is inside the image's URL, encoded, never markup.
    expect(html).not.toContain('<script');
    const img = new DOMParser().parseFromString(html, 'text/html').querySelector('img')!;
    expect(img.getAttribute('src')).toBe(svgDataUrl(svg));
  });

  it('shows nothing for a card that does not draw, or a drawing that is not SVG', () => {
    expect(coverPreview({ kind: 'cover', prompt: 'A rover' })).toBeNull();
    expect(coverPreview({ kind: 'cover', svg: '<html/>' })).toBeNull();
    expect(coverPreview({ kind: 'release', svg })).toBeNull();
    expect(drawable('<svg>' + 'x'.repeat(256 * 1024))).toBe(false);
  });
});
