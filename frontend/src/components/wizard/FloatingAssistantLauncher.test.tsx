import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { FloatingAssistantLauncher } from './FloatingAssistantLauncher';

// The assistant's button, and the way to put it out of the way: it tucks
// into a tab on the edge of the window, which brings it back, and the
// browser remembers which.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
const onOpen = vi.fn();
const mount = () => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => root.render(<FloatingAssistantLauncher left={false} bottom="16px" onOpen={onOpen} launcherRef={null} />));
};
const unmount = () => {
  act(() => root.unmount());
  container.remove();
};
afterEach(() => {
  unmount();
  localStorage.clear();
  onOpen.mockClear();
});

describe('the assistant launcher', () => {
  it('opens the assistant from its button', () => {
    mount();
    act(() => container.querySelector<HTMLButtonElement>('button[aria-label="Open the V&V Assistant"]')!.click());
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[data-assistant-tab]')).toBeNull();
  });

  it('tucks into a tab on the edge, stays tucked on the next page, and comes back from the tab', () => {
    mount();
    act(() => container.querySelector<HTMLButtonElement>('[data-assistant-tuck]')!.click());
    expect(container.querySelector('button[aria-label="Open the V&V Assistant"]')).toBeNull();
    const tab = container.querySelector<HTMLButtonElement>('[data-assistant-tab]')!;
    expect(tab.style.right).toBe('0px');
    // Another page mounts it afresh: it is still tucked.
    unmount();
    mount();
    expect(container.querySelector('[data-assistant-tab]')).not.toBeNull();
    act(() => container.querySelector<HTMLButtonElement>('[data-assistant-tab]')!.click());
    expect(container.querySelector('button[aria-label="Open the V&V Assistant"]')).not.toBeNull();
    expect(onOpen).not.toHaveBeenCalled();
    expect(localStorage.getItem('openv-assistant-tucked')).toBeNull();
  });
});
