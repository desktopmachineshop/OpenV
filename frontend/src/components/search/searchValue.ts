import type { FeatureKey } from '../../generated/contract';
import type { ChipTone } from '../ui/Chip';

// The chip search bar's model (issue #722): what a page offers (its search
// schema), what the bar holds (its value: chips and free text), and the
// helpers a page uses to put the value in the URL, read it back, and filter
// and group its rows by it. The bar owns the UI and the state; the page
// decides what a chip means for its data, on the client (filterItems,
// groupItems) or as API parameters (chipValues).

/**
 * The chip search bar on the requirements list, and saving searches. Off,
 * the list keeps the search box it had. The top-bar search is a jump box
 * either way.
 */
export const CHIP_SEARCH_FEATURE: FeatureKey = 'chip-search';

/** A facet's kind, which decides how its chips match and how a value is picked. */
export type SearchFieldType = 'enum' | 'text' | 'date' | 'person' | 'ref';

export interface SearchOption {
  value: string;
  label: string;
}

/** One facet a page offers: status, type, owner, created… */
export interface SearchField {
  key: string;
  label: string;
  type: SearchFieldType;
  /** The values an enum (or a person) can take, in the order to show them. */
  options?: SearchOption[];
  /** Loads the options when the bar first needs them (a project list, the members). */
  loadOptions?: () => Promise<SearchOption[]>;
  /** The chip's colour; the filter colour when omitted. */
  tone?: ChipTone;
  /** Listed in the Filters column (enum and date fields); the default for those. */
  inMenu?: boolean;
  /** Offered as "Search <label> for: …" while typing; the default for text, person and ref. */
  suggest?: boolean;
}

/** A preset chip: Mine, In review, Failing… */
export interface SearchQuickFilter {
  key: string;
  label: string;
}

export interface SearchGroupOption {
  key: string;
  label: string;
}

/** A page's search schema: what its bar offers. */
export interface SearchSchema {
  fields: SearchField[];
  quickFilters?: SearchQuickFilter[];
  /** The group-by options; a page without grouping leaves it out and the column is hidden. */
  groupBy?: SearchGroupOption[];
  /** Any attribute to group by, under Custom group. */
  customGroups?: SearchGroupOption[];
  /**
   * The key the page's saved searches are stored under, per user; omitted,
   * the Favourites column is hidden.
   */
  savedScope?: string;
}

export type DateRange = 'today' | 'week' | 'month' | 'quarter' | 'year';

export const DATE_RANGES: Array<{ value: DateRange; label: string }> = [
  { value: 'today', label: 'Today' },
  { value: 'week', label: 'This week' },
  { value: 'month', label: 'This month' },
  { value: 'quarter', label: 'This quarter' },
  { value: 'year', label: 'This year' },
];

/** How a filter chip compares: an exact value, or a substring. */
export type ChipMatch = 'is' | 'contains';

export type SearchChip =
  | { kind: 'filter'; field: string; values: string[]; match: ChipMatch }
  | { kind: 'quick'; key: string }
  | { kind: 'group'; key: string }
  | { kind: 'saved'; id: string; name: string; value: SearchValue };

/** What the bar holds: its chips, in the order added, and the free text after them. */
export interface SearchValue {
  chips: SearchChip[];
  text: string;
}

export const EMPTY_SEARCH: SearchValue = { chips: [], text: '' };

export type ChipKind = SearchChip['kind'];

/** The colour of each kind of chip, so a filter, a grouping and a saved search read apart. */
export const KIND_TONES: Record<ChipKind, ChipTone> = {
  filter: 'info',
  quick: 'info',
  group: 'agent',
  saved: 'warning',
};

const defaultMatch = (field: SearchField): ChipMatch =>
  field.type === 'enum' || field.type === 'date' ? 'is' : 'contains';

export const fieldOf = (schema: SearchSchema, key: string): SearchField | undefined =>
  schema.fields.find((f) => f.key === key);

const sameChip = (a: SearchChip, b: SearchChip): boolean => {
  if (a.kind !== b.kind) return false;
  switch (a.kind) {
    case 'filter':
      return b.kind === 'filter' && a.field === b.field && a.match === b.match;
    case 'quick':
      return b.kind === 'quick' && a.key === b.key;
    case 'group':
      return b.kind === 'group' && a.key === b.key;
    case 'saved':
      return b.kind === 'saved' && a.id === b.id;
  }
};

