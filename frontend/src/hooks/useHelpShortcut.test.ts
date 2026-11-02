import { isHelpShortcut, typingIn } from './useHelpShortcut';

// ? and F1 open help (the compact layout); ? is a character people type, so
// it counts only outside fields, while F1 counts everywhere.

const field = (tag: string, attrs: Record<string, string> = {}) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  document.body.appendChild(el);
  return el;
};

afterEach(() => {
  document.body.innerHTML = '';
});

describe('isHelpShortcut', () => {
  it('takes ? and F1 on the page', () => {
    expect(isHelpShortcut({ key: '?', target: document.body })).toBe(true);
    expect(isHelpShortcut({ key: 'F1', target: document.body })).toBe(true);
  });

  it('ignores other keys', () => {
    for (const key of ['/', 'h', 'Escape', 'F2', 'j']) {
      expect(isHelpShortcut({ key, target: document.body })).toBe(false);
    }
  });

  it('leaves ? alone where text is typed, but not F1', () => {
    for (const el of [
      field('input'),
      field('textarea'),
      field('select'),
      field('div', { contenteditable: 'true' }),
    ]) {
      expect(isHelpShortcut({ key: '?', target: el })).toBe(false);
      expect(isHelpShortcut({ key: 'F1', target: el })).toBe(true);
    }
  });

  it('counts a field by any ancestor that is editable', () => {
    const editor = field('div', { contenteditable: '' });
    const span = document.createElement('span');
    editor.appendChild(span);
    expect(typingIn(span)).toBe(true);
    expect(isHelpShortcut({ key: '?', target: span })).toBe(false);
  });

  it('leaves a key with Ctrl, Alt or Meta, or one already handled, to its owner', () => {
    expect(isHelpShortcut({ key: '?', ctrlKey: true, target: document.body })).toBe(false);
    expect(isHelpShortcut({ key: 'F1', altKey: true, target: document.body })).toBe(false);
    expect(isHelpShortcut({ key: '?', metaKey: true, target: document.body })).toBe(false);
    expect(isHelpShortcut({ key: 'F1', defaultPrevented: true, target: document.body })).toBe(false);
  });
});
