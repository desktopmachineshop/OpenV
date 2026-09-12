import React, { useState } from 'react';
import { EvidenceBundle, EvidenceCitation, evidenceAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { ErrorBanner, Modal } from './ui';

interface EvidencePickerProps {
  /** The recorded result the citations belong to. */
  resultId: string;
  testCaseTitle: string;
  /** The project's capture sessions, loaded once by the caller. */
  bundles: EvidenceBundle[];
  cited: EvidenceCitation[];
  onClose: () => void;
  /** Called after any change, so the caller can refresh its citation map. */
  onChanged: () => void | Promise<void>;
}

/**
 * Cite the capture a result rests on.
 *
 * A picker rather than an upload form, because the same capture is normally
 * cited by several results: the sweep was recorded once on the Evidence page,
 * and each condition's result points at it. Citing is a toggle here — the
 * bundle itself is never created or destroyed from inside a run.
 */
export const EvidencePicker: React.FC<EvidencePickerProps> = ({
  resultId,
  testCaseTitle,
  bundles,
  cited,
  onClose,
  onChanged,
}) => {
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const citedIds = new Set(cited.map((c) => c.bundle_id));

  const toggle = async (bundle: EvidenceBundle) => {
    setBusy(bundle.id);
    setError('');
    try {
      if (citedIds.has(bundle.id)) {
        await evidenceAPI.uncite(resultId, bundle.id);
      } else {
        await evidenceAPI.cite(resultId, bundle.id);
      }
      await onChanged();
    } catch (err: any) {
      setError(`Failed to update the citation: ${apiErrorMessage(err)}`);
    } finally {
      setBusy('');
    }
  };

  return (
    <Modal title={`Evidence for ${testCaseTitle}`} width={560} onClose={onClose}>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 12 }} />

      {bundles.length === 0 ? (
        <p style={{ color: 'var(--text-muted)' }}>
          This project has no evidence recorded yet. Capture sessions are added
          on the project's Evidence page, then cited here.
        </p>
      ) : (
        <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
          {bundles.map((bundle) => {
            const isCited = citedIds.has(bundle.id);
            return (
              <li
                key={bundle.id}
                style={{
                  display: 'flex',
                  gap: 10,
                  alignItems: 'center',
                  flexWrap: 'wrap',
                  padding: '10px 0',
                  borderBottom: '1px solid var(--border)',
                }}
              >
                <div style={{ flex: '1 1 240px', minWidth: 0 }}>
                  <div style={{ display: 'flex', gap: 8, alignItems: 'baseline', flexWrap: 'wrap' }}>
                    <code style={{ color: 'var(--text-muted)', fontSize: 12 }}>{bundle.ref}</code>
                    <span>{bundle.title}</span>
                  </div>
                  <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 2 }}>
                    {bundle.file_count === 0
                      ? 'Written account'
                      : `${bundle.file_count} file${bundle.file_count === 1 ? '' : 's'}`}
                    {bundle.captured_by ? ` · ${bundle.captured_by}` : ''}
                  </div>
                </div>
                <button
                  className={isCited ? 'button-secondary' : 'button'}
                  style={{ minHeight: 36 }}
                  disabled={busy === bundle.id}
                  onClick={() => toggle(bundle)}
                >
                  {busy === bundle.id ? '…' : isCited ? 'Remove' : 'Cite'}
                </button>
              </li>
            );
          })}
        </ul>
      )}

      <p style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 12 }}>
        Removing a citation says this result no longer rests on that capture.
        It does not delete the capture, which other results may still cite.
      </p>

      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 12 }}>
        <button className="button" onClick={onClose}>
          Done
        </button>
      </div>
    </Modal>
  );
};