/**
 * Adds a chip. A filter on a field and comparison the bar already holds joins
 * that chip ("Status: Draft or In review"), as a second value of the same
 * facet widens it; anything already there is not added twice.
 */
export function addChip(value: SearchValue, chip: SearchChip): SearchValue {
  const at = value.chips.findIndex((c) => sameChip(c, chip));
  if (at < 0) return { ...value, chips: [...value.chips, chip] };
  if (chip.kind !== 'filter') return value;
  const existing = value.chips[at] as Extract<SearchChip, { kind: 'filter' }>;
  const values = Array.from(new Set([...existing.values, ...chip.values]));
  if (values.length === existing.values.length) return value;
  const chips = value.chips.slice();
  chips[at] = { ...existing, values };
  return { ...value, chips };
}

export function removeChipAt(value: SearchValue, index: number): SearchValue {
  return { ...value, chips: value.chips.filter((_, i) => i !== index) };
}

/** A filter chip on an enum or date field for one value: adds it, or takes it out if there. */
export function toggleFieldValue(value: SearchValue, field: SearchField, v: string): SearchValue {
  const match = defaultMatch(field);
  const at = value.chips.findIndex((c) => c.kind === 'filter' && c.field === field.key && c.match === match);
  if (at < 0) return addChip(value, { kind: 'filter', field: field.key, values: [v], match });
  const chip = value.chips[at] as Extract<SearchChip, { kind: 'filter' }>;
  if (!chip.values.includes(v)) return addChip(value, { kind: 'filter', field: field.key, values: [v], match });
  const values = chip.values.filter((x) => x !== v);
  if (values.length === 0) return removeChipAt(value, at);
  const chips = value.chips.slice();
  chips[at] = { ...chip, values };
  return { ...value, chips };
}

export function hasFieldValue(value: SearchValue, field: SearchField, v: string): boolean {
  const match = defaultMatch(field);
  return value.chips.some((c) => c.kind === 'filter' && c.field === field.key && c.match === match && c.values.includes(v));
}

function toggleKeyed(value: SearchValue, kind: 'quick' | 'group', key: string): SearchValue {
  const at = value.chips.findIndex((c) => c.kind === kind && c.key === key);
  return at >= 0 ? removeChipAt(value, at) : addChip(value, { kind, key });
}

export const toggleQuick = (value: SearchValue, key: string) => toggleKeyed(value, 'quick', key);
export const toggleGroup = (value: SearchValue, key: string) => toggleKeyed(value, 'group', key);

export const hasQuick = (value: SearchValue, key: string) => value.chips.some((c) => c.kind === 'quick' && c.key === key);
export const hasGroup = (value: SearchValue, key: string) => value.chips.some((c) => c.kind === 'group' && c.key === key);

/** Whether the value narrows or groups anything. */
export const isActive = (value: SearchValue): boolean => value.chips.length > 0 || value.text.trim() !== '';

/**
 * The chips that filter, with each saved search's own chips in place of its
 * chip, and the free text (the bar's and any saved search's, joined). A
 * saved search inside a saved search is not followed further.
 */
export function effectiveValue(value: SearchValue): { chips: SearchChip[]; text: string } {
  const chips: SearchChip[] = [];
  const texts = [value.text.trim()];
  for (const chip of value.chips) {
    if (chip.kind === 'saved') {
      chips.push(...chip.value.chips.filter((c) => c.kind !== 'saved'));
      texts.push(chip.value.text.trim());
    } else {
      chips.push(chip);
    }
  }
  return { chips, text: texts.filter(Boolean).join(' ') };
}

/** The group-by keys in the order chosen, a saved search's included. */
export function groupKeys(value: SearchValue): string[] {
  return Array.from(new Set(effectiveValue(value).chips.flatMap((c) => (c.kind === 'group' ? [c.key] : []))));
}

/** The values the filter chips on a field ask for, for a page that filters through an API. */
export function chipValues(value: SearchValue, field: string): string[] {
  return Array.from(
    new Set(effectiveValue(value).chips.flatMap((c) => (c.kind === 'filter' && c.field === field ? c.values : [])))
  );
}

export const optionLabel = (field: SearchField | undefined, v: string): string =>
  field?.options?.find((o) => o.value === v)?.label ?? (field?.type === 'date' ? DATE_RANGES.find((r) => r.value === v)?.label ?? v : v);

