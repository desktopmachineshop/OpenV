import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { OrgSettings } from './OrgSettings';
import { qualityRulesAPI } from '../api/client';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Workspace settings keep an unsaved Quality rules change across a tab
// switch, and load the rules once, as project settings does since #475
// (#379, bug 120).

const QUALITY_RULES = {
  effective: { convention: 'shall', severities: { 'weak-word': 'error' } },
  workspace: { convention: 'shall', severities: { 'weak-word': 'error' } },
  summary: 'shall; weak wording is an error',
  catalog: {
    conventions: ['shall', 'rfc2119'],
    rules: ['weak-word'],
    severities: ['error', 'warning', 'info', 'off'],
    defaults: { convention: 'shall', severities: { 'weak-word': 'warning' } },
    labels: { shall: 'the system shall', rfc2119: 'MUST, SHOULD, MAY', 'weak-word': 'weak or subjective wording' },
  },
};

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    qualityRulesAPI: { forWorkspace: () => Promise.resolve({ data: JSON.parse(JSON.stringify(QUALITY_RULES)) }) },
  })
);
vi.mock('../components/Navbar', () => ({ Navbar: () => null }));
vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const byText = <T extends Element>(selector: string, text: string): T => {
  const found = Array.from(container.querySelectorAll<T>(selector)).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

const choose = async (node: HTMLSelectElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
};

const tab = (label: string) => byText<HTMLElement>('[role="tablist"] > *', label);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(
    {
      ...initialStore,
      orgs: [{ id: 'o1', name: 'Acme', slug: 'acme', type: 'company', plan: 'business', role: 'admin' } as any],
      activeOrgId: 'o1',
      orgsLoaded: true,
      currentUser: { id: 'u1', email: 'admin@example.com' } as any,
    },
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('OrgSettings quality rules', () => {
  it('keeps an unsaved quality-rules change across a tab switch and loads the rules once', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/org/settings?tab=quality']}>
          <DialogProvider>
            <Routes>
              <Route path="/org/settings" element={<OrgSettings />} />
            </Routes>
          </DialogProvider>
        </MemoryRouter>
      );
    });
    await flush();

    // The weak-wording rule's severity: the editor's second select, after
    // the convention.
    const weakWording = () =>
      Array.from(container.querySelectorAll<HTMLSelectElement>('select')).find((s) =>
        Array.from(s.options).some((o) => o.value === 'off')
      )!;
    expect(weakWording().value).toBe('error');

    await choose(weakWording(), 'off');
    await click(tab('General'));
    await click(tab('Quality rules'));

    expect(weakWording().value).toBe('off');
    expect(byText<HTMLButtonElement>('button', 'Save rules').disabled).toBe(false);
    expect(vi.mocked(qualityRulesAPI.forWorkspace)).toHaveBeenCalledTimes(1);
  });
});
