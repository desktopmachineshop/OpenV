// The types api/platform.ts sends and receives.
// The platform itself: the release a workspace runs and its notes, and
// the site administrator's views of workspaces and users. A declaration
// the F1 spec does not name joins this module (or its types module).
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import type { Org } from './orgs';

// The running release as the API parsed it out of the notes it was built
// with: the version, its bullets grouped for a reader, and every earlier
// release for the What's new page. Uncached, so an open tab can notice a
// newer release behind the same URL.
/** One group of notes within a release: New features, Bug fixes, and so on. */
export interface ReleaseCategory {
  name: string;
  notes: string[];
}

/** One release, as the server parsed it out of the notes it was built with. */
export interface ReleaseEntry {
  version: string;
  date: string;
  notes: string[];
  categories: ReleaseCategory[];
  markdown: string;
  /** Set when the release is a stable release: the day it became one. */
  stable_since?: string;
}

/**
 * The newest stable release (docs/release-policy.md): one of the releases,
 * designated once it has soaked, with the notes of every release since the
 * previous stable one merged group by group.
 */
export interface StableRelease {
  version: string;
  since: string;
  previous: string;
  notes: string[];
  categories: ReleaseCategory[];
}

export interface ReleaseInfo {
  version: string;
  date: string;
  notes: string[];
  categories: ReleaseCategory[];
  markdown: string;
  /**
   * Every release, newest first. The server sends the parsed sections, not
   * the notes file: the file also carries what has not shipped yet, which is
   * nobody's business but a contributor's.
   */
  releases: ReleaseEntry[];
  /** The newest stable release; null until one is designated. */
  stable: StableRelease | null;
  /** Whether this is the shared service or a dedicated instance. */
  deployment: 'shared' | 'dedicated';
}

// --- Platform administration (REQ-154, REQ-155) -----------------------------

export interface AdminWorkspace extends Org {
  members: number;
}

export interface AdminUser {
  id: string;
  name: string;
  email: string;
  auth_provider: string;
  is_admin: boolean;
  created_at: string;
}
