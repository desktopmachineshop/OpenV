import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { GlobalSearch } from './GlobalSearch';
import { searchAPI } from '../api/client';

vi.mock('../api/client', () => ({
  searchAPI: { global: vi.fn() },
}));

let refSearchOn = true;
vi.mock('../hooks/useFeature', () => ({ useFeature: () => refSearchOn }));

const navigate = vi.fn();
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const global_ = vi.mocked(searchAPI.global, { partial: true, deep: true });

const hit = (over: Partial<Record<string, unknown>> = {}) => ({
  artifact_id: 'art-30',
  project_id: 'proj-1',
  project_name: 'Alpha',
  type: 'requirement',
  title: 'Noise limit',
  snippet: 'sound pressure stays below 60 dB',
  ref: 'REQ-30',
  ...over,
});

// Searching by ref is only useful if the result says which ref it found — a
// list of titles cannot be checked against the ref that was typed.
describe('GlobalSearch ref results', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
    global_.mockReset();
    navigate.mockReset();
    refSearchOn = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.useRealTimers();
  });

  const search = async (query: string, hits: unknown[]) => {
    global_.mockResolvedValue({ data: { mode_used: 'keyword', hits } } as any);
    await act(async () => {
      root.render(<GlobalSearch />);
    });
    const input = container.querySelector('input') as HTMLInputElement;
    // React tracks the value behind the prototype setter; assigning .value
    // directly is invisible to it and onChange never fires.
    const setValue = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    await act(async () => {
      setValue.call(input, query);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    // The fetch is debounced by 300ms.
    await act(async () => {
      vi.advanceTimersByTime(400);
    });
    await act(async () => {
      await Promise.resolve();
    });
  };

  it('shows the ref alongside the title', async () => {
    await search('req-30', [hit()]);
    expect(container.textContent).toContain('REQ-30');
    expect(container.textContent).toContain('Noise limit');
  });

  it('sends the typed query through unchanged', async () => {
    await search('req-30', [hit()]);
    expect(global_).toHaveBeenCalledWith('req-30', { mode: 'keyword' });
  });

  it('renders a hit with no ref without inventing one', async () => {
    // Semantic hits come from the vector store and carry no ref.
    await search('noise', [hit({ ref: undefined, title: 'Acoustics plan' })]);
    expect(container.textContent).toContain('Acoustics plan');
    expect(container.textContent).not.toContain('REQ-');
  });

  it('tells people refs work', async () => {
    await act(async () => {
      root.render(<GlobalSearch />);
    });
    const input = container.querySelector('input') as HTMLInputElement;
    expect(input.placeholder).toContain('REQ-12');
  });

  it('does not invite a ref a workspace without the feature cannot find', async () => {
    refSearchOn = false;
    await act(async () => {
      root.render(<GlobalSearch />);
    });
    const input = container.querySelector('input') as HTMLInputElement;
    expect(input.placeholder).toBe('Search artifacts…');
  });
});
