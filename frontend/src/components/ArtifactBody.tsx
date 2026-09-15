import React, { useMemo } from 'react';
import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  isFigureRef,
  linkifyReferences,
  referenceFromHref,
  referenceUrlTransform,
} from './artifactReferences';
import { remarkSoftBreaks } from './markdownSoftBreaks';

interface ArtifactBodyProps {
  body: string;
  /**
   * Follow a reference the reader clicked. Without it references still render
   * as text — a citation that looks clickable and is not would be worse than
   * one that plainly is not.
   */
  onReferenceClick?: (ref: string) => void;
}

/**
 * An artifact's description, with its "#REQ-12" citations turned into links a
 * reader can follow.
 *
 * The references are rewritten to markdown links before rendering and the
 * anchor is intercepted here, so following a citation moves within the project
 * rather than navigating the browser away from it. `urlTransform` is what
 * carries the reference scheme past the renderer's URL sanitiser; without it
 * the anchors arrive here with an empty href and click through to the page the
 * reader is already on.
 */
export const ArtifactBody: React.FC<ArtifactBodyProps> = ({ body, onReferenceClick }) => {
  const source = useMemo(
    () => (onReferenceClick ? linkifyReferences(body) : body),
    [body, onReferenceClick]
  );

  return (
    <div className="markdown-content">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkSoftBreaks]}
        urlTransform={(url, key, node) => referenceUrlTransform(url, key, node, defaultUrlTransform)}
        components={{
          a: ({ href, children, ...rest }) => {
            const ref = referenceFromHref(href);
            if (!ref || !onReferenceClick) {
              // An ordinary link: opened in a new tab so a reader does not
              // lose the document they were reading.
              return (
                <a href={href} target="_blank" rel="noreferrer" {...rest}>
                  {children}
                </a>
              );
            }
            return (
              <a
                href={href}
                title={isFigureRef(ref) ? `Open ${ref}` : `Go to ${ref}`}
                onClick={(e) => {
                  e.preventDefault();
                  onReferenceClick(ref);
                }}
                style={{ color: 'var(--accent-strong)', fontWeight: 600, textDecoration: 'none' }}
              >
                {children}
              </a>
            );
          },
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  );
};

export default ArtifactBody;
