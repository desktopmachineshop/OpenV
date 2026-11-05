import React, { useEffect, useRef, useState } from 'react';
import { buttonStyle } from '../ui/buttonStyles';
import { ChipSearchFilters } from './ChipSearchFilters';
import { check, columnHeading, divider, menuItem, smallInput, stepMenuFocus } from './chipSearchStyles';
import { SearchOption, SearchSchema, SearchValue, addChip, hasGroup, isActive, toggleGroup } from './searchValue';
import type { SavedSearchesController } from './useSavedSearches';

interface MenuProps {
  schema: SearchSchema;
  value: SearchValue;
  onChange: (next: SearchValue) => void;
  onClose: () => void;
  saved?: SavedSearchesController;
  quickCounts?: Record<string, number>;
  loaded: Record<string, SearchOption[]>;
  phone: boolean;
  align: 'left' | 'right';
}

// The panel the bar's caret opens: Filters, Group by (when the page groups)
// and Favourites (when the page saves searches), side by side; a phone gets
// them stacked in a bottom sheet. The arrow keys walk its items and Escape
// closes it, back to the caret.
export const ChipSearchMenu: React.FC<MenuProps> = (p) => {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    // Focus the first item, so the keyboard lands in the menu it opened.
    ref.current?.querySelector<HTMLElement>('[role^="menuitem"]')?.focus();
  }, []);
  const showGroups = (p.schema.groupBy?.length ?? 0) > 0 || (p.schema.customGroups?.length ?? 0) > 0;
  const showSaved = !!p.schema.savedScope && !!p.saved;
  const columns = 1 + (showGroups ? 1 : 0) + (showSaved ? 1 : 0);

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      p.onClose();
    } else if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && (e.target as HTMLElement).getAttribute('role')?.startsWith('menuitem')) {
      e.preventDefault();
      stepMenuFocus(ref.current, e.key === 'ArrowDown' ? 1 : -1);
    }
  };

  const surface: React.CSSProperties = p.phone
    ? {
        position: 'fixed',
        left: 0,
        right: 0,
        bottom: 0,
        maxHeight: '75vh',
        overflowY: 'auto',
        borderRadius: '12px 12px 0 0',
        paddingBottom: 'calc(12px + env(safe-area-inset-bottom, 0px))',
        display: 'grid',
        gap: 12,
      }
    : {
        position: 'absolute',
        top: 'calc(100% + 4px)',
        [p.align]: 0,
        width: `min(${columns * 230}px, calc(100vw - 32px))`,
        maxHeight: 460,
        overflowY: 'auto',
        borderRadius: 8,
        display: 'grid',
        gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
        gap: 12,
      };

  return (
    <>
      {p.phone && <div aria-hidden="true" onClick={p.onClose} style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 1199 }} />}
      <div
        ref={ref}
        className="ov-chip-search-menu"
        aria-label="Search options"
        onKeyDown={onKeyDown}
        style={{
          ...surface,
          padding: 10,
          boxSizing: 'border-box',
          background: 'var(--surface)',
          color: 'var(--text)',
          border: '1px solid var(--border)',
          boxShadow: '0 8px 24px rgba(0,0,0,0.2)',
          zIndex: 1200,
          textAlign: 'left',
        }}
      >
        <ChipSearchFilters schema={p.schema} value={p.value} onChange={p.onChange} quickCounts={p.quickCounts} loaded={p.loaded} />
        {showGroups && <GroupColumn schema={p.schema} value={p.value} onChange={p.onChange} />}
        {showSaved && p.saved && <SavedColumn saved={p.saved} value={p.value} onChange={p.onChange} />}
      </div>
    </>
  );
};

