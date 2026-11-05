import type { FactsIndex } from '../../utils/artifactFacts';
import { STATUS_META, priorityOf, statusOf, typeLabel } from '../../utils/artifactFacts';
import { ARTIFACT_TYPE_CATALOG } from '../../generated/contract';
import type { SearchAccessors, SearchOption, SearchSchema } from '../../components/search/searchValue';
import type { Artifact } from './shared';

// The requirements list's search schema (issue #722): what its chip search
// bar offers, built from the artifacts in view, and how a chip reads an
// artifact. The bar and its helpers do the rest (components/search).

/** The scope the list's saved searches are stored under. */
export const LIST_SEARCH_SCOPE = 'requirements';

/** The attributes the list reads as fields of their own, not as custom ones. */
const KNOWN_ATTRIBUTES = new Set(['owner', 'priority', 'status', 'verification_method', 'target_release', 'target_release_until']);

const ownerOf = (a: Artifact): string => (typeof a.attributes?.owner === 'string' ? a.attributes.owner : '');

/** The heading an artifact sits under, nearest first; undefined at the top. */
export function sectionOf(a: Artifact, byId: Map<string, Artifact>): Artifact | undefined {
  let parent = a.parent_id ? byId.get(a.parent_id) : undefined;
  while (parent && parent.type !== 'heading') parent = parent.parent_id ? byId.get(parent.parent_id) : undefined;
  return parent;
}

const attributeText = (v: unknown): string => (v == null ? '' : typeof v === 'string' ? v : typeof v === 'number' || typeof v === 'boolean' ? String(v) : JSON.stringify(v));

const sortedOptions = (values: Iterable<string>, label: (v: string) => string = (v) => v): SearchOption[] =>
  Array.from(new Set(values))
    .filter(Boolean)
    .map((value) => ({ value, label: label(value) }))
    .sort((a, b) => a.label.localeCompare(b.label));

/** The list's schema over the project's artifacts. */
export function buildListSchema(artifacts: Artifact[]): SearchSchema {
  const present = new Set(artifacts.map((a) => a.type));
  const types = ARTIFACT_TYPE_CATALOG.filter((t) => present.has(t.value)).map((t) => ({ value: t.value as string, label: t.label as string }));
  for (const t of present) if (!types.some((o) => o.value === t)) types.push({ value: t, label: typeLabel(t) });
  const priorityOrder = ['must', 'should', 'could', "won't", 'wont'];
  const priorities = sortedOptions(artifacts.map(priorityOf)).sort((a, b) => {
    const ia = priorityOrder.indexOf(a.value.toLowerCase());
    const ib = priorityOrder.indexOf(b.value.toLowerCase());
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.label.localeCompare(b.label);
  });
  const headings = artifacts
    .filter((a) => a.type === 'heading')
    .map((h) => ({ value: h.id, label: [h.doc_number, h.title].filter(Boolean).join(' ') }));
  const attributes = Array.from(
    new Set(artifacts.flatMap((a) => Object.keys(a.attributes || {}).filter((k) => !KNOWN_ATTRIBUTES.has(k))))
  ).sort();
  return {
    fields: [
      { key: 'status', label: 'Status', type: 'enum', options: Object.entries(STATUS_META).map(([value, m]) => ({ value, label: m.label })) },
      { key: 'type', label: 'Type', type: 'enum', options: types },
      { key: 'priority', label: 'Priority', type: 'enum', options: priorities },
      { key: 'owner', label: 'Owner', type: 'person', options: sortedOptions(artifacts.map(ownerOf)) },
      { key: 'title', label: 'Title', type: 'text' },
      { key: 'ref', label: 'Ref', type: 'ref' },
      { key: 'body', label: 'Text', type: 'text', suggest: false },
      { key: 'parent', label: 'Heading', type: 'enum', options: headings, inMenu: false },
      { key: 'created', label: 'Created', type: 'date' },
      { key: 'updated', label: 'Updated', type: 'date' },
      ...attributes.map((k) => ({ key: `attr:${k}`, label: k.replace(/_/g, ' '), type: 'text' as const, suggest: false })),
    ],
    quickFilters: [
      { key: 'mine', label: 'Mine' },
      { key: 'in_review', label: 'In review' },
      { key: 'failing', label: 'Failing' },
      { key: 'suspect', label: 'Suspect' },
    ],
    groupBy: [
      { key: 'status', label: 'Status' },
      { key: 'type', label: 'Type' },
      { key: 'priority', label: 'Priority' },
      { key: 'owner', label: 'Owner' },
      { key: 'parent', label: 'Heading' },
    ],
    customGroups: attributes.map((k) => ({ key: `attr:${k}`, label: k.replace(/_/g, ' ') })),
    savedScope: LIST_SEARCH_SCOPE,
  };
}

/**
 * How the list's chips read an artifact. me holds the signed-in member's
 * name and email, which Mine matches an owner against (without regard to
 * case), as the owner field holds whichever the editor offered.
 */
export function listAccessors(artifacts: Artifact[], facts: FactsIndex, me: string[]): SearchAccessors<Artifact> & {
  groupValue: (a: Artifact, key: string) => string;
} {
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const mine = me.map((m) => m.trim().toLowerCase()).filter(Boolean);
  const fieldValue = (a: Artifact, field: string): string => {
    switch (field) {
      case 'status':
        return statusOf(a);
      case 'type':
        return a.type;
      case 'priority':
        return priorityOf(a);
      case 'owner':
        return ownerOf(a);
      case 'title':
        return a.title;
      case 'ref':
        return a.ref ?? '';
      case 'body':
        return a.body || '';
      case 'parent':
        return sectionOf(a, byId)?.id ?? '';
      case 'created':
        return a.created_at || '';
      case 'updated':
        return a.updated_at || '';
      default:
        return field.startsWith('attr:') ? attributeText(a.attributes?.[field.slice(5)]) : '';
    }
  };
  return {
    fieldValue,
    groupValue: fieldValue,
    quickMatch: (a, key) => {
      switch (key) {
        case 'mine':
          return mine.includes(ownerOf(a).trim().toLowerCase()) && ownerOf(a) !== '';
        case 'in_review':
          return statusOf(a) === 'in_review';
        case 'failing':
          return facts.get(a.id).vv.status === 'fail';
        case 'suspect':
          return facts.get(a.id).suspect > 0;
        default:
          return true;
      }
    },
    textMatch: (a, text) =>
      [a.ref ?? '', a.title, a.body || '', a.type, JSON.stringify(a.attributes || {})].join(' ').toLowerCase().includes(text.toLowerCase()),
  };
}
