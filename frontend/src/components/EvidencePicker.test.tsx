import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { EvidencePicker } from './EvidencePicker';
import { evidenceAPI } from '../api/client';
import { parseConditions, formatConditions, humanBytes } from '../utils/evidence';

jest.mock('../api/client', () => ({
  evidenceAPI: {
    cite: jest.fn(),
    uncite: jest.fn(),
  },
}));

jest.mock('./ui', () => ({
  Modal: ({ children }: any) => <div>{children}</div>,
  ErrorBanner: ({ message }: any) => (message ? <div role="alert">{message}</div> : null),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = evidenceAPI as jest.Mocked<typeof evidenceAPI>;

const bundle = (id: string, ref: string, over: Partial<any> = {}) => ({
  id,
  project_id: 'p-1',
  ref,
  title: `Capture ${ref}`,
  summary: '',
  captured_at: null,
  captured_by: '',
  conditions: {},
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  file_count: 1,
  total_size: 2048,
  ...over,
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
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

const flush = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

const button = (label: string): HTMLButtonElement | undefined =>
  Array.from(container.querySelectorAll('button')).find(
    (b) => (b.textContent || '').trim() === label
  ) as HTMLButtonElement | undefined;

const click = async (el: HTMLElement) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

const render = async (props: Partial<React.ComponentProps<typeof EvidencePicker>> = {}) => {
  const onChanged = props.onChanged || jest.fn();
  await act(async () => {
    root.render(
      <EvidencePicker
        resultId="result-1"
        testCaseTitle="Idle noise"
        bundles={[bundle('b-1', 'EVD-1'), bundle('b-2', 'EVD-2')]}
        cited={[]}
        onClose={jest.fn()}
        onChanged={onChanged}
        {...props}
      />
    );
  });
  await flush();
  return onChanged;
};

describe('citing a capture from a result', () => {
  it('cites a bundle the result does not yet rest on', async () => {
    api.cite.mockResolvedValue({ data: {} } as any);
    const onChanged = await render();

    expect(container.textContent).toContain('EVD-1');
    await click(button('Cite')!);

    expect(api.cite).toHaveBeenCalledWith('result-1', 'b-1');
    expect(onChanged).toHaveBeenCalled();
  });

  // The same capture is normally cited by several results, so an already-cited
  // bundle offers removal rather than a second citation.
  it('offers removal for a bundle already cited', async () => {
    api.uncite.mockResolvedValue({ data: {} } as any);
    await render({
      cited: [{ id: 'c-1', bundle_id: 'b-1', test_result_id: 'result-1', note: '', created_at: '' }],
    });

    expect(button('Remove')).toBeTruthy();
    await click(button('Remove')!);
    expect(api.uncite).toHaveBeenCalledWith('result-1', 'b-1');
    expect(api.cite).not.toHaveBeenCalled();
  });

  it('says so when the server refuses, and leaves the list up', async () => {
    api.cite.mockRejectedValue(new Error('nope'));
    await render();
    await click(button('Cite')!);

    expect(container.querySelector('[role="alert"]')).toBeTruthy();
    expect(container.textContent).toContain('EVD-1');
  });

  // Removing a citation must not read as deleting the capture: other results
  // may still rest on it.
  it('explains that removing a citation keeps the capture', async () => {
    await render();
    expect(container.textContent).toContain('does not delete the capture');
  });

  it('points at the Evidence page when the project has nothing recorded', async () => {
    await render({ bundles: [] });
    expect(container.textContent).toContain('no evidence recorded yet');
    expect(button('Cite')).toBeFalsy();
  });
});

// Conditions are free-form, so the form takes "name: value" lines. The parse
// has to survive what people actually type.
describe('conditions', () => {
  it('round-trips name: value lines', () => {
    const text = 'rig: chamber 2\nambient: 21.5 C';
    const parsed = parseConditions(text);
    expect(parsed).toEqual({ rig: 'chamber 2', ambient: '21.5 C' });
    expect(formatConditions(parsed)).toBe(text);
  });

  it('keeps a line with no colon rather than dropping what somebody typed', () => {
    const parsed = parseConditions('rig: chamber 2\njust a remark');
    expect(parsed.rig).toBe('chamber 2');
    expect(Object.values(parsed)).toContain('just a remark');
  });

  it('ignores blank lines and a line with no name', () => {
    expect(parseConditions('\n\nrig: chamber 2\n  \n: orphaned\n')).toEqual({
      rig: 'chamber 2',
    });
  });

  it('handles a value that itself contains a colon', () => {
    expect(parseConditions('started: 09:30')).toEqual({ started: '09:30' });
  });
});

describe('file sizes', () => {
  it('reads the way a person would say it', () => {
    expect(humanBytes(512)).toBe('512 B');
    expect(humanBytes(2048)).toBe('2.0 KB');
    expect(humanBytes(5 * 1024 * 1024)).toBe('5.0 MB');
    expect(humanBytes(3 * 1024 * 1024 * 1024)).toBe('3.0 GB');
  });
});
