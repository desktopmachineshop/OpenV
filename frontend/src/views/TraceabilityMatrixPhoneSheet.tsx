import React, { useEffect, useRef } from 'react';
import { Link } from 'react-router-dom';
import type { Artifact } from '../api/client';
import { Chip, buttonStyle } from '../components/ui';
import type { CoverageRow } from './TraceabilityMatrixCoverage';

// One requirement's row of the phone matrix, opened from the bottom: its
// statement, what is linked in each column by name, each test with its
// latest result, and what needs attention.

interface Props {
  coverage: CoverageRow;
  projectId: string;
  artifactMap: Record<string, Artifact>;
  onClose: () => void;
}

const GROUPS = [
  { column: 'need', label: 'User needs' },
  { column: 'design', label: 'Design outputs' },
  { column: 'test', label: 'Test cases' },
  { column: 'hazard', label: 'Hazards' },
] as const;

export const TraceabilityMatrixPhoneSheet: React.FC<Props> = ({ coverage, projectId, artifactMap, onClose }) => {
  const closeRef = useRef<HTMLButtonElement>(null);
  const { row, cells, issues } = coverage;
  const req = artifactMap[row.requirement_id];

  useEffect(() => {
    closeRef.current?.focus();
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  return (
    <div style={{ position: 'fixed', inset: 0, zIndex: 950 }}>
      <div aria-hidden="true" onClick={onClose} style={{ position: 'absolute', inset: 0, background: 'var(--overlay)' }} />
      <section
        role="dialog"
        aria-modal="true"
        aria-label={`${req?.ref ? req.ref + ' ' : ''}${row.title}`}
        className="safe-area-bottom"
        style={{
          position: 'absolute',
          left: 0,
          right: 0,
          bottom: 0,
          maxHeight: '85vh',
          display: 'flex',
          flexDirection: 'column',
          background: 'var(--surface)',
          borderRadius: '10px 10px 0 0',
          boxShadow: '0 -8px 32px rgba(0, 0, 0, 0.18)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8, padding: '14px 8px 12px 16px', borderBottom: '1px solid var(--border)' }}>
          <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
            {req?.ref && (
              <span style={{ fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace', fontSize: 12, color: 'var(--accent-text)' }}>{req.ref}</span>
            )}
            <h3 style={{ margin: 0, fontSize: 16, color: 'var(--text)' }}>{row.title}</h3>
            {req?.body && (
              <p style={{ margin: 0, fontSize: 13, lineHeight: '18px', color: 'var(--text-secondary)', display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
                {req.body}
              </p>
            )}
          </div>
          <button
            ref={closeRef}
            type="button"
            aria-label="Close"
            onClick={onClose}
            style={{ width: 44, height: 44, flexShrink: 0, border: 'none', background: 'none', color: 'var(--text-muted)', fontSize: 22, cursor: 'pointer', padding: 0 }}
          >
            ×
          </button>
        </div>

        <div style={{ overflowY: 'auto', padding: '0 16px' }}>
          {issues.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, padding: '12px 0', borderBottom: '1px solid var(--border)' }}>
              {issues.map((i) => (
                <Chip key={i.text} tone={i.tone}>{i.text}</Chip>
              ))}
            </div>
          )}
          {GROUPS.map((g) => {
            const cell = cells.find((c) => c.column === g.column)!;
            return (
              <div key={g.column} style={{ padding: '10px 0', borderBottom: '1px solid var(--border)' }}>
                <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 2 }}>{g.label}</div>
                {cell.ids.length === 0 && <div style={{ fontSize: 13, color: 'var(--text-muted)', padding: '6px 0' }}>None linked</div>}
                {cell.ids.map((id) => {
                  const a = artifactMap[id];
                  const result = g.column === 'test' ? row.latest_results?.[id] || 'not-run' : '';
                  return (
                    <Link
                      key={id}
                      to={`/projects/${projectId}/requirements?artifact=${id}`}
                      style={{ minHeight: 44, display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none', color: 'var(--text)' }}
                    >
                      {a?.ref && (
                        <span style={{ fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace', fontSize: 12, color: 'var(--accent-text)', flexShrink: 0 }}>{a.ref}</span>
                      )}
                      <span style={{ flex: 1, minWidth: 0, fontSize: 14 }}>{a?.title || id}</span>
                      {result && <Chip>{result}</Chip>}
                    </Link>
                  );
                })}
              </div>
            );
          })}
        </div>

        <div style={{ display: 'flex', gap: 8, padding: '12px 16px' }}>
          <Link
            to={`/projects/${projectId}/impact?artifact=${row.requirement_id}`}
            style={{ ...buttonStyle('secondary'), flex: 1, minHeight: 44, display: 'flex', alignItems: 'center', justifyContent: 'center', textDecoration: 'none' }}
          >
            Show impact
          </Link>
          <Link
            to={`/projects/${projectId}/requirements?artifact=${row.requirement_id}`}
            style={{ ...buttonStyle('primary'), flex: 1, minHeight: 44, display: 'flex', alignItems: 'center', justifyContent: 'center', textDecoration: 'none' }}
          >
            Open requirement
          </Link>
        </div>
      </section>
    </div>
  );
};
