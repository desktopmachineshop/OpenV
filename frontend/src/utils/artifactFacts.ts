import type { Artifact, ArtifactStatus, CoverageEntry, Link } from '../api/client';
import type { ChipTone } from '../components/ui/Chip';
import { ARTIFACT_TYPE_CATALOG } from '../generated/contract';

// What the requirements views show about an artifact beside its title: its
// review status, priority, trace counts (with suspect links), verification
// state and when it last changed. Derived on the client from data the module
// already holds (the artifacts, the links, the quality scores) and the V&V
// coverage report, so the table, the tree, the layers and the detail panel
// all read the same answer.

/** A row's quality summary, as the module loads it (issue #217). */
export interface QualityFacts {
  score: number;
  band: string;
  findingCount: number;
}

export const STATUS_META: Record<ArtifactStatus, { label: string; tone: ChipTone }> = {
  draft: { label: 'Draft', tone: 'neutral' },
  in_review: { label: 'In review', tone: 'info' },
  approved: { label: 'Approved', tone: 'success' },
  superseded: { label: 'Superseded', tone: 'neutral' },
};

/** The review status; legacy rows predate the column and use the attribute mirror. */
export const statusOf = (artifact: Artifact): ArtifactStatus => {
  const raw = artifact.status || artifact.attributes?.status;
  return raw && raw in STATUS_META ? (raw as ArtifactStatus) : 'draft';
};

/** The MoSCoW priority attribute, when the project uses one. */
export const priorityOf = (artifact: Artifact): string => {
  const raw = artifact.attributes?.priority;
  return typeof raw === 'string' || typeof raw === 'number' ? String(raw) : '';
};

/** The verification-method attribute, when set. */
export const verificationMethodOf = (artifact: Artifact): string => {
  const raw = artifact.attributes?.verification_method;
  return typeof raw === 'string' ? raw : '';
};

export const typeLabel = (type: string): string =>
  ARTIFACT_TYPE_CATALOG.find((t) => t.value === type)?.label ?? type;

/** A short tag for an artifact type, as the layers and level views mark cards. */
export const typeTag = (type: string): string => {
  const short: Record<string, string> = {
    heading: 'SEC',
    description: 'DESC',
    persona: 'PER',
    'user-need': 'NEED',
    requirement: 'REQ',
    'design-item': 'DES',
    'test-case': 'TC',
    hazard: 'HAZ',
    other: 'OTH',
  };
  return short[type] ?? type.slice(0, 4).toUpperCase();
};

export const qualityTone = (band: string): ChipTone =>
  band === 'good' ? 'success' : band === 'fair' ? 'warning' : 'danger';

// Which way a link runs from one of its ends. Most link types point from the
// artifact that answers to the one it answers to (a test verifies a
// requirement, a requirement derives from a need), so the far end of an
// outgoing link sits above; decomposes-to and impacts point downwards.
const DOWNWARD_TYPES = new Set(['decomposes-to', 'impacts']);

export type TraceDirection = 'up' | 'down' | 'peer';

export const traceDirection = (link: Link, artifactId: string): TraceDirection => {
  if (link.type === 'relates-to') return 'peer';
  const outgoing = link.from_id === artifactId;
  const downward = DOWNWARD_TYPES.has(link.type);
  return outgoing === downward ? 'down' : 'up';
};

/** The far end of a link, seen from one of its ends. */
export const otherEnd = (link: Link, artifactId: string): string =>
  link.from_id === artifactId ? link.to_id : link.from_id;

/** A verification state, its words and the colour of its dot. */
export interface VVFacts {
  /** pass | fail | blocked | partial | unrun | uncovered | method-missing | verified-manually | none */
  status: string;
  label: string;
  /** "passing/total" below an artifact, for a section or a parent. */
  count?: string;
}

export const vvColor = (status: string): string => {
  switch (status) {
    case 'pass':
    case 'verified-manually':
      return 'var(--success)';
    case 'fail':
      return 'var(--danger)';
    case 'blocked':
    case 'method-missing':
      return 'var(--warning)';
    case 'partial':
      return 'var(--accent)';
    default:
      return 'var(--neutral)';
  }
};

const PASSING = new Set(['pass', 'verified-manually']);

export interface RowFacts {
  up: number;
  down: number;
  suspect: number;
  vv: VVFacts;
  quality?: QualityFacts;
}

export interface FactsIndex {
  get: (artifactId: string) => RowFacts;
  /** The latest result of a test case in any run, from the coverage report. */
  testResult: (testCaseId: string) => string;
  /** The requirement's coverage entry, when it has one. */
  coverage: (artifactId: string) => CoverageEntry | undefined;
  /** The links touching an artifact. */
  linksOf: (artifactId: string) => Link[];
  /** Requirements below an artifact (itself included) that carry a rollup. */
  verifiedBelow: (artifactId: string) => { passing: number; total: number; failing: number; blocked: number };
}

const EMPTY_VV: VVFacts = { status: 'none', label: '—' };

/**
 * Index what the rows show, once per render of the module: artifacts, the
 * links between them, the quality scores by artifact id and the coverage
 * report's entries.
 */
