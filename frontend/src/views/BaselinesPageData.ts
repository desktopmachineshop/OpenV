// The Baselines page's model: one comparison as rows of changes, read from the
// baseline diff the compare page already uses, with each artifact's reference
// and text taken from the two snapshots it compares.
import type { Artifact, BaselineDiff, BaselineDiffModified } from '../api/client';

export type ChangeKind = 'added' | 'changed' | 'removed' | 'link added' | 'link removed';
export type ChangeFilter = 'all' | 'text' | 'links';

export interface ChangeRow {
  key: string;
  kind: ChangeKind;
  /** The artifact the row is about; for a link, the one it starts from. */
  id: string;
  ref: string;
  title: string;
  fields: string;
  /** A modified entry's flags, for the text panel. */
  modified?: BaselineDiffModified;
}

export const CHANGE_TONE: Record<ChangeKind, 'success' | 'info' | 'danger'> = {
  added: 'success',
  changed: 'info',
  removed: 'danger',
  'link added': 'success',
  'link removed': 'danger',
};

/** The fields a modified entry flags as changed; the body reads as "text". */
export const changedFields = (m: BaselineDiffModified): string[] =>
  [
    m.title_changed && 'title',
    m.body_changed && 'text',
    m.type_changed && 'type',
    m.status_changed && 'status',
    m.parent_changed && 'parent',
  ].filter((f): f is string => !!f);

export const changeRows = (diff: BaselineDiff, refs: Map<string, string>): ChangeRow[] => {
  const ref = (id: string) => refs.get(id) || '';
  return [
    ...diff.modified.map((m) => ({
      key: `m-${m.id}`,
      kind: 'changed' as const,
      id: m.id,
      ref: ref(m.id),
      title: m.new_title,
      fields: changedFields(m).join(', '),
      modified: m,
    })),
    ...diff.added.map((a) => ({ key: `a-${a.id}`, kind: 'added' as const, id: a.id, ref: ref(a.id), title: a.title, fields: 'new' })),
    ...diff.removed.map((a) => ({ key: `r-${a.id}`, kind: 'removed' as const, id: a.id, ref: ref(a.id), title: a.title, fields: '—' })),
    ...diff.links_added.map((l) => ({
      key: `la-${l.from_id}-${l.to_id}-${l.type}`,
      kind: 'link added' as const,
      id: l.from_id,
      ref: ref(l.from_id),
      title: `${l.type} ${ref(l.to_id) || l.to_title}`,
      fields: 'links',
    })),
    ...diff.links_removed.map((l) => ({
      key: `lr-${l.from_id}-${l.to_id}-${l.type}`,
      kind: 'link removed' as const,
      id: l.from_id,
      ref: ref(l.from_id),
      title: `${l.type} ${ref(l.to_id) || l.to_title}`,
      fields: 'links',
    })),
  ];
};

export const filterRows = (rows: ChangeRow[], filter: ChangeFilter): ChangeRow[] =>
  filter === 'all'
    ? rows
    : filter === 'links'
      ? rows.filter((r) => r.kind === 'link added' || r.kind === 'link removed')
      : rows.filter((r) => r.modified && (r.modified.body_changed || r.modified.title_changed));

/** References by artifact id from both sides, the newer side winning. */
export const refMap = (...sides: (Artifact[] | undefined)[]): Map<string, string> => {
  const m = new Map<string, string>();
  for (const side of sides) for (const a of side || []) if (a.ref) m.set(a.id, a.ref);
  return m;
};
