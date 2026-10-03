// Artifacts, trace links, review rounds, the chatter feed, requirement
// quality rules and findings, search and duplicate detection.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type {
  Artifact,
  ArtifactStatus,
  ChatterEntry,
  DuplicatesResponse,
  Link,
  LinkedArtifact,
  QualityReport,
  QualityRuleSet,
  QualityRules,
  QualityScore,
  ReviewQueue,
  ReviewRoundResult,
  SearchMode,
  SearchResponse,
} from './types/artifacts';

/** How a foreign artifact is cited: "Landing gear / REQ-12". */
export const qualifiedRef = (l: LinkedArtifact): string =>
  `${l.project_name ? `${l.project_name} / ` : ''}${l.ref || l.id.substring(0, 8)}`;

// Server-side page size for artifact listings (the backend defaults to and
// caps at 1000 per request; the total rides on the X-Total-Count header).
const ARTIFACT_PAGE_LIMIT = 1000;

export const artifactAPI = {
  // copied_from names the artifact a duplicate or a paste came from; the
  // server opens the new artifact's feed with a "Copied from REQ-12" note.
  create: (payload: Partial<Artifact> & { copied_from?: string }) =>
    client.post<Artifact>('/api/v1/artifacts', payload),
  get: (id: string) =>
    client.get<Artifact>(`/api/v1/artifacts/${id}`),
  // One page of a project's artifacts in stable tree order. The response body
  // is a plain array; res.headers['x-total-count'] carries the total so
  // callers can page until exhaustion.
  listPage: (
    projectId: string,
    opts?: { type?: string; owner?: string; limit?: number; offset?: number; docNumbers?: boolean }
  ) =>
    client.get<Artifact[]>('/api/v1/artifacts', {
      params: {
        project_id: projectId,
        type: opts?.type,
        owner: opts?.owner,
        limit: opts?.limit,
        offset: opts?.offset,
        // Opt-in: computing section numbers reads the whole project, which a
        // plain paginated fetch has no reason to pay for.
        doc_numbers: opts?.docNumbers ? '1' : undefined,
      },
    }),
  // All of a project's artifacts, fetched through the paged API. The module
  // view renders artifacts as a parent_id tree, so it structurally needs the
  // full set to build hierarchy; most projects fit in one page. UI follow-up
  // (issue #136): render the tree incrementally (lazy-load subtrees) so huge
  // projects don't need this loop at all.
  list: async (projectId: string, type?: string, owner?: string): Promise<{ data: Artifact[] }> => {
    const all: Artifact[] = [];
    for (;;) {
      const res = await client.get<Artifact[]>('/api/v1/artifacts', {
        params: {
          project_id: projectId,
          type,
          owner,
          limit: ARTIFACT_PAGE_LIMIT,
          offset: all.length,
          // This is the whole-project read the tree view renders, so it is
          // the one call that wants section numbers.
          doc_numbers: '1',
        },
      });
      const page = res.data || [];
      all.push(...page);
      const total = parseInt(res.headers?.['x-total-count'] ?? '', 10);
      // Stop on a short page, or once we have everything the server counted.
      // A server that doesn't send the header (older API) implies short-page
      // termination only.
      if (page.length < ARTIFACT_PAGE_LIMIT || (Number.isFinite(total) && all.length >= total)) {
        return { data: all };
      }
    }
  },
  update: (id: string, payload: Partial<Artifact>) =>
    client.put<Artifact>(`/api/v1/artifacts/${id}`, payload),
  // One review state-machine transition; the server enforces legality
  // (400 unknown status, 409 illegal transition, 403 insufficient role).
  changeStatus: (id: string, status: ArtifactStatus) =>
    client.put<Artifact>(`/api/v1/artifacts/${id}/status`, { status }),
  delete: (id: string) =>
    client.delete(`/api/v1/artifacts/${id}`),
  getVersions: (id: string) =>
    client.get<Artifact[]>(`/api/v1/artifacts/${id}/versions`),
  restoreVersion: (id: string, version: number) =>
    client.post<Artifact>(`/api/v1/artifacts/${id}/restore`, { version }),
};

