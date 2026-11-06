import { detectPriority } from './artifactPriority';

// Priority detection reads the strongest normative keyword in the text.
describe('detectPriority', () => {
  it.each([
    ['The hoist shall lift 2 t', 'must', 'shall'],
    ['Loads must not tilt', 'must', 'must'],
    ['The operator is required to sign off', 'must', 'is required to'],
    ['The jig should be painted', 'should', 'should'],
    ['The cell may include a second positioner', 'could', 'may'],
    ['It shall weigh under 5 kg and should be blue', 'must', 'shall'],
    ['SHOULD match case-insensitively', 'should', 'should'],
  ])('%s → %s', (text, priority, word) => {
    expect(detectPriority(text)).toEqual({ priority, word });
  });
  it('finds nothing without a keyword, or inside another word', () => {
    expect(detectPriority('Lifting points are balanced')).toBeNull();
    expect(detectPriority('Marshall the parts; mayonnaise')).toBeNull();
  });
});
