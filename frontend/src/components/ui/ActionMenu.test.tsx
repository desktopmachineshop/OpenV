import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ActionMenu } from './ActionMenu';

// The action menu: the actions that do not earn a button of their own, behind
// one trigger. A popover on a computer, a sheet on a phone; choosing an entry,
// Escape and a click outside close it.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialWidth = window.innerWidth;
let container: HTMLDivElement;
let root: Root;

const mount = async (ui: React.ReactElement, width = 1280) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: initialWidth });
});

const trigger = () => container.querySelector('button[aria-haspopup="menu"]') as HTMLButtonElement;
const menu = () => container.querySelector('[role="menu"]') as HTMLElement | null;
const entries = () => Array.from(container.querySelectorAll('[role="menuitem"]')) as HTMLButtonElement[];
const click = async (el: Element) => {
  await act(async () => {
    (el as HTMLElement).click();
  });
};

describe('ActionMenu', () => {
  it('opens under its trigger, runs the chosen entry and closes', async () => {
    const history = vi.fn();
    const archive = vi.fn();
    await mount(
      <ActionMenu
        label="More actions"
        items={[
          { key: 'history', label: 'History', onSelect: history },
          { key: 'archive', label: 'Archive', danger: true, onSelect: archive },
        ]}
      />
    );
    expect(menu()).toBeNull();
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    await click(trigger());
    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    expect(menu()?.className).toBe('ov-action-menu');
    expect(entries().map((e) => e.textContent)).toEqual(['History', 'Archive']);
    expect(entries()[1].className).toContain('is-danger');
    await click(entries()[0]);
    expect(history).toHaveBeenCalledTimes(1);
    expect(archive).not.toHaveBeenCalled();
    expect(menu()).toBeNull();
  });

  it('closes on Escape and on a click outside', async () => {
    await mount(<ActionMenu label="More actions" items={[{ key: 'a', label: 'A', onSelect: () => {} }]} />);
    await click(trigger());
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(menu()).toBeNull();
    await click(trigger());
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    expect(menu()).toBeNull();
  });

  it('is a sheet from the bottom of the screen on a phone, over a scrim that closes it', async () => {
    await mount(<ActionMenu label="More actions" items={[{ key: 'a', label: 'A', onSelect: () => {} }]} />, 390);
    await click(trigger());
    expect(menu()?.className).toContain('is-sheet');
    await click(container.querySelector('.ov-action-scrim') as Element);
    expect(menu()).toBeNull();
  });

  it('does not run a disabled entry, and draws nothing with no entries', async () => {
    const run = vi.fn();
    await mount(<ActionMenu label="More actions" items={[{ key: 'a', label: 'A', disabled: true, hint: 'Not now', onSelect: run }]} />);
    await click(trigger());
    expect(entries()[0].disabled).toBe(true);
    expect(entries()[0].title).toBe('Not now');
    await click(entries()[0]);
    expect(run).not.toHaveBeenCalled();
    act(() => root.render(<ActionMenu label="More actions" items={[]} />));
    expect(container.querySelector('button')).toBeNull();
  });
});
