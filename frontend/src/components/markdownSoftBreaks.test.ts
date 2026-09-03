import { splitSoftBreaks } from './markdownSoftBreaks';

// Markdown folds a single newline into a space, so requirement text typed as
// separate lines rendered as one run-on paragraph. The writer's lines are what
// the document says, so they survive.
describe('splitSoftBreaks', () => {
  it('leaves a single line as one text node', () => {
    expect(splitSoftBreaks('one line')).toEqual([{ type: 'text', value: 'one line' }]);
  });

  it('turns each newline into a break between the lines', () => {
    expect(splitSoftBreaks('first\nsecond')).toEqual([
      { type: 'text', value: 'first' },
      { type: 'break' },
      { type: 'text', value: 'second' },
    ]);
  });

  it('keeps consecutive newlines as consecutive breaks without empty text', () => {
    const got = splitSoftBreaks('a\n\nb');
    expect(got).toEqual([
      { type: 'text', value: 'a' },
      { type: 'break' },
      { type: 'break' },
      { type: 'text', value: 'b' },
    ]);
    expect(got.some((n) => n.type === 'text' && n.value === '')).toBe(false);
  });

  it('handles a leading and a trailing newline', () => {
    expect(splitSoftBreaks('\nx')).toEqual([{ type: 'break' }, { type: 'text', value: 'x' }]);
    expect(splitSoftBreaks('x\n')).toEqual([{ type: 'text', value: 'x' }, { type: 'break' }]);
  });
});
