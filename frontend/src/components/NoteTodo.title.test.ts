// The router is mocked as it is in the view tests: this suite exercises a
// pure function and Link never renders here, so an inert stand-in keeps the
// module graph small.
vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
}));

import { noteTitle } from './NoteTodo';

describe('noteTitle', () => {
  it('uses the note as-is when it is already short', () => {
    expect(noteTitle('Check the seal spec')).toBe('Check the seal spec');
  });

  it('takes only the first line', () => {
    // A note is often a paragraph and a list; the to-do wants the ask.
    expect(noteTitle('Check the seal spec\n\n- the O-ring\n- the groove')).toBe(
      'Check the seal spec'
    );
  });

  it('trims surrounding whitespace', () => {
    expect(noteTitle('   Check the seal spec  \n')).toBe('Check the seal spec');
  });

  it('cuts a long note at a word boundary and marks it', () => {
    const long = `${'word '.repeat(60)}end`;
    const title = noteTitle(long);

    expect(title.length).toBeLessThanOrEqual(121);
    expect(title.endsWith('…')).toBe(true);
    // Cutting mid-word reads as a bug, so the break lands on a space.
    expect(title.slice(0, -1).endsWith('word')).toBe(true);
  });

  it('still cuts a single unbroken run of characters', () => {
    // No space to break on: better a hard cut than a title that is one
    // 400-character token.
    const title = noteTitle('x'.repeat(400));

    expect(title.endsWith('…')).toBe(true);
    expect(title.length).toBe(121);
  });

  it('survives an empty note', () => {
    expect(noteTitle('')).toBe('');
  });
});
