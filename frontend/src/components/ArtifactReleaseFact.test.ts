import { releaseRangeLabel } from './ArtifactReleaseFact';
import type { TargetRelease } from '../api/client';

// The Releases fact in the artifact detail panel names the artifact's own
// range as the Releases page does, and nothing for an id the project lacks.

const r = (id: string, name: string, position: number) => ({ id, name, position }) as unknown as TargetRelease;
const releases = [r('mvp', 'MVP', 0), r('v1', 'V1.0', 1), r('v2', 'V2', 2)];

describe('releaseRangeLabel', () => {
  it.each([
    [{ target_release: 'mvp' }, 'MVP onwards'],
    [{ target_release: 'v1', target_release_until: 'v1' }, 'V1.0 only'],
    [{ target_release: 'mvp', target_release_until: 'v2' }, 'MVP to V2'],
    [{ target_release: 'v2', target_release_until: 'mvp' }, 'V2 onwards'],
    [{ target_release: 'mvp', target_release_until: 'gone' }, 'MVP onwards'],
    [{ target_release: 'gone' }, ''],
    [{}, ''],
  ])('%j reads %j', (attributes, label) => {
    expect(releaseRangeLabel(attributes, releases)).toBe(label);
  });
});
