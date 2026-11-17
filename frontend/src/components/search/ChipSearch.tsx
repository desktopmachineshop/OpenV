import React, { useEffect, useId, useMemo, useRef, useState } from 'react';
import { chipStyle } from '../ui/Chip';
import { ChipSearchMenu } from './ChipSearchMenu';
import {
  ChipSuggestion,
  KIND_TONES,
  SearchChip,
  SearchOption,
  SearchSchema,
  SearchValue,
  addChip,
  chipParts,
  fieldOf,
  removeChipAt,
  suggestChips,
} from './searchValue';
import type { SavedSearchesController } from './useSavedSearches';

export interface ChipSearchProps {
  schema: SearchSchema;
  value: SearchValue;
  onChange: (next: SearchValue) => void;
  /** The input's accessible name. */
  ariaLabel: string;
  placeholder?: string;
  phone?: boolean;
  /** The member's saved searches for schema.savedScope (useSavedSearches). */
  saved?: SavedSearchesController;
  /** A count beside each quick filter in the panel. */
  quickCounts?: Record<string, number>;
  /** A count beside each value of a field in the panel, by field key then value. */
  optionCounts?: Record<string, Record<string, number>>;
  /** Which edge of the bar the panel lines up with. */
  align?: 'left' | 'right';
  /**
   * Off, typing offers no chips of the bar's own: a page that shows its own
   * list under the bar (the top-bar search's results) offers them there.
   */
  suggestions?: boolean;
  /** Runs before the bar's own keys; preventDefault keeps the bar out of it. */
  onInputKeyDown?: (e: React.KeyboardEvent<HTMLInputElement>) => void;
  onInputFocus?: () => void;
  inputRef?: React.RefObject<HTMLInputElement>;
  /** The top bar's dark surface, or the page's. */
  variant?: 'page' | 'sidebar';
  helpKey?: string;
  /** Drawn under the bar, inside its box (the top-bar search's results). */
  children?: React.ReactNode;
}

/** Options from each field's loadOptions, fetched once when the bar first needs them. */
export function useLoadedOptions(schema: SearchSchema, wanted: boolean): Record<string, SearchOption[]> {
  const [loaded, setLoaded] = useState<Record<string, SearchOption[]>>({});
  const started = useRef(new Set<string>());
  useEffect(() => {
    if (!wanted) return;
    for (const field of schema.fields) {
      if (!field.loadOptions || started.current.has(field.key)) continue;
      started.current.add(field.key);
      field
        .loadOptions()
        .then((options) => setLoaded((prev) => ({ ...prev, [field.key]: options })))
        .catch(() => started.current.delete(field.key));
    }
  }, [schema, wanted]);
  return loaded;
}