/** A chip's facet ("Status", "Group by", "Saved") and its value ("In review or Draft"). */
export function chipParts(schema: SearchSchema, chip: SearchChip): { facet: string; label: string } {
  switch (chip.kind) {
    case 'filter': {
      const field = fieldOf(schema, chip.field);
      const values = chip.values.map((v) => optionLabel(field, v));
      return {
        facet: field?.label ?? chip.field,
        label: (chip.match === 'contains' ? 'contains ' : '') + values.join(' or '),
      };
    }
    case 'quick':
      return { facet: 'Filter', label: schema.quickFilters?.find((q) => q.key === chip.key)?.label ?? chip.key };
    case 'group': {
      const all = [...(schema.groupBy ?? []), ...(schema.customGroups ?? [])];
      return { facet: 'Group by', label: all.find((g) => g.key === chip.key)?.label ?? chip.key };
    }
    case 'saved':
      return { facet: 'Saved', label: chip.name };
  }
}

/** One line for the whole value, for a title or a screen reader. */
export function describeValue(schema: SearchSchema, value: SearchValue): string {
  const parts = value.chips.map((c) => {
    const { facet, label } = chipParts(schema, c);
    return `${facet}: ${label}`;
  });
  if (value.text.trim()) parts.push(`text "${value.text.trim()}"`);
  return parts.length ? parts.join(', ') : 'No filters';
}

// ── Suggestions while typing ──────────────────────────────────────────────

export interface ChipSuggestion {
  id: string;
  label: string;
  chip: SearchChip;
}

/**
 * What typing offers: an enum or person value whose label holds the text
 * ("Status: In review"), and "Search <field> for: <text>" for each text,
 * person and ref field. Choosing one turns it into a chip.
 */
export function suggestChips(
  schema: SearchSchema,
  text: string,
  loaded: Record<string, SearchOption[]> = {}
): ChipSuggestion[] {
  const q = text.trim();
  if (!q) return [];
  const lower = q.toLowerCase();
  const out: ChipSuggestion[] = [];
  for (const field of schema.fields) {
    const options = field.options ?? loaded[field.key] ?? [];
    if (field.type === 'enum' || field.type === 'person') {
      for (const o of options) {
        if (o.label.toLowerCase().includes(lower) || o.value.toLowerCase() === lower) {
          out.push({
            id: `${field.key}=${o.value}`,
            label: `${field.label}: ${o.label}`,
            chip: { kind: 'filter', field: field.key, values: [o.value], match: 'is' },
          });
        }
      }
    }
    const suggest = field.suggest ?? (field.type === 'text' || field.type === 'person' || field.type === 'ref');
    if (suggest) {
      out.push({
        id: `${field.key}~${q}`,
        label: `Search ${field.label} for: ${q}`,
        chip: { kind: 'filter', field: field.key, values: [q], match: 'contains' },
      });
    }
  }
  return out;
}

// ── The URL ───────────────────────────────────────────────────────────────

/** The query parameters the value is written to, under an optional prefix. */
export const searchParamKeys = (prefix = '') => ({
  text: `${prefix}q`,
  filter: `${prefix}f`,
  quick: `${prefix}qf`,
  group: `${prefix}g`,
  saved: `${prefix}sv`,
});

const encodeValues = (values: string[]) => values.map(encodeURIComponent).join('|');
const decodeValues = (raw: string) =>
  raw
    .split('|')
    .map((v) => {
      try {
        return decodeURIComponent(v);
      } catch {
        return v;
      }
    })
    .filter((v) => v !== '');

/**
 * Writes the value into a copy of params: f=status:draft|in_review for an
 * exact filter, f=owner~dav for a substring, qf=mine, g=status, sv= a saved
 * search as JSON (so a shared link carries it, whoever opens it), q= the
 * text. Every key of the prefix is replaced; others are kept.
 */
export function writeSearchParams(params: URLSearchParams, value: SearchValue, prefix = ''): URLSearchParams {
  const keys = searchParamKeys(prefix);
  const out = new URLSearchParams(params);
  Object.values(keys).forEach((k) => out.delete(k));
  for (const chip of value.chips) {
    switch (chip.kind) {
      case 'filter':
        out.append(keys.filter, `${chip.field}${chip.match === 'contains' ? '~' : ':'}${encodeValues(chip.values)}`);
        break;
      case 'quick':
        out.append(keys.quick, chip.key);
        break;
      case 'group':
        out.append(keys.group, chip.key);
        break;
      case 'saved':
        out.append(keys.saved, JSON.stringify({ id: chip.id, name: chip.name, value: chip.value }));
        break;
    }
  }
  if (value.text.trim()) out.set(keys.text, value.text);
  return out;
}

