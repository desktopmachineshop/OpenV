import type React from 'react';

/** One choice a Picker offers. */
export interface PickerOption {
  /** What is stored when this is chosen. */
  value: string;
  /** What the row and the closed field show. */
  label: string;
  /** Rows with the same group sit under one heading, in the order given. */
  group?: string;
  /** A second, muted line under the label. */
  hint?: string;
  /** Muted text at the row's end: a date, a kind, a count. */
  meta?: string;
  /** Drawn before the label: a colour dot, an avatar, a reference. */
  lead?: React.ReactNode;
  /** Extra text the search matches but the row does not show. */
  keywords?: string;
  /** Indent, in levels, for a tree. */
  depth?: number;
}

/** A row of the open list: an option, the "not set" row or "use what was typed". */
export type PickerRow =
  | { kind: 'none'; value: '' }
  | { kind: 'option'; value: string; option: PickerOption }
  | { kind: 'create'; value: string };

/** Lists longer than this get a search box. */
export const SEARCH_THRESHOLD = 8;

/** Case-insensitive match anywhere in the label or keywords. */
export const matches = (option: PickerOption, query: string): boolean => {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return `${option.label} ${option.keywords || ''}`.toLowerCase().includes(q);
};

/**
 * The rows the open list shows for a query. The "not set" row leads when the
 * field is optional and nothing is typed; free text adds a last row to use
 * exactly what was typed when no option is called that.
 */
export const buildRows = (
  options: PickerOption[],
  query: string,
  opts: { noneLabel?: string; freeText?: boolean }
): PickerRow[] => {
  const q = query.trim();
  const rows: PickerRow[] = [];
  if (opts.noneLabel && !q) rows.push({ kind: 'none', value: '' });
  for (const option of options) {
    if (matches(option, q)) rows.push({ kind: 'option', value: option.value, option });
  }
  if (opts.freeText && q && !options.some((o) => o.label.toLowerCase() === q.toLowerCase())) {
    rows.push({ kind: 'create', value: q });
  }
  return rows;
};

/** Where the match falls in a label, to underline it; null when it does not. */
export const matchRange = (label: string, query: string): [number, number] | null => {
  const q = query.trim().toLowerCase();
  if (!q) return null;
  const at = label.toLowerCase().indexOf(q);
  return at < 0 ? null : [at, at + q.length];
};

/**
 * The next row whose label starts with a typed letter, searching on from the
 * one after `from` and wrapping, as a native select does. -1 when none does.
 */
export const typeAhead = (
  rows: PickerRow[],
  from: number,
  letter: string,
  noneLabel = ''
): number => {
  const k = letter.toLowerCase();
  for (let step = 1; step <= rows.length; step++) {
    const i = (from + step) % rows.length;
    const row = rows[i];
    const label = row.kind === 'option' ? row.option.label : row.kind === 'none' ? noneLabel : row.value;
    if (label.toLowerCase().startsWith(k)) return i;
  }
  return -1;
};
