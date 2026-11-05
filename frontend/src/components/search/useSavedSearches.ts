import { useCallback, useEffect, useState } from 'react';
import { savedSearchAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { SearchValue, parseSearchValue } from './searchValue';

/** A saved search as the bar lists it. */
export interface SavedSearchEntry {
  id: string;
  name: string;
  value: SearchValue;
  shared: boolean;
  /** The signed-in member's own, which they may delete. */
  mine: boolean;
}

/** What the bar's Favourites column reads and calls. */
export interface SavedSearchesController {
  items: SavedSearchEntry[];
  loading: boolean;
  error: string;
  save: (name: string, value: SearchValue, shared: boolean) => Promise<SavedSearchEntry | null>;
  remove: (id: string) => Promise<void>;
}

/**
 * A member's saved searches for one scope, from the server (per user and
 * workspace, so they follow the member between devices). Off (no scope, or
 * the feature gate shut), it lists nothing and saves nothing.
 */
export function useSavedSearches(scope: string | undefined, enabled: boolean, currentUserId?: string): SavedSearchesController {
  const [items, setItems] = useState<SavedSearchEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const on = enabled && !!scope;

  useEffect(() => {
    if (!on || !scope) {
      setItems([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    savedSearchAPI
      .list(scope)
      .then((res) => {
        if (cancelled) return;
        const rows = Array.isArray(res.data?.saved_searches) ? res.data.saved_searches : [];
        setItems(
          rows.flatMap((r) => {
            const value = parseSearchValue(r.value);
            return value ? [{ id: r.id, name: r.name, value, shared: r.shared, mine: !currentUserId || r.user_id === currentUserId }] : [];
          })
        );
        setError('');
      })
      .catch((err) => {
        if (!cancelled) setError(apiErrorMessage(err, 'Could not load saved searches'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [on, scope, currentUserId]);

  const save = useCallback(
    async (name: string, value: SearchValue, shared: boolean) => {
      if (!on || !scope) return null;
      try {
        // A saved search is stored without saved-search chips of its own:
        // they are applied as their chips (effectiveValue), so store those.
        const stored: SearchValue = {
          chips: value.chips.flatMap((c) => (c.kind === 'saved' ? c.value.chips : [c])),
          text: [value.text, ...value.chips.flatMap((c) => (c.kind === 'saved' ? [c.value.text] : []))]
            .map((t) => t.trim())
            .filter(Boolean)
            .join(' '),
        };
        const res = await savedSearchAPI.create({ scope, name, value: stored as unknown as Record<string, unknown>, shared });
        const entry: SavedSearchEntry = { id: res.data.id, name: res.data.name, value: stored, shared: res.data.shared, mine: true };
        setItems((prev) => [entry, ...prev.filter((p) => p.id !== entry.id)]);
        setError('');
        return entry;
      } catch (err) {
        setError(apiErrorMessage(err, 'Could not save the search'));
        return null;
      }
    },
    [on, scope]
  );

  const remove = useCallback(
    async (id: string) => {
      try {
        await savedSearchAPI.delete(id);
        setItems((prev) => prev.filter((p) => p.id !== id));
        setError('');
      } catch (err) {
        setError(apiErrorMessage(err, 'Could not delete the saved search'));
      }
    },
    []
  );

  return { items, loading, error, save, remove };
}