/**
 * Reads a value back from the URL, in the order each kind was written:
 * filters, quick filters, groups, saved searches. With a schema, chips the
 * page does not offer (a field it dropped, a hand-edited link) are left out.
 */
export function readSearchParams(params: URLSearchParams, schema?: SearchSchema, prefix = ''): SearchValue {
  const keys = searchParamKeys(prefix);
  let value: SearchValue = { chips: [], text: params.get(keys.text) ?? '' };
  for (const raw of params.getAll(keys.filter)) {
    const m = /^([^:~]+)([:~])(.*)$/.exec(raw);
    if (!m) continue;
    const [, field, sep, rest] = m;
    if (schema && !fieldOf(schema, field)) continue;
    const values = decodeValues(rest);
    if (values.length === 0) continue;
    value = addChip(value, { kind: 'filter', field, values, match: sep === '~' ? 'contains' : 'is' });
  }
  for (const key of params.getAll(keys.quick)) {
    if (schema && !schema.quickFilters?.some((q) => q.key === key)) continue;
    value = addChip(value, { kind: 'quick', key });
  }
  for (const key of params.getAll(keys.group)) {
    const options = [...(schema?.groupBy ?? []), ...(schema?.customGroups ?? [])];
    if (schema && !options.some((g) => g.key === key)) continue;
    value = addChip(value, { kind: 'group', key });
  }
  for (const raw of params.getAll(keys.saved)) {
    const saved = parseSavedChip(raw);
    if (saved) value = addChip(value, saved);
  }
  return value;
}

/** A SearchValue from stored JSON, or null when it is not one. */
export function parseSearchValue(raw: unknown): SearchValue | null {
  if (!raw || typeof raw !== 'object') return null;
  const obj = raw as { chips?: unknown; text?: unknown };
  if (!Array.isArray(obj.chips)) return null;
  const chips: SearchChip[] = [];
  for (const c of obj.chips) {
    if (!c || typeof c !== 'object') continue;
    const chip = c as Record<string, unknown>;
    if (chip.kind === 'filter' && typeof chip.field === 'string' && Array.isArray(chip.values)) {
      const values = chip.values.filter((v): v is string => typeof v === 'string');
      if (values.length) chips.push({ kind: 'filter', field: chip.field, values, match: chip.match === 'contains' ? 'contains' : 'is' });
    } else if ((chip.kind === 'quick' || chip.kind === 'group') && typeof chip.key === 'string') {
      chips.push({ kind: chip.kind, key: chip.key });
    }
  }
  return { chips, text: typeof obj.text === 'string' ? obj.text : '' };
}

function parseSavedChip(raw: string): SearchChip | null {
  try {
    const obj = JSON.parse(raw) as { id?: unknown; name?: unknown; value?: unknown };
    const value = parseSearchValue(obj.value);
    if (typeof obj.id !== 'string' || typeof obj.name !== 'string' || !value) return null;
    return { kind: 'saved', id: obj.id, name: obj.name, value };
  } catch {
    return null;
  }
}

// ── Filtering and grouping on the client ──────────────────────────────────

export interface SearchAccessors<T> {
  /** A row's value(s) for a field: what its chips compare against. */
  fieldValue: (item: T, field: string) => string | string[] | null | undefined;
  /** Whether a row passes a quick filter. */
  quickMatch?: (item: T, key: string) => boolean;
  /** Whether a row matches the free text; by default, any field value holds it. */
  textMatch?: (item: T, text: string) => boolean;
  /** Today, for the date ranges; now by default. */
  now?: Date;
}

/** The first moment of a date range, in local time. */
export function rangeStart(range: string, now: Date): Date | null {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  switch (range) {
    case 'today':
      return d;
    case 'week': {
      // Weeks start on Monday.
      const back = (d.getDay() + 6) % 7;
      d.setDate(d.getDate() - back);
      return d;
    }
    case 'month':
      return new Date(now.getFullYear(), now.getMonth(), 1);
    case 'quarter':
      return new Date(now.getFullYear(), Math.floor(now.getMonth() / 3) * 3, 1);
    case 'year':
      return new Date(now.getFullYear(), 0, 1);
    default:
      return null;
  }
}

