import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Avatar } from './Avatar';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// Issue #319: the avatar sits in a flex row beside a name that can be long,
// and it was shrunk to the width of its one letter. It must keep its circle.
describe('Avatar', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('renders the initial in a fixed, non-shrinking circle', () => {
    act(() => root.render(<Avatar name="dave" />));
    const el = container.firstChild as HTMLElement;
    expect(el.textContent).toBe('D');
    expect(el.style.width).toBe('28px');
    expect(el.style.height).toBe('28px');
    expect(el.style.minWidth).toBe('28px');
    expect(el.style.flexShrink).toBe('0');
    expect(el.style.borderRadius).toBe('50%');
  });

  it('crops a picture to the circle instead of stretching it', () => {
    act(() => root.render(<Avatar src="https://example.test/p.png" name="dave" size={40} />));
    const img = container.querySelector('img') as HTMLImageElement;
    expect(img.src).toBe('https://example.test/p.png');
    expect(img.style.width).toBe('40px');
    expect(img.style.height).toBe('40px');
    expect(img.style.flexShrink).toBe('0');
    expect(img.style.objectFit).toBe('cover');
  });

  it('resolves an uploaded picture against the API base', () => {
    act(() => root.render(<Avatar src="/api/v1/users/u1/avatar?v=7" name="dave" />));
    const img = container.querySelector('img') as HTMLImageElement;
    expect(img.src).toBe('http://localhost:8080/api/v1/users/u1/avatar?v=7');
  });

  it('falls back to a question mark with no name', () => {
    act(() => root.render(<Avatar />));
    expect(container.textContent).toBe('?');
  });
});
