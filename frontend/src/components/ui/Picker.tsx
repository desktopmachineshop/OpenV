import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { FeatureKey } from '../../generated/contract';
import { useFeature } from '../../hooks/useFeature';
import {
  PickerOption,
  PickerRow,
  SEARCH_THRESHOLD,
  buildRows,
  matchRange,
  typeAhead,
} from './pickerModel';

export type { PickerOption } from './pickerModel';

/**
 * The editor's dropdowns: the Picker in place of the browser's select and
 * suggestion list. Without it a field keeps the native control.
 */
export const EDITOR_PICKERS_FEATURE: FeatureKey = 'editor-pickers';

export interface PickerProps {
  /** The trigger's id, so a <label htmlFor> names it as it named the select. */
  id: string;
  value: string;
  options: PickerOption[];
  onChange: (value: string) => void;
  /** The "not set" row of an optional field, and what the closed field shows then. */
  noneLabel?: string;
  /** Any value may be typed (the owner); the options are suggestions. */
  freeText?: boolean;
  placeholder?: string;
  /** Overrides the default: a search box when there are more than 8 options. */
  searchable?: boolean;
  disabled?: boolean;
  'aria-label'?: string;
  'aria-describedby'?: string;
}

/**
 * One dropdown for every choice field. Opening it always lists every option,
 * the current one ticked and in view, whatever the field holds; typing (in
 * the field when any value is allowed, else in its search box) narrows the
 * list. It follows the ARIA combobox pattern, so the keyboard and screen
 * readers treat it as they treat a select.
 */
export const Picker: React.FC<PickerProps> = (props) => {
  const on = useFeature(EDITOR_PICKERS_FEATURE);
  return on ? <PickerControl {...props} /> : <NativeFallback {...props} />;
};

// searchable is the Picker's own: a native control has no search box, and the prop must not reach the DOM.
const NativeFallback: React.FC<PickerProps> = ({ id, value, options, onChange, noneLabel, freeText, placeholder, disabled, searchable: _searchable, ...aria }) => {
  if (freeText) {
    return (
      <>
        <input id={id} list={`${id}-options`} value={value} placeholder={placeholder} disabled={disabled}
          onChange={(e) => onChange(e.target.value)} {...aria} />
        <datalist id={`${id}-options`}>
          {options.map((o) => <option key={o.value} value={o.value} />)}
        </datalist>
      </>
    );
  }
  return (
    <select id={id} value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} {...aria}>
      {noneLabel !== undefined && <option value="">{noneLabel}</option>}
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
  );
};

