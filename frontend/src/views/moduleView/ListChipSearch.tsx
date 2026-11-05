import React from 'react';
import { ChipSearch } from '../../components/search/ChipSearch';
import type { SearchSchema, SearchValue } from '../../components/search/searchValue';
import { useSavedSearches } from '../../components/search/useSavedSearches';

interface ListChipSearchProps {
  searchSchema: SearchSchema;
  searchValue: SearchValue;
  setSearchValue: (next: SearchValue) => void;
  chipQuickCounts: Record<string, number>;
  phone: boolean;
  /** A baseline is read-only and belongs to no saved search. */
  canSave: boolean;
  userId?: string;
}

// The requirements list's chip search bar (issue #722): the shared bar over
// the list's schema (listSearch.ts), with the member's saved searches for the
// list. It takes useListView's chip search fields as they come (ModuleView
// spreads them); useListView puts the value in the URL and applies it.
export const ListChipSearch: React.FC<ListChipSearchProps> = (p) => {
  const saved = useSavedSearches(p.searchSchema.savedScope, p.canSave, p.userId);
  return (
    <ChipSearch
      schema={p.searchSchema}
      value={p.searchValue}
      onChange={p.setSearchValue}
      ariaLabel="Search this project"
      placeholder="Search, or pick filters…"
      phone={p.phone}
      saved={p.canSave ? saved : undefined}
      quickCounts={p.chipQuickCounts}
      align="right"
      helpKey="requirements.search"
    />
  );
};
