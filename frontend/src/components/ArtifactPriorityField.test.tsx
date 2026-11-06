import React, { act, useState } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { useAppStore } from '../state/store';

let detection = true;
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: { priorityDetection: () => Promise.resolve({ data: { enabled: detection } }) },
  })
);

import { ArtifactPriorityField } from './ArtifactPriorityField';

// The built-in Priority field: with the project's detection on, it follows the
// strongest shall, should or may in the text until someone picks a value; a
// value the form opened with, or one picked by hand, stays.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

describe('ArtifactPriorityField', () => {
  let container: HTMLDivElement;
  let root: Root;
  let setText: (t: string) => void = () => {};

  const Harness: React.FC<{ initial: string; initialText: string }> = ({ initial, initialText }) => {
    const [value, setValue] = useState(initial);
    const [text, set] = useState(initialText);
    setText = set;
    return (
      <>
        <ArtifactPriorityField projectId="p1" text={text} value={value} initiallySet={Boolean(initial)} onChange={(v) => setValue(v || '')} />
        <output data-testid="value">{value}</output>
      </>
    );
  };

  beforeEach(() => {
    detection = true;
    useAppStore.setState({
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'editor-pickers': true } },
    } as any);
    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => { root = createRoot(container); });
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const mount = async (initial: string, text: string) => {
    await act(async () => { root.render(<Harness initial={initial} initialText={text} />); });
  };
  const value = () => container.querySelector('output')!.textContent;
  const hint = () => container.querySelector('#priority-hint')?.textContent || '';

  it('follows the text while nobody has picked a priority', async () => {
    await mount('', 'The hoist shall lift 2 t');
    expect(value()).toBe('must');
    expect(hint()).toContain('“shall”');
    await act(async () => setText('The hoist should lift 2 t'));
    expect(value()).toBe('should');
    await act(async () => setText('The hoist lifts 2 t'));
    expect(value()).toBe('');
  });

  it('keeps a priority the form opened with, and offers the detected one', async () => {
    await mount('could', 'The hoist shall lift 2 t');
    expect(value()).toBe('could');
    expect(hint()).toContain('Set by hand');
    const use = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Use Must')!;
    await act(async () => use.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(value()).toBe('must');
  });

  it('leaves the value alone when the project has detection off', async () => {
    detection = false;
    await mount('', 'The hoist shall lift 2 t');
    expect(value()).toBe('');
    expect(hint()).toBe('');
  });
});
