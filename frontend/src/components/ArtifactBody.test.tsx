import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ArtifactBody } from './ArtifactBody';
import { REFERENCE_SCHEME } from './artifactReferences';

// The bug this guards against: react-markdown blanks the href of any URL
// scheme it does not recognise as safe, and the reference scheme is not one of
// them. Every citation therefore rendered as an anchor with an empty href,
// which fell through the "is this a reference?" check to the ordinary-link
// branch and opened target="_blank" — a new tab showing the page the reader
// was already on. It looked like a figure link going to the wrong place; it
// was every reference link going nowhere at all.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const render = async (el: React.ReactElement) => {
  await act(async () => {
    root.render(el);
  });
};

const anchors = () => Array.from(container.querySelectorAll('a'));

describe('ArtifactBody references', () => {
  it('keeps the reference in the href instead of blanking it', async () => {
    await render(<ArtifactBody body="as shown in #REQ-17-FIG-1 above" onReferenceClick={jest.fn()} />);
    const [link] = anchors();
    expect(link).toBeDefined();
    expect(link.getAttribute('href')).toBe(`${REFERENCE_SCHEME}REQ-17-FIG-1`);
  });

  it('never opens a reference in a new tab', async () => {
    await render(<ArtifactBody body="see #REQ-12 and ##REQ-99-FIG-2" onReferenceClick={jest.fn()} />);
    for (const link of anchors()) {
      expect(link.getAttribute('target')).toBeNull();
    }
  });

  it('hands the reference to the app rather than navigating', async () => {
    const onReferenceClick = jest.fn();
    await render(<ArtifactBody body="as shown in #REQ-17-FIG-1" onReferenceClick={onReferenceClick} />);
    const [link] = anchors();
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    await act(async () => {
      link.dispatchEvent(event);
    });
    expect(onReferenceClick).toHaveBeenCalledWith('REQ-17-FIG-1');
    expect(event.defaultPrevented).toBe(true);
  });

  it('follows a figure on another artifact, marker and all', async () => {
    const onReferenceClick = jest.fn();
    await render(<ArtifactBody body="as built in ##REQ-99-FIG-2" onReferenceClick={onReferenceClick} />);
    const [link] = anchors();
    // The reader sees which citation reaches outside the artifact.
    expect(link.textContent).toBe('##REQ-99-FIG-2');
    // The app is handed the bare reference to resolve.
    await act(async () => {
      link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    expect(onReferenceClick).toHaveBeenCalledWith('REQ-99-FIG-2');
  });

  it('still sanitises an ordinary link, and still opens it in a new tab', async () => {
    await render(
      <ArtifactBody body="[docs](https://example.com) and [bad](javascript:alert(1))" onReferenceClick={jest.fn()} />
    );
    const [ok, bad] = anchors();
    expect(ok.getAttribute('href')).toBe('https://example.com');
    expect(ok.getAttribute('target')).toBe('_blank');
    expect(bad.getAttribute('href')).toBe('');
  });

  it('renders references as plain text when there is nothing to follow them with', async () => {
    await render(<ArtifactBody body="as shown in #REQ-17-FIG-1" />);
    expect(anchors()).toHaveLength(0);
    expect(container.textContent).toContain('#REQ-17-FIG-1');
  });

  it('leaves markdown headings as headings', async () => {
    await render(<ArtifactBody body={'## Interfaces\n\nbody'} onReferenceClick={jest.fn()} />);
    expect(container.querySelector('h2')?.textContent).toBe('Interfaces');
    expect(anchors()).toHaveLength(0);
  });
});
