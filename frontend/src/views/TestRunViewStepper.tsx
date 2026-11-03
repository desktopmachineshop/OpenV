// Recording a run on a phone, one test case at a time: the case's
// procedure, the notes and measured values, the evidence it rests on, four
// large outcome buttons, and a bar to record it and move to the next. It
// covers the screen, the way a person at a rig holds the phone.
import React, { useEffect, useState } from 'react';
import type { TestRun } from '../api/client';
import { Chip, toneColors, toneFor } from '../components/ui';
import { FILL, StackedBar, countResults, refStyle, resultSegments } from './VVDashboardParts';
import { MethodChip, ResultRow, STATUS_LABEL, STATUS_OPTIONS, evidenceLabel, evidenceTitle, verifiesText } from './TestRunViewCells';

interface Props {
  run: TestRun;
  rows: ResultRow[];
  index: number;
  onIndex: (index: number) => void;
  readOnly: boolean;
  onClose: () => void;
  /** Saves an outcome; resolves to the result's id, or null when it failed. */
  onRecord: (row: ResultRow, status: string, notes: string) => Promise<string | null>;
  /** Opens the evidence picker for a row's result. */
  onCite: (row: ResultRow, resultId: string) => void;
}

const label: React.CSSProperties = {
  fontSize: 12,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  color: 'var(--text-muted)',
};

