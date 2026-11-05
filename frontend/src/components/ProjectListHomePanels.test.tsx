import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { linkAPI } from '../api/client';
import { ActivityRows, NeedsPanel } from './ProjectListHomePanels';
import type { TodoItem } from './ProjectListHomeData';

// The Needs you rows confirm or remove a suspect link in place, and every
// row opens the thing it names (#719); the Activity rows link to what they
// describe (#718).
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), { linkAPI: { confirm: () => Promise.resolve({ data: {} }), delete: () => Promise.resolve({ data: {} }) } })
);

let answer = true;
const confirmFn = vi.fn(() => Promise.resolve(answer));
vi.mock('./ui', async () => ({
  ...(await vi.importActual<typeof import('./ui')>('./ui')),
  useConfirm: () => confirmFn,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const suspect: TodoItem = {
  key: 'suspect-l1',
  kind: 'suspect',
  ref: 'TC-45',
  title: 'verifies REQ-73 Change impact analysis',
  meta: 'An end changed after linking · confirm or remove the link',
  action: 'Review',
  to: '/projects/p1/review',
  open: '/projects/p1/requirements?artifact=tc45',
  linkId: 'l1',
};
const approve: TodoItem = {
  key: 'approve-a1',
  kind: 'approve',
  ref: 'REQ-9',
  title: 'Detect wheel slip',
  meta: 'In review · 2h',
  action: 'Review',
  to: '/projects/p1/requirements?artifact=a1',
  open: '/projects/p1/requirements?artifact=a1',
};

const render = async (el: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(<MemoryRouter>{el}</MemoryRouter>);
  });
};

const flush = () => act(async () => {
  for (let i = 0; i < 4; i++) await Promise.resolve();
});

const button = (label: RegExp) =>
  Array.from(container.querySelectorAll('button')).find((b) => label.test(b.getAttribute('aria-label') || ''))!;

beforeEach(() => {
  vi.clearAllMocks();
  answer = true;
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('NeedsPanel', () => {
  it('confirms a suspect link in place: the row drops out and the count follows', async () => {
    await render(<NeedsPanel items={[suspect, approve]} limit={8} emptyText="Nothing" action={<a href="/r">Review all</a>} />);
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 2');
    expect(container.textContent).toContain('Review all');
    await act(async () => button(/^Confirm the link/).click());
    await flush();
    expect(linkAPI.confirm).toHaveBeenCalledWith('l1');
    expect(container.textContent).not.toContain('TC-45');
    expect(container.querySelector('h2')?.textContent).toBe('Needs you 1');
  });

  it('removes a suspect link only once the member says so', async () => {
    await render(<NeedsPanel items={[suspect]} limit={8} emptyText="Nothing waits" />);
    answer = false;
    await act(async () => button(/^Remove the link/).click());
    await flush();
    expect(confirmFn).toHaveBeenCalledTimes(1);
    expect(linkAPI.delete).not.toHaveBeenCalled();
    answer = true;
    await act(async () => button(/^Remove the link/).click());
    await flush();
    expect(linkAPI.delete).toHaveBeenCalledWith('l1');
    expect(container.textContent).toContain('Nothing waits');
  });

  it('opens the artifact from the row, and labels other rows by what they do', async () => {
    await render(<NeedsPanel items={[suspect, approve]} limit={8} emptyText="Nothing" />);
    const hrefs = Array.from(container.querySelectorAll('li a')).map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('/projects/p1/requirements?artifact=tc45');
    const actions = Array.from(container.querySelectorAll('li > a:last-child')).map((a) => a.textContent);
    expect(actions).toEqual(['Review']);
  });
});

describe('ActivityRows', () => {
  it('links a row to what it describes, and leaves one with nowhere to go plain', async () => {
    await render(
      <ActivityRows
        items={[
          { id: 'e1', who: 'David', what: 'linked', ref: 'TC-177 → REQ-229', when: '10:42', tone: 'info', to: '/projects/p1/requirements?artifact=tc177' },
          { id: 'e2', who: 'David', what: 'deleted', ref: 'Old item', when: '10:40', tone: 'warn', to: '' },
        ]}
      />
    );
    const links = container.querySelectorAll('li a');
    expect(links).toHaveLength(1);
    expect(links[0].getAttribute('href')).toBe('/projects/p1/requirements?artifact=tc177');
    expect(links[0].textContent).toContain('David linked TC-177 → REQ-229');
  });
});
