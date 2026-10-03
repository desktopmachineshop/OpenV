import React from 'react';
import type { FilterPanelRow, FilterPreset, Setter } from './shared';

// The filter panel the ⚙ button opens under the search box: exact match, the
// AND / OR logic, the field filter rows and the saved presets.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface FilterPanelProps {
  searchText: string;
  setSearchText: Setter<string>;
  searchExact: boolean;
  setSearchExact: Setter<boolean>;
  filterLogic: 'and' | 'or';
  setFilterLogic: Setter<'and' | 'or'>;
  filterRows: FilterPanelRow[];
  setFilterRows: Setter<FilterPanelRow[]>;
  finiteFields: string[];
  getFieldUniqueValues: (fieldName: string) => string[];
  fieldOptions: Array<{ value: string; label: string }>;
  filterPresetName: string;
  setFilterPresetName: Setter<string>;
  filterPresets: FilterPreset[];
  selectedPreset: string;
  setSelectedPreset: Setter<string>;
  savePresets: (presets: FilterPreset[]) => void;
  applyPreset: (presetData: string) => void;
}

export const FilterPanel: React.FC<FilterPanelProps> = ({
  searchText,
  setSearchText,
  searchExact,
  setSearchExact,
  filterLogic,
  setFilterLogic,
  filterRows,
  setFilterRows,
  finiteFields,
  getFieldUniqueValues,
  fieldOptions,
  filterPresetName,
  setFilterPresetName,
  filterPresets,
  selectedPreset,
  setSelectedPreset,
  savePresets,
  applyPreset,
}) => (
  <div style={{
    marginTop: '12px',
    border: '1px solid var(--border)',
    borderRadius: '6px',
    padding: '10px',
    backgroundColor: 'var(--surface-alt)',
    display: 'flex',
    flexDirection: 'column',
    gap: '10px',
  }}>
    <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
      <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: 'var(--text)' }}>
        <input
          type="checkbox"
          checked={searchExact}
          onChange={(e) => setSearchExact(e.target.checked)}
        />
        Exact match
      </label>
      <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
        <span style={{ fontSize: '12px', color: 'var(--text)' }}>Filter logic</span>
        <select
          value={filterLogic}
          onChange={(e) => setFilterLogic(e.target.value === 'or' ? 'or' : 'and')}
          style={{
            padding: '6px',
            borderRadius: '4px',
            border: '1px solid var(--neutral-mid)',
            fontSize: '12px',
          }}
        >
          <option value="and">AND</option>
          <option value="or">OR</option>
        </select>
      </div>
    </div>
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
      {filterRows.map((row) => {
        const isFiniteField = finiteFields.includes(row.field);
        const comparatorIsFinite = ['equals', 'not-equals'].includes(row.comparator);
        const shouldUseSelect = isFiniteField && comparatorIsFinite;
        const fieldValues = shouldUseSelect ? getFieldUniqueValues(row.field) : [];

        return (
          <div key={row.id} style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
            <select
              value={row.field}
              onChange={(e) => {
                const next = filterRows.map((item) =>
                  item.id === row.id ? { ...item, field: e.target.value } : item
                );
                setFilterRows(next);
              }}
              style={{
                flex: '0 0 140px',
                padding: '6px',
                borderRadius: '4px',
                border: '1px solid var(--neutral-mid)',
                fontSize: '12px',
              }}
            >
              {fieldOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <select
              value={row.comparator}
              onChange={(e) => {
                const next = filterRows.map((item) =>
                  item.id === row.id ? { ...item, comparator: e.target.value } : item
                );
                setFilterRows(next);
              }}
              style={{
                flex: '0 0 140px',
                padding: '6px',
                borderRadius: '4px',
                border: '1px solid var(--neutral-mid)',
                fontSize: '12px',
              }}
            >
              <option value="contains">Contains</option>
              <option value="not-contains">Not contains</option>
              <option value="equals">Equals</option>
              <option value="not-equals">Not equals</option>
              <option value="starts-with">Starts with</option>
              <option value="ends-with">Ends with</option>
              <option value="gt">Greater than</option>
              <option value="lt">Less than</option>
            </select>
            {shouldUseSelect ? (
              <select
                value={row.value}
                onChange={(e) => {
                  const next = filterRows.map((item) =>
                    item.id === row.id ? { ...item, value: e.target.value } : item
                  );
                  setFilterRows(next);
                }}
                style={{
                  flex: 1,
                  padding: '6px',
                  borderRadius: '4px',
                  border: '1px solid var(--neutral-mid)',
                  fontSize: '12px',
                }}
              >
                <option value="">-- Select {row.field} --</option>
                {fieldValues.map((val) => (
                  <option key={val} value={val}>
                    {val}
                  </option>
                ))}
              </select>
            ) : (
              <input
                type="text"
                value={row.value}
                onChange={(e) => {
                  const next = filterRows.map((item) =>
                    item.id === row.id ? { ...item, value: e.target.value } : item
                  );
                  setFilterRows(next);
                }}
                placeholder="Contains..."
                style={{
                  flex: 1,
                  padding: '6px',
                  borderRadius: '4px',
                  border: '1px solid var(--neutral-mid)',
                  fontSize: '12px',
                }}
              />
            )}
            <button
              onClick={() => {
                const next = filterRows.filter((item) => item.id !== row.id);
                setFilterRows(
                  next.length > 0
                    ? next
                    : [{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]
                );
              }}
              className="button-secondary"
              style={{ padding: '5px 8px', fontSize: '12px' }}
              title="Remove filter"
            >
              Remove
            </button>
          </div>
        );
      })}
      <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
        <button
          onClick={() => {
            setFilterRows((prev) => [
              ...prev,
              { id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' },
            ]);
          }}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '12px' }}
        >
          + Add filter
        </button>
        <button
          onClick={() => {
            const name = filterPresetName.trim();
            if (!name) return;
            const data = JSON.stringify({
              searchText,
              searchExact,
              filterLogic,
              filterRows,
            });
            const next = filterPresets.filter((preset) => preset.name !== name);
            next.unshift({ name, data });
            setFilterPresetName('');
            setSelectedPreset(name);
            savePresets(next);
          }}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '12px' }}
        >
          Save preset
        </button>
        <button
          onClick={() => {
            setSearchText('');
            setSearchExact(false);
            setFilterLogic('and');
            setFilterRows([{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]);
          }}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '12px' }}
        >
          Clear filters
        </button>
      </div>
      <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center' }}>
        <input
          type="text"
          value={filterPresetName}
          onChange={(e) => setFilterPresetName(e.target.value)}
          placeholder="Preset name"
          style={{
            flex: '0 0 180px',
            padding: '6px',
            borderRadius: '4px',
            border: '1px solid var(--neutral-mid)',
            fontSize: '12px',
          }}
        />
        <select
          value={selectedPreset}
          onChange={(e) => {
            const value = e.target.value;
            setSelectedPreset(value);
            const preset = filterPresets.find((item) => item.name === value);
            if (preset) {
              applyPreset(preset.data);
            }
          }}
          style={{
            flex: '0 0 220px',
            padding: '6px',
            borderRadius: '4px',
            border: '1px solid var(--neutral-mid)',
            fontSize: '12px',
          }}
        >
          <option value="">Saved presets</option>
          {filterPresets.map((preset) => (
            <option key={preset.name} value={preset.name}>
              {preset.name}
            </option>
          ))}
        </select>
        <button
          onClick={() => {
            if (!selectedPreset) return;
            const next = filterPresets.filter((preset) => preset.name !== selectedPreset);
            setSelectedPreset('');
            savePresets(next);
          }}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '12px' }}
        >
          Delete preset
        </button>
      </div>
    </div>
  </div>
);
