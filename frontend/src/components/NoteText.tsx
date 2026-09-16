import React, { useMemo } from 'react';
import { isFigureRef } from './artifactReferences';

/**
 * A note's text, with its "#REQ-12" citations and "@name" mentions marked up.
 *
 * Notes are plain prose, not markdown, and they stay that way: running years
 * of existing notes through a markdown renderer would start reinterpreting
 * every asterisk and leading "#" people have already written. So this splits
 * the text on the tokens it knows and leaves every other character exactly as
 * typed, with the surrounding element's `pre-wrap` preserving the layout.
 *
 * A reference is only a link when there is somewhere to send the reader:
 * without `onReferenceClick` it renders as marked text, because a citation
 * that looks clickable and is not would be worse than one that plainly is not.
 */
interface NoteTextProps {
  text: string;
  onReferenceClick?: (ref: string) => void;
}

// A reference or a mention at a word boundary. The two are told apart by the
// character the token starts with, which is why they share one pattern: a
// single pass keeps the surrounding text in one piece.
const tokenPattern = /(^|\s)(#{1,2}[A-Za-z][A-Za-z0-9]*-\d+(?:-FIG-\d+)?|@{1,2}[\w.-]+)/g;

interface Piece {
  text: string;
  token?: 'reference' | 'mention';
  /** The reference or handle, without its marker. */
  value?: string;
  /** Whether the token was written with a doubled marker. */
  doubled?: boolean;
}

/** Split a note into plain runs and the tokens between them. */
export const notedPieces = (text: string): Piece[] => {
  const pieces: Piece[] = [];
  let last = 0;
  for (const m of text.matchAll(tokenPattern)) {
    const lead = m[1] || '';
    const token = m[2];
    const start = (m.index ?? 0) + lead.length;
    if (start > last) pieces.push({ text: text.slice(last, start) });
    const isMention = token.startsWith('@');
    const marker = token.match(isMention ? /^@+/ : /^#+/)![0];
    pieces.push({
      text: token,
      token: isMention ? 'mention' : 'reference',
      value: token.slice(marker.length),
      doubled: marker.length === 2,
    });
    last = start + token.length;
  }
  if (last < text.length) pieces.push({ text: text.slice(last) });
  return pieces;
};

export const NoteText: React.FC<NoteTextProps> = ({ text, onReferenceClick }) => {
  const pieces = useMemo(() => notedPieces(text || ''), [text]);
  return (
    <>
      {pieces.map((piece, i) => {
        if (piece.token === 'mention') {
          return (
            <span
              key={i}
              title={
                piece.doubled
                  ? `Raises a to-do for ${piece.value}`
                  : `Mentions ${piece.value}`
              }
              style={{ color: 'var(--accent)', fontWeight: 600 }}
            >
              {piece.text}
            </span>
          );
        }
        if (piece.token === 'reference') {
          const ref = piece.value as string;
          if (!onReferenceClick) {
            return (
              <span key={i} style={{ fontWeight: 600 }}>
                {piece.text}
              </span>
            );
          }
          return (
            <a
              key={i}
              href={`#${ref}`}
              title={isFigureRef(ref) ? `Open ${ref}` : `Go to ${ref}`}
              onClick={(e) => {
                e.preventDefault();
                onReferenceClick(ref);
              }}
              style={{ fontWeight: 600 }}
            >
              {piece.text}
            </a>
          );
        }
        return <React.Fragment key={i}>{piece.text}</React.Fragment>;
      })}
    </>
  );
};

export default NoteText;
