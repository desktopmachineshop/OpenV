import { useEffect, useState } from 'react';
import { releasesAPI, TARGET_RELEASE_ATTR, TARGET_RELEASE_UNTIL_ATTR, type Artifact, type TargetRelease } from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { TARGET_RELEASES_FEATURE } from '../features';

// The Releases fact in the artifact detail panel's row of facts (Type,
// Priority, Owner, Verify by, …): the artifact's own release range, named as
// the Releases page and the editor name it ("MVP onwards", "MVP to V1.0",
// "V1.0 only"). An id the project doesn't have counts as unset, so it shows
// nothing; nor does an artifact that sets no range of its own (the range it
// inherits from a heading is on the Releases page and in its editor).
//
// A project's releases are read once per project and kept for the visit:
// the panel is redrawn on every selection.

const cache = new Map<string, Promise<TargetRelease[]>>();

const releasesOf = (projectId: string): Promise<TargetRelease[]> => {
  let hit = cache.get(projectId);
  if (!hit) {
    hit = releasesAPI
      .list(projectId)
      .then((res) => res.data || [])
      .catch(() => {
        cache.delete(projectId);
        return [];
      });
    cache.set(projectId, hit);
  }
  return hit;
};

/** Forget a project's releases, after they change (and between tests). */
export const forgetReleases = (projectId?: string) => (projectId ? cache.delete(projectId) : cache.clear());

const str = (v: unknown) => (typeof v === 'string' ? v : '');

/** The range in words, or '' when the artifact names no release the project has. */
export const releaseRangeLabel = (attributes: Record<string, unknown> | undefined, releases: TargetRelease[]): string => {
  const from = releases.find((r) => r.id === str(attributes?.[TARGET_RELEASE_ATTR]));
  if (!from) return '';
  const until = releases.find((r) => r.id === str(attributes?.[TARGET_RELEASE_UNTIL_ATTR]));
  if (!until || releases.indexOf(until) < releases.indexOf(from)) return `${from.name} onwards`;
  return until.id === from.id ? `${from.name} only` : `${from.name} to ${until.name}`;
};

/** The artifact's Releases fact, or '' where target releases are off or it sets none. */
export const useReleaseRangeLabel = (artifact: Artifact): string => {
  const on = useFeature(TARGET_RELEASES_FEATURE);
  const [releases, setReleases] = useState<TargetRelease[]>([]);
  const projectId = artifact.project_id;
  const targets = Boolean(str(artifact.attributes?.[TARGET_RELEASE_ATTR]));
  useEffect(() => {
    if (!on || !projectId || !targets) return undefined;
    let live = true;
    void releasesOf(projectId).then((list) => live && setReleases(list));
    return () => {
      live = false;
    };
  }, [on, projectId, targets]);
  return on && targets ? releaseRangeLabel(artifact.attributes, releases) : '';
};
