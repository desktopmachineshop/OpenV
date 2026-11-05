import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { GlobalSearch } from './GlobalSearch';
import { savedSearchAPI, searchAPI } from '../api/client';
import { useAppStore } from '../state/store';

vi.mock('../api/client', async (orig) => mockApi(await orig()));
vi.mock('../hooks/useFeature', () => ({ useFeature: () => true }));

const navigate = vi.fn();
vi.mock('react-router-dom', () => ({ useNavigate: () => navigate }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const global_ = vi.mocked(searchAPI.global, { partial: true, deep: true });
const listSaved = vi.mocked(savedSearchAPI.list, { partial: true, deep: true });

const hit = (over: Partial<Record<string, unknown>> = {}) => ({
  artifact_id: 'art-30',
  project_id: 'proj-1',
  project_name: 'Alpha',
  type: 'requirement',
  title: 'Noise limit',
  snippet: 'sound pressure stays below 60 dB',
  ref: 'REQ-30',
  status: 'in_review',
  owner: 'Dana',
  ...over,
});

// The top-bar search on the chip search bar (issue #722): its chips reach
// the server as facets, a ref still opens the artifact it names, and the
// results are grouped by kind.
describe('GlobalSearch with chips', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
    global_.mockReset();
    listSaved.mockReset();
    listSaved.mockResolvedValue({ data: { saved_searches: [] } } as any);
    navigate.mockReset();
    useAppStore.setState({ projects: [{ id: 'proj-1', name: 'Alpha' } as any, { id: 'proj-2', name: 'Beta' } as any] });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.useRealTimers();
    useAppStore.setState({ projects: [] });
  });

  const input = () => container.querySelector('input[role="combobox"]') as HTMLInputElement;
  const settle = async () => {
    await act(async () => {
      vi.advanceTimersByTime(400);
    });
    await act(async () => {
      await Promise.resolve();
    });
  };
  const type = async (text: string) => {
    const setValue = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    await act(async () => {
      input().focus();
      setValue.call(input(), text);
      input().dispatchEvent(new Event('input', { bubbles: true }));
    });
    await settle();
  };
  const key = async (k: string) =>
    act(async () => {
      input().dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
    });

  it('opens the artifact a typed ref names with Enter', async () => {
    global_.mockResolvedValue({ data: { mode_used: 'keyword', hits: [hit(), hit({ artifact_id: 'art-31', ref: 'REQ-301' })] } } as any);
    await act(async () => root.render(<GlobalSearch />));
    expect(input().placeholder).toContain('REQ-12');
    await type('REQ-30');
    expect(global_).toHaveBeenCalledWith('REQ-30', { mode: 'keyword', projects: [], types: [], statuses: [], owners: [], facets: true });
    await key('Enter');
    expect(navigate).toHaveBeenCalledWith('/projects/proj-1/requirements?artifact=art-30');
  });

  it('groups the results by kind and turns an offered filter into a chip the search sends', async () => {
    global_.mockResolvedValue({
      data: { mode_used: 'keyword', hits: [hit(), hit({ artifact_id: 'tc-1', type: 'test-case', title: 'Noise test', ref: 'TC-1' }), hit({ artifact_id: 'r2', title: 'Noise floor', ref: 'REQ-2' })] },
    } as any);
    await act(async () => root.render(<GlobalSearch />));
    await type('alpha');
    const groups = Array.from(container.querySelectorAll('[role="group"]')).map((g) => g.getAttribute('aria-label'));
    expect(groups[0]).toBe('Requirements');
    expect(groups[1]).toBe('Test Cases');
    expect(groups).toContain('Filter by');
    const reqs = container.querySelector('[role="group"][aria-label="Requirements"]')!;
    expect(reqs.textContent).toContain('Noise limit');
    expect(reqs.textContent).toContain('Noise floor');
    expect(container.textContent).toContain('Alpha · Dana');

    const offer = Array.from(container.querySelectorAll('[role="button"]')).find((b) => b.textContent === 'Project: Alpha') as HTMLElement;
    expect(offer).toBeTruthy();
    await act(async () => offer.click());
    await settle();
    expect(container.querySelector('.ov-search-chip')?.textContent).toContain('Alpha');
    expect(global_).toHaveBeenLastCalledWith('', { mode: 'keyword', projects: ['proj-1'], types: [], statuses: [], owners: [], facets: true });
  });

  it('searches with nothing to do until there is text or a chip', async () => {
    await act(async () => root.render(<GlobalSearch />));
    await type('   ');
    expect(global_).not.toHaveBeenCalled();
  });
});
