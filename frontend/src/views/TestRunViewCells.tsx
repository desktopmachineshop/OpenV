// The rows of a test run and how their cells read: a test case's method as
// a tag (manual and physical in the agent purple, since agents may not run
// them), its result as a chip-shaped picker, and its evidence.
import React from 'react';
import type { EvidenceCitation, ExecutionMethod } from '../api/client';
import { Chip, chipStyle, toneFor } from '../components/ui';
import type { ChipTone } from '../components/ui';

export const STATUS_OPTIONS = ['pass', 'fail', 'blocked', 'not-run'] as const;

export const STATUS_LABEL: Record<string, string> = {
  pass: 'Pass',
  fail: 'Fail',
  blocked: 'Blocked',
  'not-run': 'Not run',
};

export interface ResultRow {
  testCaseId: string;
  ref: string;
  testCaseTitle: string;
  /** The test case's text: its procedure. */
  body: string;
  executionMethod: ExecutionMethod;
  /** Refs (or titles) of the requirements the test case verifies. */
  verifies: string[];
  versionTested: number | null;
  status: string;
  notes: string;
  executedAt: string | null;
  byAgent: boolean;
  /** Null until an outcome has been recorded — there is nothing to cite
   *  evidence against until then. */
  resultId: string | null;
  citations: EvidenceCitation[];
}

const executionLabel: Record<ExecutionMethod, string> = {
  automated: 'automated',
  manual: 'manual (human)',
  physical: 'physical test',
};

export const methodTone = (method: ExecutionMethod): ChipTone => (method === 'automated' ? 'neutral' : 'agent');

export const MethodChip: React.FC<{ method: ExecutionMethod }> = ({ method }) => (
  <Chip
    tone={methodTone(method)}
    title={
      method === 'automated'
        ? 'An agent may execute this test case.'
        : 'Reserved for a person — agents cannot run it or record its result.'
    }
  >
    {executionLabel[method]}
  </Chip>
);

/** A result's outcome as a picker drawn like its chip. */
export const StatusSelect: React.FC<{
  value: string;
  disabled: boolean;
  onChange: (status: string) => void;
  label?: string;
}> = ({ value, disabled, onChange, label }) => (
  <select
    value={value}
    disabled={disabled}
    aria-label={label || 'Result'}
    onChange={(e) => onChange(e.target.value)}
    style={{
      ...chipStyle(toneFor(value), 'md'),
      height: 28,
      width: 'auto',
      paddingRight: 6,
      cursor: disabled ? 'default' : 'pointer',
    }}
  >
    {STATUS_OPTIONS.map((s) => (
      <option key={s} value={s} style={{ background: 'var(--surface)', color: 'var(--text)' }}>
        {s}
      </option>
    ))}
  </select>
);

/** What the evidence cell or button reads for a row. */
export const evidenceLabel = (row: ResultRow, readOnly: boolean): string => {
  const cited = row.citations || [];
  if (cited.length === 1) return cited[0].bundle_ref || 'cited';
  if (cited.length > 1) return `${cited[0].bundle_ref} +${cited.length - 1}`;
  if (readOnly) return '—';
  return row.executionMethod !== 'automated' ? '+ evidence needed' : '+ evidence';
};

export const evidenceTitle = (row: ResultRow): string =>
  row.citations.length
    ? row.citations.map((c) => `${c.bundle_ref} ${c.bundle_title}`).join(', ')
    : 'Cite the capture this result rests on';

// Evidence lives in its own column because a physical result is only as
// good as what backs it. A case that needs a rig or a person shows the
// prompt even when nothing is cited yet; an automated one does not nag.
export const EvidenceCell: React.FC<{ row: ResultRow; readOnly: boolean; onCite: (row: ResultRow) => void }> = ({
  row,
  readOnly,
  onCite,
}) => {
  const needsEvidence = row.executionMethod !== 'automated';
  if (!row.resultId) {
    return (
      <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
        {needsEvidence ? 'record a result first' : '—'}
      </span>
    );
  }
  const cited = row.citations.length > 0;
  return (
    <button
      onClick={() => onCite(row)}
      data-help-key="evidence"
      disabled={readOnly}
      title={evidenceTitle(row)}
      style={{
        background: 'none',
        border: 'none',
        padding: '4px 6px',
        minHeight: 32,
        minWidth: 36,
        cursor: readOnly ? 'default' : 'pointer',
        color: cited ? 'var(--accent-text)' : needsEvidence ? 'var(--warning-text)' : 'var(--text-muted)',
        fontFamily: cited ? 'source-code-pro, Menlo, Monaco, Consolas, monospace' : undefined,
        fontWeight: cited || needsEvidence ? 600 : undefined,
        fontSize: 12,
        textAlign: 'left',
      }}
    >
      {evidenceLabel(row, readOnly)}
    </button>
  );
};

export const AgentMark: React.FC = () => (
  <span title="Recorded by an agent run" style={{ marginLeft: 6 }}>
    <Chip tone="agent">agent</Chip>
  </span>
);

/** "TC-3 · verifies REQ-2, REQ-4" */
export const verifiesText = (row: ResultRow): string =>
  row.verifies.length ? `verifies ${row.verifies.join(', ')}` : 'verifies nothing yet';