const GroupColumn: React.FC<{ schema: SearchSchema; value: SearchValue; onChange: (v: SearchValue) => void }> = ({ schema, value, onChange }) => {
  const custom = schema.customGroups ?? [];
  return (
    <div role="menu" aria-label="Group by" style={{ minWidth: 0 }}>
      <p style={columnHeading}>
        <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
          <rect x="3" y="3" width="14" height="5" rx="1" />
          <rect x="3" y="12" width="14" height="5" rx="1" />
        </svg>
        Group by
      </p>
      {(schema.groupBy ?? []).map((g) => {
        const on = hasGroup(value, g.key);
        return (
          <button key={g.key} type="button" role="menuitemcheckbox" aria-checked={on} onClick={() => onChange(toggleGroup(value, g.key))} style={menuItem(on)}>
            <span style={check(on)} aria-hidden="true">{on ? '✓' : ''}</span>
            {g.label}
          </button>
        );
      })}
      {custom.length > 0 && (
        <>
          <div style={divider} role="separator" />
          <label style={{ display: 'grid', gap: 4, padding: '0 8px', fontSize: 12, color: 'var(--text-muted)' }}>
            Custom group
            <select
              aria-label="Custom group"
              value=""
              onChange={(e) => e.target.value && onChange(addChip(value, { kind: 'group', key: e.target.value }))}
              style={smallInput}
            >
              <option value="">Any attribute…</option>
              {custom.map((g) => (
                <option key={g.key} value={g.key}>
                  {g.label}
                </option>
              ))}
            </select>
          </label>
        </>
      )}
    </div>
  );
};

const SavedColumn: React.FC<{ saved: SavedSearchesController; value: SearchValue; onChange: (v: SearchValue) => void }> = ({ saved, value, onChange }) => {
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState('');
  const [shared, setShared] = useState(false);
  const save = async () => {
    if (!name.trim()) return;
    const entry = await saved.save(name.trim(), value, shared);
    if (entry) {
      setNaming(false);
      setName('');
      setShared(false);
      onChange({ chips: [{ kind: 'saved', id: entry.id, name: entry.name, value: entry.value }], text: '' });
    }
  };
  return (
    <div role="menu" aria-label="Favourites" style={{ minWidth: 0 }}>
      <p style={columnHeading}>
        <span aria-hidden="true">★</span>
        Favourites
      </p>
      {saved.loading && <span style={{ ...menuItem(), color: 'var(--text-muted)', cursor: 'default' }}>Loading…</span>}
      {!saved.loading && saved.items.length === 0 && (
        <span style={{ ...menuItem(), color: 'var(--text-muted)', cursor: 'default' }}>No saved searches yet</span>
      )}
      {saved.items.map((s) => {
        const on = value.chips.some((c) => c.kind === 'saved' && c.id === s.id);
        return (
          <div key={s.id} style={{ display: 'flex', alignItems: 'center' }}>
            <button
              type="button"
              role="menuitemcheckbox"
              aria-checked={on}
              title={s.shared ? (s.mine ? 'Shared with the workspace' : 'Shared by a colleague') : undefined}
              onClick={() =>
                onChange(on ? { ...value, chips: value.chips.filter((c) => !(c.kind === 'saved' && c.id === s.id)) } : { chips: [{ kind: 'saved', id: s.id, name: s.name, value: s.value }], text: '' })
              }
              style={{ ...menuItem(on), flex: 1, minWidth: 0 }}
            >
              <span aria-hidden="true" style={{ color: on ? 'var(--warning-text)' : 'var(--text-muted)' }}>
                ★
              </span>
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.name}</span>
              {s.shared && <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>shared</span>}
            </button>
            {s.mine && (
              <button
                type="button"
                aria-label={`Delete saved search ${s.name}`}
                onClick={() => saved.remove(s.id)}
                style={{ ...buttonStyle('ghost', { compact: true }), minHeight: 26, padding: '0 6px' }}
              >
                ×
              </button>
            )}
          </div>
        );
      })}
      <div style={divider} role="separator" />
      {!naming ? (
        <button type="button" role="menuitem" disabled={!isActive(value)} onClick={() => setNaming(true)} style={{ ...menuItem(), opacity: isActive(value) ? 1 : 0.5 }}>
          Save current search
        </button>
      ) : (
        <div style={{ display: 'grid', gap: 6, padding: '0 8px' }}>
          <input
            aria-label="Name of the saved search"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                save();
              }
            }}
            placeholder="Name, e.g. Release 1.4"
            maxLength={120}
            style={smallInput}
          />
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)' }}>
            <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} />
            Share with the workspace
          </label>
          <button type="button" onClick={save} disabled={!name.trim()} style={buttonStyle('primary', { compact: true })}>
            Save
          </button>
        </div>
      )}
      {saved.error && <p role="alert" style={{ margin: '6px 8px 0', fontSize: 12, color: 'var(--danger-strong)' }}>{saved.error}</p>}
    </div>
  );
};
