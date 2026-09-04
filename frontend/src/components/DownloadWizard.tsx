import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { DownloadFormat, DownloadOptions, projectAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Modal } from './ui';
import {
  DOWNLOAD_FORMATS,
  FormSelection,
  attachmentLabel,
  describeSelection,
  formatBytes,
  isArchive,
  selectAll,
  selectsNothing,
  toWire,
  toggle,
} from '../utils/downloadSelection';

interface DownloadWizardProps {
  projectId: string;
  /** Which snapshot to take: a baseline id, or 'live'/undefined for the project as it stands. */
  baselineId?: string;
  onClose: () => void;
}

// Taking a project away is one button and one wizard: choose the shape, then
// choose the content. The two steps are the two questions a person actually
// has — "what do I need this as" and "how much of it do I need" — and they are
// separate because the second is worth reading and the first is not.
//
// Everything the form offers comes from the project: its sections, the artifact
// types it holds, the attachment categories actually attached. A filter that
// would return nothing is never offered.
export const DownloadWizard: React.FC<DownloadWizardProps> = ({ projectId, baselineId, onClose }) => {
  const [step, setStep] = useState<'format' | 'content'>('format');
  const [format, setFormat] = useState<DownloadFormat>('pdf');
  const [options, setOptions] = useState<DownloadOptions | null>(null);
  const [selection, setSelection] = useState<FormSelection>(selectAll(null));
  const [loading, setLoading] = useState(true);
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    projectAPI
      .downloadOptions(projectId, baselineId)
      .then((res) => {
        if (cancelled) return;
        setOptions(res.data);
        // Everything starts ticked: the wizard opens on the download the old
        // buttons produced, and narrowing is a deliberate act.
        setSelection(selectAll(res.data));
      })
      .catch((err) => {
        if (!cancelled) setError(apiErrorMessage(err, 'Could not read what this project holds'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, baselineId]);

  const empty = selectsNothing(selection, options);
  const summary = useMemo(() => describeSelection(selection, options), [selection, options]);

  const handleDownload = useCallback(async () => {
    setDownloading(true);
    setError('');
    try {
      await projectAPI.download(projectId, format, toWire(selection, options), baselineId);
      onClose();
    } catch (err: any) {
      setError(apiErrorMessage(err, 'The download could not be built'));
    } finally {
      setDownloading(false);
    }
  }, [projectId, format, selection, options, baselineId, onClose]);

  const checkbox = (checked: boolean, label: React.ReactNode, onChange: () => void, hint?: string) => (
    <label
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 8,
        padding: '6px 0',
        cursor: 'pointer',
        fontSize: 13,
        color: 'var(--text)',
      }}
    >
      {/* The app's global stylesheet stretches every input to full width; a
          box and a radio have to opt out of that or the label lands elsewhere. */}
      <input
        type="checkbox"
        checked={checked}
        onChange={onChange}
        style={{ ...tickStyle, marginTop: 2 }}
      />
      <span>
        {label}
        {hint && (
          <span style={{ color: 'var(--text-muted)', fontSize: 12, marginLeft: 6 }}>{hint}</span>
        )}
      </span>
    </label>
  );

  return (
    <Modal title="Download this project" width={560} onClose={onClose}>
      {error && (
        <div
          style={{
            background: 'var(--danger-soft)',
            color: 'var(--danger)',
            padding: '8px 10px',
            borderRadius: 4,
            fontSize: 13,
            marginBottom: 12,
          }}
        >
          {error}
        </div>
      )}

      <div style={{ display: 'flex', gap: 6, marginBottom: 14, fontSize: 12 }}>
        {(['format', 'content'] as const).map((s, i) => (
          <button
            key={s}
            onClick={() => setStep(s)}
            style={{
              flex: 1,
              padding: '6px 8px',
              borderRadius: 4,
              border: '1px solid var(--border)',
              background: step === s ? 'var(--accent)' : 'transparent',
              color: step === s ? 'var(--accent-fg)' : 'var(--text-muted)',
              cursor: 'pointer',
            }}
          >
            {i + 1}. {s === 'format' ? 'Format' : 'Content'}
          </button>
        ))}
      </div>

      {step === 'format' && (
        <div>
          {DOWNLOAD_FORMATS.map((choice) => (
            <label
              key={choice.format}
              style={{
                display: 'block',
                border: `1px solid ${format === choice.format ? 'var(--accent)' : 'var(--border)'}`,
                borderRadius: 6,
                padding: '10px 12px',
                marginBottom: 8,
                cursor: 'pointer',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <input
                  type="radio"
                  name="download-format"
                  checked={format === choice.format}
                  onChange={() => setFormat(choice.format)}
                  style={tickStyle}
                />
                <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--text)' }}>{choice.label}</span>
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 24 }}>
                {choice.description}
              </div>
            </label>
          ))}
        </div>
      )}

      {step === 'content' && (
        <div>
          {loading && <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>Reading the project…</div>}

          {!loading && options && (
            <>
              {options.sections.length > 0 && (
                <section style={{ marginBottom: 14 }}>
                  <SectionHeading
                    title="Sections"
                    action={
                      selection.sections.length === options.sections.length ? 'Clear all' : 'Select all'
                    }
                    onAction={() =>
                      setSelection((s) => ({
                        ...s,
                        sections:
                          s.sections.length === options.sections.length
                            ? []
                            : options.sections.map((x) => x.id),
                      }))
                    }
                  />
                  {options.sections.map((section) =>
                    checkbox(
                      selection.sections.includes(section.id),
                      <>
                        {section.number ? `${section.number}. ` : ''}
                        {section.title}
                      </>,
                      () => setSelection((s) => ({ ...s, sections: toggle(s.sections, section.id) })),
                      `${section.artifacts} artifact${section.artifacts === 1 ? '' : 's'}`
                    )
                  )}
                </section>
              )}

              {options.types.length > 0 && (
                <section style={{ marginBottom: 14 }}>
                  <SectionHeading
                    title="Artifact types"
                    action={selection.types.length === options.types.length ? 'Clear all' : 'Select all'}
                    onAction={() =>
                      setSelection((s) => ({
                        ...s,
                        types:
                          s.types.length === options.types.length ? [] : options.types.map((t) => t.type),
                      }))
                    }
                  />
                  {options.types.map((type) =>
                    checkbox(
                      selection.types.includes(type.type),
                      type.type,
                      () => setSelection((s) => ({ ...s, types: toggle(s.types, type.type) })),
                      `${type.count}`
                    )
                  )}
                  {checkbox(
                    selection.includeHeadings,
                    'Headings',
                    () => setSelection((s) => ({ ...s, includeHeadings: !s.includeHeadings })),
                    'the sections that organise the document'
                  )}
                </section>
              )}

              <section>
                <SectionHeading title="Attachments" />
                {options.attachments.length === 0 && (
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    Nothing is attached to this project yet.
                  </div>
                )}
                {options.attachments.map((cat) =>
                  checkbox(
                    selection.attachments.includes(cat.category),
                    attachmentLabel(cat.category),
                    () =>
                      setSelection((s) => ({ ...s, attachments: toggle(s.attachments, cat.category) })),
                    `${cat.count} file${cat.count === 1 ? '' : 's'}, ${formatBytes(cat.bytes)}`
                  )
                )}
                {isArchive(selection) && (
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
                    The files travel with the document, so this download is a .zip.
                  </div>
                )}
              </section>
            </>
          )}
        </div>
      )}

      <div
        style={{
          borderTop: '1px solid var(--border-soft)',
          marginTop: 16,
          paddingTop: 12,
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        <div style={{ flex: 1, fontSize: 12, color: empty ? 'var(--danger)' : 'var(--text-muted)' }}>
          {empty ? 'Nothing is selected — this download would be empty.' : summary}
        </div>
        {step === 'format' ? (
          <button
            onClick={() => setStep('content')}
            style={primaryButton}
          >
            Next
          </button>
        ) : (
          <button onClick={() => setStep('format')} style={secondaryButton}>
            Back
          </button>
        )}
        <button
          onClick={handleDownload}
          disabled={downloading || empty || loading}
          style={{
            ...primaryButton,
            background: downloading || empty || loading ? 'var(--neutral-mid)' : 'var(--success-bright)',
            cursor: downloading || empty || loading ? 'not-allowed' : 'pointer',
          }}
        >
          {downloading ? 'Preparing…' : 'Download'}
        </button>
      </div>
    </Modal>
  );
};

// A checkbox or radio keeps its own size: index.css gives every input
// width:100%, which would push the label it belongs to across the dialog.
const tickStyle: React.CSSProperties = {
  width: 16,
  height: 16,
  minWidth: 16,
  flexShrink: 0,
  padding: 0,
  margin: 0,
};

const primaryButton: React.CSSProperties = {
  padding: '7px 14px',
  borderRadius: 4,
  border: 'none',
  background: 'var(--accent)',
  color: 'var(--accent-fg)',
  fontSize: 13,
  cursor: 'pointer',
};

const secondaryButton: React.CSSProperties = {
  ...primaryButton,
  background: 'transparent',
  border: '1px solid var(--border)',
  color: 'var(--text-muted)',
};

const SectionHeading: React.FC<{ title: string; action?: string; onAction?: () => void }> = ({
  title,
  action,
  onAction,
}) => (
  <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 2 }}>
    <div
      style={{
        fontSize: 11,
        fontWeight: 700,
        letterSpacing: 1,
        textTransform: 'uppercase',
        color: 'var(--text-muted)',
      }}
    >
      {title}
    </div>
    {action && onAction && (
      <button
        onClick={onAction}
        style={{
          background: 'none',
          border: 'none',
          color: 'var(--accent)',
          fontSize: 11,
          cursor: 'pointer',
          padding: 0,
        }}
      >
        {action}
      </button>
    )}
  </div>
);
