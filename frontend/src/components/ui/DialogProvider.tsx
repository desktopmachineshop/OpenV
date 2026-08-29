import React, { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import { ConfirmDialog } from './ConfirmDialog';
import { PromptDialog } from './PromptDialog';

export interface ConfirmOptions {
  title?: string;
  message: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

export interface AlertOptions {
  title?: string;
  message: React.ReactNode;
  confirmLabel?: string;
}

export interface PromptOptions {
  title?: string;
  message?: React.ReactNode;
  label?: string;
  defaultValue?: string;
  placeholder?: string;
  confirmLabel?: string;
  allowEmpty?: boolean;
}

type ConfirmFn = (options: ConfirmOptions | string) => Promise<boolean>;
type AlertFn = (options: AlertOptions | string) => Promise<void>;
/** Resolves the entered string, or null if the user cancelled. */
type PromptFn = (options: PromptOptions | string) => Promise<string | null>;

interface DialogContextValue {
  confirm: ConfirmFn;
  alert: AlertFn;
  prompt: PromptFn;
}

const DialogContext = createContext<DialogContextValue | null>(null);

type Pending =
  | { kind: 'confirm'; options: ConfirmOptions; resolve: (ok: boolean) => void }
  | { kind: 'alert'; options: AlertOptions; resolve: () => void }
  | { kind: 'prompt'; options: PromptOptions; resolve: (value: string | null) => void };

/**
 * App-wide dialog service replacing window.confirm / window.prompt / alert.
 *
 * Usage (async/await friendly):
 *   const confirm = useConfirm();
 *   if (!(await confirm({ message: 'Delete this?', danger: true }))) return;
 *
 *   const prompt = usePrompt();
 *   const name = await prompt({ title: 'New crew', label: 'Name' });
 *   if (name === null) return; // cancelled
 */
export const DialogProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [pending, setPending] = useState<Pending | null>(null);
  // Queue so overlapping requests (rare) are shown one after another.
  const queueRef = useRef<Pending[]>([]);
  // Mirror of `pending` so enqueue/finish can decide queue-vs-show without
  // side effects inside a state updater — React StrictMode double-invokes
  // updaters, which previously pushed each queued entry twice and made a
  // ghost dialog reopen after the queued one was dismissed.
  const pendingRef = useRef<Pending | null>(null);

  const enqueue = useCallback((entry: Pending) => {
    if (pendingRef.current) {
      queueRef.current.push(entry);
      return;
    }
    pendingRef.current = entry;
    setPending(entry);
  }, []);

  const finish = useCallback(() => {
    const next = queueRef.current.shift() || null;
    pendingRef.current = next;
    setPending(next);
  }, []);

  const confirm = useCallback<ConfirmFn>(
    (options) =>
      new Promise<boolean>((resolve) => {
        const opts = typeof options === 'string' ? { message: options } : options;
        enqueue({ kind: 'confirm', options: opts, resolve });
      }),
    [enqueue]
  );

  const alert = useCallback<AlertFn>(
    (options) =>
      new Promise<void>((resolve) => {
        const opts = typeof options === 'string' ? { message: options } : options;
        enqueue({ kind: 'alert', options: opts, resolve });
      }),
    [enqueue]
  );

  const prompt = useCallback<PromptFn>(
    (options) =>
      new Promise<string | null>((resolve) => {
        const opts = typeof options === 'string' ? { title: options } : options;
        enqueue({ kind: 'prompt', options: opts, resolve });
      }),
    [enqueue]
  );

  const value = useMemo<DialogContextValue>(
    () => ({ confirm, alert, prompt }),
    [confirm, alert, prompt]
  );

  return (
    <DialogContext.Provider value={value}>
      {children}
      {pending?.kind === 'confirm' && (
        <ConfirmDialog
          title={pending.options.title}
          message={pending.options.message}
          confirmLabel={pending.options.confirmLabel}
          cancelLabel={pending.options.cancelLabel}
          danger={pending.options.danger}
          onConfirm={() => {
            pending.resolve(true);
            finish();
          }}
          onCancel={() => {
            pending.resolve(false);
            finish();
          }}
        />
      )}
      {pending?.kind === 'alert' && (
        <ConfirmDialog
          title={pending.options.title}
          message={pending.options.message}
          confirmLabel={pending.options.confirmLabel || 'OK'}
          hideCancel
          onConfirm={() => {
            pending.resolve();
            finish();
          }}
          onCancel={() => {
            pending.resolve();
            finish();
          }}
        />
      )}
      {pending?.kind === 'prompt' && (
        <PromptDialog
          title={pending.options.title}
          message={pending.options.message}
          label={pending.options.label}
          defaultValue={pending.options.defaultValue}
          placeholder={pending.options.placeholder}
          confirmLabel={pending.options.confirmLabel}
          allowEmpty={pending.options.allowEmpty}
          onSubmit={(v) => {
            pending.resolve(v);
            finish();
          }}
          onCancel={() => {
            pending.resolve(null);
            finish();
          }}
        />
      )}
    </DialogContext.Provider>
  );
};

const useDialogContext = (): DialogContextValue => {
  const ctx = useContext(DialogContext);
  if (!ctx) {
    throw new Error('Dialog hooks must be used within a <DialogProvider>');
  }
  return ctx;
};

/** window.confirm replacement — resolves true when the user confirms. */
export const useConfirm = (): ConfirmFn => useDialogContext().confirm;

/** alert() replacement — resolves when the user dismisses the dialog. */
export const useAlert = (): AlertFn => useDialogContext().alert;

/** window.prompt replacement — resolves the value, or null on cancel. */
export const usePrompt = (): PromptFn => useDialogContext().prompt;
