import React, { useState } from 'react';

interface ExpandableTextProps {
  text: string;
  /** Collapsed preview length in characters. */
  limit: number;
  style?: React.CSSProperties;
}

/**
 * Renders text in full when it fits the limit, otherwise a collapsed preview
 * with a toggle — long log messages stay reachable instead of being cut off.
 */
export const ExpandableText: React.FC<ExpandableTextProps> = ({ text, limit, style }) => {
  const [expanded, setExpanded] = useState(false);
  if (text.length <= limit) {
    return <span style={style}>{text}</span>;
  }
  return (
    <>
      <span style={style}>{expanded ? text : text.slice(0, limit) + '…'}</span>
      <button
        onClick={() => setExpanded((v) => !v)}
        style={{
          display: 'block',
          background: 'none',
          border: 'none',
          padding: 0,
          marginTop: 2,
          color: 'var(--accent-strong)',
          cursor: 'pointer',
          fontSize: 11.5,
          fontFamily: 'inherit',
        }}
      >
        {expanded ? 'Show less' : `Show all (${text.length.toLocaleString()} chars)`}
      </button>
    </>
  );
};
