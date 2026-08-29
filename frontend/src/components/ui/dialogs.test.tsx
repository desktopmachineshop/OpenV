import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ConfirmDialog } from './ConfirmDialog';
import { PromptDialog } from './PromptDialog';

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