// The chip search bar (issue #722): every filter, grouping and saved search
// as a chip inside one bar, each with its own ×, and the free text after
// them. Backspace in an empty box takes the last chip off; typing offers
// chips ("Search Owner for: dav"); the caret opens the panel of filters,
// groupings and saved searches. The page owns what the value means.
export const ChipSearch: React.FC<ChipSearchProps> = (p) => {
  const id = useId();
  const ownInput = useRef<HTMLInputElement>(null);
  const inputRef = p.inputRef ?? ownInput;
  const boxRef = useRef<HTMLDivElement>(null);
  const caretRef = useRef<HTMLButtonElement>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [focused, setFocused] = useState(false);
  const [active, setActive] = useState(-1);
  const loaded = useLoadedOptions(p.schema, menuOpen || p.value.text !== '');
  const offers: ChipSuggestion[] = useMemo(
    () => (p.suggestions === false ? [] : suggestChips(p.schema, p.value.text, loaded).slice(0, 8)),
    [p.suggestions, p.schema, p.value.text, loaded]
  );
  const showOffers = focused && offers.length > 0 && !menuOpen;
  const sidebar = p.variant === 'sidebar';

  useEffect(() => setActive(-1), [p.value.text]);

  // A click outside the bar closes the panel and the offers.
  useEffect(() => {
    if (!menuOpen && !focused) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
        setFocused(false);
      }
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [menuOpen, focused]);

  const choose = (s: ChipSuggestion) => {
    p.onChange({ ...addChip(p.value, s.chip), text: '' });
    inputRef.current?.focus();
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    p.onInputKeyDown?.(e);
    if (e.defaultPrevented) return;
    if (e.key === 'Backspace' && p.value.text === '' && p.value.chips.length > 0) {
      e.preventDefault();
      p.onChange(removeChipAt(p.value, p.value.chips.length - 1));
    } else if (e.key === 'ArrowDown' && e.altKey) {
      e.preventDefault();
      setMenuOpen(true);
    } else if (showOffers && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault();
      setActive((i) => (e.key === 'ArrowDown' ? Math.min(i + 1, offers.length - 1) : Math.max(i - 1, -1)));
    } else if (showOffers && e.key === 'Enter' && active >= 0) {
      e.preventDefault();
      choose(offers[active]);
    } else if (e.key === 'Escape') {
      if (showOffers) setFocused(false);
      else if (menuOpen) setMenuOpen(false);
    }
  };

  const chip = (c: SearchChip, i: number) => {
    const { facet, label } = chipParts(p.schema, c);
    const tone = (c.kind === 'filter' && fieldOf(p.schema, c.field)?.tone) || KIND_TONES[c.kind];
    const kindWord = c.kind === 'group' ? 'grouping' : c.kind === 'saved' ? 'saved search' : 'filter';
    return (
      <span key={`${c.kind}-${i}`} className="ov-search-chip" style={{ ...chipStyle(tone), flex: 'none', gap: 4, fontWeight: 600, maxWidth: 260 }}>
        <span style={{ opacity: 0.75, fontWeight: 700 }}>{c.kind === 'saved' ? '★' : facet}</span>
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</span>
        <button
          type="button"
          aria-label={`Remove ${kindWord} ${facet}: ${label}`}
          onClick={() => {
            p.onChange(removeChipAt(p.value, i));
            inputRef.current?.focus();
          }}
          className="ov-tap"
          style={{ border: 'none', background: 'transparent', color: 'inherit', padding: '0 2px', fontSize: 14, lineHeight: 1, cursor: 'pointer' }}
        >
          ×
        </button>
      </span>
    );
  };

  return (
    <div ref={boxRef} style={{ position: 'relative', minWidth: 0, flex: 1 }} data-help-key={p.helpKey}>
      <div
        onClick={(e) => {
          if (e.target === e.currentTarget) inputRef.current?.focus();
        }}
        style={{
          display: 'flex',
          alignItems: 'center',
          flexWrap: p.phone ? 'nowrap' : 'wrap',
          overflowX: p.phone ? 'auto' : undefined,
          gap: 4,
          minHeight: p.phone ? 40 : 30,
          padding: '2px 2px 2px 6px',
          boxSizing: 'border-box',
          border: `1px solid ${sidebar ? 'var(--sidebar-border)' : 'var(--border)'}`,
          borderRadius: 6,
          background: sidebar ? 'var(--sidebar-menu-bg)' : 'var(--surface-alt)',
        }}
      >
        <span role="list" aria-label="Active filters" style={{ display: 'contents' }}>
          {p.value.chips.map((c, i) => (
            <span role="listitem" key={i} style={{ display: 'contents' }}>
              {chip(c, i)}
            </span>
          ))}
        </span>
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-label={p.ariaLabel}
          aria-expanded={showOffers}
          aria-controls={showOffers ? `${id}-offers` : undefined}
          aria-activedescendant={showOffers && active >= 0 ? `${id}-offer-${active}` : undefined}
          aria-autocomplete="list"
          value={p.value.text}
          placeholder={p.value.chips.length ? '' : p.placeholder}
          onChange={(e) => p.onChange({ ...p.value, text: e.target.value })}
          onFocus={() => {
            setFocused(true);
            p.onInputFocus?.();
          }}
          onKeyDown={onKeyDown}
          style={{
            flex: '1 0 80px',
            minWidth: 80,
            height: p.phone ? 34 : 24,
            border: 'none',
            outline: 'none',
            background: 'transparent',
            color: sidebar ? 'var(--sidebar-text)' : 'var(--text)',
            fontSize: p.phone ? 16 : sidebar ? 12 : 13,
          }}
        />
        <button
          ref={caretRef}
          type="button"
          aria-label="Filters, group by and saved searches"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((o) => !o)}
          style={{
            flex: 'none',
            width: p.phone ? 36 : 26,
            height: p.phone ? 34 : 24,
            border: 'none',
            borderLeft: `1px solid ${sidebar ? 'var(--sidebar-border)' : 'var(--border-soft)'}`,
            background: 'transparent',
            color: sidebar ? 'var(--sidebar-text)' : 'var(--text-muted)',
            cursor: 'pointer',
          }}
        >
          <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ transform: menuOpen ? 'rotate(180deg)' : undefined }}>
            <path d="M5 8l5 5 5-5" />
          </svg>
        </button>
      </div>
      {showOffers && (
        <ul
          id={`${id}-offers`}
          role="listbox"
          aria-label="Suggested filters"
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            [p.align ?? 'left']: 0,
            zIndex: 1200,
            width: 'min(320px, calc(100vw - 32px))',
            margin: 0,
            padding: 4,
            listStyle: 'none',
            background: 'var(--surface)',
            color: 'var(--text)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            boxShadow: '0 8px 24px rgba(0,0,0,0.2)',
          }}
        >
          {offers.map((s, i) => (
            <li
              key={s.id}
              id={`${id}-offer-${i}`}
              role="option"
              aria-selected={i === active}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => choose(s)}
              onMouseEnter={() => setActive(i)}
              style={{ padding: '6px 8px', borderRadius: 4, fontSize: 13, cursor: 'pointer', background: i === active ? 'var(--surface-hover)' : 'transparent' }}
            >
              {s.label}
            </li>
          ))}
        </ul>
      )}
      {menuOpen && (
        <ChipSearchMenu
          schema={p.schema}
          value={p.value}
          onChange={p.onChange}
          onClose={() => {
            setMenuOpen(false);
            caretRef.current?.focus();
          }}
          saved={p.saved}
          quickCounts={p.quickCounts}
          optionCounts={p.optionCounts}
          loaded={loaded}
          phone={!!p.phone}
          align={p.align ?? 'right'}
        />
      )}
      {!menuOpen && p.children}
    </div>
  );
};
