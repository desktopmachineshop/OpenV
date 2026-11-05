import type { FilterPanelRow } from './shared';

/**
 * One line for the list's search and filter panel rows, as the Filter
 * button's title and the filters-active chip show it (the list without the
 * chip search bar).
 */
export function filterSummary(searchText: string, searchExact: boolean, filterRows: FilterPanelRow[], filterLogic: 'and' | 'or'): string {
  const parts: string[] = [];

  if (searchText.trim()) {
    parts.push(searchExact ? `Exact search: "${searchText.trim()}"` : `Search: "${searchText.trim()}"`);
  }

  const activeRows = filterRows.filter((row) => row.value.trim() !== '');
  if (activeRows.length > 0) {
    const rowSummaries = activeRows.map((row) => `${row.field} ${row.comparator} "${row.value.trim()}"`);
    parts.push(`Filters (${filterLogic.toUpperCase()}): ${rowSummaries.join(filterLogic === 'and' ? ' + ' : ' | ')}`);
  }

  if (parts.length === 0) {
    return 'No filters applied';
  }

  return parts.join(' • ');
}
