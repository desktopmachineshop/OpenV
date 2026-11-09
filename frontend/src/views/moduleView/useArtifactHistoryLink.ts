import { useEffect, useState } from 'react';
import { artifactAPI, type Artifact } from '../../api/client';

/**
 * Opening an artifact at a point in its history, from a link that says when.
 *
 * The soft delete keeps every version of an artifact (REQ-4), and a link
 * write versions both its ends with a snapshot of their links, so the
 * project's history can answer "what did this look like then":
 *
 * - `?artifact=` naming a deleted artifact opens it from its versions,
 *   read-only and marked deleted, instead of an empty page.
 * - `&link=<id>&at=<time>` (an Activity line for a removed link) previews the
 *   source artifact as it was while it still had that link: the newest
 *   version up to that time whose link snapshot holds it.
 * - `&at=<time>` alone previews the version that was current then.
 *
 * Nothing is fetched for an ordinary link to a live artifact.
 */

type Version = Pick<Artifact, 'version' | 'valid_from' | 'valid_to' | 'attributes'>;

const snapshotHas = (v: Version, linkId: string): boolean =>
  Array.isArray(v.attributes?.links_snapshot) &&
  v.attributes.links_snapshot.some((l: { id?: string } | null) => l?.id === linkId);

/**
 * The version a link means, or null for "the current one". Versions written a
 * moment after the event (a link write versions both ends before it is
 * published) still count, so a second of slack is allowed.
 */
export const pickVersion = <V extends Version>(versions: V[], linkId?: string | null, at?: string | null): V | null => {
  const t = at ? Date.parse(at) : NaN;
  const sorted = [...versions].sort((a, b) => b.version - a.version);
  const upTo = Number.isNaN(t) ? sorted : sorted.filter((v) => Date.parse(v.valid_from) <= t + 1000);
  if (linkId) {
    const holding = upTo.find((v) => snapshotHas(v, linkId));
    if (holding) return holding;
  }
  if (!Number.isNaN(t)) {
    return upTo.find((v) => !v.valid_to || Date.parse(v.valid_to) > t) || upTo[0] || null;
  }
  return null;
};

interface Options {
  artifactId: string | null;
  /** Whether the project's live artifacts include it. */
  live: boolean;
  linkId?: string | null;
  at?: string | null;
  /**
   * Off in a baseline, which has its own copy of every artifact, and until
   * the project's artifacts have loaded, so a live one is not mistaken for
   * one missing from the list.
   */
  enabled: boolean;
  setPreviewVersion: (v: Artifact | null) => void;
}

/** The deleted artifact the URL names, once its history has been read; null otherwise. */
export const useArtifactHistoryLink = ({ artifactId, live, linkId, at, enabled, setPreviewVersion }: Options): Artifact | null => {
  const [deleted, setDeleted] = useState<Artifact | null>(null);
  useEffect(() => {
    setDeleted(null);
    if (!enabled || !artifactId || (live && !linkId && !at)) return undefined;
    let gone = false;
    artifactAPI
      .getVersions(artifactId)
      .then((res) => {
        if (gone) return;
        const versions = [...(Array.isArray(res.data) ? res.data : [])].sort((a, b) => b.version - a.version);
        const latest = versions[0];
        // A newest version still open is a live artifact the list has not
        // brought in yet: nothing to do here.
        if (!latest) return;
        const isDeleted = Boolean(latest.valid_to);
        if (!live && !isDeleted) return;
        const chosen = pickVersion(versions, linkId, at);
        if (isDeleted) {
          setDeleted(latest);
          setPreviewVersion(chosen && chosen.version !== latest.version ? chosen : null);
        } else if (chosen && chosen.valid_to) {
          setPreviewVersion(chosen);
        }
      })
      .catch(() => undefined);
    return () => {
      gone = true;
    };
    // setPreviewVersion is a state setter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifactId, live, linkId, at, enabled]);
  return deleted;
};
