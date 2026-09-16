import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { NoteText, notedPieces } from './NoteText';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

describe('notedPieces', () => {
  it('leaves prose with no tokens in one piece', () => {
    expect(notedPieces('checked against the datasheet')).toEqual([
      { text: 'checked against the datasheet' },
    ]);
  });

  it('splits out a reference and keeps the text around it', () => {
    const got = notedPieces('see #REQ-12 first');
    expect(got.map((p) => p.text)).toEqual(['see ', '#REQ-12', ' first']);
    expect(got[1]).toMatchObject({ token: 'reference', value: 'REQ-12', doubled: false });
  });

  it('marks a doubled reference as reaching outside the artifact', () => {
    expect(notedPieces('see ##REQ-99-FIG-2')[1]).toMatchObject({
      token: 'reference',
      value: 'REQ-99-FIG-2',
      doubled: true,
    });
  });

  it('splits out a mention', () => {
    expect(notedPieces('thanks @dana')[1]).toMatchObject({
      token: 'mention',
      value: 'dana',
      doubled: false,
    });
  });

  it('marks a doubled mention as a to-do', () => {
    expect(notedPieces('@@dana please look')[0]).toMatchObject({
      token: 'mention',
      value: 'dana',
      doubled: true,
    });
  });

  it('leaves a markdown heading alone', () => {
    // Notes are prose, not markdown: "# Heading" is text somebody typed.
    expect(notedPieces('# Heading')).toEqual([{ text: '# Heading' }]);
  });

  it('leaves an email address alone', () => {
    expect(notedPieces('mail dana@example.com')).toEqual([{ text: 'mail dana@example.com' }]);
  });

  it('keeps newlines, which the note relies on for its layout', () => {
    expect(notedPieces('one\n\ntwo').map((p) => p.text).join('')).toBe('one\n\ntwo');
  });
});

describe('NoteText', () => {
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

  const render = (text: string, onReferenceClick?: (ref: string) => void) =>
    act(() => {
      root.render(<NoteText text={text} onReferenceClick={onReferenceClick} />);
    });

  it('links a reference when there is somewhere to send the reader', () => {
    const go = vi.fn();
    render('see #REQ-12', go);
    const anchor = container.querySelector('a') as HTMLAnchorElement;
    expect(anchor.textContent).toBe('#REQ-12');
    act(() => {
      anchor.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    expect(go).toHaveBeenCalledWith('REQ-12');
  });

  it('renders a reference as marked text when there is nowhere to go', () => {
    render('see #REQ-12');
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toBe('see #REQ-12');
  });

  it('says what a doubled mention will do', () => {
    render('@@dana please look');
    const span = container.querySelector('span') as HTMLSpanElement;
    expect(span.title).toBe('Raises a to-do for dana');
  });

  it('reproduces the note exactly', () => {
    const note = 'see #REQ-12 and ##REQ-99-FIG-2, @@dana — thanks @sam';
    render(note, () => {});
    expect(container.textContent).toBe(note);
  });
});
