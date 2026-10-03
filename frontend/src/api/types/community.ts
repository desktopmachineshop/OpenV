// The types api/community.ts sends and receives.
// What members share: shared products, share links and the open-source
// showcase.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import type { ProjectExport } from './projects';

/**
 * The community pool of joke demo products for the new-project wizard.
 *
 * This is the one API surface in OpenV whose reads and writes cross
 * workspaces on purpose: everyone sees the same pool, so it grows as people
 * share what their agents invent. The server sanitizes every entry to inert
 * plain text, rate-limits publishing per workspace, and returns no author
 * identity — `report` is the path for anything that should not be there.
 */
/** What a vote or unvote settles on: the entry's counts and your own vote. */
export interface SharedProductVotes {
  votes: number;
  votes_week: number;
  voted: boolean;
}

/** How the pool is ordered: newest first, or the two vote leaderboards. */
export type SharedProductSort = 'recent' | 'top' | 'top_week';

// --- Share links (REQ-149) and the open-source showcase (REQ-151) ----------

export type ShareLinkRole = 'public' | 'reviewer';

export interface ShareLink {
  id: string;
  project_id: string;
  role: ShareLinkRole;
  label: string;
  created_by?: string;
  created_at: string;
  expires_at?: string | null;
  revoked_at?: string | null;
  // Only on the answer that minted the link: the token is stored hashed
  // and cannot be shown again.
  token?: string;
  url?: string;
}

/** What a share link, or the open-source page, shows of a project. */
export interface SharedProject {
  role: 'public' | 'reviewer';
  project: { id: string; name: string; description: string };
  workspace: string;
  baseline?: { id: string; name: string; created_at: string };
  counts: Record<string, number>;
  // The read-only snapshot; absent on a reviewer link, which opens the app.
  snapshot?: ProjectExport;
}

export interface OpenSourceProject {
  project_id: string;
  name: string;
  description: string;
  workspace: string;
  baseline_id: string;
  baseline: string;
  snapshot_at: string;
  counts: Record<string, number>;
}