export const linkAPI = {
  create: (payload: Partial<Link>) =>
    client.post<Link>('/api/v1/links', payload),
  get: (id: string) =>
    client.get<Link>(`/api/v1/links/${id}`),
  list: (projectId: string) =>
    client.get<Link[]>('/api/v1/links', { params: { project_id: projectId } }),
  listForArtifactVersion: (artifactId: string, version: number) =>
    client.get<Link[]>(`/api/v1/artifacts/${artifactId}/links`, { params: { version } }),
  // Live links straight from the link table (no version snapshot involved).
  // Use this for the artifact currently on screen: link writes version-bump
  // the counterpart artifact server-side, so a client-held version number can
  // be stale and a version-scoped fetch would miss fresh links (issue #169).
  listForArtifact: (artifactId: string) =>
    client.get<Link[]>(`/api/v1/artifacts/${artifactId}/links`),
  update: (id: string, payload: Partial<Link>) =>
    client.put<Link>(`/api/v1/links/${id}`, payload),
  // Clears the suspect flag: the caller vouches the link still holds after
  // the linked artifact's content changed. Editor role required.
  confirm: (id: string) =>
    client.put<Link>(`/api/v1/links/${id}/confirm`),
  delete: (id: string) =>
    client.delete(`/api/v1/links/${id}`),
};

export const reviewAPI = {
  // The reviewer's daily driver: suspect links + in-review artifacts for one
  // project, in a single round trip. Viewer role suffices to read it.
  get: (projectId: string) =>
    client.get<ReviewQueue>(`/api/v1/projects/${projectId}/review-queue`),
  // Start a review round for the whole project: every draft in scope goes to
  // in_review at once. Editor role. Safe to run again — an approved artifact
  // nobody has changed stays approved, and one edited since it was approved
  // is already back in draft, so the re-run picks up exactly what changed.
  // Omit types for the default scope (everything but headings and
  // descriptions).
  startRound: (projectId: string, types?: string[]) =>
    client.post<ReviewRoundResult>(`/api/v1/projects/${projectId}/review-round`, { types }),
};

export const chatterAPI = {
  create: (payload: { artifact_id: string; message: string }) =>
    client.post<ChatterEntry>('/api/v1/chatter', payload),
  list: (artifactId: string) =>
    client.get<ChatterEntry[]>('/api/v1/chatter', { params: { artifact_id: artifactId } }),
};

// An empty rule set clears the level's override, so it inherits again.
export const qualityRulesAPI = {
  forProject: (projectId: string) =>
    client.get<QualityRules>(`/api/v1/projects/${projectId}/quality-rules`),
  setForProject: (projectId: string, payload: QualityRuleSet) =>
    client.put<QualityRules>(`/api/v1/projects/${projectId}/quality-rules`, payload),
  forWorkspace: (orgId: string) =>
    client.get<QualityRules>(`/api/v1/orgs/${orgId}/quality-rules`),
  setForWorkspace: (orgId: string, payload: QualityRuleSet) =>
    client.put<QualityRules>(`/api/v1/orgs/${orgId}/quality-rules`, payload),
};

export const qualityAPI = {
  // Per-requirement quality scores + findings for a project (viewer role).
  project: (projectId: string, baselineId?: string) =>
    client.get<QualityReport>(`/api/v1/projects/${projectId}/quality`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  // Single-artifact lint (400 for non-requirement types).
  artifact: (artifactId: string) =>
    client.get<QualityScore>(`/api/v1/artifacts/${artifactId}/quality`),
};

export const searchAPI = {
  global: (q: string, opts?: { limit?: number; mode?: SearchMode }) =>
    client.get<SearchResponse>('/api/v1/search', {
      params: {
        q,
        ...(opts?.limit ? { limit: opts.limit } : {}),
        ...(opts?.mode ? { mode: opts.mode } : {}),
      },
    }),
};

export const duplicatesAPI = {
  forProject: (projectId: string) =>
    client.get<DuplicatesResponse>(`/api/v1/projects/${projectId}/duplicates`),
};
