import { describe, expect, it } from 'vitest';
import { pickTask } from './planSelection';

const order = ['a', 'b', 'c', 'd', 'e'];
const none = { ids: [], anchor: '' };

describe('pickTask', () => {
  it('picks one task on a plain click, dropping the rest', () => {
    expect(pickTask(order, { ids: ['a', 'b'], anchor: 'b' }, 'd')).toEqual({ ids: ['d'], anchor: 'd' });
  });

  it('adds and drops a task with Ctrl or Cmd', () => {
    const two = pickTask(order, { ids: ['a'], anchor: 'a' }, 'c', { ctrlKey: true });
    expect(two).toEqual({ ids: ['a', 'c'], anchor: 'c' });
    expect(pickTask(order, two, 'a', { metaKey: true }).ids).toEqual(['c']);
  });

  it('takes the run from the anchor with Shift, either way round', () => {
    expect(pickTask(order, { ids: ['b'], anchor: 'b' }, 'd', { shiftKey: true }).ids).toEqual(['b', 'c', 'd']);
    expect(pickTask(order, { ids: ['d'], anchor: 'd' }, 'b', { shiftKey: true })).toEqual({ ids: ['b', 'c', 'd'], anchor: 'd' });
  });

  it('keeps the anchor through Shift-clicks, so the run is redrawn from it', () => {
    const first = pickTask(order, { ids: ['b'], anchor: 'b' }, 'e', { shiftKey: true });
    expect(pickTask(order, first, 'c', { shiftKey: true }).ids).toEqual(['b', 'c']);
  });

  it('adds the run to what is picked with Ctrl and Shift', () => {
    expect(pickTask(order, { ids: ['a', 'c'], anchor: 'c' }, 'e', { ctrlKey: true, shiftKey: true }).ids).toEqual(['a', 'c', 'd', 'e']);
  });

  it('picks the one task when Shift has nothing to run from', () => {
    expect(pickTask(order, none, 'c', { shiftKey: true })).toEqual({ ids: ['c'], anchor: 'c' });
  });
});