const PickerControl: React.FC<PickerProps> = ({
  id, value, options, onChange, noneLabel, freeText, placeholder, searchable, disabled,
  'aria-label': ariaLabel, 'aria-describedby': describedBy,
}) => {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const [up, setUp] = useState(false);
  const [focused, setFocused] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement & HTMLInputElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const hasSearch = !freeText && (searchable ?? options.length > SEARCH_THRESHOLD);
  const rows = useMemo(() => buildRows(options, query, { noneLabel, freeText }), [options, query, noneLabel, freeText]);
  const selected = options.find((o) => o.value === value);
  const listId = `${id}-list`;

  const openList = () => {
    if (open || disabled) return;
    setQuery('');
    const all = buildRows(options, '', { noneLabel, freeText });
    setActive(Math.max(0, all.findIndex((r) => r.value === value)));
    setOpen(true);
  };
  const close = (refocus = false) => {
    setOpen(false);
    setQuery('');
    if (refocus) trigger.current?.focus();
  };
  const choose = (row: PickerRow | undefined) => {
    if (!row) return;
    onChange(row.value);
    close(!freeText);
  };

  // Open upwards when the list would run off the bottom of the window.
  useLayoutEffect(() => {
    if (!open || !root.current || !menu.current) return;
    const r = root.current.getBoundingClientRect();
    const h = menu.current.offsetHeight;
    setUp(window.innerHeight - r.bottom < h + 12 && r.top > h + 12);
    if (hasSearch) search.current?.focus();
  }, [open, hasSearch]);

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) close();
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open]);

  useEffect(() => {
    if (open) document.getElementById(`${id}-opt-${active}`)?.scrollIntoView?.({ block: 'nearest' });
  }, [open, active, id]);

  const onKeyDown = (e: React.KeyboardEvent) => {
    const n = rows.length;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open) return openList();
      if (n) setActive((a) => (a + (e.key === 'ArrowDown' ? 1 : n - 1)) % n);
    } else if ((e.key === 'Home' || e.key === 'End') && open && !freeText) {
      e.preventDefault();
      setActive(e.key === 'Home' ? 0 : n - 1);
    } else if (e.key === 'Enter' || (e.key === ' ' && !freeText && e.target === trigger.current)) {
      if (!open) {
        if (!freeText) { e.preventDefault(); openList(); }
        return;
      }
      e.preventDefault();
      choose(rows[active]);
    } else if (e.key === 'Escape' && open) {
      e.preventDefault();
      e.stopPropagation();
      close(true);
    } else if (e.key === 'Tab') {
      close();
    } else if (!freeText && e.target === trigger.current && e.key.length === 1 && /\S/.test(e.key)) {
      if (hasSearch) {
        // The letter starts the search rather than being lost.
        e.preventDefault();
        openList();
        setQuery(e.key);
        setActive(0);
        return;
      }
      if (!open) openList();
      const next = typeAhead(rows, active, e.key, noneLabel);
      if (next >= 0) setActive(next);
    }
  };

  const shared = {
    id,
    ref: trigger,
    role: 'combobox',
    'aria-expanded': open,
    'aria-controls': listId,
    'aria-activedescendant': open && !hasSearch && rows.length ? `${id}-opt-${active}` : undefined,
    'aria-label': ariaLabel,
    'aria-describedby': describedBy,
    disabled,
    onKeyDown,
    onFocus: () => setFocused(true),
    onBlur: () => setFocused(false),
  } as const;

  return (
    <div ref={root} style={{ position: 'relative', minWidth: 0 }}>
      <div style={fieldStyle(open || focused, disabled)} onMouseDown={(e) => {
        if (e.target === e.currentTarget) { e.preventDefault(); trigger.current?.focus(); open ? close() : openList(); }
      }}>
        {freeText ? (
          <input {...shared} type="text" autoComplete="off" aria-autocomplete="list" value={value} placeholder={placeholder}
            style={triggerStyle} onClick={openList}
            onChange={(e) => { onChange(e.target.value); setQuery(e.target.value); setActive(0); if (!open) setOpen(true); }} />
        ) : (
          <button {...shared} type="button" aria-haspopup="listbox" style={triggerStyle} onClick={() => (open ? close() : openList())}>
            {selected?.lead}
            <span style={{ ...ellipsisStyle, color: selected ? 'var(--text)' : 'var(--text-muted)' }}>
              {selected ? selected.label : value || noneLabel || placeholder || 'Choose…'}
            </span>
          </button>
        )}
        {freeText && value && !disabled && (
          <button type="button" aria-label="Clear" title="Clear" style={iconStyle}
            onClick={() => { onChange(''); setQuery(''); trigger.current?.focus(); openList(); }}>
            <svg width="11" height="11" viewBox="0 0 12 12" aria-hidden="true"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" fill="none" /></svg>
          </button>
        )}
        <span aria-hidden="true" style={{ ...iconStyle, pointerEvents: 'none', transform: open ? 'rotate(180deg)' : undefined }}>
          <svg width="12" height="12" viewBox="0 0 12 12"><path d="M2.5 4.5L6 8l3.5-3.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" fill="none" /></svg>
        </span>
      </div>
      {open && (
        <div ref={menu} style={menuStyle(up)} onMouseDown={(e) => { if (e.target !== search.current) e.preventDefault(); }}>
          {hasSearch && (
            <input ref={search} type="text" aria-label="Search options" placeholder="Search" autoComplete="off" value={query}
              role="combobox" aria-expanded aria-controls={listId} aria-autocomplete="list"
              aria-activedescendant={rows.length ? `${id}-opt-${active}` : undefined}
              onChange={(e) => { setQuery(e.target.value); setActive(0); }} onKeyDown={onKeyDown} style={searchStyle} />
          )}
          <PickerRows id={id} listId={listId} rows={rows} active={active} value={value} query={freeText || hasSearch ? query : ''}
            noneLabel={noneLabel} label={ariaLabel} freeText={freeText} onHover={setActive} onChoose={(i) => choose(rows[i])} />
        </div>
      )}
    </div>
  );
};

