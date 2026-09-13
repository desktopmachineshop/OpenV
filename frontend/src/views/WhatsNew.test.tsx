import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { WhatsNew } from './WhatsNew';
import { releaseAPI } from '../api/client';

// The page is the release history and nothing else. It used to render the
// whole RELEASE_NOTES.md file the API sent, which meant customers were shown
// the instructions written for contributors and the bullets of a release
// that had not shipped. These tests are that regression, and the shape the
// page was asked for: the version you were upgraded to, then what is new,
// what was tidied and what was fixed, kept apart.

jest.mock('../api/client', () => ({
  releaseAPI: { current: jest.fn() },
}));

jest.mock('../components/Navbar', () => ({
  Navbar: ({ title }: any) => require('react').createElement('div', null, title),
}));

// No active workspace: the page has no channel to speak for, and the
// releases stand on their own.
const storeState: any = { orgs: [], activeOrgId: null, features: null };
jest.mock('../state/store', () => ({
  useAppStore: () => storeState,
}));

const current = releaseAPI.current as jest.Mock;

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const release = {
  version: '0.2.0',
  date: '2026-09-14',
  notes: ['Export to Excel', 'Baselines load again'],
  categories: [
    { name: 'New features', notes: ['Export to Excel'] },
    { name: 'Bug fixes', notes: ['Baselines load again'] },
  ],
  markdown: '',
  releases: [
    {
      version: '0.2.0',
      date: '2026-09-14',
      notes: ['Export to Excel', 'Baselines load again'],
      categories: [
        { name: 'New features', notes: ['Export to Excel'] },
        { name: 'Bug fixes', notes: ['Baselines load again'] },
      ],
      markdown: '',
    },
    {
      version: '2026-09-12',
      date: '2026-09-12',
      notes: ['Something older'],
      categories: [{ name: 'Changes', notes: ['Something older'] }],
      markdown: '',
    },
  ],
  stable: null,
  deployment: 'shared',
};

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  current.mockReset();
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

const mount = async () => {
  await act(async () => {
    root.render(<WhatsNew />);
  });
};

it('names the version a reader was upgraded to, and groups what changed', async () => {
  current.mockResolvedValue({ data: release });
  await mount();
  const text = container.textContent || '';
  expect(text).toContain('OpenV version upgraded to 0.2.0');
  expect(text).toContain('New features');
  expect(text).toContain('Export to Excel');
  expect(text).toContain('Bug fixes');
  expect(text).toContain('Baselines load again');
  // In reading order: what is new before what was fixed.
  expect(text.indexOf('New features')).toBeLessThan(text.indexOf('Bug fixes'));
});

it('shows every release, newest first', async () => {
  current.mockResolvedValue({ data: release });
  await mount();
  const headings = Array.from(container.querySelectorAll('h2')).map((h) => h.textContent);
  expect(headings).toEqual(['OpenV version upgraded to 0.2.0', 'OpenV update 2026-09-12']);
});

// A release from before OpenV had version numbers has no groups of its own.
// "Changes" is the parser's placeholder, not copy, so it is not shown — but
// the bullets under it are.
it('keeps an older dated release readable without inventing a group for it', async () => {
  current.mockResolvedValue({ data: release });
  await mount();
  expect(container.textContent).toContain('Something older');
  expect(container.textContent).not.toContain('Changes');
});

it('says what went wrong instead of showing an empty page', async () => {
  current.mockRejectedValue({ response: { data: { error: 'Session expired' } } });
  await mount();
  expect(container.textContent).toContain('Session expired');

  // And falls back to its own wording when the failure says nothing.
  await act(async () => root.unmount());
  container.remove();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  current.mockRejectedValue({});
  await mount();
  expect(container.textContent).toContain('Could not load the release notes');
});
