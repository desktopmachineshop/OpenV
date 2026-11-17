import type { PickerOption } from './ui/Picker';

// A requirement's MoSCoW priority, stored under the "priority" attribute that
// the requirements list, table, header and search already read. It is a
// separate vocabulary from the normative keyword in the text; a project that
// grades its requirements by that keyword turns on priority detection
// (internal/domain/settings/priority.go) and the editor fills Priority from
// it until someone picks a value.

export const PRIORITY_ATTR = 'priority';

export type Priority = 'must' | 'should' | 'could';

export const PRIORITY_OPTIONS: (PickerOption & { value: Priority })[] = [
  { value: 'must', label: 'Must', hint: 'Needed for the release', keywords: 'M' },
  { value: 'should', label: 'Should', hint: 'Important, but the release can go without it', keywords: 'S' },
  { value: 'could', label: 'Could', hint: 'Desirable if time allows', keywords: 'C' },
];

export const priorityLabel = (p: string): string =>
  PRIORITY_OPTIONS.find((o) => o.value === p)?.label || p;

// Strongest first: a requirement that says both shall and should is a Must.
const RULES: [Priority, RegExp][] = [
  ['must', /\b(shall|must|(?:is|are) required to)\b/i],
  ['should', /\b(should|(?:is|are) recommended to)\b/i],
  ['could', /\b(may|could|(?:is|are) permitted to|optionally)\b/i],
];

/** The priority a requirement's own wording implies, and the word that says so. */
export const detectPriority = (text: string): { priority: Priority; word: string } | null => {
  for (const [priority, re] of RULES) {
    const m = text.match(re);
    if (m) return { priority, word: m[1].toLowerCase() };
  }
  return null;
};