export const TestRunStepper: React.FC<Props> = ({ run, rows, index, onIndex, readOnly, onClose, onRecord, onCite }) => {
  const row = rows[index];
  const [status, setStatus] = useState(row?.status || 'not-run');
  const [notes, setNotes] = useState(row?.notes || '');
  const [saving, setSaving] = useState(false);

  // A new case, or the saved result of this one changing, resets the draft.
  useEffect(() => {
    setStatus(row?.status || 'not-run');
    setNotes(row?.notes || '');
  }, [row?.testCaseId, row?.status, row?.notes]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  if (!row) return null;
  const counts = countResults(rows);
  const last = index === rows.length - 1;
  const dirty = status !== row.status || notes !== row.notes;

  /** Saves the draft when it differs from what is recorded; resolves to the result id. */
  const save = async (): Promise<string | null> => {
    if (!dirty && row.resultId) return row.resultId;
    setSaving(true);
    try {
      return await onRecord(row, status, notes);
    } finally {
      setSaving(false);
    }
  };

  const next = async () => {
    // An untouched case is skipped, not recorded as not-run.
    if (!readOnly && dirty && !(await save())) return;
    if (last) onClose();
    else onIndex(index + 1);
  };

  const cite = async () => {
    if (readOnly) return;
    const id = await save();
    if (id) onCite(row, id);
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Record ${row.testCaseTitle}`}
      data-help-key="vv.runs"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1500,
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--bg-app)',
        color: 'var(--text)',
      }}
    >
      <header style={{ flex: 'none', background: 'var(--sidebar-bg)', color: '#ecf0f1', padding: '4px 4px 12px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, minHeight: 48 }}>
          <button
            type="button"
            aria-label="Leave recording"
            onClick={onClose}
            style={{ width: 44, height: 44, border: 'none', background: 'transparent', color: '#ecf0f1', fontSize: 22, cursor: 'pointer' }}
          >
            ×
          </button>
          <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: '#bdc3c7' }}>
              {readOnly ? `Test run · ${run.status}` : 'Test run'}
            </span>
            <span style={{ fontSize: 16, fontWeight: 700, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {run.name}
            </span>
          </div>
          <span style={{ fontSize: 13, fontWeight: 600, color: '#bdc3c7', paddingRight: 12 }}>
            {index + 1} / {rows.length}
          </span>
        </div>
        <div style={{ margin: '0 12px' }}>
          <StackedBar segments={resultSegments(counts)} total={rows.length} height={4} radius={999} track="#34495e" />
        </div>
      </header>

      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '14px 16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', fontSize: 12, color: 'var(--text-muted)' }}>
            {row.ref && <span style={{ ...refStyle, fontWeight: 600 }}>{row.ref}</span>}
            <span>· {verifiesText(row)}</span>
          </span>
          <h1 style={{ margin: 0, fontSize: 20, fontWeight: 700, lineHeight: 1.25 }}>{row.testCaseTitle}</h1>
        </div>

        {rows.length <= 24 && (
          <ol aria-label="Test cases" style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', gap: 3 }}>
            {rows.map((r, i) => (
              <li
                key={r.testCaseId}
                aria-current={i === index ? 'step' : undefined}
                style={{
                  flex: 1,
                  height: 4,
                  borderRadius: 2,
                  background: i === index ? FILL.accent : r.status === 'not-run' ? 'var(--border)' : toneColors(toneFor(r.status)).fg,
                }}
              />
            ))}
          </ol>
        )}

        <section
          style={{
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 12,
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
          }}
        >
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, justifyContent: 'space-between' }}>
            <span style={label}>
              Test case {index + 1} of {rows.length}
            </span>
            <MethodChip method={row.executionMethod} />
          </span>
          <p style={{ margin: 0, fontSize: 15, whiteSpace: 'pre-wrap' }}>
            {row.body || <span style={{ color: 'var(--text-muted)' }}>This test case has no procedure written.</span>}
          </p>
          {row.versionTested != null && (
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Last recorded against v{row.versionTested}</span>
          )}
          <button
            type="button"
            onClick={cite}
            disabled={saving || readOnly || (!row.resultId && !dirty)}
            title={evidenceTitle(row)}
            data-help-key="evidence"
            style={{
              minHeight: 44,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 8,
              border: '1px dashed var(--neutral-mid)',
              borderRadius: 6,
              background: 'var(--surface-alt)',
              color: 'var(--text-secondary)',
              fontSize: 14,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            <span aria-hidden="true">📎</span>
            {row.citations.length ? `Evidence · ${evidenceLabel(row, readOnly)}` : readOnly ? 'No evidence cited' : 'Attach evidence'}
          </button>
        </section>

        <fieldset style={{ margin: 0, padding: 0, border: 'none', display: 'flex', flexDirection: 'column', gap: 8 }}>
          <legend style={{ ...label, padding: '0 0 8px' }}>Result</legend>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
            {STATUS_OPTIONS.map((s) => {
              const t = toneColors(toneFor(s));
              const on = status === s;
              return (
                <button
                  key={s}
                  type="button"
                  aria-pressed={on}
                  disabled={readOnly || saving}
                  onClick={() => setStatus(s)}
                  style={{
                    height: 52,
                    borderRadius: 8,
                    border: on ? `2px solid ${t.fg}` : `1px solid ${t.border}`,
                    background: on ? t.fg : s === 'not-run' ? 'var(--surface)' : t.bg,
                    color: on ? '#fff' : t.fg,
                    fontSize: 16,
                    fontWeight: 700,
                    cursor: readOnly ? 'default' : 'pointer',
                    opacity: readOnly && !on ? 0.6 : 1,
                  }}
                >
                  {STATUS_LABEL[s]}
                </button>
              );
            })}
          </div>
        </fieldset>

        <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, fontWeight: 500 }}>
          Notes and measured values
          <textarea
            rows={3}
            value={notes}
            readOnly={readOnly}
            onChange={(e) => setNotes(e.target.value)}
            placeholder={readOnly ? '' : 'What you measured, and anything worth knowing next time'}
            style={{ boxSizing: 'border-box', padding: '10px 12px', fontSize: 16, resize: 'vertical' }}
          />
        </label>
        {row.executedAt && (
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            Recorded {new Date(row.executedAt).toLocaleString()} <Chip>{row.status}</Chip>
            {row.byAgent && ' · by an agent run'}
          </span>
        )}
      </div>

      <div
        style={{
          flex: 'none',
          display: 'flex',
          gap: 8,
          padding: '10px 16px 18px',
          background: 'var(--surface)',
          borderTop: '1px solid var(--border)',
        }}
      >
        <button
          type="button"
          aria-label="Previous test case"
          disabled={index === 0 || saving}
          onClick={() => onIndex(index - 1)}
          style={{
            width: 52,
            height: 44,
            border: '1px solid var(--border)',
            borderRadius: 8,
            background: 'var(--surface)',
            color: 'var(--text-secondary)',
            fontSize: 20,
            cursor: index === 0 ? 'default' : 'pointer',
          }}
        >
          ‹
        </button>
        <button
          type="button"
          onClick={next}
          disabled={saving}
          style={{
            flex: 1,
            height: 44,
            border: 'none',
            borderRadius: 8,
            background: 'var(--accent-strong)',
            color: '#fff',
            fontSize: 15,
            fontWeight: 700,
            cursor: 'pointer',
          }}
        >
          {saving ? 'Saving…' : readOnly ? (last ? 'Done' : 'Next test case') : last ? 'Record and finish' : 'Record and next'}
        </button>
      </div>
    </div>
  );
};
