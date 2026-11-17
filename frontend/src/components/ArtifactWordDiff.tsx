import React from 'react';
import { wordDiff } from '../utils/baselinesDiff';

// An artifact's text before and after a change, word by word: what went
// struck out in red, what came underlined in green. The Baselines page's
// comparison and the activity log's edit detail draw the same diff.
export const WordDiff: React.FC<{ before: string; after: string; fontSize?: number }> = ({ before, after, fontSize = 14 }) => (
  <p
    style={{
      margin: 0,
      fontSize,
      lineHeight: 1.7,
      color: 'var(--text)',
      whiteSpace: 'pre-wrap',
    }}
  >
    {wordDiff(before, after).map((p, i) =>
      p.kind === 'same' ? (
        <span key={i}>{p.text}</span>
      ) : p.kind === 'del' ? (
        <del
          key={i}
          style={{
            background: 'var(--tint-red)',
            color: 'var(--danger-strong)',
          }}
        >
          {p.text}
        </del>
      ) : (
        <ins
          key={i}
          style={{
            background: 'var(--tint-green)',
            color: 'var(--success-text)',
            textDecoration: 'none',
            borderBottom: '1px solid var(--tint-green-border)',
          }}
        >
          {p.text}
        </ins>
      )
    )}
  </p>
);