export const buildFacts = (
  artifacts: Artifact[],
  links: Link[],
  quality: Record<string, QualityFacts> | undefined,
  coverage: CoverageEntry[]
): FactsIndex => {
  const byArtifact = new Map<string, Link[]>();
  for (const link of links) {
    for (const end of [link.from_id, link.to_id]) {
      const list = byArtifact.get(end);
      if (list) list.push(link);
      else byArtifact.set(end, [link]);
    }
  }
  const entries = new Map(coverage.map((e) => [e.requirement_id, e]));
  const results = new Map<string, string>();
  for (const entry of coverage) {
    for (const [tc, result] of Object.entries(entry.latest_results || {})) results.set(tc, result);
  }
  const children = new Map<string | null, Artifact[]>();
  for (const a of artifacts) {
    const key = a.parent_id ?? null;
    const list = children.get(key);
    if (list) list.push(a);
    else children.set(key, [a]);
  }

  const below = new Map<string, { passing: number; total: number; failing: number; blocked: number }>();
  const verifiedBelow = (id: string) => {
    const known = below.get(id);
    if (known) return known;
    const tally = { passing: 0, total: 0, failing: 0, blocked: 0 };
    below.set(id, tally);
    const own = entries.get(id);
    if (own) {
      tally.total += 1;
      if (PASSING.has(own.rollup)) tally.passing += 1;
      else if (own.rollup === 'fail') tally.failing += 1;
      else if (own.rollup === 'blocked') tally.blocked += 1;
    }
    for (const child of children.get(id) || []) {
      const sub = verifiedBelow(child.id);
      tally.passing += sub.passing;
      tally.total += sub.total;
      tally.failing += sub.failing;
      tally.blocked += sub.blocked;
    }
    return tally;
  };

  const typeOf = new Map(artifacts.map((a) => [a.id, a.type]));
  const vvOf = (id: string): VVFacts => {
    const own = entries.get(id);
    if (own) return { status: own.rollup, label: own.rollup };
    if (typeOf.get(id) === 'test-case') {
      const result = results.get(id);
      return result ? { status: result, label: result } : { status: 'unrun', label: 'not run' };
    }
    const tally = verifiedBelow(id);
    if (tally.total === 0) return EMPTY_VV;
    const status = tally.failing
      ? 'fail'
      : tally.passing === tally.total
        ? 'pass'
        : tally.blocked
          ? 'blocked'
          : tally.passing
            ? 'partial'
            : 'unrun';
    return { status, label: status === 'unrun' ? 'not run' : status, count: `${tally.passing}/${tally.total}` };
  };

  const cache = new Map<string, RowFacts>();
  return {
    get: (id) => {
      const known = cache.get(id);
      if (known) return known;
      let up = 0;
      let down = 0;
      let suspect = 0;
      for (const link of byArtifact.get(id) || []) {
        const dir = traceDirection(link, id);
        if (dir === 'up') up += 1;
        else if (dir === 'down') down += 1;
        if (link.suspect) suspect += 1;
      }
      const facts = { up, down, suspect, vv: vvOf(id), quality: quality?.[id] };
      cache.set(id, facts);
      return facts;
    },
    testResult: (id) => results.get(id) || 'not-run',
    coverage: (id) => entries.get(id),
    linksOf: (id) => byArtifact.get(id) || [],
    verifiedBelow,
  };
};

/** The figures above the list: how much there is, and how much of it holds up. */
export interface ModuleFigures {
  artifacts: number;
  tracedPct: number | null;
  verifiedPct: number | null;
  suspectLinks: number;
  qualityFindings: number;
}

export const moduleFigures = (
  artifacts: Artifact[],
  links: Link[],
  quality: Record<string, QualityFacts> | undefined,
  coverage: CoverageEntry[]
): ModuleFigures => {
  const content = artifacts.filter((a) => a.type !== 'heading' && a.type !== 'description');
  const linked = new Set<string>();
  links.forEach((l) => {
    linked.add(l.from_id);
    linked.add(l.to_id);
  });
  const traced = content.filter((a) => linked.has(a.id)).length;
  const ids = new Set(artifacts.map((a) => a.id));
  const entries = coverage.filter((e) => ids.has(e.requirement_id));
  const verified = entries.filter((e) => PASSING.has(e.rollup)).length;
  return {
    artifacts: artifacts.length,
    tracedPct: content.length ? Math.round((traced / content.length) * 100) : null,
    verifiedPct: entries.length ? Math.round((verified / entries.length) * 100) : null,
    suspectLinks: links.filter((l) => l.suspect).length,
    qualityFindings: Object.entries(quality || {})
      .filter(([id]) => ids.has(id))
      .reduce((sum, [, q]) => sum + q.findingCount, 0),
  };
};

/** "2h", "3d", "5w": how long ago, as the dense rows say it. */
export const ago = (iso: string | undefined, now: number = Date.now()): string => {
  const t = iso ? Date.parse(iso) : NaN;
  if (Number.isNaN(t)) return '';
  const minutes = Math.max(0, Math.round((now - t) / 60000));
  if (minutes < 60) return `${Math.max(1, minutes)}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.round(hours / 24);
  if (days < 14) return `${days}d`;
  const weeks = Math.round(days / 7);
  if (weeks < 9) return `${weeks}w`;
  const date = new Date(t);
  return date.getFullYear() === new Date(now).getFullYear()
    ? date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    : date.toLocaleDateString(undefined, { month: 'short', year: 'numeric' });
};
