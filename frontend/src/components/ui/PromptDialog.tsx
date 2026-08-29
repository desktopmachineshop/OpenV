import React, { useEffect, useRef, useState } from 'react';

export interface PromptDialogProps {
  title?: string;
  message?: React.ReactNode;
  /** Label above the input. */
  label?: string;
  defaultValue?: string;
  placeholder?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Allow submitting an empty value (default: OK is disabled while empty). */
  allowEmpty?: boolean;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}

/**
 * Token-styled replacement for window.prompt. Render it conditionally —
 * mounting the component opens it. Enter submits, Escape cancels, focus
 * starts in the input with its content selected.
 */
export const PromptDialog: React.FC<PromptDialogProps> = ({
  title,
  message,
  label,
  defaultValue = '',
  placeholder,
  confirmLabel = 'OK',
  cancelLabel = 'Cancel',
  allowEmpty = false,
  onSubmit,
  onCancel,
}) => {
  const [value, setValue] = useState(defaultValue);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const canSubmit = allowEmpty || !!value.trim();

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!canSubmit) return;
    onSubmit(value);
  };

  return (
    <div
      onClick={onCancel}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          onCancel();
        }
      }}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--overlay)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 3000,
      }}
    >
      <form
        role="dialog"
        aria-modal="true"
        className="card"
        onClick={(e) => e.stopPropagation()}
        onSubmit={submit}
        style={{
          width: 440,
          maxWidth: 'calc(100vw - 40px)',
          background: 'var(--surface)',
          borderRadius: 8,
          padding: 22,
          margin: 0,
        }}
      >
        {title && <h3 style={{ marginTop: 0, marginBottom: 10, color: 'var(--text)', fontSize: 16 }}>{title}</h3>}
        {message && (
          <div style={{ fontSize: 13.5, color: 'var(--text-body)', lineHeight: 1.5, marginBottom: 12 }}>
            {message}
          </div>
        )}
        <div className="form-group" style={{ marginBottom: 18 }}>
          {label && <label style={{ fontSize: 12.5 }}>{label}</label>}
          <input
            ref={inputRef}
            value={value}
            placeholder={placeholder}
            onChange={(e) => setValue(e.target.value)}
          />
        </div>
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
          <button
            type="button"
            className="button-secondary"
            style={{ width: 'auto', padding: '8px 16px', fontSize: 13 }}
            onClick={onCancel}
          >
            {cancelLabel}
          </button>
          <button
            type="submit"
            className="button"
            style={{ width: 'auto', padding: '8px 16px', fontSize: 13 }}
            disabled={!canSubmit}
          >
            {confirmLabel}
          </button>
        </div>
      </form>
    </div>
  );
};