const PickerRows: React.FC<{
  id: string; listId: string; rows: PickerRow[]; active: number; value: string; query: string;
  noneLabel?: string; label?: string; freeText?: boolean; onHover: (i: number) => void; onChoose: (i: number) => void;
}> = ({ id, listId, rows, active, value, query, noneLabel, label, freeText, onHover, onChoose }) => {
  if (!rows.length) {
    const message = query.trim()
      ? `Nothing matches “${query.trim()}”.`
      : freeText
        ? 'Nothing to suggest yet. Type a name to use it.'
        : 'There is nothing to choose from yet.';
    return <div id={listId} role="listbox" aria-label={label} style={{ padding: 10, color: 'var(--text-muted)' }}>{message}</div>;
  }
  let group: string | undefined;
  return (
    <div id={listId} role="listbox" aria-label={label} style={{ maxHeight: 264, overflowY: 'auto' }}>
      {rows.map((row, i) => {
        const heading = row.kind === 'option' && row.option.group && row.option.group !== group ? row.option.group : null;
        if (row.kind === 'option') group = row.option.group;
        const isSel = row.kind !== 'create' && row.value === value;
        const o = row.kind === 'option' ? row.option : null;
        const text = o ? o.label : row.kind === 'none' ? noneLabel || '' : `Use “${row.value}”`;
        const range = o ? matchRange(o.label, query) : null;
        return (
          <React.Fragment key={`${row.kind}:${row.value}`}>
            {heading && <div role="presentation" style={groupStyle}>{heading}</div>}
            <div id={`${id}-opt-${i}`} role="option" aria-selected={isSel} data-value={row.value}
              aria-label={text} aria-description={o?.hint}
              onMouseMove={() => i !== active && onHover(i)} onClick={() => onChoose(i)}
              style={rowStyle(i === active, isSel, row.kind)}>
              <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" style={{ flex: 'none', color: 'var(--accent)', visibility: isSel ? 'visible' : 'hidden' }}>
                <path d="M3 8.5l3 3 7-7" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              {o?.depth ? <span style={{ width: o.depth * 14, flex: 'none' }} /> : null}
              {o?.lead}
              <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                <span style={ellipsisStyle}>
                  {range ? (
                    <>
                      {text.slice(0, range[0])}
                      <span style={{ fontWeight: 700, textDecoration: 'underline', textDecorationColor: 'var(--accent)', textUnderlineOffset: 2 }}>{text.slice(range[0], range[1])}</span>
                      {text.slice(range[1])}
                    </>
                  ) : text}
                </span>
                {o?.hint && <span style={{ ...ellipsisStyle, fontSize: 12, fontWeight: 400, color: 'var(--text-muted)' }}>{o.hint}</span>}
              </span>
              {o?.meta && <span style={{ flex: 'none', fontSize: 12, fontWeight: 400, color: 'var(--text-muted)' }}>{o.meta}</span>}
            </div>
          </React.Fragment>
        );
      })}
    </div>
  );
};

const ellipsisStyle: React.CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };
const fieldStyle = (lit: boolean, disabled?: boolean): React.CSSProperties => ({
  display: 'flex', alignItems: 'center', minHeight: 38, borderRadius: 6, cursor: disabled ? 'not-allowed' : 'pointer',
  border: `1px solid ${lit ? 'var(--accent)' : 'var(--border)'}`, background: 'var(--surface)',
  boxShadow: lit ? '0 0 0 3px color-mix(in srgb, var(--accent) 28%, transparent)' : 'none',
  opacity: disabled ? 0.6 : 1, transition: 'border-color .12s, box-shadow .12s',
});
const triggerStyle: React.CSSProperties = {
  flex: 1, minWidth: 0, border: 0, background: 'transparent', padding: '8px 12px', margin: 0, font: 'inherit',
  color: 'var(--text)', outline: 'none', boxShadow: 'none', display: 'flex', alignItems: 'center', gap: 8,
  textAlign: 'left', cursor: 'inherit', width: 'auto',
};
const iconStyle: React.CSSProperties = {
  flex: 'none', width: 32, height: 34, display: 'grid', placeItems: 'center', border: 0, padding: 0, margin: 0,
  background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', transition: 'transform .15s',
};
const menuStyle = (up: boolean): React.CSSProperties => ({
  position: 'absolute', left: 0, right: 0, zIndex: 30, minWidth: 220, padding: 4,
  ...(up ? { bottom: 'calc(100% + 4px)' } : { top: 'calc(100% + 4px)' }),
  background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 8,
  boxShadow: '0 6px 20px rgba(0,0,0,.14), 0 1px 3px rgba(0,0,0,.08)',
});
const searchStyle: React.CSSProperties = {
  display: 'block', width: '100%', boxSizing: 'border-box', margin: '0 0 4px', padding: '7px 10px', font: 'inherit',
  border: '1px solid var(--border-soft)', borderRadius: 5, background: 'var(--surface-alt)', color: 'var(--text)',
};
const groupStyle: React.CSSProperties = {
  fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '.06em', color: 'var(--text-muted)', padding: '8px 10px 4px',
};
const rowStyle = (active: boolean, sel: boolean, kind: PickerRow['kind']): React.CSSProperties => ({
  display: 'flex', alignItems: 'center', gap: 10, padding: '7px 10px', borderRadius: 5, cursor: 'pointer',
  background: active ? 'var(--surface-hover)' : 'transparent',
  color: kind === 'create' || sel ? 'var(--accent-text)' : kind === 'none' ? 'var(--text-muted)' : 'var(--text)',
  fontWeight: sel ? 600 : 400, fontStyle: kind === 'none' ? 'italic' : 'normal',
  ...(kind === 'create' ? { borderTop: '1px solid var(--border-soft)', marginTop: 4 } : {}),
});