const asList = (v: string | string[] | null | undefined): string[] =>
  v == null ? [] : Array.isArray(v) ? v : [v];

function chipMatches<T>(schema: SearchSchema, chip: Extract<SearchChip, { kind: 'filter' }>, item: T, acc: SearchAccessors<T>): boolean {
  const field = fieldOf(schema, chip.field);
  const have = asList(acc.fieldValue(item, chip.field));
  if (field?.type === 'date') {
    const now = acc.now ?? new Date();
    return have.some((raw) => {
      const t = Date.parse(raw);
      if (Number.isNaN(t)) return false;
      return chip.values.some((range) => {
        const start = rangeStart(range, now);
        return start !== null && t >= start.getTime();
      });
    });
  }
  const lowered = have.map((h) => h.toLowerCase());
  return chip.values.some((want) => {
    const w = want.toLowerCase();
    return chip.match === 'contains' ? lowered.some((h) => h.includes(w)) : lowered.some((h) => h === w);
  });
}

/**
 * The rows the value leaves in view: every filter chip matches (a chip's
 * values are alternatives), every quick filter passes, and the free text
 * matches. Group chips do not filter.
 */
export function filterItems<T>(items: T[], value: SearchValue, schema: SearchSchema, acc: SearchAccessors<T>): T[] {
  const { chips, text } = effectiveValue(value);
  const filters = chips.filter((c): c is Extract<SearchChip, { kind: 'filter' }> => c.kind === 'filter');
  const quick = chips.filter((c): c is Extract<SearchChip, { kind: 'quick' }> => c.kind === 'quick');
  const t = text.trim().toLowerCase();
  const textMatch =
    acc.textMatch ??
    ((item: T, q: string) =>
      schema.fields.some((f) => asList(acc.fieldValue(item, f.key)).some((v) => v.toLowerCase().includes(q.toLowerCase()))));
  return items.filter(
    (item) =>
      filters.every((c) => chipMatches(schema, c, item, acc)) &&
      quick.every((c) => (acc.quickMatch ? acc.quickMatch(item, c.key) : true)) &&
      (!t || textMatch(item, t))
  );
}

export interface ItemGroup<T> {
  key: string;
  label: string;
  rows: T[];
}

/**
 * The rows under one heading per value of the group-by keys, in their
 * order ("Draft · Requirement"): a key that is a field with options orders
 * its groups as the options do, any other alphabetically, and rows with no
 * value come last, under "No <label>". Rows keep their order within a group.
 */
export function groupItems<T>(
  items: T[],
  keys: string[],
  schema: SearchSchema,
  groupValue: (item: T, key: string) => string
): ItemGroup<T>[] {
  if (keys.length === 0) return [];
  const allGroups = [...(schema.groupBy ?? []), ...(schema.customGroups ?? [])];
  const labelFor = (key: string, v: string) => {
    const field = fieldOf(schema, key);
    if (!v) return `No ${(allGroups.find((g) => g.key === key)?.label ?? field?.label ?? key).toLowerCase()}`;
    return optionLabel(field, v);
  };
  const rank = (key: string, v: string): [number, string] => {
    if (!v) return [Number.MAX_SAFE_INTEGER, ''];
    const options = fieldOf(schema, key)?.options;
    const i = options ? options.findIndex((o) => o.value === v) : -1;
    return [i >= 0 ? i : options ? options.length : 0, labelFor(key, v).toLowerCase()];
  };
  const groups = new Map<string, { values: string[]; rows: T[] }>();
  for (const item of items) {
    const values = keys.map((k) => groupValue(item, k) ?? '');
    const id = values.join('\u0000');
    const g = groups.get(id);
    if (g) g.rows.push(item);
    else groups.set(id, { values, rows: [item] });
  }
  const compare = (a: string[], b: string[]) => {
    for (let i = 0; i < keys.length; i++) {
      const [ra, la] = rank(keys[i], a[i]);
      const [rb, lb] = rank(keys[i], b[i]);
      if (ra !== rb) return ra - rb;
      if (la !== lb) return la < lb ? -1 : 1;
    }
    return 0;
  };
  return Array.from(groups.entries())
    .sort(([, a], [, b]) => compare(a.values, b.values))
    .map(([id, g]) => ({ key: id, label: g.values.map((v, i) => labelFor(keys[i], v)).join(' · '), rows: g.rows }));
}
