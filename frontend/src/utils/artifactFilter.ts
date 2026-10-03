// The ModuleView filter engine: the search box and the filter panel's rows,
// decided per artifact. Pure functions of their arguments, so the trimming,
// lowercasing and gt/lt ordering they apply can be tested without rendering
// the view; ModuleView passes in the panel state it holds.
import type { Artifact } from '../api/client';

/** One row of the filter panel. The panel's row id is not read here. */
export type FilterRow = { field: string; value: string; comparator: string };

export const matchesSearch = (artifact: Artifact, query: string, searchExact: boolean): boolean => {
  const normalized = query.trim();
  if (!normalized) return true;

  const haystack = [
    artifact.title,
    artifact.body,
    artifact.type,
    artifact.id,
    artifact.parent_id ?? '',
    JSON.stringify(artifact.attributes || {}),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();

  if (searchExact) {
    // "Exact match": the query must appear as a whole word/phrase, i.e. not
    // as a substring of a longer word ("log" no longer matches "catalog").
    const escaped = normalized.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`(^|[^a-z0-9_])${escaped}($|[^a-z0-9_])`).test(haystack);
  }

  return haystack.includes(normalized.toLowerCase());
};

export const applyComparator = (fieldValue: string, comparator: string, compareValue: string): boolean => {
  const normalizedField = fieldValue.toLowerCase();
  const normalizedCompare = compareValue.toLowerCase();

  if (comparator === 'equals') {
    return normalizedField === normalizedCompare;
  }

  if (comparator === 'not-equals') {
    return normalizedField !== normalizedCompare;
  }

  if (comparator === 'starts-with') {
    return normalizedField.startsWith(normalizedCompare);
  }

  if (comparator === 'ends-with') {
    return normalizedField.endsWith(normalizedCompare);
  }

  if (comparator === 'not-contains') {
    return !normalizedField.includes(normalizedCompare);
  }

  if (comparator === 'gt' || comparator === 'lt') {
    const fieldNumber = Number(fieldValue);
    const compareNumber = Number(compareValue);
    if (!Number.isNaN(fieldNumber) && !Number.isNaN(compareNumber)) {
      return comparator === 'gt' ? fieldNumber > compareNumber : fieldNumber < compareNumber;
    }

    const fieldDate = Date.parse(fieldValue);
    const compareDate = Date.parse(compareValue);
    if (!Number.isNaN(fieldDate) && !Number.isNaN(compareDate)) {
      return comparator === 'gt' ? fieldDate > compareDate : fieldDate < compareDate;
    }

    return comparator === 'gt'
      ? normalizedField > normalizedCompare
      : normalizedField < normalizedCompare;
  }

  return normalizedField.includes(normalizedCompare);
};

export const matchesFieldFilters = (
  artifact: Artifact,
  filterRows: FilterRow[],
  filterLogic: 'and' | 'or',
  getFieldValue: (artifact: Artifact, field: string) => string
): boolean => {
  const activeRows = filterRows.filter((row) => row.value.trim() !== '');
  if (activeRows.length === 0) {
    return true;
  }

  const evaluations = activeRows.map((row) => {
    const value = row.value.trim().toLowerCase();
    const fieldValue = getFieldValue(artifact, row.field);
    return applyComparator(fieldValue, row.comparator, value);
  });

  return filterLogic === 'and'
    ? evaluations.every(Boolean)
    : evaluations.some(Boolean);
};
