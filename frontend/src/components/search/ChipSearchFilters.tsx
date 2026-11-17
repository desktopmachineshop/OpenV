import React, { useState } from 'react';
import { buttonStyle } from '../ui/buttonStyles';
import { check, columnHeading, divider, menuItem, smallInput } from './chipSearchStyles';
import {
  ChipMatch,
  DATE_RANGES,
  SearchOption,
  SearchSchema,
  SearchValue,
  addChip,
  hasFieldValue,
  hasQuick,
  toggleFieldValue,
  toggleQuick,
} from './searchValue';

interface FiltersProps {
  schema: SearchSchema;
  value: SearchValue;
  onChange: (next: SearchValue) => void;
  quickCounts?: Record<string, number>;
  optionCounts?: Record<string, Record<string, number>>;
  /** Options loaded by a field's loadOptions, by field key. */
  loaded: Record<string, SearchOption[]>;
}

// The panel's Filters column: the quick filters, then each enum and date
// facet as a sub-menu that opens in place, then Custom filter… for a
// condition on any field.
export const ChipSearchFilters: React.FC<FiltersProps> = ({ schema, value, onChange, quickCounts, optionCounts, loaded }) => {
  const [open, setOpen] = useState<string>('');
  const [custom, setCustom] = useState(false);
  const menuFields = schema.fields.filter((f) => f.inMenu ?? (f.type === 'enum' || f.type === 'date'));

  return (
    <div role="menu" aria-label="Filters" style={{ minWidth: 0 }}>
      <p style={columnHeading}>
        <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
          <path d="M3 5h14M6 10h8M8 15h4" />
        </svg>
        Filters
      </p>
      {(schema.quickFilters ?? []).map((q) => {
        const on = hasQuick(value, q.key);
        return (
          <button key={q.key} type="button" role="menuitemcheckbox" aria-checked={on} onClick={() => onChange(toggleQuick(value, q.key))} style={menuItem(on)}>
            <span style={check(on)} aria-hidden="true">{on ? '✓' : ''}</span>
            <span style={{ flex: 1 }}>{q.label}</span>
            {quickCounts?.[q.key] !== undefined && <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{quickCounts[q.key]}</span>}
          </button>
        );
      })}
      {(schema.quickFilters ?? []).length > 0 && menuFields.length > 0 && <div style={divider} role="separator" />}
      {menuFields.map((field) => {
        const options = field.type === 'date' ? DATE_RANGES : field.options ?? loaded[field.key] ?? [];
        const expanded = open === field.key;
        const count = options.filter((o) => hasFieldValue(value, field, o.value)).length;
        return (
          <div key={field.key}>
            <button
              type="button"
              role="menuitem"
              aria-haspopup="menu"
              aria-expanded={expanded}
              onClick={() => setOpen(expanded ? '' : field.key)}
              style={menuItem(count > 0)}
            >
              <span style={{ flex: 1 }}>
                {field.label}
                {count > 0 ? ` (${count})` : ''}
              </span>
              <span aria-hidden="true" style={{ color: 'var(--text-muted)', transform: expanded ? 'rotate(90deg)' : undefined }}>
                ›
              </span>
            </button>
            {expanded && (
              <div role="menu" aria-label={field.label} style={{ paddingLeft: 12 }}>
                {options.length === 0 && <span style={{ ...menuItem(), color: 'var(--text-muted)', cursor: 'default' }}>None</span>}
                {options.map((o) => {
                  const on = hasFieldValue(value, field, o.value);
                  return (
                    <button
                      key={o.value}
                      type="button"
                      role="menuitemcheckbox"
                      aria-checked={on}
                      onClick={() => onChange(toggleFieldValue(value, field, o.value))}
                      style={menuItem(on)}
                    >
                      <span style={check(on)} aria-hidden="true">{on ? '✓' : ''}</span>
                      <span style={{ flex: 1 }}>{field.type === 'date' ? `${field.label}: ${o.label}` : o.label}</span>
                      {optionCounts?.[field.key] && <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{optionCounts[field.key][o.value] ?? 0}</span>}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        );
      })}
      <div style={divider} role="separator" />
      <button type="button" role="menuitem" aria-expanded={custom} onClick={() => setCustom((c) => !c)} style={menuItem(custom)}>
        Custom filter…
      </button>
      {custom && <CustomFilter schema={schema} onAdd={(field, match, v) => onChange(addChip(value, { kind: 'filter', field, values: [v], match }))} />}
    </div>
  );
};

// Custom filter…: a condition on any field the page offers, "<field> is /
// contains <value>", added as a chip.
const CustomFilter: React.FC<{ schema: SearchSchema; onAdd: (field: string, match: ChipMatch, value: string) => void }> = ({ schema, onAdd }) => {
  const fields = schema.fields.filter((f) => f.type !== 'date');
  const [field, setField] = useState(fields[0]?.key ?? '');
  const [match, setMatch] = useState<ChipMatch>('contains');
  const [text, setText] = useState('');
  const add = () => {
    if (!field || !text.trim()) return;
    onAdd(field, match, text.trim());
    setText('');
  };
  return (
    <div style={{ display: 'grid', gap: 6, padding: '4px 8px 8px' }}>
      <select aria-label="Field" value={field} onChange={(e) => setField(e.target.value)} style={smallInput}>
        {fields.map((f) => (
          <option key={f.key} value={f.key}>
            {f.label}
          </option>
        ))}
      </select>
      <select aria-label="Condition" value={match} onChange={(e) => setMatch(e.target.value === 'is' ? 'is' : 'contains')} style={smallInput}>
        <option value="contains">contains</option>
        <option value="is">is</option>
      </select>
      <input
        aria-label="Value"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            add();
          }
        }}
        placeholder="Value"
        style={smallInput}
      />
      <button type="button" onClick={add} disabled={!text.trim()} style={buttonStyle('secondary', { compact: true })}>
        Add filter
      </button>
    </div>
  );
};
