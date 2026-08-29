import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ConfirmDialog } from './ConfirmDialog';
import { PromptDialog } from './PromptDialog';
import { DialogProvider, useConfirm, ConfirmOptions } from './DialogProvider';

// React 18 requires this flag when driving createRoot through act().
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const render = (el: React.ReactElement) => {
  act(() => {
    root.render(el);
  });
};

const click = (el: Element) => {
  act(() => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
};

const keyDown = (el: Element, key: string) => {
  act(() => {
    el.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));
  });
};

const buttonByText = (text: string): HTMLButtonElement => {
  const btn = Array.from(container.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === text
  );
  if (!btn) throw new Error(`Button "${text}" not found`);
  return btn;
};

describe('ConfirmDialog', () => {
  it('renders the title and message and resolves confirm', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();
    render(
      <ConfirmDialog
        title="Delete thing"
        message="Really delete?"
        confirmLabel="Delete"
        danger
        onConfirm={onConfirm}
        onCancel={onCancel}
      />
    );
    expect(container.textContent).toContain('Delete thing');
    expect(container.textContent).toContain('Really delete?');
    click(buttonByText('Delete'));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('cancels via the Cancel button and via Escape', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();
    render(<ConfirmDialog message="Sure?" onConfirm={onConfirm} onCancel={onCancel} />);
    click(buttonByText('Cancel'));
    expect(onCancel).toHaveBeenCalledTimes(1);

    keyDown(buttonByText('Confirm'), 'Escape');
    expect(onCancel).toHaveBeenCalledTimes(2);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('confirms on Enter and focuses the confirm button on mount', () => {
    const onConfirm = jest.fn();
    render(<ConfirmDialog message="Go?" onConfirm={onConfirm} onCancel={() => undefined} />);
    const confirmBtn = buttonByText('Confirm');
    expect(document.activeElement).toBe(confirmBtn);
    keyDown(confirmBtn, 'Enter');
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('does not confirm when Enter is pressed with the Cancel button focused', () => {
    // Regression (#142): the overlay used to intercept Enter and call
    // onConfirm unconditionally, so a keyboard user who tabbed to Cancel and
    // pressed Enter fired the destructive action.
    const onConfirm = jest.fn();
    const onCancel = jest.fn();
    render(
      <ConfirmDialog
        message="Really delete?"
        confirmLabel="Delete"
        danger
        onConfirm={onConfirm}
        onCancel={onCancel}
      />
    );
    const cancelBtn = buttonByText('Cancel');
    act(() => {
      cancelBtn.focus();
    });
    keyDown(cancelBtn, 'Enter');
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('hides the cancel button in alert mode', () => {
    render(
      <ConfirmDialog
        message="Heads up"
        hideCancel
        onConfirm={() => undefined}
        onCancel={() => undefined}
      />
    );
    expect(container.querySelectorAll('button')).toHaveLength(1);
    expect(buttonByText('OK')).toBeTruthy();
  });
});

describe('PromptDialog', () => {
  it('submits the default value and disables OK when empty', () => {
    const onSubmit = jest.fn();
    render(
      <PromptDialog
        title="Name it"
        label="Name"
        defaultValue="hello"
        onSubmit={onSubmit}
        onCancel={() => undefined}
      />
    );
    expect(container.textContent).toContain('Name it');
    const ok = buttonByText('OK');
    expect(ok.disabled).toBe(false);
    click(ok);
    expect(onSubmit).toHaveBeenCalledWith('hello');
  });

  it('disables OK while the input is empty unless allowEmpty', () => {
    render(<PromptDialog onSubmit={() => undefined} onCancel={() => undefined} />);
    expect(buttonByText('OK').disabled).toBe(true);
  });

  it('allows empty submits with allowEmpty', () => {
    const onSubmit = jest.fn();
    render(<PromptDialog allowEmpty onSubmit={onSubmit} onCancel={() => undefined} />);
    const ok = buttonByText('OK');
    expect(ok.disabled).toBe(false);
    click(ok);
    expect(onSubmit).toHaveBeenCalledWith('');
  });

  it('cancels via Escape', () => {
    const onCancel = jest.fn();
    render(<PromptDialog onSubmit={() => undefined} onCancel={onCancel} />);
    const input = container.querySelector('input');
    expect(input).toBeTruthy();
    expect(document.activeElement).toBe(input);
    keyDown(input as Element, 'Escape');
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe('DialogProvider', () => {
  // Captures the confirm() function from inside the provider tree.
  let confirmFn: ((options: ConfirmOptions | string) => Promise<boolean>) | null = null;

  const CaptureConfirm: React.FC = () => {
    confirmFn = useConfirm();
    return null;
  };

  const renderProvider = () => {
    confirmFn = null;
    // StrictMode is essential here: it double-invokes state updaters, which
    // is exactly what made the old enqueue-inside-updater push queued
    // dialogs twice (#142 ghost dialog).
    render(
      <React.StrictMode>
        <DialogProvider>
          <CaptureConfirm />
        </DialogProvider>
      </React.StrictMode>
    );
  };

  it('resolves false when Enter is pressed with Cancel focused (regression #142)', async () => {
    renderProvider();
    let result: Promise<boolean>;
    act(() => {
      result = confirmFn!({ message: 'Delete it?', confirmLabel: 'Delete', danger: true });
    });
    const cancelBtn = buttonByText('Cancel');
    act(() => {
      cancelBtn.focus();
    });
    keyDown(cancelBtn, 'Enter');
    await expect(result!).resolves.toBe(false);
    expect(container.querySelector('[role="alertdialog"]')).toBeNull();
  });

  it('shows a dialog requested while another is open exactly once (StrictMode queue regression #142)', async () => {
    renderProvider();
    let first: Promise<boolean>;
    let second: Promise<boolean>;
    act(() => {
      first = confirmFn!({ message: 'first dialog', confirmLabel: 'YesOne' });
      second = confirmFn!({ message: 'second dialog', confirmLabel: 'YesTwo' });
    });

    // Only the first dialog is visible; the second is queued.
    expect(container.textContent).toContain('first dialog');
    expect(container.textContent).not.toContain('second dialog');

    click(buttonByText('YesOne'));
    await expect(first!).resolves.toBe(true);

    // The queued dialog now shows (once).
    expect(container.textContent).not.toContain('first dialog');
    expect(container.textContent).toContain('second dialog');
    expect(container.querySelectorAll('[role="alertdialog"]')).toHaveLength(1);

    click(buttonByText('YesTwo'));
    await expect(second!).resolves.toBe(true);

    // No ghost copy of either dialog reopens after both are resolved.
    expect(container.querySelector('[role="alertdialog"]')).toBeNull();
    expect(container.textContent).not.toContain('second dialog');
  });
});
