import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { OrgSwitcher } from './OrgSwitcher';
import { useAppStore } from '../state/store';
import { type Org } from '../api/client';

// #727: on a computer the workspace name takes the room the bar gives it and
// is cut short, with the full name as its tooltip, only when it runs out. A
// phone keeps its cap.
vi.mock('../api/client', async (orig) => mockApi(await orig()));

let phone = false;
vi.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isPhone: phone, isCompact: phone }) }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
const NAME = 'Desktop Machine Shop';
let container: HTMLDivElement;
let root: Root;

const mount = async () => {
  useAppStore.setState(
    { ...initialStore, activeOrgId: 'o1', orgs: [{ id: 'o1', name: NAME, slug: 'o1', type: 'company', plan: 'business', role: 'member', created_at: '' } as Org] },
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter>
        <OrgSwitcher />
      </MemoryRouter>
    );
  });
};

const name = () => container.querySelector<HTMLElement>('[data-org-name]')!;

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('the workspace name in the bar', () => {
  it('has no fixed cap on a computer, and a tooltip only when cut short', async () => {
    phone = false;
    await mount();
    expect(name().textContent).toBe(NAME);
    expect(name().style.maxWidth).toBe('');
    const button = container.querySelector<HTMLButtonElement>('button[title="Switch workspace"]')!;
    await act(async () => button.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
    expect(name().getAttribute('title')).toBeNull();

    Object.defineProperty(name(), 'scrollWidth', { configurable: true, value: 300 });
    Object.defineProperty(name(), 'clientWidth', { configurable: true, value: 120 });
    await act(async () => button.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
    expect(name().getAttribute('title')).toBe(NAME);
  });

  it('keeps the cap on a phone', async () => {
    phone = true;
    await mount();
    expect(name().style.maxWidth).not.toBe('');
  });
});
