import React from 'react';

interface AvatarProps {
  /** Picture URL; falls back to an initial when empty. */
  src?: string | null;
  /** Name (or email) the initial is taken from. */
  name?: string | null;
  /** Diameter in pixels. */
  size?: number;
}

/**
 * A user's picture, or the first letter of their name, in a fixed circle.
 *
 * Every render site puts the avatar in a flex row next to a name that can be
 * long, and on a phone the row is narrow. A plain box with width and height
 * set is still a flex item that shrinks: it was squeezed to the width of its
 * one letter and became a tall oval (issue #319). The circle here refuses to
 * shrink, and a picture is cropped to the circle rather than stretched.
 */
export const Avatar: React.FC<AvatarProps> = ({ src, name, size = 28 }) => {
  const box: React.CSSProperties = {
    width: size,
    height: size,
    minWidth: size,
    minHeight: size,
    borderRadius: '50%',
    flexShrink: 0,
    boxSizing: 'border-box',
  };

  if (src) {
    return <img src={src} alt="" style={{ ...box, objectFit: 'cover', display: 'block' }} />;
  }

  return (
    <div
      aria-hidden="true"
      style={{
        ...box,
        background: 'var(--accent)',
        color: 'var(--accent-fg)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: Math.round(size * 0.46),
        fontWeight: 700,
        lineHeight: 1,
      }}
    >
      {(name || '?').charAt(0).toUpperCase()}
    </div>
  );
};
