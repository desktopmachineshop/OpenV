import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { SharedProjectView } from './SharedProjectView';

// A public share link names each artifact by its type. Every type the server
// has gets a label, and nothing that is not a type does (#379, bug 81).

const T = '2026-03-01T09:00:00Z';

const art = (id: string, ref: string, type: string, title: string, order: number) => ({
  id,
  ref,
  project_id: 'p1',
  parent_id: null,
  type,
  title,
  body: `${title}.`,
  sort_order: order,
  status: 'draft',
  attributes: {},
  version: 1,
  created_at: T,
  updated_at: T,
});

const ARTIFACTS = [
  art('d-1', 'DSC-1', 'description', 'Scope of the pump', 1),
  art('o-1', 'OTH-1', 'other', 'Glossary', 2),
  art('r-1', 'REQ-1', 'requirement', 'The pump shall start within 2 seconds', 3),
];

const SHARED = {
  role: 'public',
  project: { id: 'p1', name: 'Fuel pump', description: '' },
  workspace: 'Acme',
  counts: { description: 2, other: 1, requirement: 3 },
  snapshot: {
    exported_at: T,
    version: '1',
    project_id: 'p1',
    project_name: 'Fuel pump',
    project_description: '',
    artifacts: ARTIFACTS,
    links: [],
    attachments: [],
  },
};

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    shareLinkAPI: { open: () => Promise.resolve({ data: JSON.parse(JSON.stringify(SHARED)) }) },
  })
);

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

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

// The detail card's type badge: the span after the artifact's ref. The
// detail card is the last card on the page; the tree is the first.
const typeBadge = () => {
  const cards = container.querySelectorAll('.card');
  const ref = cards[cards.length - 1]?.querySelector('code');
  return (ref?.nextElementSibling?.textContent ?? '').trim();
};

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('SharedProjectView type labels', () => {
  it('labels a description and an other artifact by their type', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/s/tok-1']}>
          <Routes>
            <Route path="/s/:token" element={<SharedProjectView source="share" />} />
          </Routes>
        </MemoryRouter>
      );
    });
    await flush();

    expect(container.textContent).toContain('2 descriptions · 1 other · 3 requirements');

    await act(async () => {
      byText<HTMLElement>('div', 'Scope of the pump').click();
    });
    await flush();
    expect(typeBadge()).toBe('Description');

    await act(async () => {
      byText<HTMLElement>('div', 'Glossary').click();
    });
    await flush();
    expect(typeBadge()).toBe('Other');

    await act(async () => {
      byText<HTMLElement>('div', 'The pump shall start within 2 seconds').click();
    });
    await flush();
    expect(typeBadge()).toBe('Requirement');
  });
});
