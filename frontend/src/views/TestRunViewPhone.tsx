// A test run's cases on a phone: one row each with its reference, what it
// verifies, its method and its result. A row opens the recorder at it.
import React from 'react';
import { Chip } from '../components/ui';
import { cardHeadStyle, cardStyle, cardTitleStyle, refStyle } from './VVDashboardParts';
import { MethodChip, ResultRow, evidenceLabel, verifiesText } from './TestRunViewCells';

export const PhoneCaseList: React.FC<{
  rows: ResultRow[];
  readOnly: boolean;
  onOpen: (index: number) => void;
}> = ({ rows, readOnly, onOpen }) => (
  <section aria-label="Test cases" style={cardStyle}>
    <div style={cardHeadStyle}>
      <h3 style={cardTitleStyle}>Test cases</h3>
      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{rows.length}</span>
    </div>
    {rows.length === 0 && (
      <div style={{ padding: 12, fontSize: 13, color: 'var(--text-muted)' }}>
        This project has no test cases yet. Write them on the Requirements page.
      </div>
    )}
    {rows.map((row, i) => (
      <button
        key={row.testCaseId}
        type="button"
        onClick={() => onOpen(i)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          width: '100%',
          minHeight: 56,
          padding: '8px 12px',
          border: 'none',
          borderTop: i ? '1px solid var(--border-soft)' : 'none',
          background: 'var(--surface)',
          color: 'var(--text)',
          textAlign: 'left',
          cursor: 'pointer',
        }}
      >
        <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 12, color: 'var(--text-muted)', minWidth: 0 }}>
            {row.ref && <span style={refStyle}>{row.ref}</span>}
            <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{verifiesText(row)}</span>
          </span>
          <span style={{ fontSize: 14, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{row.testCaseTitle}</span>
          <span style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap', fontSize: 12, color: 'var(--text-muted)' }}>
            <MethodChip method={row.executionMethod} />
            {row.resultId && (row.citations.length > 0 || row.executionMethod !== 'automated') && (
              <span style={{ color: row.citations.length ? 'var(--accent-text)' : 'var(--warning-text)', fontWeight: 600 }}>
                {evidenceLabel(row, readOnly)}
              </span>
            )}
          </span>
        </span>
        <Chip>{row.status}</Chip>
      </button>
    ))}
  </section>
);
