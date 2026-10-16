// The released history the public /changelog page renders.
//
// releases.json is generated from RELEASE_NOTES.md at the repository root by
// `python3 scripts/release_notes.py site`, which the release workflows run
// with every cut; CI's `site --check` fails when the two disagree. It holds
// released sections only, never Unreleased, so nothing reaches the public
// page before it ships. The image copies only frontend/, which is why the
// history is a file here rather than read from the notes at build time.
import data from './releases.json';

export interface ChangelogGroup {
  /** New features, Security fixes, Maintenance updates, Bug fixes, or Changes. */
  name: string;
  /** Each bullet as written: one sentence or two of markdown. */
  notes: string[];
}

export interface ChangelogRelease {
  /** A semantic version, or the date heading of a release from before versions. */
  version: string;
  /** The day it shipped, YYYY-MM-DD. */
  date: string;
  /** The day it became the stable release, or "". */
  stableSince: string;
  groups: ChangelogGroup[];
}

/** Newest first. */
export const RELEASES: readonly ChangelogRelease[] = data.releases;

/** The group name the parser gives the bullets of a release from before groups. */
export const UNGROUPED = 'Changes';

const SEMVER = /^\d+\.\d+\.\d+$/;

export const isNumbered = (release: ChangelogRelease): boolean => SEMVER.test(release.version);

/** The fragment that links to one release: #v0-21-4, or #u2026-09-12 for a dated one. */
export const releaseAnchor = (release: ChangelogRelease): string =>
  (isNumbered(release) ? 'v' : 'u') + release.version.replace(/\./g, '-');
