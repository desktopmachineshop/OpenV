import { isTypingTarget, readingStepFor } from './readingKeys';

// A bare letter as a shortcut lives or dies on the guard: J must step to the
// next artifact while reading and must be the letter J everywhere else.

const facts = (over: Partial<Parameters<typeof readingStepFor>[0]> = {}) => ({
  key: 'j',
  target: null,
  altKey: false,
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  ...over,
});

describe('readingStepFor', () => {
  it('reads j as next and k as previous', () => {
    expect(readingStepFor(facts())).toBe('next');
    expect(readingStepFor(facts({ key: 'k' }))).toBe('previous');
  });

  it('reads them through caps lock, which is not a modifier', () => {
    expect(readingStepFor(facts({ key: 'J' }))).toBe('next');
    expect(readingStepFor(facts({ key: 'K' }))).toBe('previous');
  });

  it('ignores any other key', () => {
    ['a', 'ArrowDown', 'Enter', 'Escape', ' '].forEach((key) => {
      expect(readingStepFor(facts({ key }))).toBeNull();
    });
  });

  it('leaves a capital J to whoever was typing it', () => {
    expect(readingStepFor(facts({ key: 'J', shiftKey: true }))).toBeNull();
  });

  it('never claims a keystroke a modifier already owns', () => {
    expect(readingStepFor(facts({ metaKey: true }))).toBeNull();
    expect(readingStepFor(facts({ ctrlKey: true }))).toBeNull();
    expect(readingStepFor(facts({ altKey: true }))).toBeNull();
  });

  it('stands down while the editor or a dialog has the screen', () => {
    expect(readingStepFor(facts({ busy: true }))).toBeNull();
  });

  it('stands down inside a field', () => {
    const input = document.createElement('input');
    const textarea = document.createElement('textarea');
    const select = document.createElement('select');
    [input, textarea, select].forEach((target) => {
      expect(readingStepFor(facts({ target }))).toBeNull();
    });
  });

  it('steps from a plain element, such as the pane itself', () => {
    expect(readingStepFor(facts({ target: document.createElement('div') }))).toBe('next');
  });
});

describe('isTypingTarget', () => {
  it('sees a contenteditable region, and anything inside one', () => {
    const editable = document.createElement('div');
    editable.setAttribute('contenteditable', 'true');
    const span = document.createElement('span');
    editable.appendChild(span);
    expect(isTypingTarget(editable)).toBe(true);
    expect(isTypingTarget(span)).toBe(true);
  });

  it('does not count contenteditable="false", which is ordinary content', () => {
    const fixed = document.createElement('div');
    fixed.setAttribute('contenteditable', 'false');
    expect(isTypingTarget(fixed)).toBe(false);
  });

  it('is false for nothing at all', () => {
    expect(isTypingTarget(null)).toBe(false);
  });
});
